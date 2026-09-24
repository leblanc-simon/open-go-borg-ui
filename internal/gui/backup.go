package gui

import (
	"context"
	"errors"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/config"
	"leblanc.io/open-go-borg-ui/internal/core"
	"leblanc.io/open-go-borg-ui/internal/format"
	"leblanc.io/open-go-borg-ui/internal/history"
	"leblanc.io/open-go-borg-ui/internal/secret"
)

// progressInterval espace les mises à jour de la progression : Borg émet bien
// plus d'événements qu'un œil n'en lit.
const progressInterval = 200 * time.Millisecond

// backupScreen est l'écran Sauvegarde (EF-40, EF-50 à EF-52).
type backupScreen struct {
	u *ui

	sources []string
	list    *widget.List

	start    *widget.Button
	cancel   *widget.Button
	progress *widget.ProgressBarInfinite
	phase    *widget.Label
	counters *widget.Label
	path     *widget.Label
	result   *fyne.Container

	stop context.CancelFunc

	content fyne.CanvasObject
}

func newBackupScreen(u *ui) *backupScreen {
	b := &backupScreen{u: u}
	t := u.t

	b.list = widget.NewList(
		func() int { return len(b.sources) },
		func() fyne.CanvasObject {
			return container.NewBorder(nil, nil, nil,
				widget.NewButtonWithIcon(t("backup_screen.remove"), theme.DeleteIcon(), nil),
				widget.NewLabel(""))
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			row := item.(*fyne.Container)
			row.Objects[0].(*widget.Label).SetText(b.sources[id])
			row.Objects[1].(*widget.Button).OnTapped = func() { b.removeSource(id) }
		},
	)
	add := widget.NewButtonWithIcon(t("backup_screen.add"), theme.FolderOpenIcon(), b.addSource)

	b.start = widget.NewButtonWithIcon(t("backup_screen.start"), theme.UploadIcon(), b.run)
	b.start.Importance = widget.HighImportance
	b.cancel = widget.NewButtonWithIcon(t("backup_screen.cancel"), theme.CancelIcon(), b.abort)
	b.cancel.Hide()
	b.progress = widget.NewProgressBarInfinite()
	b.progress.Stop()
	b.progress.Hide()
	b.phase = widget.NewLabel("")
	b.counters = widget.NewLabel("")
	b.path = widget.NewLabel("")
	b.path.Truncation = fyne.TextTruncateEllipsis
	b.result = container.NewVBox()

	folders := container.NewBorder(
		widget.NewLabelWithStyle(t("backup_screen.sources"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewHBox(add), nil, nil, b.list)
	actions := container.NewVBox(
		widget.NewSeparator(),
		container.NewHBox(b.start, b.cancel),
		b.progress, b.phase, b.counters, b.path, b.result,
	)
	b.content = page(pageHeader(t("backup_screen.title"), t("backup_screen.subtitle")),
		container.NewBorder(nil, actions, nil, nil, folders))
	b.loadSources()
	return b
}

// loadSources relit les dossiers du profil.
func (b *backupScreen) loadSources() {
	profile, err := b.u.st.Profile()
	if err != nil {
		return
	}
	b.sources = append([]string(nil), profile.Sources...)
	b.list.Refresh()
}

// addSource ouvre le sélecteur de dossiers et ajoute le dossier choisi.
func (b *backupScreen) addSource() {
	dialog.ShowFolderOpen(func(folder fyne.ListableURI, err error) {
		if err != nil || folder == nil {
			return
		}
		path := folder.Path()
		for _, existing := range b.sources {
			if existing == path {
				return
			}
		}
		b.saveSources(append(append([]string(nil), b.sources...), path))
	}, b.u.win)
}

// removeSource retire un dossier.
func (b *backupScreen) removeSource(index int) {
	if index < 0 || index >= len(b.sources) {
		return
	}
	updated := append(append([]string(nil), b.sources[:index]...), b.sources[index+1:]...)
	b.saveSources(updated)
}

// saveSources enregistre les dossiers dans la configuration.
func (b *backupScreen) saveSources(sources []string) {
	cfg, err := config.Load(b.u.st.ConfigPath)
	if err == nil {
		var profile *config.Profile
		if profile, err = cfg.Profile(b.u.st.ProfileName); err == nil {
			profile.Sources = sources
			err = config.Save(b.u.st.ConfigPath, cfg)
		}
	}
	if err != nil {
		dialog.ShowError(err, b.u.win)
		return
	}
	b.sources = sources
	b.list.Refresh()
}

// run lance la sauvegarde hors du fil de l'interface.
func (b *backupScreen) run() {
	t := b.u.t
	b.result.RemoveAll()

	profile, err := b.u.st.Profile()
	if err != nil {
		b.showFailure(err)
		return
	}
	if len(profile.Sources) == 0 {
		b.showMessage("backup.no_sources")
		return
	}
	if profile.Encryption.Encrypted() {
		if _, err := b.u.st.Secrets().Get(profile.Name); errors.Is(err, secret.ErrNotFound) {
			b.showMessage("backup_screen.passphrase_missing")
			return
		}
	}

	ctx, stop := context.WithCancel(context.Background())
	b.stop = stop
	b.setRunning(true)
	b.phase.SetText(t("backup.starting", map[string]any{"Count": len(profile.Sources)}))

	var (
		report     *core.BackupReport
		runErr     error
		historyErr error
	)
	async(func() {
		defer stop()
		runner, err := b.u.st.Runner()
		if err != nil {
			runErr = err
			return
		}
		env, err := b.u.st.Environment(profile)
		if err != nil {
			runErr = err
			return
		}
		// Un historique inaccessible n'empêche pas la sauvegarde, mais il
		// est signalé : elle ne laisserait sinon aucune trace.
		store, err := b.u.st.History()
		if err != nil {
			historyErr = err
		} else {
			defer store.Close()
		}
		service := b.u.st.BackupService(profile, runner, store)
		report, runErr = service.Run(ctx, core.BackupRequest{
			Profile: profile,
			Env:     env,
			OnPhase: func(phase core.Phase) { fyne.Do(func() { b.showPhase(phase) }) },
			OnEvent: b.progressHandler(),
		})
	}, func() {
		b.setRunning(false)
		b.showReport(report, runErr)
		if historyErr == nil && report != nil {
			historyErr = report.HistoryErr
		}
		if historyErr != nil {
			b.showMessage("history.unavailable", map[string]any{"Message": historyErr.Error()})
		}
		if b.u.home != nil {
			b.u.home.refresh()
		}
	})
}

// abort demande l'arrêt de la sauvegarde en cours (EF-52). Borg reçoit
// l'interruption et s'arrête proprement ; l'exécution est consignée comme
// annulée.
func (b *backupScreen) abort() {
	if b.stop != nil {
		b.cancel.Disable()
		b.phase.SetText(b.u.t("backup_screen.cancelling"))
		b.stop()
	}
}

// setRunning bascule l'écran entre repos et sauvegarde en cours.
func (b *backupScreen) setRunning(running bool) {
	if running {
		b.start.Disable()
		b.cancel.Enable()
		b.cancel.Show()
		b.progress.Show()
		b.progress.Start()
		return
	}
	b.start.Enable()
	b.cancel.Hide()
	b.progress.Stop()
	b.progress.Hide()
	b.phase.SetText("")
	b.counters.SetText("")
	b.path.SetText("")
}

// showPhase affiche l'étape en cours.
func (b *backupScreen) showPhase(phase core.Phase) {
	keys := map[core.Phase]string{
		core.PhaseCloudScan: "backup.cloud_scan",
		core.PhaseBackup:    "backup_screen.running",
		core.PhasePrune:     "backup.pruning",
		core.PhaseCompact:   "backup.compacting",
	}
	b.phase.SetText(b.u.t(keys[phase]))
}

// progressHandler retourne le récepteur des événements de Borg. Il est appelé
// hors du fil de l'interface et n'y revient qu'à intervalle régulier.
func (b *backupScreen) progressHandler() func(borg.Event) {
	var last time.Time
	return func(event borg.Event) {
		if event.Kind != borg.EventArchiveProgress || event.Finished || time.Since(last) < progressInterval {
			return
		}
		last = time.Now()
		counters := b.u.t("backup_screen.counters", map[string]any{
			"Files": event.Files,
			"Size":  format.Size(event.OriginalSize),
		})
		path := event.Path
		fyne.Do(func() {
			b.counters.SetText(counters)
			b.path.SetText(path)
		})
	}
}

// showReport affiche l'issue de la sauvegarde.
func (b *backupScreen) showReport(report *core.BackupReport, err error) {
	t := b.u.t
	if err != nil {
		if report != nil && report.Run.Status == history.StatusCancelled {
			b.showMessage("backup_screen.cancelled")
			return
		}
		b.showFailure(err)
		return
	}

	run := report.Run
	data := map[string]any{
		"Files":    run.Files,
		"Original": format.Size(run.OriginalSize),
		"Stored":   format.Size(run.DeduplicatedSize),
		"Duration": format.Duration(t, run.Duration()),
	}
	b.showMessage("backup.finished", data)
	if run.Warnings > 0 {
		var detail string
		for _, warning := range report.Result.Warnings() {
			detail += warning.Text + "\n"
		}
		b.result.Add(b.u.explanation("backup.warnings", map[string]any{"Count": run.Warnings}, detail))
	}
	if len(report.CloudSkipped) > 0 {
		b.showMessage("backup.cloud_skipped", map[string]any{"Count": len(report.CloudSkipped)})
	}
	if report.MaintenanceErr != nil {
		b.result.Add(b.u.explanation(core.ErrorKeyMaintenance, nil, report.MaintenanceErr.Error()))
	}
}

// showMessage ajoute un message traduit au résultat.
func (b *backupScreen) showMessage(key string, data ...map[string]any) {
	var values map[string]any
	if len(data) > 0 {
		values = data[0]
	}
	label := widget.NewLabel(b.u.t(key, values))
	label.Wrapping = fyne.TextWrapWord
	b.result.Add(label)
}

// showFailure affiche un échec traduit, le détail replié.
func (b *backupScreen) showFailure(err error) {
	b.result.Add(b.u.explanation(errorKey(err), nil, err.Error()))
}
