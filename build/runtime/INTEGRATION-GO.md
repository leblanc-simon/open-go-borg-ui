# Intégration du runtime dans le code Go

Ce document accompagne la recette `build-runtime.ps1` vers le dépôt applicatif.
Il dit ce qui est prêt, ce qui reste à faire à la main, et ce que le code Go
doit changer maintenant que le runtime est livré en `.tar.gz` et non plus en
`.zip`.

État au 23 septembre 2026, runtime `1.4.5-cygwin.1`.

---

## 1. Ce qui est prêt

L'archive est construite, élaguée, et éprouvée après extraction dans un dossier
neuf : `borg init`, `create`, `list`, `extract`, avec comparaison du contenu
relu. Elle se trouve dans `dist/` :

| | |
|---|---|
| Fichier | `borgui-runtime-1.4.5-cygwin.1.tar.gz` |
| Taille | 62 517 029 octets (59,6 Mio) |
| SHA-256 | `1926b2941925e2ccfd155eeb66a4067d93401d7248483cf0f5212d66371e60f7` |
| Contenu | 4 755 fichiers, 295 dossiers, 553 liens symboliques, 17 liens durs |
| Arbre installé | 188,7 Mio |

L'archive est reproductible : deux constructions du même arbre donnent le même
fichier octet pour octet, donc la même empreinte.

---

## 2. Ce que tu dois faire à la main

### 2.1 Ne pas committer l'archive

`dist/` et `work/` n'ont rien à faire dans le dépôt : 60 Mo pour l'un,
plusieurs centaines de Mo pour l'autre. Seuls `build-runtime.ps1`,
`packages.txt`, `requirements.txt`, `README.md` et ce fichier sont du code
source. Il n'y a pas de `.gitignore` dans ce dossier — à créer :

```gitignore
dist/
work/
```

### 2.2 Publier l'archive

L'adresse doit être **versionnée et stable** : elle est compilée dans une
version donnée de l'application, qui ne changera jamais de moteur en cours de
route.

```bash
gh release create runtime-1.4.5-cygwin.1 \
    build/runtime/dist/borgui-runtime-1.4.5-cygwin.1.tar.gz \
    build/runtime/dist/SHA256SUMS \
    --title "Runtime Borg 1.4.5 (Cygwin, révision 1)" \
    --notes "Moteur de sauvegarde pour les postes Windows. Empreinte dans SHA256SUMS."
```

### 2.3 Ce que devient `SHA256SUMS`

Il est publié **pour la vérification manuelle par un administrateur**, et pour
rien d'autre. L'application ne doit jamais le télécharger ni s'en servir :
l'empreinte qui fait foi est celle compilée dans le binaire, faute de quoi
quiconque peut remplacer l'archive **et** son empreinte (SEC-01).

Si le code Go télécharge `SHA256SUMS` aujourd'hui, c'est un défaut de sécurité
à corriger, pas une compatibilité à préserver.

### 2.4 Conserver le dossier des paquets

`work/packages/` contient les `.tar.xz` Cygwin exacts qui ont servi.
Conserve-le et publie-le avec l'archive : c'est la seule garantie de pouvoir
reconstruire à l'identique quand les miroirs auront tourné. La reconstruction
se fait alors avec `--local-install --local-package-dir`, sans réseau.

---

## 3. `internal/borgruntime/spec.go`

```go
    Version:     "1.4.5-cygwin.1",
    BorgVersion: "1.4.5",
    SHA256:      "1926b2941925e2ccfd155eeb66a4067d93401d7248483cf0f5212d66371e60f7",
    Size:        62517029,
```

Et l'`URL`, qui pointe désormais sur un `.tar.gz`. **L'extension du fichier
change** : toute construction de nom de fichier ou de chemin de cache qui
suppose `.zip` est à reprendre.

---

## 4. Ce que le téléchargeur doit changer

### 4.1 Pourquoi le format a changé

Un arbre Cygwin contient 553 liens symboliques. Cygwin les représente par un
fichier ordinaire marqué de l'attribut Windows « système », que ZIP ne
transporte pas : à l'extraction, ils redevenaient des fichiers inertes de
quelques octets, et `/bin/python3`, `/bin/awk` et toute la ferme
`/etc/alternatives` cessaient d'exister. ZIP ne transportait pas davantage les
droits POSIX, et les outils de la plateforme n'écrivaient pas ses entrées de
dossier : les dossiers vides, `/tmp` en tête, disparaissaient aussi.

Borg survivait à tout cela, parce que son script d'entrée vise un vrai
exécutable. C'est ce qui a rendu le défaut invisible pendant longtemps : une
vérification fonctionnelle passait sur une archive largement abîmée.

### 4.2 L'ordre des opérations

