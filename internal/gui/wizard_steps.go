package gui

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/config"
	"leblanc.io/open-go-borg-ui/internal/core"
	"leblanc.io/open-go-borg-ui/internal/format"
	"leblanc.io/open-go-borg-ui/internal/probe"
	"leblanc.io/open-go-borg-ui/internal/schedule"
	"leblanc.io/open-go-borg-ui/internal/station"
)

// newScheduler construit l'ordonnanceur du système. Les tests le remplacent
// pour ne jamais toucher à celui du poste.
var newScheduler = schedule.New

// hetznerUser reconnaît un sous-compte Hetzner : u123456 ou u123456-sub2.
var hetznerUser = regexp.MustCompile(`^u[0-9]+(-sub[0-9]+)?$`)

// engineStep vérifie que le moteur de sauvegarde est présent, et l'installe
// sous Windows (EF-02 à EF-08).
func (w *wizardView) engineStep() fyne.CanvasObject {
	t := w.u.t
	status := w.paragraph("wizard.engine.checking")
	actions := container.NewVBox()

	var check func()
	check = func() {
		status.SetText(t("wizard.engine.checking"))
		actions.RemoveAll()
		var version string
		var err error
		async(func() {
			var runner borg.Runner
			if runner, err = w.u.st.Runner(); err == nil {
				version, err = runner.Version(context.Background())
			}
		}, func() {
			if err == nil {
				status.SetText(t("wizard.engine.ready", map[string]any{"Version": version}))
				w.setReady(true)
				return
			}
			w.setReady(false)
			if runtime.GOOS != "windows" {
				// Sous Linux, le moteur vient de la distribution (EF-08).
				status.SetText(t("wizard.engine.missing_linux", map[string]any{"Command": station.InstallHint()}))
				actions.Add(widget.NewButtonWithIcon(t("wizard.engine.recheck"), theme.ViewRefreshIcon(), check))
				return
			}
			status.SetText(t("wizard.engine.missing_windows"))
			actions.Add(w.runtimeInstaller(check))
		})
	}
	check()
	return container.NewVBox(w.paragraph("wizard.engine.text"), status, actions)
}

// runtimeInstaller propose l'installation du moteur sous Windows : par
// téléchargement, ou depuis un fichier obtenu ailleurs (EF-06).
func (w *wizardView) runtimeInstaller(done func()) fyne.CanvasObject {
	t := w.u.t
	manager := w.u.st.RuntimeManager()
	progress := widget.NewProgressBar()
	progress.Hide()
	result := container.NewVBox()

	install := func(run func(context.Context) (string, error)) {
		progress.Show()
		result.RemoveAll()
		var err error
		async(func() { _, err = run(context.Background()) }, func() {
			progress.Hide()
			if err != nil {
				result.Add(w.u.explanation("wizard.engine.install_failed", nil, err.Error()))
				return
			}
			done()
		})
	}

	download := widget.NewButtonWithIcon(t("wizard.engine.download"), theme.DownloadIcon(), func() {
		install(func(ctx context.Context) (string, error) {
			return manager.Install(ctx, func(downloaded, total int64) {
				if total > 0 {
					value := float64(downloaded) / float64(total)
					onUI(func() { progress.SetValue(value) })
				}
			})
		})
	})
	if !manager.Spec().Published() {
		download.Disable()
	}
	fromFile := widget.NewButtonWithIcon(t("wizard.engine.from_file"), theme.FileIcon(), func() {
		dialog.ShowFileOpen(func(file fyne.URIReadCloser, err error) {
			if err != nil || file == nil {
				return
			}
			path := file.URI().Path()
			file.Close()
			install(func(ctx context.Context) (string, error) { return manager.InstallFromArchive(ctx, path) })
		}, w.u.win)
	})
	return container.NewVBox(container.NewHBox(download, fromFile), progress, result)
}

