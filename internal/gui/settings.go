package gui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"leblanc.io/open-go-borg-ui/internal/config"
	"leblanc.io/open-go-borg-ui/internal/schedule"
	"leblanc.io/open-go-borg-ui/internal/station"
	"leblanc.io/open-go-borg-ui/internal/version"
	"leblanc.io/open-go-borg-ui/internal/wizard"
)

// settingsScreen est l'écran Réglages (EI-01) : la planification, le
// changement de destination, l'export et l'import de la configuration, et
// la version. L'application reste pleinement utilisable sans jamais
// l'ouvrir (EI-05).
type settingsScreen struct {
	u *ui

	// Planification en cours de modification, copie de celle du profil.
	plan         config.Schedule
	planStatus   *widget.Label
	savePlan     *widget.Button
	planResult   *fyne.Container
	configResult *fyne.Container

	content fyne.CanvasObject
}

func newSettingsScreen(u *ui) *settingsScreen {
	s := &settingsScreen{u: u}
	t := u.t

	profile, err := u.st.Profile()
	if err != nil {
		s.content = widget.NewLabel(err.Error())
		return s
	}

	s.content = page(pageHeader(t("settings.title"), t("settings.subtitle")),
		split(
			column(s.scheduleCard(profile), s.destinationCard(profile)),
			column(s.configurationCard(), s.aboutCard()),
			360,
		))
	return s
}

// scheduleCard règle la sauvegarde automatique : fréquence, heure et jour
// (EF-60, EF-61).
func (s *settingsScreen) scheduleCard(profile *config.Profile) fyne.CanvasObject {
	t := s.u.t
	s.plan = profile.Schedule
	s.planStatus = widget.NewLabel("")
	s.planStatus.Wrapping = fyne.TextWrapWord
	s.planStatus.Importance = widget.LowImportance
	s.planResult = container.NewVBox()
	s.savePlan = widget.NewButtonWithIcon(t("settings.schedule_save"), theme.DocumentSaveIcon(), s.applySchedule)
	s.savePlan.Importance = widget.HighImportance

	editor := s.u.scheduleEditor(&s.plan, func(plan schedule.Plan, err error) {
		s.planResult.RemoveAll()
		if err != nil {
			s.planStatus.SetText(t("schedule.invalid"))
			s.savePlan.Disable()
			return
		}
		if plan.Frequency == schedule.Manual {
			s.planStatus.SetText(t("backup_screen.manual"))
		} else {
			s.planStatus.SetText(s.u.describePlan(plan))
		}
		s.savePlan.Enable()
	})

	return card(container.NewVBox(
		caption(strings.ToUpper(t("settings.schedule"))),
		editor,
		s.planStatus,
		container.NewHBox(s.savePlan),
		s.planResult,
	))
}

// applySchedule enregistre la planification et met la tâche planifiée en
// accord : installée, remplacée ou retirée (EF-60).
func (s *settingsScreen) applySchedule() {
	t := s.u.t
	s.savePlan.Disable()
	s.planResult.RemoveAll()
	plan := s.plan

	var err error
	async(func() {
		var cfg *config.Config
		var profile *config.Profile
		if cfg, err = config.Load(s.u.st.ConfigPath); err != nil {
			return
		}
		if profile, err = cfg.Profile(s.u.st.ProfileName); err != nil {
			return
		}
		profile.Schedule = plan
		if err = config.Save(s.u.st.ConfigPath, cfg); err != nil {
			return
		}
		err = applySchedule(s.u.st, profile, t)
	}, func() {
		s.savePlan.Enable()
		if err != nil {
			s.planResult.Add(s.u.explanation("settings.schedule_failed", nil, err.Error()))
			return
		}
		s.planResult.Add(s.u.renderLine(testLine{ok: true, key: "settings.schedule_saved"}))
		// L'écran Sauvegarde et l'écran État rappellent la planification.
		s.u.backup.load()
		s.u.home.refresh()
	})
}

