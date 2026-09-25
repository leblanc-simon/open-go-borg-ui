#!/bin/sh
# Construit le paquet .deb d'OpenGoBorgUI (LI-03, PA-06).
#
#   MAINTAINER="Nom <adresse>" packaging/build-deb.sh <exécutable> <version> [dossier de sortie]
#
# L'exécutable est celui de « make build ». Il est lié à la glibc de la
# machine qui l'a compilé : pour couvrir Ubuntu 22.04 et Debian 12 (ENF-06),
# il se construit sur la plus ancienne des deux, Ubuntu 22.04.
#
# Les dépendances envers les bibliothèques partagées sont calculées par
# dpkg-shlibdeps, comme pour un paquet Debian ordinaire ; Borg vient de la
# distribution (EF-08), en 1.2 au moins.
set -eu

binary=$1
version=$2
out=${3:-dist}
package=opengoborgui
arch=amd64
here=$(cd "$(dirname "$0")" && pwd)
top=$(cd "$here/.." && pwd)

root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT
# mktemp crée un dossier privé ; il devient la racine de l'archive.
chmod 755 "$root"

install -Dm755 "$binary" "$root/usr/bin/borgui"
# Le nom affiché de l'application, pour qui le tape dans un terminal.
ln -s borgui "$root/usr/bin/$package"
install -Dm644 "$here/linux/io.leblanc.borgui.desktop" "$root/usr/share/applications/io.leblanc.borgui.desktop"
for size in 16 32 48 64 128 256 512; do
	install -Dm644 "$here/icons/borgui-$size.png" \
		"$root/usr/share/icons/hicolor/${size}x${size}/apps/io.leblanc.borgui.png"
done
install -d "$root/usr/share/doc/$package"
cat "$top/LICENSE" "$top/NOTICE" > "$root/usr/share/doc/$package/copyright"
chmod 644 "$root/usr/share/doc/$package/copyright"

# dpkg-shlibdeps travaille depuis une arborescence de paquet source.
work=$(mktemp -d)
trap 'rm -rf "$root" "$work"' EXIT
mkdir -p "$work/debian"
printf 'Source: %s\n\nPackage: %s\nArchitecture: %s\n' "$package" "$package" "$arch" > "$work/debian/control"
shlibs=$(cd "$work" && dpkg-shlibdeps -O "$root/usr/bin/borgui" 2>/dev/null | sed -n 's/^shlibs:Depends=//p')

# Le mainteneur est fourni par le Makefile : il ne doit pas dépendre de
# l'identité git de la machine de construction, absente en CI.
maintainer=${MAINTAINER:-}
if [ -z "$maintainer" ]; then
	echo "MAINTAINER non défini (« Nom <adresse> »)" >&2
	exit 1
fi
size=$(du -sk "$root/usr" | cut -f1)
mkdir -p "$root/DEBIAN"
cat > "$root/DEBIAN/control" <<CONTROL
Package: $package
Version: $version
Architecture: $arch
Maintainer: $maintainer
Installed-Size: $size
Depends: $shlibs, borgbackup (>= 1.2)
Suggests: gnome-keyring
Section: utils
Priority: optional
Homepage: https://github.com/leblanc-simon/open-go-borg-ui
Description: sauvegardes BorgBackup vers une Hetzner Storage Box
 OpenGoBorgUI configure et supervise les sauvegardes d'un poste vers une
 Storage Box Hetzner : assistant de premier lancement, sauvegardes
 planifiées par un timer systemd de l'utilisateur, restauration guidée,
 vérifications mensuelles. Il pilote le borg de la distribution et
 fonctionne sans droits d'administrateur.
CONTROL

mkdir -p "$out"
dpkg-deb --root-owner-group --build "$root" "$out/${package}_${version}_${arch}.deb" >/dev/null
echo "$out/${package}_${version}_${arch}.deb"
