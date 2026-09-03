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
        throw "paquets inconnus du miroir Cygwin : $names — corrigez packages.txt"
    }
}

# Invoke-Cygwin exécute une commande dans un arbre Cygwin donné.
function Invoke-Cygwin([string] $Root, [string] $Script) {
    $bash = Join-Path $Root 'bin\bash.exe'
    if (-not (Test-Path $bash)) {
        throw "arbre Cygwin incomplet : $bash est absent"
    }
    & $bash '-lc' $Script
    if ($LASTEXITCODE -ne 0) { throw "commande Cygwin en échec ($LASTEXITCODE) : $Script" }
}

# Assert-Interpreter éprouve l'interpréteur d'un arbre.
#
# Le fichier peut exister sans être exécutable — bibliothèque manquante, arbre
# installé à moitié. C'est donc l'exécution qui fait foi, et non la présence,
# faute de quoi le défaut ne se manifeste que par un « command not found » au
# milieu d'un script shell, qui ne désigne pas sa cause.
function Assert-Interpreter([string] $Root) {
    $bash = Join-Path $Root 'bin\bash.exe'
    if (-not (Test-Path $bash)) {
        throw "arbre Cygwin incomplet dans $Root : bin\bash.exe est absent"
    }
    & $bash '-lc' "$Python --version" | ForEach-Object { Write-Host "    $_" }
    if ($LASTEXITCODE -ne 0) {
        throw "$Python ne s'exécute pas dans $Root : le paquet $PythonPackage manque ou l'arbre est incomplet"
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

Write-Step "Runtime $RuntimeVersion (Python $PythonSeries)"
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
$Python -m pip download --no-binary :all: --dest /tmp/sources --requirement /tmp/plain.txt
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
    Set-Content -Path $RequirementsFile -Value ($header + "`n`n" + ($entries -join "`n")) -Encoding utf8
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
$Python -m pip wheel --no-binary :all: --require-hashes \
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

Invoke-Cygwin $BuildRoot @"
set -e
cp /tmp/wheels/*.whl '$(ConvertTo-CygwinPath $StageRoot)/tmp/'
"@
Invoke-Cygwin $StageRoot @"
set -e
$Python -m pip install --no-cache-dir --no-index --no-deps /tmp/*.whl
rm -f /tmp/*.whl
"@

# 6. Élagage.
#
# La cible est de tenir sous 100 Mo installés (ENF-02). Sont retirés la
# documentation, les traductions, les en-têtes de développement et les fichiers
# produits par la machine de construction, qui n'ont aucun sens ailleurs.
Write-Step 'Élagage'
Invoke-Cygwin $StageRoot @"
set -e
rm -rf /usr/share/doc /usr/share/man /usr/share/info /usr/share/locale
rm -rf /usr/include /usr/lib/pkgconfig /usr/share/terminfo
find $PythonLibDir -type d -name test -prune -exec rm -rf {} + 2>/dev/null || true
find $PythonLibDir -type d -name tests -prune -exec rm -rf {} + 2>/dev/null || true
find / -name '*.a' -delete 2>/dev/null || true
# Traces de la machine de construction : comptes, journaux, fichiers
# temporaires. Les laisser exposerait le poste de construction et n'aurait
# aucun sens sur celui de l'utilisateur.
rm -rf /var/log/* /tmp/* /home/* /etc/passwd /etc/group
"@

# 7. Vérification fonctionnelle.
#
# Une archive qui ne sait pas se relire n'a aucune valeur : le moteur est
# éprouvé ici même, sur un dépôt jetable, avec la convention de chemins de
# l'application.
Write-Step 'Vérification du moteur'
$reported = Invoke-Cygwin $StageRoot 'borg --version'
Write-Host "    $reported"
if ($reported -notmatch [regex]::Escape($BorgVersion)) {
    throw "le moteur rapporte « $reported » au lieu de $BorgVersion"
}
Invoke-Cygwin $StageRoot @'
set -e
export BORG_UNKNOWN_UNENCRYPTED_REPO_ACCESS_IS_OK=yes
rm -rf /tmp/recette && mkdir -p /tmp/recette/source
echo "contenu de recette" > /tmp/recette/source/fichier.txt
borg init --encryption=none /tmp/recette/depot
cd /cygdrive
borg create /tmp/recette/depot::essai "$(cygpath -w /tmp/recette/source | sed 's|^\([A-Za-z]\):\\|\L\1/|; s|\\|/|g')"
borg list /tmp/recette/depot::essai
rm -rf /tmp/recette
'@

# 8. Archive et empreinte.
Write-Step 'Archive'
$archive = Join-Path $Output "borgui-runtime-$RuntimeVersion.zip"
if (Test-Path $archive) { Remove-Item $archive }
Compress-Archive -Path (Join-Path $StageRoot '*') -DestinationPath $archive -CompressionLevel Optimal

$digest = (Get-FileHash $archive -Algorithm SHA256).Hash.ToLower()
$size   = (Get-Item $archive).Length
"$digest  $(Split-Path $archive -Leaf)" | Set-Content (Join-Path $Output 'SHA256SUMS') -Encoding ascii

Write-Host ''
Write-Step 'Runtime construit'
Write-Host "    archive   : $archive"
Write-Host "    taille    : $([math]::Round($size / 1MB, 1)) Mio"
Write-Host "    empreinte : $digest"
Write-Host ''
Write-Host 'À reporter dans internal/borgruntime/spec.go :'
Write-Host "    Version:     `"$RuntimeVersion`","
Write-Host "    BorgVersion: `"$BorgVersion`","
Write-Host "    SHA256:      `"$digest`","
Write-Host "    Size:        $size,"
