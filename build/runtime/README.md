# Recette de construction du runtime Borg pour Windows

Ce dossier produit l'archive que l'application télécharge au premier lancement
sur un poste Windows. C'est le livrable LI-05 du cahier des charges :
reproductible, à versions épinglées, publiée avec son empreinte.

## Ce qu'est le runtime

Une installation Cygwin autonome contenant le code **officiel** de Borg, non
modifié : l'interpréteur Python, les bibliothèques de compression et de
chiffrement, le client OpenSSH et le shell par lequel l'application lance ses
commandes. Rien de plus.

Il n'est pas embarqué dans l'exécutable, pour trois raisons : l'exécutable doit
rester sous 30 Mo (ENF-01), le moteur doit pouvoir être remplacé sans
reconstruire l'application, et un binaire de 130 Mo qui extrait puis exécute
`python.exe` est exactement ce que les antivirus signalent.

## Construire

Sur une machine Windows x86-64, sans droits administrateur, 2 Go d'espace
libre :

```powershell
cd build\runtime
.\build-runtime.ps1 -BorgVersion 1.4.5 -Revision 1
```

Le script enchaîne huit étapes : récupération du programme d'installation
Cygwin et vérification de sa signature, téléchargement des paquets, arbre de
compilation, compilation de Borg depuis ses sources, composition de l'arbre
livré, élagage, vérification fonctionnelle, archive et empreinte.

Il produit dans `dist/` l'archive `borgui-runtime-<version>.zip`, le fichier
`SHA256SUMS`, et affiche les valeurs à reporter dans `spec.go`.

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
    build/runtime/dist/borgui-runtime-1.4.5-cygwin.1.zip \
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

## Mettre à jour le moteur

Incrémenter `Revision` pour un changement d'empaquetage à Borg constant,
changer `BorgVersion` pour un changement de moteur. Dans les deux cas, le
runtime s'installe dans un dossier versionné distinct : la mise à jour ne casse
pas une sauvegarde en cours et le retour arrière consiste à réinstaller la
version précédente. Aucune mise à jour n'est silencieuse (EF-09).