// destinationCard propose de changer de destination, en passant de nouveau
// par l'assistant : test, chiffrement, clé de secours.
func (s *settingsScreen) destinationCard(profile *config.Profile) fyne.CanvasObject {
	t := s.u.t
	kind := t("destination.kind_hetzner")
	if profile.Destination.Kind == config.KindSSH {
		kind = t("destination.kind_ssh")
	}
	address := newText("", sizeSmall, colorMuted, fyne.TextStyle{Monospace: true}).abbreviated()
	if url, err := profile.Destination.RepositoryURL(); err == nil {
		address.SetText(url)
	}
	text := widget.NewLabel(t("settings.destination_text"))
	text.Wrapping = fyne.TextWrapWord
	text.Importance = widget.LowImportance

	change := widget.NewButtonWithIcon(t("settings.destination_change"), theme.StorageIcon(), s.changeDestination)
	return card(container.NewVBox(
		caption(strings.ToUpper(t("settings.destination"))),
		container.NewBorder(nil, nil, container.NewCenter(newBubble(theme.StorageIcon(), toneInfo, 36)), nil,
			container.NewVBox(newText(kind, theme.SizeNameText, theme.ColorNameForeground, fyne.TextStyle{Bold: true}), address)),
		text,
		container.NewHBox(change),
	))
}

// keyChoice propose de garder la clé de connexion du poste ou d'en créer
// une nouvelle. Garder la clé est présélectionné : elle est peut-être déjà
// autorisée sur la destination visée.
func (s *settingsScreen) keyChoice() (*widget.RadioGroup, func() bool) {
	t := s.u.t
	keep, renew := t("settings.key_keep"), t("settings.key_new")
	choice := widget.NewRadioGroup([]string{keep, renew}, nil)
	choice.Required = true
	choice.SetSelected(keep)
	return choice, func() bool { return choice.Selected == renew }
}

// busy signale une sauvegarde en cours, qui interdit de reconfigurer le
// poste sous elle.
func (s *settingsScreen) busy() bool {
	if s.u.backup.stop == nil {
		return false
	}
	dialog.ShowInformation(s.u.t("settings.busy_title"), s.u.t("settings.busy"), s.u.win)
	return true
}

// confirmReconfiguration demande confirmation d'une reconfiguration, avec
// le choix de la clé de connexion, puis la lance.
func (s *settingsScreen) confirmReconfiguration(title, message string, start func(newKey bool) *wizard.State) {
	t := s.u.t
	explanation := widget.NewLabel(message)
	explanation.Wrapping = fyne.TextWrapWord
	keyHint := widget.NewLabel(t("settings.key_hint"))
	keyHint.Wrapping = fyne.TextWrapWord
	keyHint.Importance = widget.LowImportance
	choice, newKey := s.keyChoice()

	confirm := dialog.NewCustomConfirm(title, t("settings.continue"), t("restore.confirm_cancel"),
		container.NewVBox(explanation, widget.NewSeparator(), caption(strings.ToUpper(t("settings.key"))), choice, keyHint),
		func(ok bool) {
			if !ok {
				return
			}
			if err := s.u.reconfigure(start(newKey())); err != nil {
				dialog.ShowError(err, s.u.win)
			}
		}, s.u.win)
	confirm.Resize(fyne.NewSize(600, 400))
	confirm.Show()
}

// changeDestination reprend l'assistant pour une nouvelle destination ; le
// poste garde la sienne jusqu'à la validation.
func (s *settingsScreen) changeDestination() {
	if s.busy() {
		return
	}
	profile, err := s.u.st.Profile()
	if err != nil {
		dialog.ShowError(err, s.u.win)
		return
	}
	t := s.u.t
	s.confirmReconfiguration(t("settings.destination_change_title"), t("settings.destination_change_text"),
		func(newKey bool) *wizard.State { return wizard.ForDestination(*profile, newKey) })
}

