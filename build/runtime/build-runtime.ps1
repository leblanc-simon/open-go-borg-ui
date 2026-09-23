<#
.SYNOPSIS
    Construit le runtime Borg livré aux postes Windows (LI-05).

.DESCRIPTION
    Le runtime est une installation Cygwin autonome contenant le code officiel
    de Borg, non modifié. Il n'est pas embarqué dans l'exécutable : celui-ci le
    télécharge au premier lancement et vérifie son empreinte avant de
    l'exécuter. Cette recette produit l'archive et l'empreinte à publier.

    La construction se fait en deux temps, comme le fait tout paquet Cygwin
    autonome : un arbre de compilation, jetable, où Borg est compilé depuis ses
    sources ; puis un arbre de livraison, minimal, où seul le résultat est
    copié. Les outils de compilation pèsent plus que tout le reste et n'ont
    rien à faire sur le poste d'un utilisateur.

    Aucun droit administrateur n'est nécessaire, ici comme à l'exécution.

.EXAMPLE
    .\build-runtime.ps1 -BorgVersion 1.4.5 -Revision 1

.EXAMPLE
    .\build-runtime.ps1 -UpdateHashes
    Renseigne les empreintes de requirements.txt, à faire une fois par version
    de Borg, puis relire le fichier avant de construire.

.NOTES
    À exécuter sur une machine Windows x86-64. Prévoir 2 Go d'espace libre.
#>

