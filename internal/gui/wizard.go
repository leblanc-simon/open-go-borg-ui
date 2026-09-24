package gui

import (
	"errors"
	"fmt"
	"os"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/config"
	"leblanc.io/open-go-borg-ui/internal/schedule"
	"leblanc.io/open-go-borg-ui/internal/secret"
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
	header   *fyne.Container
	body     *fyne.Container
	back     *widget.Button
	next     *widget.Button
	// saveErr garde le dernier échec d'enregistrement, pour les tests.
	saveErr error
}

// showWizard ouvre l'assistant de premier lancement, repris s'il avait été
// interrompu (EF-11).
func (u *ui) showWizard() {
	path := wizard.Path(u.st.StateDir)
	state, err := wizard.Load(path)
	if err != nil || state == nil || state.Reconfigure != wizard.FirstRun {
		state = wizard.New(station.Hostname())
	}
	u.openWizard(state, path)
}

// reconfigure reprend l'assistant sur un poste configuré : changement de
// destination ou import d'une configuration. Rien de ce qui est en service
// — configuration, mot de passe, clé de connexion, planification — n'est
// touché avant la validation finale.
func (u *ui) reconfigure(state *wizard.State) error {
	if state.NewKey {
		// La nouvelle clé est écrite à côté de l'actuelle, qui sert encore
		// aux sauvegardes planifiées tant que la reconfiguration n'est pas
		// validée.
		current, err := config.SSHKeyPath()
		if err != nil {
			return err
		}
		state.Profile.Destination.SSHKey = current + "-" + time.Now().Format("20060102-150405")
	}
	if u.restore != nil {
		u.restore.win.Close()
	}
	path := wizard.ReconfigurePath(u.st.StateDir)
	u.openWizard(state, path)
	u.wizard.save()
	return nil
}

// openWizard affiche l'assistant dans la fenêtre principale.
func (u *ui) openWizard(state *wizard.State, path string) {
	w := &wizardView{u: u, state: state, path: path}
	u.wizard = w

	w.title = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	w.position = widget.NewLabel("")
	w.body = container.NewVBox()
	w.back = widget.NewButtonWithIcon(u.t("wizard.back"), theme.NavigateBackIcon(), w.goBack)
	w.next = widget.NewButtonWithIcon(u.t("wizard.next"), theme.NavigateNextIcon(), w.goNext)
	w.next.Importance = widget.HighImportance

	left := container.NewHBox(w.back)
	if state.Reconfigure != wizard.FirstRun {
		// Une reconfiguration s'abandonne : le poste reste tel qu'il était.
		cancel := widget.NewButtonWithIcon(u.t("wizard.cancel"), theme.CancelIcon(), w.abandon)
		cancel.Importance = widget.LowImportance
		left = container.NewHBox(cancel, w.back)
	}
	w.header = container.NewBorder(nil, nil, nil, w.position, w.title)
	footer := container.NewBorder(widget.NewSeparator(), nil, left, w.next)
	u.win.SetContent(container.NewPadded(container.NewBorder(
		container.NewVBox(w.header, widget.NewSeparator()), footer, nil, nil,
		container.NewVScroll(w.body))))
	w.render()
}

// environment construit l'environnement de Borg de la destination en
// préparation, avec son propre mot de passe.
func (w *wizardView) environment() (borg.Environment, error) {
	return w.u.st.EnvironmentWithSecret(&w.state.Profile, w.state.SecretName())
}

// abandon renonce à une reconfiguration, après confirmation.
func (w *wizardView) abandon() {
	t := w.u.t
	dialog.ShowConfirm(t("wizard.cancel_title"), t("wizard.cancel_question"), func(confirmed bool) {
		if !confirmed {
			return
		}
		w.discard()
		w.u.wizard = nil
		w.u.showMain()
	}, w.u.win)
}

// discard efface ce que la reconfiguration avait préparé : son mot de passe
// provisoire, sa nouvelle clé, son avancement.
func (w *wizardView) discard() {
	if err := w.u.st.Secrets().Delete(w.state.SecretName()); err != nil && !errors.Is(err, secret.ErrNotFound) {
		dialog.ShowError(err, w.u.win)
	}
	if w.state.NewKey {
		removeKey(w.state.Profile.Destination.SSHKey)
	}
	if err := wizard.Clear(w.path); err != nil {
		dialog.ShowError(err, w.u.win)
	}
}

// removeKey supprime une clé de connexion et sa partie publique. Une clé
// déjà absente n'est pas une erreur.
func removeKey(path string) {
	if path == "" {
		return
	}
	os.Remove(path)
	os.Remove(path + ".pub")
}

// render affiche l'étape courante.
func (w *wizardView) render() {
	t := w.u.t
	current, total := w.state.Position()
	w.position.SetText(t("wizard.position", map[string]any{"Current": current, "Total": total}))
	w.title.SetText(t("wizard." + string(w.state.Step) + ".title"))
	// L'indication d'étape change de largeur : l'en-tête se remet en page.
	w.header.Refresh()
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
	if w.state.Reconfigure != wizard.FirstRun {
		if err := w.commitReconfiguration(); err != nil {
			dialog.ShowError(err, w.u.win)
			return
		}
	} else if err := config.Save(w.u.st.ConfigPath, &config.Config{Profiles: []config.Profile{profile}}); err != nil {
		dialog.ShowError(err, w.u.win)
		return
	}
	w.u.st.ProfileName = profile.Name

	// La planification est installée si elle a été choisie, retirée sinon ;
	// un échec ne retient pas la première sauvegarde, il est signalé.
	scheduleErr := applySchedule(w.u.st, &profile, w.u.t)
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

// commitReconfiguration met en service la destination préparée : son mot de
// passe prend la place de l'ancien, puis la configuration est remplacée.
// Si l'enregistrement échoue, l'ancien mot de passe est rétabli : le poste
// reste tel qu'il était.
func (w *wizardView) commitReconfiguration() error {
	st := w.u.st
	profile := w.state.Profile
	secrets := st.Secrets()
	previous, err := st.Profile()
	if err != nil {
		return err
	}
	previousKey, err := st.SSHKeyPath(previous)
	if err != nil {
		return err
	}

	oldSecret, oldErr := secrets.Get(profile.Name)
	restore := func() {
		if oldErr == nil {
			secrets.Set(profile.Name, oldSecret)
		} else {
			secrets.Delete(profile.Name)
		}
	}
	if profile.Encryption.Encrypted() {
		pending, err := secrets.Get(w.state.SecretName())
		if err != nil {
			return err
		}
		if _, err := secrets.Set(profile.Name, pending); err != nil {
			restore()
			return err
		}
	}

	cfg, err := config.Load(st.ConfigPath)
	if err == nil {
		replaced := false
		for i := range cfg.Profiles {
			if cfg.Profiles[i].Name == profile.Name {
				cfg.Profiles[i], replaced = profile, true
			}
		}
		if !replaced {
			cfg.Profiles = []config.Profile{profile}
		}
		err = config.Save(st.ConfigPath, cfg)
	}
	if err != nil {
		restore()
		return err
	}

	// La nouvelle configuration est en service : ce qui ne sert plus
	// s'efface. Une destination non chiffrée n'a plus de mot de passe, et
	// l'ancienne clé de connexion, remplacée, ne doit pas rester sur le poste.
	secrets.Delete(w.state.SecretName())
	if !profile.Encryption.Encrypted() {
		secrets.Delete(profile.Name)
	}
	if w.state.NewKey && previousKey != profile.Destination.SSHKey {
		removeKey(previousKey)
	}
	return nil
}