1. Télécharger l'archive.
2. **Vérifier le SHA-256 des octets reçus contre la constante compilée**, avant
   toute écriture sur disque et avant toute décompression (SEC-01).
3. Décompresser : `gzip.NewReader` puis `tar.NewReader`.
4. Écrire l'arbre.

### 4.3 Les quatre types d'entrées à traiter

Les noms d'entrées commencent tous par `./` — l'archive est produite avec
`tar -C <arbre> .`. Passe-les par `path.Clean` et refuse tout chemin absolu ou
contenant `..` : l'archive n'en contient aucun aujourd'hui, mais c'est le
téléchargeur qui doit en répondre.

| Type `tar` | Nombre | Traitement |
|---|---|---|
| `TypeDir` | 295 | `os.MkdirAll`. **Créer même les dossiers vides** : `/tmp` en est un, et son absence se voit à chaque commande |
| `TypeReg` | 4 755 | écriture ordinaire |
| `TypeSymlink` | 553 | format Cygwin, voir §5 |
| `TypeLink` | 17 | `os.Link` — fonctionne sur NTFS sans privilège. Exemples : `./bin/dash.exe` → `./bin/ash.exe`, `./bin/gawk.exe` → `./bin/gawk-5.4.0.exe`. En cas d'échec, copier le fichier cible est un repli acceptable |

Les modes POSIX portés par l'archive n'ont pas de sens sur Windows : les
ignorer est correct, mais ne pas échouer dessus.

---

## 5. Écrire les liens à la manière de Cygwin

C'est le point délicat, et celui qui demande le plus d'attention au test.

**Ne pas créer de liens NTFS natifs.** Cela demande le privilège
`SeCreateSymbolicLinkPrivilege`, donc des droits d'administrateur ou le mode
développeur — alors que le runtime doit s'installer sans ni l'un ni l'autre
(TR-20).

Un lien Cygwin est un fichier ordinaire qui porte l'attribut
`FILE_ATTRIBUTE_SYSTEM` — **sans cet attribut, rien n'est interprété** — et
dont le contenu commence par la signature `!<symlink>`.

Deux encodages de la cible coexistent dans l'arbre, tous deux relus par
Cygwin. Mesurés sur l'arbre construit :

| Forme | Contenu après la signature | Exemple |
|---|---|---|
| Historique | la cible en octets bruts, terminée par `00` | `/bin/awk` → `!<symlink>gawk.exe\0`, 19 octets |
| Courante | BOM `FF FE`, la cible en UTF-16LE, terminée par `00 00` | `/bin/python3` → 64 octets pour `/etc/alternatives/python3` |

Écris la **forme courante** : c'est celle que produit l'appel `symlink()` de
Cygwin, et la seule qui accepte une cible non ASCII. La forme historique vient
des archives de paquets et n'a pas à être reproduite.

```go
// writeCygwinSymlink écrit un lien symbolique tel que Cygwin le représente :
// un fichier marqué « système » contenant une signature, puis la cible en
// UTF-16LE. Les liens NTFS natifs demanderaient un privilège que l'application
// n'a pas.
func writeCygwinSymlink(chemin, cible string) error {
    var b bytes.Buffer
    b.WriteString("!<symlink>")
    b.Write([]byte{0xFF, 0xFE}) // BOM UTF-16LE
    for _, u := range utf16.Encode([]rune(cible)) {
        b.WriteByte(byte(u))
        b.WriteByte(byte(u >> 8))
    }
    b.Write([]byte{0x00, 0x00}) // terminaison

    if err := os.WriteFile(chemin, b.Bytes(), 0o644); err != nil {
        return err
    }
    p, err := syscall.UTF16PtrFromString(chemin)
    if err != nil {
        return err
    }
    return syscall.SetFileAttributes(p, syscall.FILE_ATTRIBUTE_SYSTEM)
}
```

La cible est le `Linkname` de l'en-tête tar, tel quel : c'est déjà un chemin
POSIX. Sur les 553 liens, 270 ont une cible absolue (`/etc/alternatives/python3`)
et 283 une cible relative (`gawk.exe`) — les deux formes sont à écrire sans
transformation.

> **À éprouver.** Ce format a été relevé sur des fichiers écrits par Cygwin
> lui-même, et non validé en écrivant depuis Go puis en relisant depuis Cygwin.
> C'est le premier test à passer : extraire avec le code Go, puis vérifier avec
> `bash -lc 'ls -la /bin/python3'` que la ligne commence par `l` et non par `-`,
> et que `python3 --version` répond.

---

## 6. Deux pièges mesurés

### 6.1 Le premier interpréteur bavarde sur la sortie standard

