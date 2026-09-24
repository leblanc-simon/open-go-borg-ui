package gui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/config"
	"leblanc.io/open-go-borg-ui/internal/core"
	"leblanc.io/open-go-borg-ui/internal/format"
	"leblanc.io/open-go-borg-ui/internal/history"
	"leblanc.io/open-go-borg-ui/internal/schedule"
	"leblanc.io/open-go-borg-ui/internal/secret"
	"leblanc.io/open-go-borg-ui/internal/station"
)

// progressInterval espace les mises à jour de la progression : Borg émet bien
// plus d'événements qu'un œil n'en lit.
const progressInterval = 200 * time.Millisecond

// backupScreen est l'écran Sauvegarde (EF-40 à EF-45, EF-50 à EF-52) : les
// dossiers envoyés, ce qui est laissé de côté, et la sauvegarde elle-même.
type backupScreen struct {
	u *ui

	sources []string
	list    *widget.List
	empty   fyne.CanvasObject
	count   *badge

	presets []*widget.Check
	plan    *text
	nextRun *text
	catchUp *widget.Label

	start    *widget.Button
	cancel   *widget.Button
	runPanel fyne.CanvasObject
	body     *fyne.Container
	progress *widget.ProgressBarInfinite
	phase    *widget.Label
	counters *widget.Label
	path     *widget.Label
	result   *fyne.Container

	stop context.CancelFunc

	content fyne.CanvasObject
}

// sideWidth est la largeur de la colonne des réglages de l'écran.
const sideWidth = 320

func newBackupScreen(u *ui) *backupScreen {
	b := &backupScreen{u: u}
	t := u.t

	b.start = widget.NewButtonWithIcon(t("backup_screen.start"), theme.UploadIcon(), b.run)
	b.start.Importance = widget.HighImportance
	b.cancel = widget.NewButtonWithIcon(t("backup_screen.cancel"), theme.CancelIcon(), b.abort)
	b.cancel.Importance = widget.DangerImportance
	b.cancel.Hide()

	b.body = container.NewBorder(b.runCard(), nil, nil, nil,
		split(b.foldersCard(), column(b.excludesCard(), b.scheduleCard()), sideWidth))
	b.content = page(pageHeader(t("backup_screen.title"), t("backup_screen.subtitle"), b.cancel, b.start), b.body)
	b.load()
	return b
}

// runCard montre la sauvegarde en cours, puis son issue. Elle reste
// masquée tant qu'aucune sauvegarde n'a été lancée depuis l'écran.
func (b *backupScreen) runCard() fyne.CanvasObject {
	b.progress = widget.NewProgressBarInfinite()
	b.progress.Stop()
	b.progress.Hide()
	b.phase = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	b.phase.Hide()
	b.counters = widget.NewLabel("")
	b.counters.Hide()
	b.path = widget.NewLabel("")
	b.path.Hide()
	b.path.Truncation = fyne.TextTruncateEllipsis
	b.path.Importance = widget.LowImportance
	b.result = container.NewVBox()

	b.runPanel = container.NewVBox(
		card(container.NewVBox(b.phase, b.progress, b.counters, b.path, b.result)),
		spacer(4),
	)
	b.runPanel.Hide()
	return b.runPanel
}

// foldersCard est la liste des dossiers sauvegardés.
func (b *backupScreen) foldersCard() fyne.CanvasObject {
	t := b.u.t
	b.list = widget.NewList(
		func() int { return len(b.sources) },
		func() fyne.CanvasObject {
			path := newText("", theme.SizeNameText, theme.ColorNameForeground, fyne.TextStyle{})
			path.truncate = true
			remove := widget.NewButtonWithIcon(t("backup_screen.remove"), theme.DeleteIcon(), nil)
			remove.Importance = widget.LowImportance
			return container.NewBorder(nil, nil,
				newBubble(theme.FolderIcon(), toneInfo, 32), remove,
				container.NewPadded(path))
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			row := item.(*fyne.Container)
			row.Objects[0].(*fyne.Container).Objects[0].(*text).SetText(b.sources[id])
			row.Objects[2].(*widget.Button).OnTapped = func() { b.removeSource(id) }
		},
	)

	add := widget.NewButtonWithIcon(t("backup_screen.add_short"), theme.ContentAddIcon(), b.addSource)
	add.Importance = widget.LowImportance
	firstAdd := widget.NewButtonWithIcon(t("backup_screen.add"), theme.FolderOpenIcon(), b.addSource)
	emptyText := widget.NewLabel(t("backup_screen.empty_text"))
	emptyText.Alignment = fyne.TextAlignCenter
	emptyText.Wrapping = fyne.TextWrapWord
	emptyText.Importance = widget.LowImportance
	b.empty = container.NewVBox(layout.NewSpacer(),
		container.NewCenter(newBubble(theme.FolderIcon(), toneNeutral, 52)),
		container.NewCenter(newText(t("backup_screen.empty_title"), theme.SizeNameSubHeadingText, theme.ColorNameForeground, fyne.TextStyle{Bold: true})),
		emptyText,
		container.NewCenter(firstAdd),
		layout.NewSpacer(),
	)

	b.count = newBadge("", toneInfo)
	title := container.NewHBox(
		newText(strings.ToUpper(t("backup_screen.sources")), sizeSmall, colorMuted, fyne.TextStyle{Bold: true}),
		container.NewCenter(b.count))
	return card(container.NewBorder(
		container.NewVBox(container.NewBorder(nil, nil, container.NewCenter(title), add), widget.NewSeparator()),
		nil, nil, nil,
		container.NewStack(b.list, b.empty),
	))
}

