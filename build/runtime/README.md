# Recette de construction du runtime Borg pour Windows

Ce dossier produit l'archive que l'application télécharge au premier lancement
sur un poste Windows. C'est le livrable LI-05 du cahier des charges :
reproductible, à versions épinglées, publiée avec son empreinte.

## Ce qu'est le runtime

Une installation Cygwin autonome contenant le code **officiel** de Borg, non
modifié : l'interpréteur Python, les bibliothèques de compression et de
chiffrement, le client OpenSSH et le shell par lequel l'application lance ses
commandes. Rien de plus.

La série de Python empaquetée est **3.12**, celle que Cygwin distribue par
défaut (le méta-paquet `python3` pointe sur `python312`). Elle se change par
le paramètre `-PythonSeries`, sous deux conditions : que Cygwin la propose, et
que Borg l'accepte — la 1.4.5 exige Python 3.10 ou plus récent. La liste de
paquets `python39` donnée par la documentation d'installation de Borg n'est
donc plus utilisable ; c'est une erreur qui se manifeste tard, à l'installation
de Borg par pip.

Il n'est pas embarqué dans l'exécutable, pour trois raisons : l'exécutable doit
rester sous 30 Mo (ENF-01), le moteur doit pouvoir être remplacé sans
reconstruire l'application, et un binaire de 130 Mo qui extrait puis exécute
`python.exe` est exactement ce que les antivirus signalent.

## Construire

Sur une machine Windows x86-64, sans droits administrateur, 2 Go d'espace
libre :

La première construction d'une version de Borg se fait en deux temps. Les
empreintes des dépendances Python ne peuvent être calculées que sur une machine
capable de télécharger et de lire leurs sources : elles ne figurent donc pas
dans le dépôt tant qu'une construction ne les a pas produites.

```powershell
cd build\runtime
.\build-runtime.ps1 -UpdateHashes          # renseigne requirements.txt
git diff requirements.txt                  # à relire avant de valider
.\build-runtime.ps1 -BorgVersion 1.4.5 -Revision 1
```

Les constructions suivantes n'ont besoin que de la dernière commande. Si les
empreintes manquent, le script s'arrête avant de compiler quoi que ce soit et
rappelle la marche à suivre.

Les commandes envoyées au runtime sont débarrassées de leurs retours chariot
avant d'être passées à `bash`, qui les prendrait pour une partie de la
commande : l'échec se présente alors comme une option invalide, ou comme un
fichier introuvable dont le nom paraît pourtant correct.

Le script est enregistré en **UTF-8 avec marque d'ordre des octets** (BOM), et
doit le rester : Windows PowerShell lit un fichier qui en est dépourvu comme de
l'ANSI. Les caractères accentués sont alors mal décodés et certains — le tiret
cadratin notamment — deviennent des guillemets typographiques, que PowerShell
accepte comme délimiteurs de chaîne. Une chaîne se referme au milieu d'une
ligne et l'analyse du fichier échoue, très loin de sa cause. Le `.gitattributes`
du dépôt fixe cet encodage.

### Si Windows refuse d'exécuter le script

`L'exécution de scripts est désactivée sur ce système` est le refus par défaut
de Windows, pas un défaut du script. Le plus direct, sans rien changer au
poste :

```powershell
powershell.exe -ExecutionPolicy Bypass -File .\build-runtime.ps1 -BorgVersion 1.4.5 -Revision 1
```

Pour ne pas le répéter à chaque construction, la politique se change pour le
seul utilisateur courant, sans droits administrateur :

```powershell
Set-ExecutionPolicy -Scope CurrentUser -ExecutionPolicy RemoteSigned
```

Si l'une comme l'autre échouent, la restriction vient d'une stratégie de
groupe. `Get-ExecutionPolicy -List` indique laquelle : une valeur portée par
`MachinePolicy` ou `UserPolicy` prime sur tout le reste et relève de
l'administration du parc, pas d'un contournement. Construisez alors le runtime
sur une machine hors domaine — c'est une opération ponctuelle, faite une fois
par version de moteur, dont le résultat est une archive publiée.

Si le fichier a été téléchargé plutôt que cloné, Windows le marque en outre
comme venant d'Internet : `Unblock-File .\build-runtime.ps1` lève ce marquage.

### Si Windows bloque les binaires du runtime