// destinationStep recueille la destination, la teste et regarde si elle a
// déjà servi (EF-20 à EF-26, EF-34).
func (w *wizardView) destinationStep() fyne.CanvasObject {
	t := w.u.t
	profile := &w.state.Profile
	results := container.NewVBox()

	invalidate := func() {
		w.state.DestinationChecked = false
		w.state.RepositoryReady = false
		w.state.RepositoryExists = false
		w.setReady(false)
		results.RemoveAll()
	}

	user := widget.NewEntry()
	user.SetPlaceHolder("u123456")
	user.SetText(profile.Destination.User)
	user.OnChanged = func(value string) { profile.Destination.User = strings.TrimSpace(value); invalidate() }

	name := widget.NewEntry()
	name.SetText(profile.Destination.Repo)
	name.OnChanged = func(value string) { profile.Destination.Repo = strings.TrimSpace(value); invalidate() }

	url := widget.NewEntry()
	url.SetPlaceHolder("ssh://utilisateur@serveur:22/./sauvegardes")
	if profile.Destination.Kind == config.KindSSH {
		url.SetText(profile.Destination.Repo)
	}
	url.OnChanged = func(value string) { profile.Destination.Repo = strings.TrimSpace(value); invalidate() }

	// Hetzner installe deux versions de Borg ; la 1.4 est proposée, la 1.2
	// reste possible (EF-21).
	engine := widget.NewSelect([]string{"borg-1.4", "borg-1.2"}, func(value string) {
		if profile.Destination.RemotePath != value {
			profile.Destination.RemotePath = value
			invalidate()
		}
	})
	engine.SetSelected(profile.Destination.RemotePath)

	hetznerForm := widget.NewForm(
		widget.NewFormItem(t("destination.account"), user),
		widget.NewFormItem(t("destination.name"), name),
		widget.NewFormItem(t("destination.engine"), engine),
	)
	sshForm := widget.NewForm(widget.NewFormItem(t("wizard.destination.url"), url))

	kinds := []string{t("destination.kind_hetzner"), t("destination.kind_ssh")}
	kind := widget.NewRadioGroup(kinds, func(choice string) {
		if choice == kinds[1] {
			profile.Destination.Kind = config.KindSSH
			hetznerForm.Hide()
			sshForm.Show()
		} else {
			profile.Destination.Kind = config.KindHetzner
			sshForm.Hide()
			hetznerForm.Show()
		}
		invalidate()
	})
	kind.Horizontal = true
	if profile.Destination.Kind == config.KindSSH {
		kind.SetSelected(kinds[1])
	} else {
		kind.SetSelected(kinds[0])
	}
	// Une configuration importée ne redemande que le sous-compte (EF-101).
	if w.state.Imported {
		kind.Disable()
		name.Disable()
		engine.Disable()
	}

	key := widget.NewLabel("")
	copyKey := widget.NewButtonWithIcon(t("destination.copy_key"), theme.ContentCopyIcon(), func() {
		w.u.app.Clipboard().SetContent(key.Text)
	})
	var keyText string
	var keyErr error
	async(func() {
		path, err := w.u.st.SSHKeyPath(profile)
		if err == nil {
			if _, err = probe.EnsureKey(path); err == nil {
				keyText, err = probe.PublicKey(path)
			}
		}
		keyErr = err
	}, func() {
		if keyErr != nil {
			key.SetText(keyErr.Error())
			return
		}
		key.SetText(keyText)
	})

	// checkUser retourne la clé du message bloquant un sous-compte mal
	// formé, ou "".
	checkUser := func() string {
		if profile.Destination.Kind == config.KindHetzner && !hetznerUser.MatchString(profile.Destination.User) {
			return "wizard.destination.invalid_user"
		}
		return ""
	}

	var test *widget.Button
	var runTest func(pin bool)
	runTest = func(pin bool) {
		if key := checkUser(); key != "" {
			results.RemoveAll()
			results.Add(w.paragraph(key))
			return
		}
		test.Disable()
		results.RemoveAll()
		results.Add(w.paragraph("destination.testing"))
		var (
			lines       []testLine
			fingerprint string
			unknownHost bool
			ok          bool
			state       core.DestinationState
			inspectErr  error
		)
		async(func() {
			lines, fingerprint, unknownHost, ok = w.u.probeDestination(profile, pin)
			if !ok {
				return
			}
			var runner borg.Runner
			var env borg.Environment
			if runner, inspectErr = w.u.st.Runner(); inspectErr != nil {
				return
			}
			if env, inspectErr = w.environment(); inspectErr != nil {
				return
			}
			state, inspectErr = core.InspectDestination(context.Background(), runner, env)
		}, func() {
			test.Enable()
			results.RemoveAll()
			for _, line := range lines {
				results.Add(w.u.renderLine(line))
			}
			if unknownHost {
				w.u.confirmPin(fingerprint, func() { runTest(true) })
			}
			if !ok {
				return
			}
			if inspectErr != nil {
				results.Add(w.u.renderLine(testLine{key: errorKey(inspectErr), detail: inspectErr.Error()}))
				return
			}
			w.acceptDestination(state, results)
		})
	}
	test = widget.NewButtonWithIcon(t("destination.test"), theme.MediaPlayIcon(), func() { runTest(false) })
	installer := w.u.keyInstaller(func() (*config.Profile, error) { return profile, nil }, checkUser, func() { runTest(false) })

	if w.state.DestinationChecked {
		w.setReady(true)
		results.Add(w.paragraph("wizard.destination.checked"))
	}

	return container.NewVBox(
		w.paragraph("wizard.destination.text"),
		kind, hetznerForm, sshForm,
		widget.NewSeparator(),
		widget.NewLabelWithStyle(t("destination.key"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		codeBlock(key), container.NewHBox(copyKey), w.paragraph("destination.hetzner_steps"),
		installer,
		widget.NewSeparator(),
		container.NewHBox(test), results,
	)
}

// acceptDestination retient ce que la destination a révélé d'elle-même.
func (w *wizardView) acceptDestination(state core.DestinationState, results *fyne.Container) {
	w.state.DestinationChecked = true
	w.state.RepositoryExists = state.Exists
	switch {
	case !state.Exists:
		results.Add(w.u.renderLine(testLine{ok: true, key: "wizard.destination.empty"}))
	case state.Encrypted:
		// Le mode exact se lira une fois la passphrase connue ; le choix du
		// chiffrement ne sera pas redemandé (EF-34).
		w.state.Profile.Encryption = config.EncryptionRepokey
		results.Add(w.u.renderLine(testLine{ok: true, key: "wizard.destination.existing_encrypted"}))
	default:
		w.state.Profile.Encryption = config.EncryptionNone
		w.state.RepositoryReady = true
		results.Add(w.u.renderLine(testLine{ok: true, key: "wizard.destination.existing_clear",
			data: map[string]any{"Size": format.Size(state.Size)}}))
	}
	w.save()
	w.setReady(true)
}

// encryptionStep fixe le chiffrement d'une destination neuve, ou recueille
// la passphrase d'une destination chiffrée existante (EF-30 à EF-36).
func (w *wizardView) encryptionStep() fyne.CanvasObject {
	t := w.u.t
	profile := &w.state.Profile
	result := container.NewVBox()

	passphrase := widget.NewPasswordEntry()
	passphrase.SetPlaceHolder(t("wizard.encryption.passphrase"))
	confirmation := widget.NewPasswordEntry()
	confirmation.SetPlaceHolder(t("wizard.encryption.confirmation"))

	if w.state.RepositoryReady {
		// Le choix est fait et la destination préparée : il est définitif
		// (EF-31, EF-32).
		w.setReady(true)
		return container.NewVBox(w.paragraph("wizard.encryption.done", map[string]any{
			"Mode": w.u.encryptionLabel(profile.Encryption),
		}))
	}

	if w.state.RepositoryExists {
		verify := widget.NewButtonWithIcon(t("wizard.encryption.verify"), theme.ConfirmIcon(), nil)
		verify.OnTapped = func() {
			if passphrase.Text == "" {
				return
			}
			verify.Disable()
			w.withPassphrase(passphrase.Text, result, func(ctx context.Context, runner borg.Runner, env borg.Environment) error {
				state, err := core.VerifyPassphrase(ctx, runner, env)
				if err == nil && state.Mode != "" {
					profile.Encryption = config.Encryption(state.Mode)
				}
				return err
			}, func() { verify.Enable() })
		}
		return container.NewVBox(w.paragraph("wizard.encryption.existing"), passphrase, verify, result)
	}

	encrypted := t("wizard.encryption.choice_encrypted")
	clear := t("wizard.encryption.choice_clear")
	passwords := container.NewVBox(w.paragraph("wizard.encryption.passphrase_text"), passphrase, confirmation)
	choice := widget.NewRadioGroup([]string{encrypted, clear}, func(value string) {
		if value == clear {
			profile.Encryption = config.EncryptionNone
			passwords.Hide()
		} else {
			profile.Encryption = config.EncryptionRepokey
			passwords.Show()
		}
	})
	// Le mode chiffré est présélectionné et recommandé (PA-04, EF-30).
	if profile.Encryption == config.EncryptionNone {
		choice.SetSelected(clear)
	} else {
		choice.SetSelected(encrypted)
	}

	create := widget.NewButtonWithIcon(t("wizard.encryption.create"), theme.ConfirmIcon(), nil)
	create.OnTapped = func() {
		if profile.Encryption.Encrypted() {
			if passphrase.Text == "" || passphrase.Text != confirmation.Text {
				result.RemoveAll()
				result.Add(w.paragraph("wizard.encryption.mismatch"))
				return
			}
		}
		create.Disable()
		choice.Disable()
		mode := profile.Encryption.BorgMode()
		w.withPassphrase(passphrase.Text, result, func(ctx context.Context, runner borg.Runner, env borg.Environment) error {
			return core.CreateDestination(ctx, runner, env, mode)
		}, func() {
			create.Enable()
			choice.Enable()
		})
	}

	return container.NewVBox(
		w.paragraph("wizard.encryption.text"),
		choice,
		w.paragraph("wizard.encryption.consequences"),
		w.paragraph("wizard.encryption.irreversible"),
		passwords,
		container.NewHBox(create),
		result,
	)
}

// withPassphrase enregistre la passphrase, s'il y en a une, puis exécute
// action contre la destination. En cas de succès, la destination est prête ;
// en cas d'échec, retry rend la main à l'utilisateur.
func (w *wizardView) withPassphrase(passphrase string, result *fyne.Container,
	action func(context.Context, borg.Runner, borg.Environment) error, retry func()) {
	profile := &w.state.Profile
	result.RemoveAll()
	result.Add(w.paragraph("wizard.encryption.working"))

	var fallback bool
	var err error
	async(func() {
		if profile.Encryption.Encrypted() {
			// Le trousseau du système garde la passphrase (EF-36) : elle
			// n'est jamais écrite dans l'état de l'assistant.
			if fallback, err = w.u.st.Secrets().Set(w.state.SecretName(), passphrase); err != nil {
				return
			}
		}
		var runner borg.Runner
		var env borg.Environment
		if runner, err = w.u.st.Runner(); err != nil {
			return
		}
		if env, err = w.environment(); err != nil {
			return
		}
		err = action(context.Background(), runner, env)
	}, func() {
		result.RemoveAll()
		if err != nil {
			retry()
			key := errorKey(err)
			if errors.Is(err, core.ErrPassphraseWrong) {
				key = "error.passphrase_wrong"
			}
			result.Add(w.u.explanation(key, nil, err.Error()))
			return
		}
		w.state.RepositoryReady = true
		w.save()
		result.Add(w.u.renderLine(testLine{ok: true, key: "wizard.encryption.ready"}))
		if fallback {
			result.Add(w.paragraph("wizard.encryption.fallback", map[string]any{"Dir": w.u.st.StateDir}))
		}
		w.setReady(true)
	})
}

// encryptionLabel traduit un mode de chiffrement, sans le vocabulaire de Borg
// (EI-02).
func (u *ui) encryptionLabel(mode config.Encryption) string {
	if mode.Encrypted() {
		return u.t("encryption.encrypted")
	}
	return u.t("encryption.none")
}

// foldersStep choisit les dossiers à sauvegarder et les exclusions (EF-40 à
// EF-45).
func (w *wizardView) foldersStep() fyne.CanvasObject {
	t := w.u.t
	profile := &w.state.Profile
	refreshReady := func() { w.setReady(len(profile.Sources) > 0) }

	var list *widget.List
	list = widget.NewList(
		func() int { return len(profile.Sources) },
		func() fyne.CanvasObject {
			return container.NewBorder(nil, nil, nil,
				widget.NewButtonWithIcon(t("backup_screen.remove"), theme.DeleteIcon(), nil), widget.NewLabel(""))
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			row := item.(*fyne.Container)
			row.Objects[0].(*widget.Label).SetText(profile.Sources[id])
			row.Objects[1].(*widget.Button).OnTapped = func() {
				profile.Sources = append(profile.Sources[:id:id], profile.Sources[id+1:]...)
				list.Refresh()
				refreshReady()
				w.save()
			}
		},
	)
	add := widget.NewButtonWithIcon(t("backup_screen.add"), theme.FolderOpenIcon(), func() {
		dialog.ShowFolderOpen(func(folder fyne.ListableURI, err error) {
			if err != nil || folder == nil {
				return
			}
			path := folder.Path()
			for _, existing := range profile.Sources {
				if existing == path {
					return
				}
			}
			profile.Sources = append(profile.Sources, path)
			list.Refresh()
			refreshReady()
			w.save()
		}, w.u.win)
	})

	presets := container.NewVBox()
	for _, preset := range config.ExcludePresets {
		check := widget.NewCheck(t(presetKey(preset)), func(on bool) {
			profile.Excludes = config.SetPreset(profile.Excludes, preset, on)
			w.save()
		})
		check.SetChecked(config.PresetEnabled(profile.Excludes, preset))
		presets.Add(check)
	}

	refreshReady()
	listArea := container.NewGridWrap(fyne.NewSize(640, 160), list)
	return container.NewVBox(
		w.paragraph("wizard.folders.text"),
		listArea, container.NewHBox(add),
		widget.NewSeparator(),
		w.paragraph("wizard.folders.excludes"),
		presets,
		w.paragraph("wizard.folders.cloud"),
	)
}

// presetKey retourne la clé du libellé d'un préréglage.
func presetKey(preset config.ExcludePreset) string { return "exclude." + preset.Key }

// scheduleStep choisit la fréquence des sauvegardes automatiques (EF-61).
func (w *wizardView) scheduleStep() fyne.CanvasObject {
	status := w.paragraph("")
	editor := w.u.scheduleEditor(&w.state.Profile.Schedule, func(plan schedule.Plan, err error) {
		if err != nil {
			status.SetText(w.u.t("schedule.invalid"))
			w.setReady(false)
			return
		}
		status.SetText(w.u.describePlan(plan))
		w.setReady(true)
		w.save()
	})
	return container.NewVBox(
		w.paragraph("wizard.schedule.text"),
		editor,
		status,
		w.paragraph("wizard.schedule.catch_up"),
	)
}

// scheduleEditor règle une planification : fréquence, heure et jour
// (EF-61). Le rattrapage des exécutions manquées est toujours actif
// (EF-62). changed reçoit la planification après chaque modification, ou
// l'erreur qui la rend invalide.
func (u *ui) scheduleEditor(s *config.Schedule, changed func(schedule.Plan, error)) fyne.CanvasObject {
	t := u.t
	s.CatchUpIfMissed = true
	if s.At == "" {
		s.At = "12:30"
	}

	days := []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}
	dayLabels := make([]string, len(days))
	for i, day := range days {
		weekday, _ := schedule.ParseDay(day)
		dayLabels[i] = t(schedule.DayKey(weekday))
	}

	validate := func() { changed(schedule.FromConfig(*s)) }

	at := widget.NewEntry()
	at.SetText(s.At)
	at.OnChanged = func(value string) { s.At = strings.TrimSpace(value); validate() }
	day := widget.NewSelect(dayLabels, func(label string) {
		for i, l := range dayLabels {
			if l == label {
				s.Day = days[i]
			}
		}
		validate()
	})
	if s.Day == "" {
		s.Day = "monday"
	}
	for i, d := range days {
		if d == s.Day {
			day.SetSelected(dayLabels[i])
		}
	}

	options := []string{t("wizard.schedule.daily"), t("wizard.schedule.weekly"), t("wizard.schedule.manual")}
	kinds := []string{"daily", "weekly", "manual"}
	frequency := widget.NewRadioGroup(options, func(value string) {
		for i, option := range options {
			if option == value {
				s.Kind = kinds[i]
			}
		}
		at.Enable()
		day.Enable()
		if s.Kind != "weekly" {
			day.Disable()
		}
		if s.Kind == "manual" {
			at.Disable()
		}
		validate()
	})
	for i, kind := range kinds {
		if kind == s.Kind {
			frequency.SetSelected(options[i])
		}
	}
	if frequency.Selected == "" {
		frequency.SetSelected(options[0])
	}

	return container.NewVBox(
		frequency,
		widget.NewForm(
			widget.NewFormItem(t("wizard.schedule.at"), at),
			widget.NewFormItem(t("wizard.schedule.day"), day),
		),
	)
}

// recoveryKeyStep fait mettre la clé de secours à l'abri, étape obligatoire
// et non contournable en mode chiffré (EF-35).
func (w *wizardView) recoveryKeyStep() fyne.CanvasObject {
	t := w.u.t
	key := widget.NewLabel("")
	key.TextStyle = fyne.TextStyle{Monospace: true}
	key.Wrapping = fyne.TextWrapOff
	actions := container.NewHBox()
	result := container.NewVBox()

	confirm := widget.NewCheck(t("wizard.recovery_key.confirm"), func(on bool) {
		w.state.KeyConfirmed = on
		w.setReady(on)
		w.save()
	})
	confirm.Disable()

	show := widget.NewButtonWithIcon(t("wizard.recovery_key.show"), theme.VisibilityIcon(), nil)
	show.OnTapped = func() {
		show.Disable()
		var text string
		var err error
		async(func() {
			var runner borg.Runner
			var env borg.Environment
			if runner, err = w.u.st.Runner(); err != nil {
				return
			}
			if env, err = w.environment(); err != nil {
				return
			}
			text, _, err = borg.KeyExportPaper(context.Background(), runner, env)
		}, func() {
			show.Enable()
			result.RemoveAll()
			if err != nil {
				result.Add(w.u.explanation(errorKey(err), nil, err.Error()))
				return
			}
			key.SetText(text)
			actions.RemoveAll()
			actions.Add(widget.NewButtonWithIcon(t("wizard.recovery_key.copy"), theme.ContentCopyIcon(), func() {
				w.u.app.Clipboard().SetContent(text)
			}))
			actions.Add(widget.NewButtonWithIcon(t("wizard.recovery_key.save"), theme.DocumentSaveIcon(), func() {
				w.saveKey(text, result)
			}))
			confirm.Enable()
		})
	}

	if w.state.KeyConfirmed {
		confirm.Enable()
		confirm.SetChecked(true)
	}
	return container.NewVBox(
		w.paragraph("wizard.recovery_key.text"),
		container.NewHBox(show),
		container.NewHScroll(key),
		actions,
		w.paragraph("repository.key_warning"),
		confirm,
		result,
	)
}

// saveKey enregistre la clé de secours dans un fichier choisi par
// l'utilisateur.
func (w *wizardView) saveKey(text string, result *fyne.Container) {
	dialog.ShowFileSave(func(file fyne.URIWriteCloser, err error) {
		if err != nil || file == nil {
			return
		}
		_, writeErr := file.Write([]byte(text))
		if closeErr := file.Close(); writeErr == nil {
			writeErr = closeErr
		}
		result.RemoveAll()
		if writeErr != nil {
			result.Add(w.u.explanation("error.unknown", nil, writeErr.Error()))
			return
		}
		result.Add(w.paragraph("wizard.recovery_key.saved", map[string]any{"Path": file.URI().Path()}))
	}, w.u.win)
}

// describePlan rend une planification en langage courant.
func (u *ui) describePlan(plan schedule.Plan) string {
	data := map[string]any{"At": fmt.Sprintf("%02d:%02d", plan.Hour, plan.Minute)}
	var text string
	switch plan.Frequency {
	case schedule.Daily:
		text = u.t("schedule.daily", data)
	case schedule.Weekly:
		data["Day"] = u.t(schedule.DayKey(plan.Day))
		text = u.t("schedule.weekly", data)
	default:
		return u.t("schedule.manual")
	}
	if plan.CatchUp {
		text += u.t("schedule.catch_up")
	}
	return text
}

// applySchedule met la tâche planifiée en accord avec le profil : installée
// pour une sauvegarde automatique, retirée pour une sauvegarde manuelle
// (EF-60).
func applySchedule(st *station.Station, profile *config.Profile, t format.Translate) error {
	plan, err := schedule.FromConfig(profile.Schedule)
	if err != nil {
		return err
	}
	if plan.Frequency != schedule.Manual {
		return installSchedule(st, profile, plan, t)
	}
	scheduler, err := newScheduler()
	if err != nil {
		return err
	}
	task, err := st.ScheduledTask(profile, t("schedule.task_description", map[string]any{"Profile": profile.Name}))
	if err != nil {
		return err
	}
	return scheduler.Remove(context.Background(), task.Name)
}

// installSchedule installe la tâche planifiée du profil.
func installSchedule(st *station.Station, profile *config.Profile, plan schedule.Plan, t format.Translate) error {
	scheduler, err := newScheduler()
	if err != nil {
		return err
	}
	task, err := st.ScheduledTask(profile, t("schedule.task_description", map[string]any{"Profile": profile.Name}))
	if err != nil {
		return err
	}
	return scheduler.Install(context.Background(), task, plan)
}