[CmdletBinding()]
param(
    # Version de Borg à empaqueter. Elle est épinglée par version de
    # l'application : jamais de mise à jour silencieuse du moteur.
    [string] $BorgVersion = '1.4.5',

    # Révision de l'empaquetage, à incrémenter quand le runtime change sans que
    # Borg change (correction d'une dépendance, élagage revu).
    [string] $Revision = '1',

    # Série de Python empaquetée. Elle doit exister dans Cygwin — le
    # méta-paquet python3 pointe sur python312 à ce jour — et figurer parmi
    # les versions acceptées par Borg, qui exige 3.10 ou plus récent depuis
    # la 1.4.5.
    [string] $PythonSeries = '3.12',

    # Miroir Cygwin. Un miroir daté rend la reconstruction reproductible, mais
    # le dossier de paquets produit ci-dessous en est une garantie plus sûre.
    [string] $Mirror = 'https://mirrors.kernel.org/sourceware/cygwin/',

    # Espace de travail. Il est entièrement jetable. Vide, il est placé dans
    # le dossier de la recette, une fois celui-ci résolu.
    [string] $Workspace = '',

    # Calcule les empreintes des dépendances Python et réécrit
    # requirements.txt, au lieu de construire. À faire une fois par version de
    # Borg, sur une machine de confiance.
    [switch] $UpdateHashes
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# Dossier de la recette. $PSScriptRoot est vide selon la manière dont le script
# a été lancé — contenu passé sur l'entrée standard, appel par -Command,
# copier-coller dans la console — et le bloc param() est évalué avant lui de
# toute façon. Les trois formes sont essayées, de la plus fiable à la dernière.
$ScriptRoot =
    if ($PSScriptRoot) { $PSScriptRoot }
    elseif ($MyInvocation.MyCommand.Path) { Split-Path -Parent $MyInvocation.MyCommand.Path }
    else { (Get-Location).Path }

$RequirementsFile = Join-Path $ScriptRoot 'requirements.txt'
$PackagesFile     = Join-Path $ScriptRoot 'packages.txt'
if (-not (Test-Path $PackagesFile)) {
    throw "packages.txt est introuvable dans $ScriptRoot : lancez le script depuis build\runtime, ou avec -File"
}
if (-not $Workspace) { $Workspace = Join-Path $ScriptRoot 'work' }

$RuntimeVersion = "$BorgVersion-cygwin.$Revision"
$Python         = "python$PythonSeries"                        # binaire : python3.12
$PythonPackage  = 'python' + ($PythonSeries -replace '\.', '') # paquet  : python312
$PythonLibDir   = "/usr/lib/python$PythonSeries"

$SetupExe   = Join-Path $Workspace 'setup-x86_64.exe'
$PackageDir = Join-Path $Workspace 'packages'   # artefact à conserver
$BuildRoot  = Join-Path $Workspace 'build'      # arbre de compilation, jetable
$StageRoot  = Join-Path $Workspace 'stage'      # arbre livré
$Output     = Join-Path $ScriptRoot 'dist'

function Write-Step([string] $Message) {
    Write-Host "==> $Message" -ForegroundColor Cyan
}

# ConvertTo-CygwinPath traduit un chemin Windows dans la forme comprise par le
# runtime. La lettre de lecteur est mise en minuscule, comme le sont les points
# de montage de /cygdrive et comme l'application écrit ses archives.
function ConvertTo-CygwinPath([string] $Path) {
    $normalized = $Path -replace '\\', '/'
    if ($normalized -match '^([A-Za-z]):(.*)$') {
        return '/cygdrive/' + $Matches[1].ToLower() + $Matches[2]
    }
    return $normalized
}

function Read-PackageList([string] $Section) {
    $inSection = $false
    $packages = @()
    foreach ($line in Get-Content $PackagesFile) {
        $trimmed = $line.Trim()
        if ($trimmed -match '^\[(.+)\]$') { $inSection = ($Matches[1] -eq $Section); continue }
        if (-not $inSection -or $trimmed -eq '' -or $trimmed.StartsWith('#')) { continue }
        $packages += $trimmed
    }
    if ($packages.Count -eq 0) { throw "aucun paquet dans la section [$Section]" }
    return ($packages -join ',')
}

# Get-BlockedRuntimeHelp explique le refus d'exécution le plus coûteux à
# diagnostiquer, parce qu'il ne se présente jamais comme ce qu'il est.
#
# Une protection du poste refuse le chargement d'un binaire Cygwin : Windows
# répond ACCESS_DENIED, Cygwin le traduit en EACCES, et ce qui remonte parle
# d'une extension Python « Permission denied » alors que le fichier est bien
# là, ou d'un script de post-installation sorti en 126. Rien dans ces messages
# ne nomme la cause, et l'arbre paraît seulement incomplet.
function Get-BlockedRuntimeHelp {
    return @"
un binaire du runtime n'a pas pu être exécuté : une protection du poste le
refuse. C'est ce que disent un « Permission denied » sur un fichier présent et
un post-install sorti en 126.

Un arbre Cygwin est fait de centaines de binaires non signés, écrits puis
exécutés dans la foulée : c'est le profil exact de ce que Windows arrête. Dans
l'ordre de probabilité :

1. Construire hors des dossiers protégés — Téléchargements, Bureau, Documents
   et OneDrive le sont par défaut, et tout ce qui est extrait d'une archive
   téléchargée y garde la marque « venu d'Internet » :

       .\build-runtime.ps1 -Workspace C:\borgui-build

2. Lever cette marque sur la recette elle-même, depuis son dossier :

       Get-ChildItem -Recurse -File | Unblock-File

3. Exclure l'espace de travail de l'analyse en temps réel (administrateur) :

       Add-MpPreference -ExclusionPath C:\borgui-build

4. Lire ce que Defender a bloqué, qui le nomme mieux que Cygwin :

       Get-WinEvent -LogName 'Microsoft-Windows-Windows Defender/Operational' |
           Where-Object Id -in 1116,1117,1121,1126 |
           Select-Object -First 10 TimeCreated, Message
"@
}

# Invoke-Setup lance le programme d'installation Cygwin et attend qu'il ait
# terminé.
#
# C'est une application graphique, même en mode silencieux : lancée avec
# l'opérateur d'appel, elle rend la main immédiatement et l'étape suivante
# travaille sur un arbre encore incomplet. Start-Process -Wait est le seul
# moyen fiable d'attendre, et son code de sortie le seul moyen de savoir que
# l'installation a réussi.
function Invoke-Setup([string[]] $Arguments) {
    $log = Join-Path $Workspace 'setup.log'
    $process = Start-Process -FilePath $SetupExe -ArgumentList $Arguments -Wait -PassThru `
        -NoNewWindow -RedirectStandardOutput $log
    if (Test-Path $log) { Get-Content $log | ForEach-Object { Write-Host "    $_" } }

    if ($process.ExitCode -ne 0) {
        throw "installation Cygwin en échec (code $($process.ExitCode)) : $($Arguments -join ' ')"
    }
    # Un paquet inconnu ne fait pas échouer le programme d'installation : il le
    # mentionne et poursuit. L'arbre est alors incomplet et ne le dira qu'à
    # l'usage, plusieurs étapes plus loin.
    $missing = Select-String -Path $log -Pattern "Package '(.+)' not found" -AllMatches
    if ($missing) {
        $names = ($missing.Matches | ForEach-Object { $_.Groups[1].Value }) -join ', '
        throw "paquets inconnus du miroir Cygwin : $names. Corrigez packages.txt."
    }
}

# Write-CygwinScript dépose un script dans un arbre Cygwin et rend le chemin
# par lequel bash l'y trouvera.
#
# Les scripts ne sont pas passés à « bash -c » mais exécutés par leur chemin.
# Windows PowerShell 5.1 assemble la ligne de commande d'un programme natif
# sans protéger les guillemets contenus dans un argument : un script shell qui
# en contient — et tout script correct en contient, ne serait-ce que pour citer
# une variable — est découpé en plusieurs arguments avant d'atteindre bash.
# Celui-ci n'en reçoit qu'un fragment et se plaint d'une fin de fichier
# prématurée, ou d'un mot isolé au milieu d'une ligne, sans que rien ne désigne
# la cause. Le fichier supprime la question, et reste lisible en cas d'échec.
#
# Il est écrit à la racine de l'arbre, et non dans /tmp, que les étapes
# d'élagage et de vérification vident : un script ne doit pas disparaître sous
# les pas de bash, qui le lit au fur et à mesure.
function Write-CygwinScript([string] $Root, [string] $Script) {
    # Le script vient d'un fichier en fins de ligne Windows, que bash refuse :
    # il verrait un retour chariot comme faisant partie de la commande, et se
    # plaindrait d'options invalides ou de fichiers introuvables dont le nom
    # paraît pourtant correct. Pour la même raison il est écrit sans marque
    # d'ordre des octets, que bash lirait comme le début de la première
    # commande.
    $Script = $Script -replace "`r", ''
    # Le script réunit lui-même ses deux sorties, plutôt que de laisser
    # PowerShell le faire avec « 2>&1 ». Celui-ci enveloppe alors chaque ligne
    # d'erreur d'un programme natif dans un objet d'erreur, dont il n'affiche
    # que la première ligne, noyée dans sa propre mise en forme : d'une trace
    # d'appels de plusieurs dizaines de lignes, il ne reste que « ERROR:
    # Exception », c'est-à-dire rien. Réunies dans le script, les deux sorties
    # arrivent ici comme du texte et s'affichent en entier.
    $Script = "exec 2>&1`n" + $Script
    $name = '.borgui-' + [guid]::NewGuid().ToString('N') + '.sh'
    [System.IO.File]::WriteAllText(
        (Join-Path $Root $name), $Script, (New-Object System.Text.UTF8Encoding $false))
    return $name
}

# Invoke-Cygwin exécute une commande dans un arbre Cygwin donné.
function Invoke-Cygwin([string] $Root, [string] $Script) {
    $bash = Join-Path $Root 'bin\bash.exe'
    if (-not (Test-Path $bash)) {
        throw "arbre Cygwin incomplet : $bash est absent"
    }
    $name = Write-CygwinScript $Root $Script
    try {
        # La sortie traverse la fonction — elle s'affiche au fil de l'eau, une
        # compilation étant longue, et les appelants qui la capturent la
        # reçoivent — et Tee-Object la retient au passage : c'est elle qui
        # distingue un échec de commande d'un binaire que le poste a refusé
        # d'exécuter.
        $output = @()
        & $bash '-l' "/$name" | Tee-Object -Variable output
        if ($LASTEXITCODE -ne 0) {
            if ($output -match 'Permission denied') {
                throw ((Get-BlockedRuntimeHelp) + "`ncommande : $Script")
            }
            throw "commande Cygwin en échec ($LASTEXITCODE) : $Script"
        }
    }
    finally {
        Remove-Item (Join-Path $Root $name) -Force -ErrorAction SilentlyContinue
    }
}

# Assert-Interpreter éprouve l'interpréteur d'un arbre.
#
# Le fichier peut exister sans être exécutable — bibliothèque manquante, arbre
# installé à moitié, extension refusée par une protection du poste. C'est donc
# l'exécution qui fait foi, et non la présence, faute de quoi le défaut ne se
# manifeste que par un « command not found » au milieu d'un script shell, qui
# ne désigne pas sa cause.
function Assert-Interpreter([string] $Root) {
    $bash = Join-Path $Root 'bin\bash.exe'
    if (-not (Test-Path $bash)) {
        throw "arbre Cygwin incomplet dans $Root : bin\bash.exe est absent"
    }
    # « python --version » ne charge aucune extension compilée : il réussit sur
    # un arbre où pas une bibliothèque ne l'est. Les modules importés ici en
    # sont — subprocess tire _posixsubprocess, ssl tire _ssl — et ce sont ceux
    # dont pip a besoin dès sa première commande.
    $probe = "$Python -c 'import subprocess, ssl, hashlib, zlib, sys; print(sys.version.split()[0])'"
    $name = Write-CygwinScript $Root $probe
    try {
        $output = @()
        & $bash '-l' "/$name" | Tee-Object -Variable output | ForEach-Object { Write-Host "    $_" }
        if ($LASTEXITCODE -ne 0) {
            if ($output -match 'Permission denied') { throw (Get-BlockedRuntimeHelp) }
            throw "$Python ne s'exécute pas dans $Root : le paquet $PythonPackage manque ou l'arbre est incomplet"
        }
    }
    finally {
        Remove-Item (Join-Path $Root $name) -Force -ErrorAction SilentlyContinue
    }
}

# Get-PinnedRequirements lit les dépendances épinglées, sans leurs empreintes.
function Get-PinnedRequirements {
    $requirements = @()
    foreach ($line in Get-Content $RequirementsFile) {
        if ($line -match '^\s*([A-Za-z0-9_.\-]+==[^\s\\]+)') { $requirements += $Matches[1] }
    }
    if ($requirements.Count -eq 0) { throw "aucune dépendance épinglée dans requirements.txt" }
    return $requirements
}

# Test-HashesPinned indique si les empreintes ont été renseignées. Les valeurs
# du dépôt sont des zéros : elles ne peuvent être calculées que sur une machine
# capable de compiler, donc pas au moment où le fichier a été écrit.
function Test-HashesPinned {
    return -not (Select-String -Path $RequirementsFile -Pattern '--hash=sha256:0{64}' -Quiet)
}

# Assert-BuildLocation écarte d'emblée les emplacements et les réglages où la
# construction ne peut pas aboutir.
#
# Ces vérifications coûtent une seconde ; ce qu'elles évitent coûte une
# construction entière, qui dure longtemps et qui échoue loin de sa cause.
function Test-ProtectedPath([string] $Path) {
    # Les dossiers que l'accès contrôlé aux dossiers protège d'office, dans les
    # deux langues d'affichage : le nom du dossier sur le disque suit celle de
    # l'installation de Windows.
    $protected = 'Downloads', 'Téléchargements', 'Desktop', 'Bureau',
                 'Documents', 'OneDrive'
    foreach ($name in $protected) {
        if (($Path -split '\\') -contains $name) { return $true }
    }
    return $false
}

function Assert-BuildLocation {
    if (Test-ProtectedPath $Workspace) {
        throw @"
l'espace de travail est dans un dossier protégé par Windows : $Workspace

L'accès contrôlé aux dossiers y refuse les écritures des programmes qu'il ne
connaît pas — PowerShell en est — et tout ce qui provient d'une archive
téléchargée y garde la marque « venu d'Internet ». Un arbre Cygwin ne s'y
installe pas en entier, et ce qui manque ne se voit que beaucoup plus loin.

    .\build-runtime.ps1 -Workspace C:\borgui-build
"@
    }
    if (Test-ProtectedPath $ScriptRoot) {
        throw @"
la recette est dans un dossier protégé par Windows : $ScriptRoot

Elle y écrit l'archive produite et, avec -UpdateHashes, requirements.txt : ces
écritures sont refusées par l'accès contrôlé aux dossiers sans que rien ne le
dise clairement. Clonez le dépôt ailleurs, par exemple sous C:\src.
"@
    }

    # Contrôle d'application intelligent : il refuse tout binaire non signé,
    # donc l'arbre Cygwin en entier. 0 désactivé, 1 actif, 2 évaluation.
    $policy = Get-ItemProperty 'HKLM:\SYSTEM\CurrentControlSet\Control\CI\Policy' `
        -Name VerifiedAndReputablePolicyState -ErrorAction SilentlyContinue
    if ($policy -and $policy.VerifiedAndReputablePolicyState -ne 0) {
        throw @"
le contrôle d'application intelligent (Smart App Control) est actif sur ce
poste : il refuse d'exécuter les binaires non signés, dont tout l'arbre Cygwin.

Il ne se désactive qu'une fois pour toutes — le réactiver demande une
réinstallation de Windows. Construisez plutôt le runtime sur une autre machine :
c'est une opération ponctuelle, faite une fois par version de moteur.
"@
    }
}

Write-Step "Runtime $RuntimeVersion (Python $PythonSeries)"
Assert-BuildLocation
New-Item -ItemType Directory -Force -Path $Workspace, $PackageDir, $Output | Out-Null

# 1. Programme d'installation Cygwin.
#
# Sa signature est vérifiée plutôt que son empreinte : le fichier est
# republié à chaque version de Cygwin, la signature, elle, ne change pas de
# titulaire.
Write-Step 'Récupération du programme d''installation Cygwin'
if (-not (Test-Path $SetupExe)) {
    Invoke-WebRequest -Uri 'https://cygwin.com/setup-x86_64.exe' -OutFile $SetupExe
}
# Marque « venu d'Internet » : elle vaudrait à ce programme un avertissement de
# SmartScreen, donc une fenêtre à valider au milieu d'une construction que rien
# ne surveille. Sa signature, elle, est vérifiée juste après.
Unblock-File $SetupExe
$signature = Get-AuthenticodeSignature $SetupExe
if ($signature.Status -ne 'Valid') {
    throw "signature du programme d'installation Cygwin invalide : $($signature.Status)"
}
Write-Host "    signé par $($signature.SignerCertificate.Subject)"

# 2. Téléchargement des paquets, sans installation.
#
# Le dossier obtenu est l'artefact qui rend la construction reproductible :
# conservé et republié avec l'archive, il permet de reconstruire à l'identique
# des années plus tard, quel que soit l'état des miroirs.
#
# L'arbre de compilation est d'abord effacé. Le programme d'installation tient
# sa propre base de ce qu'il a installé dans un arbre, et s'y fie même pour
# décider de ce qu'il a besoin de télécharger : sur un arbre laissé à moitié
# fait par une exécution interrompue, il conclut que tout est en place, ne
# télécharge ni n'installe plus rien, et l'arbre reste incomplet
# indéfiniment. Le dossier des paquets, lui, est conservé : il évite de tout
# retélécharger et c'est l'artefact à publier.
Write-Step 'Téléchargement des paquets Cygwin'
$buildPackages   = Read-PackageList 'build'
$releasePackages = Read-PackageList 'release'
# Les deux fichiers doivent parler de la même série de Python : une divergence
# produirait un runtime dont l'interpréteur ne connaît pas Borg.
foreach ($list in @($buildPackages, $releasePackages)) {
    if ($list -notmatch "(^|,)$PythonPackage(,|$)") {
        throw "packages.txt ne contient pas $PythonPackage : la série de Python demandée n'y figure pas"
    }
}
if (Test-Path $BuildRoot) { Remove-Item -Recurse -Force $BuildRoot }
Invoke-Setup @(
    '--quiet-mode', '--no-admin', '--no-shortcuts', '--no-desktop', '--download',
    '--site', $Mirror, '--local-package-dir', $PackageDir, '--root', $BuildRoot,
    '--packages', "$buildPackages,$releasePackages"
)

# 3. Arbre de compilation.
Write-Step 'Installation de l''arbre de compilation'
Invoke-Setup @(
    '--quiet-mode', '--no-admin', '--no-shortcuts', '--no-desktop', '--local-install',
    '--local-package-dir', $PackageDir, '--root', $BuildRoot, '--packages', $buildPackages
)
Assert-Interpreter $BuildRoot

# 3 bis. Empreintes des dépendances Python.
#
# Elles ne peuvent être calculées que sur une machine capable de télécharger et
# de lire les sources : c'est ici, une fois l'arbre de compilation en place.
if ($UpdateHashes) {
    Write-Step 'Calcul des empreintes des dépendances'
    $pinned = Get-PinnedRequirements
    Invoke-Cygwin $BuildRoot @"
set -e
rm -rf /tmp/sources && mkdir -p /tmp/sources
printf '%s\n' '$($pinned -join "' '")' > /tmp/plain.txt
# --no-deps : sans lui, pip résout les dépendances, ce qui l'amène à construire
# les métadonnées des paquets, donc à installer puis compiler leurs outils de
# construction. Les dépendances d'exécution sont déjà listées ici, une par une.
$Python -m pip download --no-deps --no-binary :all: --dest /tmp/sources --requirement /tmp/plain.txt
"@
    $lines = Invoke-Cygwin $BuildRoot @"
set -e
for source in /tmp/sources/*; do
    name=`$(basename "`$source")
    digest=`$($Python -m pip hash "`$source" | tail -n 1)
    echo "`$name `$digest"
done
"@

    $entries = @()
    foreach ($line in $lines) {
        if ($line -notmatch '^(\S+)\s+(--hash=sha256:[0-9a-f]{64})$') { continue }
        $archive, $hash = $Matches[1], $Matches[2]
        # borgbackup-1.4.5.tar.gz -> borgbackup==1.4.5
        $stem = $archive -replace '\.(tar\.gz|zip|tar\.bz2)$', ''
        $split = $stem.LastIndexOf('-')
        if ($split -lt 1) { continue }
        $entries += "{0}=={1} \`n    {2}" -f $stem.Substring(0, $split), $stem.Substring($split + 1), $hash
    }
    if ($entries.Count -eq 0) { throw "aucune empreinte calculée : la sortie de pip hash est inattendue" }

    $header = @'
# Dépendances Python du moteur, épinglées à la version près.
#
# Fichier produit par « build-runtime.ps1 -UpdateHashes », à relire avant de
# le valider : les empreintes sont exigées à l'installation
# (pip --require-hashes), faute de quoi une reconstruction six mois plus tard
# ne produirait pas le même runtime.
'@
    $content = $header + "`n`n" + ($entries -join "`n") + "`n"
    [System.IO.File]::WriteAllText($RequirementsFile, $content, (New-Object System.Text.UTF8Encoding $false))
    Write-Step "Empreintes écrites dans $RequirementsFile"
    Write-Host '    Relisez le fichier, validez-le, puis relancez la construction.'
    return
}

if (-not (Test-HashesPinned)) {
    throw @"
les empreintes de requirements.txt ne sont pas renseignées.

Calculez-les une fois, puis relisez le fichier avant de le valider :

    .\build-runtime.ps1 -UpdateHashes
"@
}

# 4. Compilation de Borg depuis ses sources.
#
# Cygwin n'a pas de roue précompilée : Borg est compilé contre les
# bibliothèques de l'arbre. Les empreintes sont exigées, faute de quoi une
# dépendance republiée changerait silencieusement le runtime.
Write-Step "Compilation de Borg $BorgVersion"
$requirements = ConvertTo-CygwinPath $RequirementsFile
Invoke-Cygwin $BuildRoot @"
set -e
# Les outils de construction sont installés à part, en roues universelles : ils
# sont écrits en Python pur et n'ont rien à compiler. --only-binary l'exige,
# plutôt que de le supposer : un outil qui n'aurait pas de roue échouerait ici,
# et non au milieu de la compilation.
#
# Il faut les backends de tous les paquets construits, pas seulement de Borg :
# --no-build-isolation interdit à pip de les installer lui-même, et il ne les
# réclame qu'au moment de lire les métadonnées, en annonçant « Cannot import
# <backend> » sans dire à quel paquet il appartient. setuptools couvre Borg et
# msgpack, flit_core couvre packaging.
$Python -m pip install --no-cache-dir --only-binary :all: \
    'setuptools>=78.1.1' 'setuptools_scm>=8' wheel flit_core

# Les bibliothèques sont désignées par leur préfixe plutôt que cherchées par
# pkg-config : sous Cygwin, /usr/include et /usr/lib sont les bons chemins, et
# c'est une dépendance de moins.
export BORG_OPENSSL_PREFIX=/usr
export BORG_LIBLZ4_PREFIX=/usr
export BORG_LIBZSTD_PREFIX=/usr
export BORG_LIBXXHASH_PREFIX=/usr

# --no-build-isolation : le paquet source de Borg déclare Cython parmi ses
# outils de construction, mais il embarque déjà le code C que Cython aurait
# produit, et son setup.py s'en contente lorsque Cython est absent. Sans cette
# option, pip installerait Cython depuis les sources — il n'en existe pas de
# roue pour Cygwin — et sa compilation échoue. Utiliser le code C publié par
# les mainteneurs de Borg est en outre plus fidèle que de le regénérer.
$Python -m pip wheel --no-build-isolation --no-binary :all: --require-hashes \
    --requirement '$requirements' --wheel-dir /tmp/wheels
"@

# 5. Arbre livré : uniquement l'exécution.
Write-Step 'Composition de l''arbre livré'
if (Test-Path $StageRoot) { Remove-Item -Recurse -Force $StageRoot }
Invoke-Setup @(
    '--quiet-mode', '--no-admin', '--no-shortcuts', '--no-desktop', '--local-install',
    '--local-package-dir', $PackageDir, '--root', $StageRoot, '--packages', $releasePackages
)
Assert-Interpreter $StageRoot

# Les roues sont posées dans l'arbre livré depuis l'arbre de compilation, qui
# est le seul à disposer de pip : pip est un outil de construction et n'a rien
# à faire dans ce qui est livré, où il pèserait pour rien.
#
# --prefix désigne la racine de l'arbre livré, et non son /usr : Cygwin monte
# /usr/bin et /usr/lib sur bin et lib à la racine, et ce montage n'existe que
# vu de l'intérieur de l'arbre. Écrire ici par un chemin Windows le contourne :
# un préfixe en /usr produirait un usr/bin et un usr/lib que l'arbre livré ne
# consulterait jamais, et l'installation paraîtrait pourtant réussie.
#
# --ignore-installed : sans lui, pip compare aux paquets de l'arbre de
# compilation, et désinstalle de celui-ci la version qu'il croit remplacer —
# l'arbre livré reçoit bien la sienne, mais l'arbre de construction est abîmé
# au passage.
Invoke-Cygwin $BuildRoot @"
set -e
$Python -m pip install --no-cache-dir --no-index --no-deps --ignore-installed \
    --prefix '$(ConvertTo-CygwinPath $StageRoot)' /tmp/wheels/*.whl
"@

# 6. Élagage.
#
# La cible est de tenir sous 100 Mo une fois l'archive produite (ENF-02) : ce
# que l'utilisateur télécharge, et non ce que l'arbre pèse une fois
# décompressé, qui est près du double. Sont retirés la
# documentation, les traductions, les en-têtes de développement et les fichiers
# produits par la machine de construction, qui n'ont aucun sens ailleurs.
Write-Step 'Élagage'
Invoke-Cygwin $StageRoot @"
set -e
rm -rf /usr/share/doc /usr/share/man /usr/share/info /usr/share/locale
rm -rf /usr/include /usr/lib/pkgconfig /usr/share/terminfo
find $PythonLibDir -type d -name test -prune -exec rm -rf {} + 2>/dev/null || true
find $PythonLibDir -type d -name tests -prune -exec rm -rf {} + 2>/dev/null || true
# -xdev est vital ici, et non une optimisation : la racine d'un arbre Cygwin
# contient /cygdrive, par lequel toutes les lettres de lecteur de la machine
# sont montées. Sans lui, « find / » sort de l'arbre, parcourt le disque entier
# et supprime les .a qu'il y trouve — ceux de l'arbre de compilation, et ceux
# de tout projet de la machine. Il traverse aussi /proc. -xdev l'arrête aux
# limites de l'arbre tout en atteignant /usr/bin et /usr/lib, qui y sont montés.
find / -xdev -name '*.a' -delete 2>/dev/null || true
# Traces de la machine de construction : comptes, journaux, fichiers
# temporaires. Les laisser exposerait le poste de construction et n'aurait
# aucun sens sur celui de l'utilisateur.
rm -rf /var/log/* /tmp/* /home/* /etc/passwd /etc/group
"@

# 7. Vérification fonctionnelle.
#
# Une archive qui ne sait pas se relire n'a aucune valeur : le moteur est
# éprouvé sur un dépôt jetable, avec la convention de chemins de l'application.
#
# L'épreuve est gardée dans une variable parce qu'elle sert deux fois : ici sur
# l'arbre élagué, pour échouer avant l'archive plutôt qu'après, et à l'étape 9
# sur l'archive extraite, qui est ce que l'utilisateur reçoit. Les deux ne sont
# pas redondantes : la compression perd des choses que l'arbre possède, et
# c'est justement ce qu'elle perd que la seconde attrape.
#
# borg extract y est ajouté : lister une archive ne prouve pas qu'on sait la
# relire, et c'est relire qui compte le jour où l'utilisateur en a besoin.
$EpreuveMoteur = @'
set -e
export BORG_UNKNOWN_UNENCRYPTED_REPO_ACCESS_IS_OK=yes
rm -rf /tmp/recette && mkdir -p /tmp/recette/source
echo "contenu de recette" > /tmp/recette/source/fichier.txt
borg init --encryption=none /tmp/recette/depot
cd /cygdrive
borg create /tmp/recette/depot::essai "$(cygpath -w /tmp/recette/source | sed 's|^\([A-Za-z]\):\\|\L\1/|; s|\\|/|g')"
borg list /tmp/recette/depot::essai
mkdir -p /tmp/recette/restaure && cd /tmp/recette/restaure
borg extract /tmp/recette/depot::essai
relu=$(find . -name fichier.txt -exec cat {} \;)
if [ "$relu" != "contenu de recette" ]; then
    echo "le contenu relu ne correspond pas : '$relu'" >&2
    exit 1
fi
cd / && rm -rf /tmp/recette
'@

Write-Step 'Vérification du moteur'
# La version est cherchée parmi les lignes de la sortie plutôt que prise pour
# la sortie entière : l'élagage vient d'effacer les dossiers personnels, et le
# premier interpréteur de connexion ouvert ensuite y réinstalle les fichiers de
# configuration par défaut en l'annonçant sur plusieurs lignes, avant que borg
# n'ait parlé.
#
# La comparaison porte alors sur une chaîne, et non sur le tableau des lignes :
# appliqué à un tableau, -notmatch n'est pas un test mais un filtre, qui rend
# les lignes ne correspondant pas. Le tableau obtenu est non vide dès qu'une
# ligne de bruit traîne, donc vrai, et la vérification échouait alors même que
# le moteur annonçait la version attendue.
$reported = @(Invoke-Cygwin $StageRoot 'borg --version') |
    Where-Object { $_ -match '^borg\s' } | Select-Object -Last 1
Write-Host "    $reported"
if (-not $reported) {
    throw "le moteur n'annonce aucune version : « borg --version » n'a rien rapporté"
}
if ($reported -notmatch [regex]::Escape($BorgVersion)) {
    throw "le moteur rapporte '$reported' au lieu de $BorgVersion"
}
Invoke-Cygwin $StageRoot $EpreuveMoteur

# 8. Archive et empreinte.
#
# Le ménage de l'élagage est refait ici, sur les seules traces que la
# vérification a pu laisser. Celle-ci ouvre des interpréteurs de connexion dans
# l'arbre livré, et chacun recrée le dossier personnel de l'utilisateur de la
# machine de construction, avec ses fichiers de configuration : sans ce
# rattrapage, l'archive publiée porterait le nom de compte de cette machine,
# précisément ce que l'élagage avait retiré.
#
# Il est fait depuis PowerShell, et non avec bash : ouvrir un interpréteur pour
# effacer ces dossiers les recréerait dans le même mouvement.
Write-Step 'Archive'
Get-ChildItem (Join-Path $StageRoot 'home') -Force -ErrorAction SilentlyContinue |
    Remove-Item -Recurse -Force
# L'archive est un tar compressé, et non un ZIP, parce qu'un arbre Cygwin ne
# tient pas dans ce dernier.
#
# Un arbre livré compte environ 570 liens symboliques. Cygwin les représente
# par un fichier ordinaire marqué de l'attribut Windows « système », que ZIP ne
# transporte pas : à l'extraction ils redeviennent des fichiers inertes de
# quelques octets, et /bin/python3, /bin/awk et toute la ferme /etc/alternatives
# cessent d'exister. ZIP ne transporte pas davantage les droits POSIX, et les
# outils de la plateforme n'écrivent pas ses entrées de dossier — les dossiers
# vides, /tmp en tête, disparaissaient aussi. tar transporte les trois.
#
# Il est lancé depuis l'arbre de COMPILATION et non depuis l'arbre livré, qui
# est sa proie : Invoke-Cygwin dépose son script à la racine de l'arbre où il
# s'exécute, et ce script se retrouverait dans l'archive.
#
# « gzip -n » n'inscrit pas l'horodatage de la compression dans l'en-tête :
# deux constructions du même arbre donnent le même fichier, donc la même
# empreinte, ce qui est tout l'intérêt de garder le dossier des paquets.
$archive = Join-Path $Output "borgui-runtime-$RuntimeVersion.tar.gz"
if (Test-Path $archive) { Remove-Item $archive }
Invoke-Cygwin $BuildRoot @"
set -e
tar --use-compress-program='gzip -n' -cf '$(ConvertTo-CygwinPath $archive)' \
    -C '$(ConvertTo-CygwinPath $StageRoot)' .
"@

$size      = (Get-Item $archive).Length
$installed = (Get-ChildItem $StageRoot -Recurse -File -Force |
    Measure-Object -Property Length -Sum).Sum

# ENF-02 : le runtime tient sous 100 Mo. La mesure qui fait foi est celle de
# l'archive, que le poste télécharge, et non celle de l'arbre une fois
# décompressé, qui est près du double — un runtime conforme aurait été refusé
# si la limite avait porté sur lui.
#
# Le refus est ici plutôt qu'à la revue : un paquet ajouté à « release » ou un
# élagage revu à la baisse doit se voir à la construction, pas six mois plus
# tard sur la connexion d'un utilisateur.
#
# L'archive refusée est effacée, et son empreinte n'est pas écrite : sans cela
# dist/ garderait un fichier d'allure publiable, que rien ne distingue d'un
# runtime accepté.
if ($size -gt 100MB) {
    Remove-Item $archive -Force
    throw @"
l'archive dépasse 100 Mo (ENF-02) : $([math]::Round($size / 1MB, 1)) Mio pour $([math]::Round($installed / 1MB, 1)) Mio installés

Elle a été effacée. Les plus gros postes de l'arbre livré :

$((Get-ChildItem $StageRoot -Directory -Force | ForEach-Object {
    $octets = (Get-ChildItem $_.FullName -Recurse -File -Force -ErrorAction SilentlyContinue |
        Measure-Object -Property Length -Sum).Sum
    [PSCustomObject]@{ Nom = $_.Name; Mio = [math]::Round($octets / 1MB, 1) }
} | Sort-Object Mio -Descending | Select-Object -First 8 |
    ForEach-Object { '    {0,-12} {1,7} Mio' -f $_.Nom, $_.Mio }) -join "`n")

Reprenez l'élagage, ou la liste [release] de packages.txt, dont chaque entrée
se paie en taille sur la connexion de l'utilisateur.
"@
}

# 9. Épreuve de ce qui est publié.
#
# L'étape 7 éprouve l'arbre ; celle-ci éprouve l'archive, extraite dans un
# dossier neuf comme le fera le poste de l'utilisateur. Les deux diffèrent par
# tout ce qu'un format d'archive peut perdre en route, et c'est de ce côté-ci
# seulement que la perte se voit.
#
# C'est l'ordre qui fait la garantie : l'empreinte n'est calculée et publiée
# qu'après. Une archive qui échoue ici est effacée, et rien de publiable ne
# reste dans dist/.
Write-Step 'Vérification de l''archive publiée'
$VerifyRoot = Join-Path $Workspace 'verify'
if (Test-Path $VerifyRoot) { Remove-Item -Recurse -Force $VerifyRoot }
New-Item -ItemType Directory -Force -Path $VerifyRoot | Out-Null
try {
    # La structure est contrôlée par le module tarfile de Python, et non par le
    # tar qui vient d'écrire l'archive. Un lecteur qui relit sa propre sortie
    # pardonne ce qu'il a mal écrit : c'est ainsi qu'une archive aux chemins
    # séparés par des antislashs était passée, relue sans broncher par la
    # bibliothèque même qui l'avait produite, et qui aurait été inexploitable
    # partout ailleurs. Ce qui compte est qu'un AUTRE lecteur s'y retrouve, car
    # celui qui extraira chez l'utilisateur est écrit en Go.
    #
    # Les liens sont comptés parce qu'ils sont la raison d'être du format : une
    # archive qui n'en contient aucun est une régression silencieuse, et le
    # moteur y survivrait assez pour que l'épreuve fonctionnelle ne dise rien.
    Invoke-Cygwin $BuildRoot @"
set -e
$Python - <<'PY'
import sys, tarfile

archive = '$(ConvertTo-CygwinPath $archive)'
liens = dossiers = 0
fautifs = []
with tarfile.open(archive) as t:
    for m in t.getmembers():
        if m.issym() or m.islnk():
            liens += 1
        elif m.isdir():
            dossiers += 1
        if '\\' in m.name or m.name.startswith('/') or '..' in m.name.split('/'):
            fautifs.append(m.name)
    noms = t.getnames()

if fautifs:
    sys.exit('chemins inexploitables, par exemple : %s' % fautifs[0])
if './tmp' not in noms:
    sys.exit('/tmp absent : bash s en plaindra a chaque commande')
if dossiers == 0:
    sys.exit('aucune entree de dossier : les dossiers vides seraient perdus')
if liens == 0:
    sys.exit('aucun lien symbolique : le format ne les transporte pas')
print('    %d liens, %d dossiers, %d entrees' % (liens, dossiers, len(noms)))
PY
"@

    # L'extraction se fait aussi depuis l'arbre de compilation : elle doit
    # écrire de vrais liens Cygwin, ce que seul un tar Cygwin sait faire.
    Invoke-Cygwin $BuildRoot @"
set -e
tar -xf '$(ConvertTo-CygwinPath $archive)' -C '$(ConvertTo-CygwinPath $VerifyRoot)'
"@
    if (-not (Test-Path (Join-Path $VerifyRoot 'tmp'))) {
        throw "/tmp est absent de l'archive extraite : bash s'en plaindra à chaque commande et le moteur n'aura pas de dossier de travail"
    }
    Assert-Interpreter $VerifyRoot
    $reportedArchive = @(Invoke-Cygwin $VerifyRoot 'borg --version') |
        Where-Object { $_ -match '^borg\s' } | Select-Object -Last 1
    Write-Host "    $reportedArchive"
    if ($reportedArchive -notmatch [regex]::Escape($BorgVersion)) {
        throw "l'archive extraite rapporte '$reportedArchive' au lieu de $BorgVersion"
    }
    Invoke-Cygwin $VerifyRoot $EpreuveMoteur
}
catch {
    Remove-Item $archive -Force -ErrorAction SilentlyContinue
    throw @"
l'archive ne fonctionne pas une fois extraite, alors que l'arbre dont elle
sort fonctionne. Elle a été effacée.

Ce que l'archive a pu perdre en route — liens symboliques, droits POSIX,
dossiers vides — est ce qu'il faut regarder d'abord. L'arbre est
conservé dans $StageRoot et l'extraction dans $VerifyRoot : comparez-les.

$($_.Exception.Message)
"@
}
Remove-Item -Recurse -Force $VerifyRoot -ErrorAction SilentlyContinue

$digest = (Get-FileHash $archive -Algorithm SHA256).Hash.ToLower()
"$digest  $(Split-Path $archive -Leaf)" | Set-Content (Join-Path $Output 'SHA256SUMS') -Encoding ascii

Write-Host ''
Write-Step 'Runtime construit'
Write-Host "    archive   : $archive"
Write-Host "    taille    : $([math]::Round($size / 1MB, 1)) Mio (limite 100 Mio)"
Write-Host "    installé  : $([math]::Round($installed / 1MB, 1)) Mio"
Write-Host "    empreinte : $digest"
Write-Host ''
Write-Host 'À reporter dans internal/borgruntime/spec.go :'
Write-Host "    Version:     `"$RuntimeVersion`","
Write-Host "    BorgVersion: `"$BorgVersion`","
Write-Host "    SHA256:      `"$digest`","
Write-Host "    Size:        $size,"