Celui-là ne se présente jamais comme ce qu'il est. Une protection du poste
refuse le chargement d'un binaire Cygwin, Windows répond `ACCESS_DENIED`,
Cygwin le traduit en `EACCES`, et ce qui remonte ressemble à un arbre
incomplet :

```
    running: ...\bin\bash.exe --norc --noprofile "/etc/postinstall/ca-certificates.sh"
    abnormal exit: exit code=126
...
  File "/usr/lib/python3.12/subprocess.py", line 104, in <module>
    from _posixsubprocess import fork_exec as _fork_exec
ImportError: Permission denied
```

L'extension est pourtant bien là, et `python --version` répond. Un arbre Cygwin
est fait de centaines de binaires non signés, écrits puis exécutés dans la
foulée : c'est le profil exact de ce que Windows arrête. Trois mécanismes
distincts, du plus courant au plus radical :

- **l'accès contrôlé aux dossiers** protège d'office Téléchargements, Bureau,
  Documents et OneDrive, et refuse les écritures des programmes qu'il ne
  connaît pas — PowerShell en est ;
- **la marque « venu d'Internet »** (MOTW) suit tout ce qui est extrait d'une
  archive téléchargée, et SmartScreen s'en sert ;
- **le contrôle d'application intelligent** (Smart App Control, Windows 11)
  refuse tout binaire non signé, sans exception configurable. Il ne se
  désactive qu'une fois pour toutes : le réactiver demande une réinstallation
  de Windows.

Le script écarte les deux premiers d'emblée — il refuse de construire depuis un
dossier protégé — et arrête la construction si le troisième est actif. Si le
refus survient malgré tout :

```powershell
.\build-runtime.ps1 -Workspace C:\borgui-build  # hors des dossiers protégés
Get-ChildItem -Recurse -File | Unblock-File       # lever le marquage MOTW
Add-MpPreference -ExclusionPath C:\borgui-build  # administrateur
```

Ce que Defender a bloqué se lit dans son journal, qui le nomme mieux que
Cygwin :

```powershell
Get-WinEvent -LogName 'Microsoft-Windows-Windows Defender/Operational' |
    Where-Object Id -in 1116,1117,1121,1126 |
    Select-Object -First 10 TimeCreated, Message
```

L'interpréteur de chaque arbre est éprouvé par l'import de ses extensions
compilées, et non par `--version` qui n'en charge aucune : un arbre bloqué se
signale à l'étape qui l'installe, et non trois étapes plus loin.

Le script enchaîne neuf étapes : récupération du programme d'installation
Cygwin et vérification de sa signature, téléchargement des paquets, arbre de
compilation, compilation de Borg depuis ses sources, composition de l'arbre
livré, élagage, vérification fonctionnelle, archive, puis vérification de
l'archive elle-même.

Les deux vérifications ne font pas double emploi. La première éprouve l'arbre,
la seconde éprouve l'archive extraite dans un dossier neuf, comme le fera le
poste de l'utilisateur — et c'est là seulement que se voit ce qu'un format
d'archive perd en route. L'empreinte n'est calculée qu'après : une archive qui
échoue est effacée, et rien de publiable ne reste dans `dist/`.

La série de Python demandée est confrontée à `packages.txt` avant toute
installation, et l'interpréteur est éprouvé après chaque installation Cygwin —
par l'import de ses extensions compilées, pas par `--version` : une divergence
entre les deux fichiers, une installation incomplète ou un binaire refusé par
le poste produirait un runtime dont l'interpréteur ne connaît pas Borg.

Le programme d'installation de Cygwin est une application graphique même en
mode silencieux : il est lancé par `Start-Process -Wait`, faute de quoi la
construction enchaînerait sur un arbre encore en cours d'installation — l'échec
se manifeste alors bien plus loin, par un « command not found » sur
l'interpréteur.

Deux de ses comportements demandent la même vigilance :

- **un paquet inconnu ne le fait pas échouer** : il l'annonce et poursuit,
  laissant un arbre incomplet. La sortie est donc relue et un
  `Package '...' not found` arrête la construction ;
- **il tient sa propre base de ce qu'il a installé dans un arbre**, et s'y fie
  jusqu'à décider de ce qu'il a besoin de télécharger. Sur un arbre laissé à
  moitié fait par une exécution interrompue, il conclut que tout est en place
  et n'installe plus rien, indéfiniment. L'arbre de compilation est donc effacé
  à chaque construction ; le dossier des paquets, lui, est conservé.

