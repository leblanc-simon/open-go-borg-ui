//go:build !nogui

package main

import (
	fyneapp "fyne.io/fyne/v2/app"

	"leblanc.io/open-go-borg-ui/internal/gui"
)

// appID identifie l'application auprès du système : préférences, stockage et
// notifications de Fyne y sont rattachés.
const appID = "io.leblanc.borgui"

// runGUI ouvre l'interface graphique, mode par défaut de l'exécutable
// (AR-03). Fyne exige CGO : l'étiquette de compilation nogui l'écarte pour
// produire une ligne de commande seule.
func (a *app) runGUI() int {
	window := gui.NewWindow(fyneapp.NewWithID(appID), gui.Deps{Station: a.Station, T: a.T})
	window.ShowAndRun()
	return exitSuccess
}
