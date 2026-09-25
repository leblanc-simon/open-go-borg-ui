#!/bin/sh
# Construit le paquet MSI d'OpenGoBorgUI (LI-02) avec wixl et msibuild
# (msitools), sous Linux : l'exécutable, lui, se compile sous Windows (Fyne
# exige CGO).
#
#   packaging/build-msi.sh <borgui.exe> <version a.b.c> [dossier de sortie]
#
# La signature de l'exécutable et du paquet (SEC-02) se fait en amont et en
# aval de ce script, avec le certificat de la publication (PA-02).
set -eu

exe=$1
version=$2
out=${3:-dist}
here=$(cd "$(dirname "$0")" && pwd)

# Un MSI n'accepte que des versions numériques a.b.c, chacune bornée.
case $version in
	*[!0-9.]* | .* | *. | *..*)
		echo "version MSI invalide : $version (attendu a.b.c)" >&2
		exit 1 ;;
esac

mkdir -p "$out"
msi="$out/OpenGoBorgUI-$version.msi"
wixl --arch x64 \
	-D Version="$version" \
	-D Exe="$exe" \
	-D Icon="$here/icons/borgui.ico" \
	-o "$msi" "$here/windows/borgui.wxs"

# wixl convertit les textes en Windows-1252 mais laisse la base « neutre » :
# Windows les lirait alors avec la page de codes du système, et les accents
# seraient faux hors d'un Windows occidental. La table spéciale
# _ForceCodepage inscrit 1252 dans la base.
idt=$(mktemp -d)
trap 'rm -rf "$idt"' EXIT
printf '\r\n\r\n1252\t_ForceCodepage\r\n' > "$idt/_ForceCodepage.idt"
msibuild "$msi" -i "$idt/_ForceCodepage.idt"
if ! msiinfo export "$msi" _ForceCodepage | grep -q '^1252'; then
	echo "$msi : page de codes 1252 non inscrite" >&2
	exit 1
fi
echo "$msi"