Si une construction a été interrompue avant cette correction, effacez
`work\build` et `work\stage` à la main : le script le fait désormais, mais la
base laissée par l'exécution précédente est ce qui bloquait les suivantes.

Il produit dans `dist/` l'archive `borgui-runtime-<version>.tar.gz`, le fichier
`SHA256SUMS`, et affiche les valeurs à reporter dans `spec.go`.

### Pourquoi un tar et non un ZIP

Un arbre livré compte environ 570 liens symboliques. Cygwin les représente par
un fichier ordinaire marqué de l'attribut Windows « système », que ZIP ne
transporte pas : à l'extraction ils redeviennent des fichiers inertes de
quelques octets, et `/bin/python3`, `/bin/awk` et toute la ferme
`/etc/alternatives` cessent d'exister. ZIP ne transporte pas davantage les
droits POSIX, et les outils de la plateforme n'écrivent pas ses entrées de
dossier — les dossiers vides, `/tmp` en tête, disparaissaient aussi, et bash
accueillait chaque commande par un `warning: could not find /tmp`.

Borg survivait à tout cela, parce que son script d'entrée vise un vrai
exécutable ; c'est ce qui rendait le défaut invisible à une vérification
fonctionnelle. tar transporte les trois.

**Conséquence pour le téléchargeur** (`internal/borgruntime`) : il extrait
désormais un `.tar.gz`, et doit écrire les liens symboliques **à la manière de
Cygwin**. Créer de vrais liens NTFS demanderait le privilège correspondant,
donc des droits d'administrateur ou le mode développeur, alors que le runtime
s'installe sans aucun des deux.

Un lien Cygwin est un fichier portant l'attribut `FILE_ATTRIBUTE_SYSTEM` — sans
lui, rien n'est interprété — et commençant par la signature `!<symlink>`. Deux
encodages de la cible coexistent dans l'arbre, tous deux relus par Cygwin :

| Forme | Contenu après la signature | Exemple mesuré |
|---|---|---|
| Historique | la cible en octets bruts, terminée par `00` | `/bin/awk` → `!<symlink>gawk.exe\0`, 19 octets |
| Courante | BOM `FF FE`, la cible en UTF-16LE, terminée par `00 00` | `/bin/python3` → 64 octets pour `/etc/alternatives/python3` |

Écrire la forme courante : c'est celle que produit l'appel `symlink()` de
Cygwin, et la seule qui accepte une cible non ASCII. La forme historique vient
des archives de paquets et n'a pas à être reproduite.

### Cython n'est pas installé, et c'est voulu

Le paquet source de Borg déclare Cython parmi ses outils de construction, mais
il embarque déjà les fichiers C que Cython aurait produits — les douze
extensions du moteur — et son `setup.py` s'en contente lorsque Cython est
absent.

C'est décisif ici : il n'existe pas de roue Cython pour Cygwin, sa compilation
depuis les sources y échoue, et pip l'installerait pourtant systématiquement à
cause de l'isolation de construction. D'où `--no-build-isolation`, les outils de
construction étant fournis séparément — ils sont écrits en Python pur et
s'installent en roues universelles.

Utiliser le code C publié par les mainteneurs de Borg est en outre plus fidèle
que de le regénérer avec une version de Cython choisie par nous.

Les bibliothèques de compression et de chiffrement sont désignées par les
variables `BORG_*_PREFIX` plutôt que cherchées par `pkg-config` : sous Cygwin,
`/usr` est le bon préfixe, et c'est une dépendance de moins.

### Pourquoi deux arbres

Borg n'a pas de roue précompilée pour Cygwin : il est compilé depuis ses
sources, ce qui exige `gcc`, `make` et les en-têtes de développement. Ces
outils pèsent plus lourd que tout le reste et n'ont rien à faire sur le poste
d'un utilisateur. L'arbre de compilation est donc jetable ; seul le résultat
est copié dans l'arbre livré, composé des paquets d'exécution uniquement.