// excludesCard propose les préréglages d'exclusion (EF-42).
func (b *backupScreen) excludesCard() fyne.CanvasObject {
	t := b.u.t
	checks := container.NewVBox()
	for _, preset := range config.ExcludePresets {
		check := widget.NewCheck(t(presetKey(preset)), nil)
		check.OnChanged = func(on bool) {
			b.update(func(profile *config.Profile) {
				profile.Excludes = config.SetPreset(profile.Excludes, preset, on)
			})
		}
		b.presets = append(b.presets, check)
		checks.Add(check)
	}
	cloud := widget.NewLabel(t("wizard.folders.cloud"))
	cloud.Wrapping = fyne.TextWrapWord
	cloud.Importance = widget.LowImportance
	return card(container.NewVBox(caption(strings.ToUpper(t("backup_screen.excludes"))), checks, cloud))
}

// scheduleCard rappelle quand les sauvegardes partent d'elles-mêmes.
func (b *backupScreen) scheduleCard() fyne.CanvasObject {
	t := b.u.t
	b.plan = newText("", theme.SizeNameSubHeadingText, theme.ColorNameForeground, fyne.TextStyle{Bold: true})
	b.plan.fit = true
	b.nextRun = muted("")
	b.catchUp = widget.NewLabel(t("backup_screen.catch_up"))
	b.catchUp.Wrapping = fyne.TextWrapWord
	b.catchUp.Importance = widget.LowImportance
	return card(container.NewVBox(
		caption(strings.ToUpper(t("backup_screen.schedule"))),
		container.NewBorder(nil, nil, container.NewCenter(newBubble(theme.CalendarIcon(), toneInfo, 36)), nil,
			container.NewVBox(b.plan, b.nextRun)),
		b.catchUp,
	))
}

// load relit le profil et remplit l'écran.
func (b *backupScreen) load() {
	profile, err := b.u.st.Profile()
	if err != nil {
		return
	}
	b.showSources(profile.Sources)
	for i, preset := range config.ExcludePresets {
		// Cocher la case sans rappeler son enregistrement.
		check, changed := b.presets[i], b.presets[i].OnChanged
		check.OnChanged = nil
		check.SetChecked(config.PresetEnabled(profile.Excludes, preset))
		check.OnChanged = changed
	}

	t := b.u.t
	plan, err := schedule.FromConfig(profile.Schedule)
	if err != nil || plan.Frequency == schedule.Manual {
		b.plan.SetText(t("home.next_manual_short"))
		b.nextRun.SetText(t("backup_screen.manual"))
		b.catchUp.Hide()
		return
	}
	data := map[string]any{"At": fmt.Sprintf("%02d:%02d", plan.Hour, plan.Minute)}
	if plan.Frequency == schedule.Weekly {
		data["Day"] = t(schedule.DayKey(plan.Day))
		b.plan.SetText(t("backup_screen.plan_weekly", data))
	} else {
		b.plan.SetText(t("backup_screen.plan_daily", data))
	}
	if next := station.NextRun(profile)(); !next.IsZero() {
		b.nextRun.SetText(t("backup_screen.next", map[string]any{"When": format.When(t, next, time.Now())}))
	}
	b.catchUp.Hidden = !plan.CatchUp
}

// showSources affiche les dossiers.
func (b *backupScreen) showSources(sources []string) {
	b.sources = append([]string(nil), sources...)
	b.list.Refresh()
	b.count.Set(fmt.Sprint(len(b.sources)), toneInfo)
	if len(b.sources) == 0 {
		b.empty.Show()
		b.list.Hide()
	} else {
		b.empty.Hide()
		b.list.Show()
	}
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
	if b.update(func(profile *config.Profile) { profile.Sources = sources }) {
		b.showSources(sources)
	}
}

// update modifie le profil et enregistre la configuration. Il indique si
// l'enregistrement a réussi ; sinon l'erreur est montrée.
func (b *backupScreen) update(change func(*config.Profile)) bool {
	cfg, err := config.Load(b.u.st.ConfigPath)
	if err == nil {
		var profile *config.Profile
		if profile, err = cfg.Profile(b.u.st.ProfileName); err == nil {
			change(profile)
			err = config.Save(b.u.st.ConfigPath, cfg)
		}
	}
	if err != nil {
		dialog.ShowError(err, b.u.win)
		return false
	}
	return true
}

// run lance la sauvegarde hors du fil de l'interface.
func (b *backupScreen) run() {
	t := b.u.t
	b.result.RemoveAll()
	b.runPanel.Show()

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
			OnPhase: func(phase core.Phase) { onUI(func() { b.showPhase(phase) }) },
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
		b.relayout()
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
	lines := []fyne.CanvasObject{b.progress, b.phase, b.counters, b.path}
	if running {
		b.start.Disable()
		b.cancel.Enable()
		b.cancel.Show()
		for _, line := range lines {
			line.Show()
		}
		b.progress.Start()
		b.relayout()
		return
	}
	b.start.Enable()
	b.cancel.Hide()
	b.progress.Stop()
	b.phase.SetText("")
	b.counters.SetText("")
	b.path.SetText("")
	// Les lignes du déroulement disparaissent : la carte ne garde que
	// l'issue.
	for _, line := range lines {
		line.Hide()
	}
	b.relayout()
}

// relayout recalcule la page après un changement de la carte de
// déroulement : elle apparaît, grandit ou rétrécit.
func (b *backupScreen) relayout() {
	b.body.Refresh()
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
		onUI(func() {
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
	b.relayout()
}

// showFailure affiche un échec traduit, le détail replié.
func (b *backupScreen) showFailure(err error) {
	b.result.Add(b.u.explanation(errorKey(err), nil, err.Error()))
	b.relayout()
}
