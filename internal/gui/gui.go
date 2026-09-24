// Package gui est l'interface graphique de l'application (Fyne).
//
// Elle n'appelle jamais Borg directement (AR-01) : elle passe par le poste
// (station) et le cœur applicatif (core), exactement comme la ligne de
// commande. Elle ne dépend que des widgets de Fyne, en Go pur : la fenêtre
// réelle est créée par l'exécutable, et ce paquet se teste sans affichage.
//
// Aucune opération ne bloque l'interface (EI-03) : tout travail — Borg,
// réseau, disque — part dans une goroutine et revient par onUI.
package gui

import (
	"context"
	"errors"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/borgruntime"
	"leblanc.io/open-go-borg-ui/internal/core"
	"leblanc.io/open-go-borg-ui/internal/format"
	"leblanc.io/open-go-borg-ui/internal/lock"
	"leblanc.io/open-go-borg-ui/internal/station"
)

// Deps est ce dont l'interface a besoin.
type Deps struct {
	Station *station.Station
	// T traduit une clé du catalogue.
	T format.Translate
}

// ui rassemble l'état partagé entre les écrans.
type ui struct {
	app fyne.App
	win fyne.Window
	st  *station.Station
	t   format.Translate

	home        *homeScreen
	backup      *backupScreen
	destination *destinationScreen
	shell       *shell
	wizard      *wizardView
}

// NewWindow construit la fenêtre principale : quatre écrans au plus — État,
// Sauvegarde, Destination, et plus tard Réglages.
func NewWindow(app fyne.App, deps Deps) fyne.Window {
	return newWindow(app, deps, &ui{})
}

// newWindow construit la fenêtre sur u, que les tests gardent pour atteindre
// les écrans.
func newWindow(app fyne.App, deps Deps, u *ui) fyne.Window {
	u.app, u.st, u.t = app, deps.Station, deps.T
	u.win = app.NewWindow(u.t("window.title"))
	u.win.Resize(fyne.NewSize(1040, 700))
	u.applyTheme()

	// Un poste sans configuration ouvre l'assistant de premier lancement,
	// repris là où il s'était arrêté s'il avait été interrompu (EF-10,
	// EF-11).
	if _, err := u.st.Profile(); err != nil {
		u.showWizard()
		return u.win
	}
	u.showMain()
	return u.win
}

// showMain affiche les écrans de l'application : quatre au plus — État,
// Sauvegarde, Destination, et plus tard Réglages.
func (u *ui) showMain() {
	u.home = newHomeScreen(u)
	u.backup = newBackupScreen(u)
	u.destination = newDestinationScreen(u)

	u.shell = newShell(u, []entry{
		{theme.HomeIcon(), u.t("tab.home"), u.home.content},
		{theme.UploadIcon(), u.t("tab.backup"), u.backup.content},
		{theme.StorageIcon(), u.t("tab.destination"), u.destination.content},
	})
	u.win.SetContent(u.shell.content)
}

// onUI exécute f dans le fil de l'interface. Les tests le remplacent : le
// pilote de test de Fyne exécute f sur place, en concurrence avec le test.
var onUI = fyne.Do

// async exécute work hors du fil de l'interface, puis done dans ce fil.
func async(work func(), done func()) {
	go func() {
		work()
		onUI(done)
	}()
}

// errorKey traduit une erreur en clé d'explication : les échecs de Borg par
// leur diagnostic, les situations prévues par la leur. Le texte brut reste
// pour le dépliant « Détails » (EI-04).
func errorKey(err error) string {
	var failed *borg.CommandError
	switch {
	case errors.As(err, &failed):
		return failed.Diagnosis.TranslationKey()
	case errors.Is(err, lock.ErrBusy):
		return core.ErrorKeyAlreadyRunning
	case errors.Is(err, borg.ErrEngineMissing), errors.Is(err, borgruntime.ErrNotInstalled):
		return "error.engine_missing"
	default:
		return borg.FailureUnknown.TranslationKey()
	}
}

// explanation construit une explication, suivie du texte brut replié sous
// « Détails ».
func (u *ui) explanation(key string, data map[string]any, detail string) fyne.CanvasObject {
	message := widget.NewLabel(u.t(key, data))
	message.Wrapping = fyne.TextWrapWord
	if detail == "" {
		return message
	}
	return container.NewVBox(message, u.details(detail))
}

// details replie un texte brut — journal de Borg, erreur technique — sous un
// dépliant « Détails » : accessible, sans encombrer l'explication (EI-04).
func (u *ui) details(detail string) fyne.CanvasObject {
	raw := widget.NewLabel(detail)
	raw.Wrapping = fyne.TextWrapWord
	raw.TextStyle = fyne.TextStyle{Monospace: true}
	return widget.NewAccordion(widget.NewAccordionItem(u.t("gui.details"), container.NewVScroll(raw)))
}

// backgroundContext est le contexte des lectures locales, qui ne s'annulent
// pas.
func backgroundContext() context.Context { return context.Background() }