// configurationCard exporte et importe la configuration (EF-100, EF-101).
func (s *settingsScreen) configurationCard() fyne.CanvasObject {
	t := s.u.t
	text := widget.NewLabel(t("settings.configuration_text"))
	text.Wrapping = fyne.TextWrapWord
	text.Importance = widget.LowImportance
	s.configResult = container.NewVBox()

	export := widget.NewButtonWithIcon(t("settings.export"), theme.DocumentSaveIcon(), s.exportConfiguration)
	importButton := widget.NewButtonWithIcon(t("settings.import"), theme.FolderOpenIcon(), s.importConfiguration)
	return card(container.NewVBox(
		caption(strings.ToUpper(t("settings.configuration"))),
		text,
		container.NewHBox(export, importButton),
		s.configResult,
	))
}

// exportConfiguration écrit la configuration du poste, sans aucun secret
// (EF-100).
func (s *settingsScreen) exportConfiguration() {
	t := s.u.t
	profile, err := s.u.st.Profile()
	if err != nil {
		dialog.ShowError(err, s.u.win)
		return
	}
	save := dialog.NewFileSave(func(file fyne.URIWriteCloser, err error) {
		if err != nil || file == nil {
			return
		}
		data, exportErr := config.Export(*profile, t("config.export_header"))
		if exportErr == nil {
			_, exportErr = file.Write(data)
		}
		if closeErr := file.Close(); exportErr == nil {
			exportErr = closeErr
		}
		s.configResult.RemoveAll()
		if exportErr != nil {
			s.configResult.Add(s.u.explanation("error.unknown", nil, exportErr.Error()))
			return
		}
		s.configResult.Add(s.u.renderLine(testLine{ok: true, key: "settings.exported",
			data: map[string]any{"Path": file.URI().Path()}}))
	}, s.u.win)
	save.SetFileName(fmt.Sprintf("opengoborgui-%s.toml", station.Hostname()))
	save.SetFilter(storage.NewExtensionFileFilter([]string{".toml"}))
	save.Show()
}

// importConfiguration remplace la configuration du poste par celle d'un
// autre : l'assistant ne redemande que le sous-compte et, le cas échéant,
// le mot de passe, puis fait vérifier les dossiers (EF-101).
func (s *settingsScreen) importConfiguration() {
	if s.busy() {
		return
	}
	t := s.u.t
	current, err := s.u.st.Profile()
	if err != nil {
		dialog.ShowError(err, s.u.win)
		return
	}
	open := dialog.NewFileOpen(func(file fyne.URIReadCloser, err error) {
		if err != nil || file == nil {
			return
		}
		// Le profil importé prend le nom de celui du poste : il le remplace.
		imported, importErr := config.Import(file, current.Name)
		file.Close()
		if importErr != nil {
			dialog.ShowError(fmt.Errorf("%s", t("config.import_invalid", map[string]any{"Message": importErr.Error()})), s.u.win)
			return
		}
		s.confirmReconfiguration(t("settings.import_title"), t("settings.import_text"),
			func(newKey bool) *wizard.State { return wizard.ForImport(imported, newKey) })
	}, s.u.win)
	open.SetFilter(storage.NewExtensionFileFilter([]string{".toml"}))
	open.Show()
}

// aboutCard présente l'application et sa version.
func (s *settingsScreen) aboutCard() fyne.CanvasObject {
	t := s.u.t
	path := s.u.st.ConfigPath
	if path == "" {
		path, _ = config.DefaultPath()
	}
	location := newText(path, sizeSmall, colorMuted, fyne.TextStyle{Monospace: true}).abbreviated()
	return card(container.NewVBox(
		caption(strings.ToUpper(t("settings.about"))),
		container.NewBorder(nil, nil, logo(56), nil, container.NewVBox(
			layout.NewSpacer(),
			newText(t("window.title"), theme.SizeNameSubHeadingText, theme.ColorNameForeground, fyne.TextStyle{Bold: true}),
			muted(t("settings.version", map[string]any{"Version": version.String()})),
			muted(t("settings.license")),
			layout.NewSpacer(),
		)),
		caption(strings.ToUpper(t("settings.config_file"))),
		location,
	))
}
