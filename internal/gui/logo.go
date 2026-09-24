package gui

import (
	_ "embed"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

// logoPNG est le logo de l'application, embarqué dans l'exécutable : il
// sert d'icône aux fenêtres et figure dans la barre latérale. La source en
// haute définition est specs/logo.png.
//
//go:embed assets/logo.png
var logoPNG []byte

// logoResource est le logo sous la forme d'une ressource Fyne.
var logoResource = fyne.NewStaticResource("logo.png", logoPNG)

// logo retourne le logo à la taille donnée, proportions conservées.
func logo(size float32) *canvas.Image {
	image := canvas.NewImageFromResource(logoResource)
	image.FillMode = canvas.ImageFillContain
	image.SetMinSize(fyne.NewSquareSize(size))
	return image
}
