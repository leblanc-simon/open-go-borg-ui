package gui

import (
	"image/color"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"leblanc.io/open-go-borg-ui/internal/format"
	"leblanc.io/open-go-borg-ui/internal/history"
	"leblanc.io/open-go-borg-ui/internal/station"
)

// historyDepth est le nombre d'exécutions montrées sur l'écran État.
const historyDepth = 30

// homeScreen est l'écran État (EF-80 à EF-82).
type homeScreen struct {
	u *ui

	indicator *canvas.Circle
	headline  *widget.Label
	details   *widget.Label
	list      *widget.List
	runs      []history.Run
	// generation numérote les relectures : seule la plus récente s'affiche,
	// une lecture plus ancienne qui finirait après elle est ignorée.
	generation int

	content fyne.CanvasObject
}

func newHomeScreen(u *ui) *homeScreen {
	h := &homeScreen{u: u}

	h.indicator = canvas.NewCircle(theme.Color(theme.ColorNameDisabled))
	indicator := container.NewGridWrap(fyne.NewSize(44, 44), h.indicator)
	h.headline = widget.NewLabelWithStyle(u.t("home.loading"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	h.headline.Wrapping = fyne.TextWrapWord
	h.details = widget.NewLabel("")
	h.details.Wrapping = fyne.TextWrapWord

	h.list = widget.NewList(
		func() int { return len(h.runs) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, item fyne.CanvasObject) {
			item.(*widget.Label).SetText(h.historyLine(h.runs[id]))
		},
	)
	h.list.OnSelected = func(id widget.ListItemID) {
		h.list.Unselect(id)
		h.showRun(h.runs[id])
	}

	top := container.NewVBox(
		container.NewBorder(nil, nil, indicator, nil, h.headline),
		h.details,
		widget.NewSeparator(),
		widget.NewLabelWithStyle(u.t("home.history"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)
	h.content = container.NewPadded(container.NewBorder(top, nil, nil, nil, h.list))
	return h
}

// refresh relit l'historique hors du fil de l'interface.
func (h *homeScreen) refresh() {
	h.generation++
	generation := h.generation
	var (
		runs    []history.Run
		next    time.Time
		loadErr error
	)
	async(func() {
		profile, err := h.u.st.Profile()
		if err != nil {
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
		runs, loadErr = store.Recent(backgroundContext(), profile.Name, historyDepth)
	}, func() {
		if generation != h.generation {
			return
		}
		if loadErr != nil {
			h.headline.SetText(h.u.t("home.unavailable"))
			h.details.SetText(loadErr.Error())
			return
		}
		h.show(runs, next, time.Now())
	})
}

// show affiche l'historique relu.
func (h *homeScreen) show(runs []history.Run, next time.Time, now time.Time) {
	h.runs = runs
	h.list.Refresh()

	summary := Summarize(runs, now)
	h.indicator.FillColor = indicatorColor(summary.Indicator)
	h.indicator.Refresh()
	h.headline.SetText(h.u.t(summary.Key, summary.Data))

	t := h.u.t
	var lines []string
	if last := summary.Last; last != nil {
		lines = append(lines, t("home.last", map[string]any{
			"Date":   last.Finished.Local().Format("2006-01-02 15:04"),
			"Status": t(last.Status.TranslationKey()),
		}))
	}
	if ok := summary.LastSuccess; ok != nil {
		lines = append(lines, t("home.last_details", map[string]any{
			"Files":    ok.Files,
			"Stored":   format.Size(ok.DeduplicatedSize),
			"Duration": format.Duration(t, ok.Duration()),
		}))
		if ok.RepositorySize > 0 {
			lines = append(lines, t("home.space", map[string]any{"Size": format.Size(ok.RepositorySize)}))
		}
	}
	if next.IsZero() {
		lines = append(lines, t("home.next_manual"))
	} else {
		lines = append(lines, t("home.next", map[string]any{"Date": next.Local().Format("2006-01-02 15:04")}))
	}
	h.details.SetText(strings.Join(lines, "\n"))
}

// historyLine rend une exécution de l'historique.
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

// indicatorColor retourne la couleur de l'indicateur, prise dans le thème
// pour rester lisible en clair comme en sombre.
func indicatorColor(indicator Indicator) color.Color {
	switch indicator {
	case IndicatorGreen:
		return theme.Color(theme.ColorNameSuccess)
	case IndicatorOrange:
		return theme.Color(theme.ColorNameWarning)
	default:
		return theme.Color(theme.ColorNameError)
	}
}