L'élagage retire `/etc/passwd`, `/etc/group` et les dossiers personnels — ce
sont des traces de la machine de construction, qui n'ont rien à faire chez
l'utilisateur. Conséquence : **le tout premier interpréteur de connexion ouvert
dans le runtime recrée le dossier personnel et l'annonce sur neuf lignes**, qui
partent sur **stdout**, pas sur stderr :

```
Copying skeleton files.
These files are for the users to personalise their cygwin experience.

They will never be overwritten nor automatically updated.

'./.bashrc' -> '/home/<utilisateur>//.bashrc'
...
```

Ces lignes précèdent la sortie de la commande demandée. Si l'application lance
`bash -lc 'borg list …'` et analyse stdout, **la première commande après
installation lui rendra neuf lignes de bruit avant la réponse de Borg**. Le
lancement suivant est propre.

Le plus simple est d'ouvrir un interpréteur de chauffe juste après
l'extraction, dont la sortie est jetée :

```go
// Absorbe le bavardage du premier interpréteur de connexion, qui recrée le
// dossier personnel et l'annonce sur stdout, avant que l'application ne se
// mette à analyser des sorties.
exec.Command(bashPath, "-lc", "true").Run()
```

À défaut, `--log-json` sur les commandes Borg rend l'analyse insensible à ce
bruit, ce qui est de toute façon plus robuste.

### 6.2 `/tmp` doit exister

Il est vide dans l'arbre livré, et donc absent de toute archive qui n'écrit pas
ses entrées de dossier. Sans lui, bash ouvre chaque commande par
`warning: could not find /tmp, please create!` et le moteur n'a aucun dossier de
travail. L'archive `.tar.gz` le contient bien ; c'est au code d'extraction
d'honorer les entrées `TypeDir`.

---

## 7. À vérifier avant de publier

La recette éprouve déjà le moteur et l'archive extraite. Restent les
vérifications qui engagent le projet, à passer une fois par version :

| Test | Vérification |
|---|---|
| TR-01 / TR-02 | Une archive créée sous Windows avec ce runtime est listée et extraite par le `borg` 1.4 officiel d'une machine Linux, dans les deux modes de chiffrement |
| TR-03 | Les chemins de l'archive commencent par la lettre de lecteur, sans préfixe `cygdrive` |
| TR-04 | Un fichier restauré est identique bit pour bit à l'original |
| TR-20 | Premier lancement sur une machine Windows vierge, **sans droits administrateur** — c'est ce test qui valide le choix des liens Cygwin plutôt que NTFS |
| TR-21 | Une archive dont l'empreinte a été altérée est rejetée |
| TR-33 | Chemins accentués, avec espaces, et de plus de 260 caractères |

S'y ajoute, propre au changement de format : après extraction par le code Go,
`ls -la /bin/python3` doit montrer un `l` en première colonne, et
`python3 --version` doit répondre. Si ce lien-là fonctionne, les 552 autres
suivent — il traverse `/etc/alternatives`, donc deux liens en chaîne.

---

## 8. État de l'intégration

Fait dans `internal/borgruntime`, le 23 septembre 2026 :

- `spec.go` épingle `SHA256` et `Size` ; `URL` reste vide en attendant la
  publication, seule la voie hors ligne est donc ouverte ;
- `extract.go` remplace l'extraction ZIP : `gzip` puis `tar`, noms passés par
  `path.Clean` et refusés s'ils sortent du dossier (cible des liens durs
  comprise), dossiers vides créés, liens durs par `os.Link` avec repli sur une
  copie, liens symboliques écrits dans la forme courante de Cygwin et marqués
  `FILE_ATTRIBUTE_SYSTEM` ;
- le nom de l'archive téléchargée suit celui de la recette
  (`Spec.ArchiveName`), en `.tar.gz` ;
- l'application ne télécharge pas `SHA256SUMS` et ne l'a jamais fait.

L'archive réelle a été extraite par ce code sous Linux (`TestArchivePubliee`) :
empreinte acceptée, 5 325 fichiers dont 553 liens Cygwin, `/tmp` présent,
`/bin/python3` au format attendu.

Le §6.1 ne concerne pas l'application : `CygwinRunner` invoque `bash -c`, qui
n'est pas un interpréteur de connexion et ne recrée donc pas le dossier
personnel. Les commandes Borg passent en outre par `--log-json`.

Reste à éprouver **sous Windows**, dans cet ordre :

1. `borgui runtime install --archive <archive>` sans droits d'administrateur
   (TR-20) ;
2. dans le runtime installé, `bin\bash.exe -c 'ls -la /bin/python3; python3 --version'` :
   la ligne doit commencer par `l`, et Python répondre — c'est la validation du
   format de lien écrit depuis Go ;
3. le même test automatisé, qui y pose aussi l'attribut « système » :
   `$env:BORGUI_RUNTIME_ARCHIVE='...'; go test ./internal/borgruntime -run TestArchivePubliee -v`.
