package gui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"leblanc.io/open-go-borg-ui/internal/config"
	"leblanc.io/open-go-borg-ui/internal/schedule"
	"leblanc.io/open-go-borg-ui/internal/station"
	"leblanc.io/open-go-borg-ui/internal/wizard"
)

// wizardView est l'assistant de premier lancement (EF-10 à EF-13) : une
// question par écran, sans connaissance de Borg, de SSH ni de chiffrement
// (EF-12).
type wizardView struct {
	u     *ui
	state *wizard.State
	path  string

	title    *widget.Label
	position *widget.Label
	body     *fyne.Container
	back     *widget.Button
	next     *widget.Button
	// saveErr garde le dernier échec d'enregistrement, pour les tests.
	saveErr error
}

// showWizard ouvre l'assistant, repris s'il avait été interrompu.
func (u *ui) showWizard() {
	path := wizard.Path(u.st.StateDir)
	state, err := wizard.Load(path)
	if err != nil || state == nil {
		state = wizard.New(station.Hostname())
	}
	w := &wizardView{u: u, state: state, path: path}
	u.wizard = w

	w.title = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	w.position = widget.NewLabel("")
	w.body = container.NewVBox()
	w.back = widget.NewButtonWithIcon(u.t("wizard.back"), theme.NavigateBackIcon(), w.goBack)
	w.next = widget.NewButtonWithIcon(u.t("wizard.next"), theme.NavigateNextIcon(), w.goNext)
	w.next.Importance = widget.HighImportance

	header := container.NewBorder(nil, nil, nil, w.position, w.title)
	footer := container.NewBorder(widget.NewSeparator(), nil, w.back, w.next)
	u.win.SetContent(container.NewPadded(container.NewBorder(
		container.NewVBox(header, widget.NewSeparator()), footer, nil, nil,
		container.NewVScroll(w.body))))
	w.render()
}

// render affiche l'étape courante.
func (w *wizardView) render() {
	t := w.u.t
	current, total := w.state.Position()
	w.position.SetText(t("wizard.position", map[string]any{"Current": current, "Total": total}))
	w.title.SetText(t("wizard." + string(w.state.Step) + ".title"))
	w.body.RemoveAll()
	w.setReady(false)
	w.back.Show()
	w.next.SetText(t("wizard.next"))

	var content fyne.CanvasObject
	switch w.state.Step {
	case wizard.StepWelcome:
		w.back.Hide()
		content = w.welcomeStep()
	case wizard.StepEngine:
		content = w.engineStep()
	case wizard.StepDestination:
		content = w.destinationStep()
	case wizard.StepEncryption:
		content = w.encryptionStep()
	case wizard.StepFolders:
		content = w.foldersStep()
	case wizard.StepSchedule:
		content = w.scheduleStep()
	case wizard.StepRecoveryKey:
		content = w.recoveryKeyStep()
	case wizard.StepFinish:
		w.next.SetText(t("wizard.finish.start"))
		content = w.finishStep()
	}
	w.body.Add(content)
}

// setReady autorise ou non le passage à l'étape suivante.
func (w *wizardView) setReady(ready bool) {
	if ready {
		w.next.Enable()
	} else {
		w.next.Disable()
	}
}

// save enregistre l'avancement (EF-11).
func (w *wizardView) save() {
	w.saveErr = w.state.Save(w.path)
	if w.saveErr != nil {
		dialog.ShowError(w.saveErr, w.u.win)
	}
}

// goNext passe à l'étape suivante, ou termine l'assistant.
func (w *wizardView) goNext() {
	if w.state.Step == wizard.StepFinish {
		w.complete()
		return
	}
	w.state.Next()
	w.save()
	w.render()
}

// goBack revient à l'étape précédente.
func (w *wizardView) goBack() {
	w.state.Previous()
	w.save()
	w.render()
}

// paragraph est un texte explicatif, renvoyé à la ligne.
func (w *wizardView) paragraph(key string, data ...map[string]any) *widget.Label {
	var values map[string]any
	if len(data) > 0 {
		values = data[0]
	}
	label := widget.NewLabel(w.u.t(key, values))
	label.Wrapping = fyne.TextWrapWord
	return label
}

// welcomeStep présente l'application, et propose d'importer la
// configuration d'un autre poste (EF-13).
func (w *wizardView) welcomeStep() fyne.CanvasObject {
	w.setReady(true)
	imported := w.paragraph("wizard.welcome.imported")
	if !w.state.Imported {
		imported.Hide()
	}
	importButton := widget.NewButtonWithIcon(w.u.t("wizard.welcome.import"), theme.FileIcon(), func() {
		dialog.ShowFileOpen(func(file fyne.URIReadCloser, err error) {
			if err != nil || file == nil {
				return
			}
			defer file.Close()
			profile, err := config.Import(file, "")
			if err != nil {
				dialog.ShowError(fmt.Errorf("%s", w.u.t("config.import_invalid", map[string]any{"Message": err.Error()})), w.u.win)
				return
			}
			w.state.Profile = profile
			w.state.Imported = true
			w.state.DestinationChecked = false
			w.state.RepositoryReady = false
			w.save()
			imported.Show()
		}, w.u.win)
	})
	return container.NewVBox(
		container.NewCenter(logo(128)),
		w.paragraph("wizard.welcome.text"),
		widget.NewSeparator(),
		w.paragraph("wizard.welcome.import_text"),
		container.NewHBox(importButton),
		imported,
	)
}

// finishStep résume ce qui va se passer.
func (w *wizardView) finishStep() fyne.CanvasObject {
	w.setReady(true)
	profile := w.state.Profile
	plan, err := schedule.FromConfig(profile.Schedule)
	scheduleText := w.u.t("schedule.manual")
	if err == nil {
		scheduleText = w.u.describePlan(plan)
	}
	return container.NewVBox(
		w.paragraph("wizard.finish.text"),
		w.paragraph("wizard.finish.summary", map[string]any{
			"Folders":  len(profile.Sources),
			"Schedule": scheduleText,
		}),
	)
}

// complete écrit la configuration, installe la planification, puis ouvre
// l'application et lance la première sauvegarde.
func (w *wizardView) complete() {
	profile := w.state.Profile
	if err := config.Save(w.u.st.ConfigPath, &config.Config{Profiles: []config.Profile{profile}}); err != nil {
		dialog.ShowError(err, w.u.win)
		return
	}
	w.u.st.ProfileName = profile.Name

	// La planification est installée si elle a été choisie ; un échec ne
	// retient pas la première sauvegarde, il est signalé sur l'écran État.
	var scheduleErr error
	if plan, err := schedule.FromConfig(profile.Schedule); err == nil && plan.Frequency != schedule.Manual {
		scheduleErr = installSchedule(w.u.st, &profile, plan, w.u.t)
	}
	if err := wizard.Clear(w.path); err != nil {
		dialog.ShowError(err, w.u.win)
	}

	w.u.wizard = nil
	w.u.showMain()
	if scheduleErr != nil {
		dialog.ShowError(fmt.Errorf("%s", w.u.t("wizard.finish.schedule_failed", map[string]any{"Message": scheduleErr.Error()})), w.u.win)
	}
	w.u.shell.show(screenBackup)
	w.u.backup.run()
}
