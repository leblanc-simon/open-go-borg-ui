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
    [string] $Workspace = ''
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

if (-not (Test-Path (Join-Path $ScriptRoot 'packages.txt'))) {
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

function Read-PackageList([string] $Section) {
    $path = Join-Path $ScriptRoot 'packages.txt'
    $inSection = $false
    $packages = @()
    foreach ($line in Get-Content $path) {
        $trimmed = $line.Trim()
        if ($trimmed -match '^\[(.+)\]$') { $inSection = ($Matches[1] -eq $Section); continue }
        if (-not $inSection -or $trimmed -eq '' -or $trimmed.StartsWith('#')) { continue }
        $packages += $trimmed
    }
    if ($packages.Count -eq 0) { throw "aucun paquet dans la section [$Section]" }
    return ($packages -join ',')
}

# Invoke-Cygwin exécute une commande dans un arbre Cygwin donné.
function Invoke-Cygwin([string] $Root, [string] $Script) {
    $bash = Join-Path $Root 'bin\bash.exe'
    & $bash '-lc' $Script
    if ($LASTEXITCODE -ne 0) { throw "commande Cygwin en échec ($LASTEXITCODE) : $Script" }
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
& $SetupExe --quiet-mode --no-admin --no-shortcuts --no-desktop --download `
    --site $Mirror --local-package-dir $PackageDir --root $BuildRoot `
    --packages "$buildPackages,$releasePackages" | Out-Null

# 3. Arbre de compilation.
Write-Step 'Installation de l''arbre de compilation'
& $SetupExe --quiet-mode --no-admin --no-shortcuts --no-desktop --local-install `
    --local-package-dir $PackageDir --root $BuildRoot --packages $buildPackages | Out-Null

# 4. Compilation de Borg depuis ses sources.
#
# Cygwin n'a pas de roue précompilée : Borg est compilé contre les
# bibliothèques de l'arbre. Les empreintes sont exigées, faute de quoi une
# dépendance republiée changerait silencieusement le runtime.
Write-Step "Compilation de Borg $BorgVersion"
$requirements = (Join-Path $ScriptRoot 'requirements.txt') -replace '\\', '/' -replace '^([A-Za-z]):', '/cygdrive/$1'
Invoke-Cygwin $BuildRoot @"
set -e
$Python -m pip install --no-cache-dir --upgrade pip wheel
$Python -m pip wheel --no-binary :all: --require-hashes \
    --requirement '$requirements' --wheel-dir /tmp/wheels
"@

# 5. Arbre livré : uniquement l'exécution.
Write-Step 'Composition de l''arbre livré'
if (Test-Path $StageRoot) { Remove-Item -Recurse -Force $StageRoot }
& $SetupExe --quiet-mode --no-admin --no-shortcuts --no-desktop --local-install `
    --local-package-dir $PackageDir --root $StageRoot --packages $releasePackages | Out-Null

Invoke-Cygwin $BuildRoot @"
set -e
cp /tmp/wheels/*.whl '$($StageRoot -replace '\\', '/' -replace '^([A-Za-z]):', '/cygdrive/$1')/tmp/'
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
borg create /tmp/recette/depot::essai "$(cygpath -u "$(cygpath -w /tmp/recette/source)" | sed 's|^/cygdrive/||')"
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
