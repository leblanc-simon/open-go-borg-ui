package gui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
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
	"leblanc.io/open-go-borg-ui/internal/station"
)

// historyDepth est le nombre d'exécutions montrées sur l'écran État.
const historyDepth = 30

// activityWidth est la largeur de la colonne d'activité.
const activityWidth = 310

// homeScreen est l'écran État (EF-80 à EF-82) : l'état du poste en une
// phrase, les chiffres qui comptent, la destination, et l'activité récente.
type homeScreen struct {
	u *ui

	// Bandeau : la phrase de synthèse, sur un fond de sa couleur (EF-80).
	tone     tone
	banner   *surface
	status   *bubble
	headline *widget.Label
	details  *widget.Label

	// Chiffres clés.
	last  *text
	space *text
	next  *text

	// Carte de la destination.
	destKind    *text
	destAddress *text
	encryption  *badge
	destTitle   *fyne.Container
	protected   *text
	protectedOf *text
	added       *text

	// Contrôles périodiques : vérification de restauration (EF-99) et
	// contrôle de la destination (EF-87).
	verification *checkPanel
	control      *checkPanel
	// stopCheck interrompt le contrôle de la destination en cours.
	stopCheck context.CancelFunc

	// Activité récente.
	list  *widget.List
	empty fyne.CanvasObject
	runs  []history.Run
	now   time.Time

	// generation numérote les relectures : seule la plus récente s'affiche,
	// une lecture plus ancienne qui finirait après elle est ignorée.
	generation int

	content fyne.CanvasObject
}

func newHomeScreen(u *ui) *homeScreen {
	h := &homeScreen{u: u, tone: toneNeutral, now: time.Now()}
	t := u.t

	start := widget.NewButtonWithIcon(t("backup_screen.start"), theme.UploadIcon(), h.startBackup)
	start.Importance = widget.HighImportance
	restore := widget.NewButtonWithIcon(t("home.restore"), theme.DownloadIcon(), u.openRestore)
	header := pageHeader(t("home.title"), t("home.subtitle"), restore, start)

	body := container.NewBorder(
		container.NewVBox(h.bannerCard(), spacer(4), h.tiles(), spacer(4)),
		nil, nil, nil,
		// La carte de la destination défile au besoin : sa hauteur ne doit
		// pas s'imposer à la fenêtre.
		split(h.destinationColumn(), h.activityCard(), activityWidth),
	)
	h.content = page(header, body)
	return h
}

