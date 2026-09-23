# Recette de la v0.1

La v0.1 est validée quand **TR-01 à TR-04 passent** (cahier des charges §8,
addendum §9) : une sauvegarde créée sous Windows par le runtime Cygwin est
relue et restaurée, **bit pour bit**, par le `borg` 1.4 officiel d'une machine
Linux, dans les deux modes de chiffrement. Si cette étape échoue,
l'architecture est à revoir.

La procédure y ajoute les vérifications que l'addendum demande dès la v0.1 :
installation sans droits d'administrateur (TR-20), chemins accentués et de plus
de 260 caractères (§6.4, TR-33), absence de toute interaction sur une
destination non chiffrée (EF-38), durées de sauvegarde initiale et
incrémentale.

Compter une demi-journée, dont l'essentiel en transferts.

---

## 0. Ce qu'il faut

| | |
|---|---|
| Poste Windows | Windows 10 ou 11, **session sans droits d'administrateur**, jamais utilisé pour le projet (ni Cygwin, ni Borg, ni le runtime) |
| Machine Linux | `borg --version` doit répondre **1.4.x**. Si la distribution fournit une autre série, prendre le binaire autonome officiel (`borg-linux-glibc*`) sur les releases GitHub de BorgBackup |
| Storage Box | un sous-compte dédié à la recette, avec « SSH support » et « External reachability » activés |
| Données | une vingtaine de gigaoctets de vraies données sur le poste Windows, en plus du jeu généré, **hors de OneDrive** : l'exclusion des fichiers à la demande n'arrive qu'en v0.2, et leur lecture déclencherait leur téléchargement |

Les deux modes de chiffrement utilisent deux destinations distinctes du même
sous-compte, donc deux fichiers de configuration.

## 1. Construire les binaires

Sur la machine de développement, depuis la racine du dépôt :

```bash
mkdir -p recette
GOOS=windows go build -o recette/borgui.exe ./cmd/borgui
GOOS=windows go build -o recette/borgui-recette.exe ./cmd/borgui-recette
go build -o recette/borgui-recette ./cmd/borgui-recette
```