C'est la démarche des empaquetages Cygwin existants de Borg, dont
[billyc/borg-cygwin](https://github.com/billyc/borg-cygwin) et
[nijave/borg-windows-package](https://github.com/nijave/borg-windows-package).
Aucun n'est repris tel quel : le premier s'arrête à Borg 1.0 et tous deux
reposent sur un mainteneur unique, ce qui est précisément le risque que le
cahier des charges demande d'éviter.

## Reproductibilité

Deux niveaux, dans cet ordre d'importance :

1. **Le dossier `work/packages/`** produit par l'étape de téléchargement
   contient les `.tar.xz` Cygwin exacts qui ont servi. **Conservez-le et
   publiez-le** avec l'archive : c'est la seule garantie de pouvoir
   reconstruire à l'identique quand les miroirs auront tourné. Une
   reconstruction se fait alors avec `--local-install --local-package-dir`,
   sans réseau.
2. **`requirements.txt`** épingle Borg et ses dépendances Python avec leurs
   empreintes, exigées à l'installation (`pip --require-hashes`).

Le [Cygwin Time Machine](http://www.crouchingtigerhiddenfruitbat.org/Cygwin/timemachine.html)
propose des miroirs datés (`--site .../circa/64bit/<date>`) et peut servir de
second recours, mais sa fraîcheur ne se contrôle pas : il ne remplace pas
l'archivage du dossier de paquets.

## Publier

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

Puis reporter dans `internal/borgruntime/spec.go` la version, l'empreinte,
l'adresse et la taille affichées par le script. Tant que `URL` et `SHA256` sont
vides, l'application refuse de télécharger quoi que ce soit et n'offre que la
voie hors ligne — elle le dit explicitement à l'utilisateur.

L'empreinte est vérifiée avant toute extraction, contre la valeur compilée dans
le binaire, jamais contre un fichier téléchargé à côté de l'archive (SEC-01).
`SHA256SUMS` n'est publié que pour la vérification manuelle par un
administrateur.

## Vérifier avant de publier

Le script vérifie déjà que le moteur répond la version attendue et qu'il sait
créer puis relire une sauvegarde. Restent les vérifications qui engagent le
projet, à passer une fois par version de runtime :

| Test | Vérification |
|---|---|
| TR-01 / TR-02 | Une archive créée sous Windows avec ce runtime est listée et extraite par le `borg` 1.4 officiel d'une machine Linux, dans les deux modes de chiffrement |
| TR-03 | Les chemins de l'archive commencent par la lettre de lecteur, sans préfixe `cygdrive` |
| TR-04 | Un fichier restauré est identique bit pour bit à l'original |
| TR-20 | Premier lancement sur une machine Windows vierge, sans droits administrateur |
| TR-21 | Une archive dont l'empreinte a été altérée est rejetée |
| TR-33 | Chemins accentués, avec espaces, et de plus de 260 caractères |

La première ligne est celle qui décide : si une archive produite par ce runtime
n'est pas relisible par un Borg officiel, le runtime est à revoir, pas
l'application.

## Antivirus et SmartScreen

Une archive contenant `python.exe` et `cygwin1.dll`, téléchargée puis exécutée
par un exécutable non signé, sera signalée par Defender et bloquée par
SmartScreen. Deux mesures, à prévoir avant le déploiement et non pendant :
signer l'exécutable de l'application (SEC-02), et documenter l'exclusion
antivirus du dossier `%LOCALAPPDATA%\borgui\runtime`.

Ce n'est pas une hypothèse : le refus se rencontre dès la construction, où il
se déguise en arbre Cygwin incomplet (voir plus haut). Le poste de
l'utilisateur subira le même traitement, à ceci près qu'il n'aura personne pour
le diagnostiquer. Deux conséquences pour l'application :

- l'installation du runtime doit **vérifier que le moteur s'exécute** — pas
  seulement que les fichiers sont là — et traduire un refus en message qui
  nomme l'antivirus, l'emplacement à exclure et la marche à suivre (EF-05) ;
- `%LOCALAPPDATA%` est retenu justement parce qu'il n'est pas protégé par
  l'accès contrôlé aux dossiers, contrairement à Documents ou au Bureau.

Un poste sous contrôle d'application intelligent ne pourra pas exécuter le
runtime, quoi que fasse l'application : c'est une limite à documenter, pas un
défaut à corriger.

## Mettre à jour le moteur

Incrémenter `Revision` pour un changement d'empaquetage à Borg constant,
changer `BorgVersion` pour un changement de moteur. Dans les deux cas, le
runtime s'installe dans un dossier versionné distinct : la mise à jour ne casse
pas une sauvegarde en cours et le retour arrière consiste à réinstaller la
version précédente. Aucune mise à jour n'est silencieuse (EF-09).