// bannerCard est le bandeau de synthèse.
func (h *homeScreen) bannerCard() fyne.CanvasObject {
	h.headline = widget.NewLabelWithStyle(h.u.t("home.loading"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	h.headline.Wrapping = fyne.TextWrapWord
	h.details = widget.NewLabel("")
	h.details.Wrapping = fyne.TextWrapWord
	h.status = newBubble(theme.InfoIcon(), toneNeutral, 44)

	h.banner = newSurface(colorCard, colorCardBorder)
	h.banner.fill = func() color.Color { return h.tone.soft() }
	h.banner.stroke = nil
	text := container.NewVBox(layout.NewSpacer(), h.headline, h.details, layout.NewSpacer())
	return container.NewStack(h.banner, container.New(layout.NewCustomPaddedLayout(10, 10, 16, 16),
		container.NewBorder(nil, nil, container.NewCenter(h.status), nil, text)))
}

// tiles sont les trois chiffres clés.
func (h *homeScreen) tiles() fyne.CanvasObject {
	t := h.u.t
	tile := func(icon fyne.Resource, value *text, label string) fyne.CanvasObject {
		return card(container.NewBorder(nil, nil,
			container.NewCenter(newBubble(icon, toneInfo, 42)), nil,
			container.NewVBox(value, caption(strings.ToUpper(label)).abbreviated()),
		))
	}
	display := func() *text {
		value := newText("—", sizeDisplay, theme.ColorNameForeground, fyne.TextStyle{Bold: true})
		value.fit = true
		return value
	}
	h.last, h.space, h.next = display(), display(), display()
	return container.NewGridWithColumns(3,
		tile(theme.HistoryIcon(), h.last, t("home.tile_last")),
		tile(theme.StorageIcon(), h.space, t("home.tile_space")),
		tile(theme.CalendarIcon(), h.next, t("home.tile_next")),
	)
}

// destinationCard résume la destination et ce qu'elle protège.
func (h *homeScreen) destinationCard() fyne.CanvasObject {
	t := h.u.t
	h.destKind = newText("", theme.SizeNameSubHeadingText, theme.ColorNameForeground, fyne.TextStyle{Bold: true})
	h.destKind.truncate = true
	h.destAddress = newText("", sizeSmall, colorMuted, fyne.TextStyle{Monospace: true})
	h.destAddress.truncate = true
	h.encryption = newBadge("", toneNeutral)
	h.protected = newText("—", sizeDisplay, theme.ColorNameForeground, fyne.TextStyle{Bold: true})
	h.protected.fit = true
	h.protectedOf = muted("")
	h.added = muted("")

	h.destTitle = container.NewBorder(nil, nil,
		container.NewCenter(newBubble(theme.StorageIcon(), toneInfo, 42)),
		container.NewVBox(h.encryption, layout.NewSpacer()),
		container.NewVBox(h.destKind, h.destAddress),
	)
	return card(container.NewVBox(
		caption(strings.ToUpper(t("home.destination"))),
		spacer(2),
		h.destTitle,
		spacer(6),
		widget.NewSeparator(),
		spacer(6),
		caption(strings.ToUpper(t("home.protected"))),
		h.protected,
		h.protectedOf,
		spacer(6),
		caption(strings.ToUpper(t("home.last_added"))),
		h.added,
		spacer(6),
		widget.NewSeparator(),
		spacer(6),
		h.verificationSection(),
		spacer(6),
		widget.NewSeparator(),
		spacer(6),
		h.controlSection(),
	))
}

// destinationColumn place la carte de la destination dans une colonne qui
// défile ; ses sections de contrôle la remettent en page quand elles
// changent de hauteur.
func (h *homeScreen) destinationColumn() fyne.CanvasObject {
	scroll := column(h.destinationCard())
	relayout := func() { scroll.Refresh() }
	h.verification.relayout = relayout
	h.control.relayout = relayout
	return scroll
}

// verificationSection montre la dernière vérification de restauration :
// un fichier tiré au hasard, extrait et comparé à l'original, une fois par
// mois (EF-99).
func (h *homeScreen) verificationSection() fyne.CanvasObject {
	t := h.u.t
	h.verification = newCheckPanel(h.u, t("verify.title"), t("verify.now"), h.verifyNow)
	return h.verification.content
}

// showVerification affiche la dernière vérification, ou son absence.
func (h *homeScreen) showVerification(check history.RestoreCheck, ok bool) {
	t := h.u.t
	if !ok {
		h.verification.show(t("verify.badge_never"), toneNeutral, t("verify.never_title"), t("verify.never"), "")
		return
	}
	date := map[string]any{"Date": check.Checked.Local().Format("2006-01-02")}
	message := t(check.Reason, map[string]any{"Path": check.Native})
	switch check.Outcome {
	case history.OutcomeIdentical:
		h.verification.show(t("verify.badge_identical"), toneSuccess, t("verify.checked_on", date), message, check.Detail)
	case history.OutcomeExtracted:
		h.verification.show(t("verify.badge_extracted"), toneInfo, t("verify.checked_on", date), message, check.Detail)
	default:
		h.verification.show(t("verify.badge_failed"), toneError, t("verify.failed_on", date), message, check.Detail)
	}
}

// verifyNow vérifie une restauration sans attendre l'échéance mensuelle,
// sur la sauvegarde la plus récente.
func (h *homeScreen) verifyNow() {
	t := h.u.t
	panel := h.verification
	panel.button.Disable()
	panel.show(t("verify.badge_running"), toneInfo, panel.when.Text, t("verify.running"), "")
	var err error
	h.withDestination(func(ctx context.Context, env destinationEnv) {
		_, err = core.VerifyLatest(ctx, env.runner, env.env, env.store, env.profile.Name, h.u.st.LockDir(), time.Now())
	}, func(setupErr error) {
		panel.button.Enable()
		if setupErr != nil {
			err = setupErr
		}
		if err == nil {
			h.refresh()
			return
		}
		if errors.Is(err, core.ErrNothingToVerify) {
			panel.show(t("verify.badge_impossible"), toneWarning, panel.when.Text, t("verify.nothing"), "")
			return
		}
		panel.show(t("verify.badge_impossible"), toneWarning, panel.when.Text, t(errorKey(err)), err.Error())
	})
}

// controlSection montre le dernier contrôle de la destination, mené une fois
// par mois (EF-87).
func (h *homeScreen) controlSection() fyne.CanvasObject {
	t := h.u.t
	h.control = newCheckPanel(h.u, t("check.title"), t("check.now"), h.checkNow)
	return h.control.content
}

// showControl affiche le dernier contrôle de la destination, ou son absence.
func (h *homeScreen) showControl(check history.RepositoryCheck, ok bool) {
	t := h.u.t
	if !ok {
		h.control.show(t("check.badge_never"), toneNeutral, t("check.never_title"), t("check.never"), "")
		return
	}
	data := map[string]any{
		"Date":     check.Checked.Local().Format("2006-01-02"),
		"Duration": format.Duration(t, check.Duration),
	}
	if check.Healthy {
		h.control.show(t("check.badge_healthy"), toneSuccess, t("check.checked_on", data), t("check.healthy_text", data), "")
		return
	}
	h.control.show(t("check.badge_problems"), toneError, t("check.problems_on", data), t("check.problems_text"), check.Detail)
}

// checkNow contrôle la destination sans attendre l'échéance mensuelle. Le
// contrôle peut durer : le bouton devient « Arrêter » le temps qu'il
// s'achève.
func (h *homeScreen) checkNow() {
	t := h.u.t
	panel := h.control
	if h.stopCheck != nil {
		panel.button.Disable()
		h.stopCheck()
		return
	}
	ctx, stop := context.WithCancel(context.Background())
	h.stopCheck = stop
	cancelled := false
	panel.button.SetText(t("check.stop"))
	panel.button.SetIcon(theme.CancelIcon())
	panel.show(t("check.badge_running"), toneInfo, panel.when.Text, t("check.running"), "")

	var err error
	h.withDestination(func(_ context.Context, env destinationEnv) {
		var last time.Time
		_, err = core.CheckNow(ctx, env.runner, env.env, env.store, env.profile.Name, h.u.st.LockDir(), time.Now,
			func(event borg.Event) {
				percent, ok := event.Percent()
				if event.Kind != borg.EventProgressPercent || !ok || time.Since(last) < progressInterval {
					return
				}
				last = time.Now()
				message := t("check.progress_text", map[string]any{"Percent": fmt.Sprintf("%.0f", percent)})
				onUI(func() { panel.setMessage(message) })
			})
		cancelled = ctx.Err() != nil
	}, func(setupErr error) {
		stop()
		h.stopCheck = nil
		panel.button.SetText(t("check.now"))
		panel.button.SetIcon(theme.ConfirmIcon())
		panel.button.Enable()
		if setupErr != nil {
			err = setupErr
		}
		switch {
		case cancelled:
			panel.show(t("check.badge_impossible"), toneWarning, panel.when.Text, t("check.cancelled"), "")
		case err != nil:
			panel.show(t("check.badge_impossible"), toneWarning, panel.when.Text, t(errorKey(err)), err.Error())
		default:
			h.refresh()
		}
	})
}

// destinationEnv rassemble ce qu'il faut pour agir sur la destination.
type destinationEnv struct {
	profile *config.Profile
	runner  borg.Runner
	env     borg.Environment
	store   *history.Store
}

// withDestination prépare l'accès à la destination hors du fil de
// l'interface, y exécute work, puis rend compte dans ce fil : done reçoit
// l'erreur de préparation, s'il y en a une.
func (h *homeScreen) withDestination(work func(context.Context, destinationEnv), done func(error)) {
	var setupErr error
	async(func() {
		var env destinationEnv
		if env.profile, setupErr = h.u.st.Profile(); setupErr != nil {
			return
		}
		if env.runner, setupErr = h.u.st.Runner(); setupErr != nil {
			return
		}
		if env.env, setupErr = h.u.st.Environment(env.profile); setupErr != nil {
			return
		}
		if env.store, setupErr = h.u.st.History(); setupErr != nil {
			return
		}
		defer env.store.Close()
		work(backgroundContext(), env)
	}, func() { done(setupErr) })
}

// activityCard est le fil des dernières exécutions.
func (h *homeScreen) activityCard() fyne.CanvasObject {
	t := h.u.t
	h.list = widget.NewList(
		func() int { return len(h.runs) },
		func() fyne.CanvasObject { return newActivityRow() },
		func(id widget.ListItemID, item fyne.CanvasObject) { h.fillActivity(h.runs[id], item) },
	)
	h.list.OnSelected = func(id widget.ListItemID) {
		h.list.Unselect(id)
		h.showRun(h.runs[id])
	}
	empty := muted(t("home.activity_empty"))
	h.empty = container.NewCenter(empty)

	title := container.NewHBox(widget.NewIcon(theme.HistoryIcon()),
		newText(t("home.activity"), theme.SizeNameText, theme.ColorNameForeground, fyne.TextStyle{Bold: true}))
	return card(container.NewBorder(
		container.NewVBox(title, spacer(4)), nil, nil, nil,
		container.NewStack(h.list, h.empty),
	))
}

// activityRow est une exécution du fil d'activité : son issue, son
// ancienneté, et une ligne de résumé.
type activityRow struct {
	widget.BaseWidget
	status  *badge
	when    *text
	title   *text
	summary *widget.Label
	content fyne.CanvasObject
}

func newActivityRow() *activityRow {
	r := &activityRow{
		status:  newBadge("", toneNeutral),
		when:    newText("", sizeSmall, colorMuted, fyne.TextStyle{}),
		title:   newText("", theme.SizeNameText, theme.ColorNameForeground, fyne.TextStyle{Bold: true}),
		summary: widget.NewLabel(""),
	}
	r.title.truncate = true
	r.summary.Truncation = fyne.TextTruncateEllipsis
	r.summary.SizeName = sizeSmall
	r.summary.Importance = widget.LowImportance

	back := newSurface(colorInset, colorCardBorder)
	back.radius = 8
	r.content = container.NewPadded(container.NewStack(back, container.New(layout.NewCustomPaddedLayout(8, 2, 10, 10),
		container.NewVBox(container.NewBorder(nil, nil, r.status, r.when), r.title, r.summary))))
	r.ExtendBaseWidget(r)
	return r
}

func (r *activityRow) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(r.content)
}

// fillActivity remplit une ligne du fil avec une exécution.
func (h *homeScreen) fillActivity(run history.Run, item fyne.CanvasObject) {
	t := h.u.t
	row := item.(*activityRow)
	row.status.Set(strings.ToUpper(t("status.short."+string(run.Status))), statusTone(run.Status))
	row.when.SetText(format.Ago(t, run.Started, h.now))
	row.title.SetText(t("home.activity_" + string(run.Status)))
	row.summary.SetText(h.runSummary(run))
	// La pastille et l'ancienneté changent de largeur : la ligne se remet
	// en page.
	row.content.Refresh()
}

// runSummary résume une exécution en une ligne.
func (h *homeScreen) runSummary(run history.Run) string {
	t := h.u.t
	switch {
	case run.Status == history.StatusSuccess || run.Status == history.StatusWarning:
		return t("home.run_summary", map[string]any{
			"Files":    run.Files,
			"Stored":   format.Size(run.DeduplicatedSize),
			"Duration": format.Duration(t, run.Duration()),
		})
	case run.ErrorKey != "":
		return t(run.ErrorKey)
	default:
		return run.Started.Local().Format("2006-01-02 15:04")
	}
}

// statusTone associe une couleur à l'issue d'une exécution.
func statusTone(status history.Status) tone {
	switch status {
	case history.StatusSuccess:
		return toneSuccess
	case history.StatusWarning, history.StatusRunning:
		return toneWarning
	case history.StatusError:
		return toneError
	default:
		return toneNeutral
	}
}

// startBackup ouvre l'écran Sauvegarde et y lance une sauvegarde, sauf si
// une est déjà en cours.
func (h *homeScreen) startBackup() {
	h.u.shell.show(screenBackup)
	if !h.u.backup.start.Disabled() {
		h.u.backup.run()
	}
}

// refresh relit l'historique hors du fil de l'interface.
func (h *homeScreen) refresh() {
	h.generation++
	generation := h.generation
	var (
		profile    *config.Profile
		runs       []history.Run
		next       time.Time
		check      history.RestoreCheck
		checked    bool
		control    history.RepositoryCheck
		controlled bool
		loadErr    error
	)
	async(func() {
		var err error
		if profile, err = h.u.st.Profile(); err != nil {
			loadErr = err
			return
		}
		next = station.NextRun(profile)()
		store, err := h.u.st.History()
		if err != nil {
			loadErr = err
			return
		}
		defer store.Close()
		if runs, loadErr = store.Recent(backgroundContext(), profile.Name, historyDepth); loadErr != nil {
			return
		}
		if check, checked, loadErr = store.LastRestoreCheck(backgroundContext(), profile.Name); loadErr != nil {
			return
		}
		control, controlled, loadErr = store.LastRepositoryCheck(backgroundContext(), profile.Name)
	}, func() {
		if generation != h.generation {
			return
		}
		if loadErr != nil {
			h.setTone(toneError, theme.ErrorIcon())
			h.headline.SetText(h.u.t("home.unavailable"))
			h.details.SetText(loadErr.Error())
			return
		}
		h.show(profile, runs, next, time.Now(), check, checked, control, controlled)
		h.showVerification(check, checked)
		h.showControl(control, controlled)
	})
}

// show affiche l'historique relu.
func (h *homeScreen) show(profile *config.Profile, runs []history.Run, next time.Time, now time.Time,
	check history.RestoreCheck, checked bool, control history.RepositoryCheck, controlled bool) {
	t := h.u.t
	h.runs, h.now = runs, now
	h.list.Refresh()
	if len(runs) == 0 {
		h.empty.Show()
	} else {
		h.empty.Hide()
	}

	summary := Summarize(runs, now).WithRestoreCheck(check, checked).WithRepositoryCheck(control, controlled)
	switch summary.Indicator {
	case IndicatorGreen:
		h.setTone(toneSuccess, theme.ConfirmIcon())
	case IndicatorOrange:
		h.setTone(toneWarning, theme.WarningIcon())
	default:
		h.setTone(toneError, theme.ErrorIcon())
	}
	h.headline.SetText(t(summary.Key, summary.Data))
	if last := summary.Last; last != nil {
		h.details.SetText(t("home.last", map[string]any{
			"Date":   last.Finished.Local().Format("2006-01-02 15:04"),
			"Status": t(last.Status.TranslationKey()),
		}))
	} else {
		h.details.SetText(t("home.never_hint"))
	}

	h.last.SetText("—")
	h.protected.SetText("—")
	h.protectedOf.SetText("")
	h.added.SetText(t("home.nothing_yet"))
	if ok := summary.LastSuccess; ok != nil {
		h.last.SetText(format.Ago(t, ok.Finished, now))
		h.protected.SetText(format.Size(ok.OriginalSize))
		h.protectedOf.SetText(t("home.protected_of", map[string]any{
			"Files": ok.Files, "Folders": len(profile.Sources),
		}))
		h.added.SetText(t("home.added", map[string]any{
			"Stored":   format.Size(ok.DeduplicatedSize),
			"Duration": format.Duration(t, ok.Duration()),
		}))
	}

	h.space.SetText("—")
	for _, run := range runs {
		if run.RepositorySize > 0 {
			h.space.SetText(format.Size(run.RepositorySize))
			break
		}
	}

	if next.IsZero() {
		h.next.SetText(t("home.next_manual_short"))
	} else {
		h.next.SetText(format.When(t, next, now))
	}

	h.showDestination(profile)
}

// showDestination remplit la carte de la destination.
func (h *homeScreen) showDestination(profile *config.Profile) {
	t := h.u.t
	kind := t("destination.kind_hetzner")
	if profile.Destination.Kind == config.KindSSH {
		kind = t("destination.kind_ssh")
	}
	h.destKind.SetText(kind)
	if address, err := profile.Destination.RepositoryURL(); err == nil {
		h.destAddress.SetText(address)
	}
	if profile.Encryption.Encrypted() {
		h.encryption.Set(strings.ToUpper(t("encryption.encrypted")), toneSuccess)
	} else {
		h.encryption.Set(strings.ToUpper(t("encryption.none")), toneWarning)
	}
	// La pastille change de largeur avec son texte : la ligne se remet en
	// page.
	h.destTitle.Refresh()
}

// setTone colore le bandeau de synthèse.
func (h *homeScreen) setTone(t tone, icon fyne.Resource) {
	h.tone = t
	h.status.Set(icon, t)
	h.banner.Refresh()
}

// showRun ouvre le détail d'une exécution : son explication en clair et,
// replié, le journal brut (EF-82, EI-04).
func (h *homeScreen) showRun(run history.Run) {
	t := h.u.t
	key := run.ErrorKey
	switch {
	case key != "":
	case run.Status == history.StatusRunning:
		key = "home.run_interrupted"
	case run.Status == history.StatusCancelled:
		key = "home.run_cancelled"
	default:
		key = "home.run_ok"
	}
	content := h.u.explanation(key, nil, run.Detail)
	if run.CloudSkipped > 0 {
		skipped := widget.NewLabel(t("backup.cloud_skipped", map[string]any{"Count": run.CloudSkipped}))
		skipped.Wrapping = fyne.TextWrapWord
		content = container.NewVBox(content, skipped)
	}
	d := dialog.NewCustom(h.historyLine(run), t("gui.close"), content, h.u.win)
	d.Resize(fyne.NewSize(560, 320))
	d.Show()
}

// historyLine rend une exécution de l'historique en une ligne, titre du
// détail.
func (h *homeScreen) historyLine(run history.Run) string {
	t := h.u.t
	data := map[string]any{
		"Date":   run.Started.Local().Format("2006-01-02 15:04"),
		"Status": t(run.Status.TranslationKey()),
	}
	switch run.Status {
	case history.StatusSuccess, history.StatusWarning:
		data["Files"] = run.Files
		data["Stored"] = format.Size(run.DeduplicatedSize)
		return t("home.run_done", data)
	default:
		return t("home.run_other", data)
	}
}