La ligne de commande de la v0.1 ne dépend pas de CGO : la compilation croisée
fonctionne. Copier `borgui.exe` et `borgui-recette.exe` sur le poste Windows,
dans `C:\Recette\` par exemple ; `borgui-recette` sur la machine Linux.

Toutes les commandes Windows qui suivent se tapent dans PowerShell, depuis
`C:\Recette`.

## 2. Installer le moteur — TR-20

```powershell
.\borgui.exe runtime install
.\borgui.exe runtime status
```

Attendu : téléchargement, empreinte acceptée, `Moteur installé : 1.4.5`, sans
aucune demande d'élévation. Le runtime se trouve dans
`%LOCALAPPDATA%\borgui\runtime\1.4.5-cygwin.1\`.

Puis la vérification propre au format d'archive : les liens symboliques
écrits par l'application doivent être relus comme tels par Cygwin.

```powershell
$rt = "$env:LOCALAPPDATA\borgui\runtime\1.4.5-cygwin.1"
& "$rt\bin\bash.exe" -c 'export PATH=/usr/bin; ls -la /bin/python3; python3 --version'
```

Attendu : une ligne qui commence par `l` et se termine par
`-> /etc/alternatives/python3`, puis `Python 3.12.x`. Une ligne commençant par
`-` signifie que l'attribut « système » n'a pas été posé.

## 3. Préparer les données et leurs empreintes

```powershell
.\borgui-recette.exe generate C:\Recette\donnees --size 1024
.\borgui-recette.exe manifest C:\Recette\donnees -o C:\Recette\donnees.tsv
.\borgui-recette.exe manifest C:\Users\<vous>\<dossier réel> -o C:\Recette\reelles.tsv
```

Le jeu généré contient des noms accentués, avec espaces, en japonais, grec et
cyrillique, un nom en Unicode décomposé, un chemin de plus de 260 caractères,
un fichier et des dossiers vides, un doublon et un fichier incompressible
d'1 Gio. Le manifeste est établi **avant** la sauvegarde : c'est lui qui fait
foi.

Pour observer les jonctions (§6.4), en créer une dans le jeu **après** le
manifeste, sans droits d'administrateur :

```powershell
cmd /c mklink /J C:\Recette\donnees\jonction C:\Recette\donnees\accents
```

Son sort n'est pas un critère de TR-01 à TR-04 : il est à consigner, pas à
réussir. `compare --ignore jonction` l'écarte du verdict ; sans cette option,
elle apparaît « en trop », avec sa nature et, si c'est un lien, sa cible — ce
qui est précisément l'observation à consigner.

## 4. Mode chiffré sous Windows

```powershell
$cfg = "--config", "C:\Recette\chiffre.toml"
.\borgui.exe @cfg config init --name recette-chiffre --user uXXXXXX --repo recette-chiffre
```

Éditer `C:\Recette\chiffre.toml` et renseigner les sources :

```toml
sources = ['C:\Recette\donnees', 'C:\Users\<vous>\<dossier réel>']
```

Puis :

```powershell
.\borgui.exe @cfg key show               # déposer la clé dans la console Hetzner
.\borgui.exe @cfg connection test --pin  # vérifier l'empreinte affichée avant d'accepter
.\borgui.exe @cfg passphrase set
```

Deux contrôles, qui éprouvent les chemins où la recette a déjà buté
(`docs/anomalie-permissions-cle-ssh.md`,
`docs/anomalie-passcommand-cwd-cygdrive.md`) :

```powershell
$rt = "$env:LOCALAPPDATA\borgui\runtime\1.4.5-cygwin.1"
# La clé telle que le ssh du runtime la voit. Attendu : -rw-------
& "$rt\bin\bash.exe" -c 'export PATH=/usr/bin; ls -la "$(cygpath "$LOCALAPPDATA")/borgui/id_ed25519"'
# Un exécutable Windows lancé depuis /cygdrive, comme Borg rappelle
# l'application pour la passphrase. Attendu : OK
# Sans guillemets imbriqués, que PowerShell 5 transmet mal : le sous-shell
# quitte /cygdrive puis passe la main, comme le fait l'application.
& "$rt\bin\bash.exe" -c 'cd /cygdrive && (cd / && exec /cygdrive/c/Windows/System32/cmd.exe /c echo OK)'
```

Toute autre sortie annonce l'échec de `backup` en mode chiffré : autant le voir
ici qu'au travers d'une trace Python.

```powershell
.\borgui.exe @cfg repository init
.\borgui.exe @cfg repository export-key  # à conserver ; l'étape 6 n'en a pas besoin, la clé est dans la destination
.\borgui.exe @cfg backup                 # sauvegarde initiale : noter la durée affichée
.\borgui.exe @cfg backup                 # incrémentale, sans rien changer : noter la durée
.\borgui.exe @cfg archives
```

Restauration sur le poste lui-même, dans un dossier neuf :

```powershell
.\borgui.exe @cfg restore --to C:\Recette\restauration-chiffre
.\borgui-recette.exe compare C:\Recette\donnees.tsv C:\Recette\restauration-chiffre\Recette\donnees --ignore jonction
.\borgui-recette.exe compare C:\Recette\reelles.tsv C:\Recette\restauration-chiffre\Users\<vous>\<dossier réel>
```

La lettre de lecteur est retirée à la restauration (`--strip-components 1`) :
`C:\Recette\donnees` revient donc sous `restauration-chiffre\Recette\donnees`.
Attendu : `Restauration fidèle` pour les deux.

## 5. Mode non chiffré sous Windows

Même séquence avec un second fichier, **sans** `passphrase set` ni
`export-key` :

```powershell
$cfg = "--config", "C:\Recette\clair.toml"
.\borgui.exe @cfg config init --name recette-clair --user uXXXXXX --repo recette-clair --clear
# renseigner les mêmes sources dans clair.toml
.\borgui.exe @cfg repository init
```

Pour la sauvegarde, vérifier au passage qu'aucune question ne peut bloquer une
exécution sans surveillance (EF-38) — l'entrée standard est fermée :

```powershell
cmd /c "borgui.exe --config C:\Recette\clair.toml backup < NUL"
```

Attendu : la sauvegarde va à son terme sans attendre de saisie. Puis
`archives`, `restore --to C:\Recette\restauration-clair` et les deux `compare`,
comme à l'étape 4.

## 6. Relecture par le borg officiel sous Linux — TR-01 à TR-04

La machine Linux n'a jamais vu ces destinations : c'est précisément ce que
l'on veut éprouver. Déposer d'abord sa propre clé publique dans la console
Hetzner, à côté de celle du poste Windows.

Copier aussi sur la machine Linux `donnees.tsv` et `reelles.tsv`. Au premier
accès, `ssh` demande d'accepter l'empreinte de la Storage Box : la comparer à
celle que `connection test --pin` a affichée sous Windows.

### Mode chiffré — TR-01

```bash
export BORG_REPO='ssh://uXXXXXX@uXXXXXX.your-storagebox.de:23/./recette-chiffre'
export BORG_REMOTE_PATH=borg-1.4
export BORG_PASSPHRASE='…'    # la passphrase saisie à l'étape 4 ; acceptable pour une recette manuelle

