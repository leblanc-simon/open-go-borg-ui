#!/usr/bin/env python3
"""Génère les icônes des paquets depuis specs/logo.png.

Les fichiers produits sont versionnés : construire un paquet n'exige ni
Python ni Pillow. Ce script n'est à relancer que si le logo change.

    python3 packaging/make-icons.py
"""
from pathlib import Path

from PIL import Image

ROOT = Path(__file__).resolve().parent.parent
SOURCE = ROOT / "specs" / "logo.png"
OUT = ROOT / "packaging" / "icons"

# Tailles du thème d'icônes freedesktop, pour le paquet .deb.
PNG_SIZES = [16, 32, 48, 64, 128, 256, 512]
# Tailles réunies dans l'icône Windows, de la liste des fichiers à la
# vignette de l'Explorateur.
ICO_SIZES = [16, 24, 32, 48, 64, 128, 256]


def square(image: Image.Image) -> Image.Image:
    """Recadre au carré sur le dessin, avec une petite marge."""
    left, top, right, bottom = image.getchannel("A").point(lambda a: 255 if a > 16 else 0).getbbox()
    side = max(right - left, bottom - top)
    side += 2 * int(side * 0.03)
    cx, cy = (left + right) // 2, (top + bottom) // 2
    return image.crop((cx - side // 2, cy - side // 2, cx - side // 2 + side, cy - side // 2 + side))


def main() -> None:
    logo = square(Image.open(SOURCE).convert("RGBA"))
    OUT.mkdir(parents=True, exist_ok=True)
    for size in PNG_SIZES:
        logo.resize((size, size), Image.LANCZOS).save(OUT / f"borgui-{size}.png", optimize=True)
    logo.resize((256, 256), Image.LANCZOS).save(OUT / "borgui.ico", sizes=[(s, s) for s in ICO_SIZES])


if __name__ == "__main__":
    main()