borg list
ARCHIVE=$(borg list --short --last 1)
borg list "::$ARCHIVE" | head -20
```

**TR-03** : chaque chemin listé commence par la lettre de lecteur en
minuscule — `c/Recette/donnees/…`, `c/Users/…` — sans `cygdrive` ni `/`
initial.

```bash
mkdir ~/restauration-chiffre && cd ~/restauration-chiffre
borg extract "::$ARCHIVE"
borgui-recette compare ~/donnees.tsv ~/restauration-chiffre/c/Recette/donnees --ignore jonction
borgui-recette compare ~/reelles.tsv ~/restauration-chiffre/c/Users/<vous>/<dossier réel>
```

**TR-01** : `borg list` et `borg extract` se terminent sans erreur.
**TR-04** : les deux `compare` répondent `Restauration fidèle`.

### Mode non chiffré — TR-02

```bash
export BORG_REPO='ssh://uXXXXXX@uXXXXXX.your-storagebox.de:23/./recette-clair'
unset BORG_PASSPHRASE
export BORG_UNKNOWN_UNENCRYPTED_REPO_ACCESS_IS_OK=yes
```

Puis les mêmes commandes, dans `~/restauration-clair`.

Sans la dernière variable, `borg` demande confirmation avant d'accéder à une
destination non chiffrée qu'il ne connaît pas : c'est le comportement que
l'application neutralise d'office (EF-38), et qu'il est instructif de voir une
fois.

## 7. Consigner les résultats

| Vérification | Chiffré | Non chiffré |
|---|---|---|
| TR-20 installation sans administrateur | | — |
| Liens Cygwin relus (`ls -la /bin/python3`) | | — |
| Restauration fidèle sous Windows (jeu généré / données réelles) | | |
| EF-38 sauvegarde sans interaction | — | |
| TR-01 / TR-02 relecture par `borg` Linux | | |
| TR-03 chemins en `c/…` | | |
| TR-04 restauration fidèle sous Linux (jeu généré / données réelles) | | |
| TR-33 chemin > 260 caractères, accents, Unicode | | |
| Jonction : ce qu'elle devient à la restauration | | |
| Volume des données réelles | | |
| Durée de la sauvegarde initiale / incrémentale | | |

En cas d'écart, `compare` liste les éléments absents, différents ou en trop ;
joindre sa sortie et celle de la commande `borg` ou `borgui` en cause.

## 8. Après la recette

Supprimer les destinations `recette-chiffre` et `recette-clair` sur la Storage
Box, retirer la clé de la machine Linux des clés autorisées, et effacer
`C:\Recette` ainsi que `%LOCALAPPDATA%\borgui` et `%APPDATA%\borgui` pour
rendre le poste à son état initial.
