package gui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
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
	"leblanc.io/open-go-borg-ui/internal/secret"
	"leblanc.io/open-go-borg-ui/internal/station"
)

// searchLimit borne les résultats d'une recherche : au-delà, il faut
// préciser le nom.
const searchLimit = 500

// backupsWidth est la largeur de la colonne des sauvegardes.
const backupsWidth = 300

// restoreView est la fenêtre de restauration (EI-01, EF-90 à EF-96) : les
// sauvegardes disponibles, le contenu de celle choisie, et où le restaurer.
type restoreView struct {
	u   *ui
	win fyne.Window

	profile *config.Profile
	runner  borg.Runner
	env     borg.Environment

	// Sauvegardes disponibles, de la plus récente à la plus ancienne, et ce
	// que l'historique de ce poste en sait.
	archives  []borg.Archive
	runs      map[string]history.Run
	backups   *widget.List
	backupsAt *fyne.Container
	count     *badge
	archive   *borg.Archive

	// Contenu de la sauvegarde choisie.
	dir      string
	searched string
	entries  []history.Entry
	contents *widget.List
	status   *fyne.Container
	location *text
	up       *widget.Button
	search   *widget.Entry
	// generation numérote les lectures du contenu : seule la plus récente
	// s'affiche.
	generation int

	// Sélection, par chemin dans la sauvegarde.
	selected map[string]history.Entry
	summary  *text

	// Destination.
	target      *widget.RadioGroup
	parent      string
	folder      string
	folderLabel *text
	change      *widget.Button

	restoreSelection *widget.Button
	restoreAll       *widget.Button
	cancel           *widget.Button
	progress         *widget.ProgressBar
	result           *fyne.Container
	stop             context.CancelFunc
	// cancelled distingue une annulation demandée de la fin normale, qui
	// libère aussi le contexte.
	cancelled bool
}

// openRestore ouvre la fenêtre de restauration, ou la ramène au premier
// plan si elle l'est déjà.
func (u *ui) openRestore() {
	if u.restore != nil {
		u.restore.win.RequestFocus()
		return
	}
	r := &restoreView{u: u, selected: map[string]history.Entry{}, parent: station.DesktopDir()}
	u.restore = r
	r.win = u.app.NewWindow(u.t("restore.window_title"))
	r.win.Resize(fyne.NewSize(1100, 720))
	r.win.SetOnClosed(func() {
		if r.stop != nil {
			r.stop()
		}
		u.restore = nil
	})
	r.win.SetContent(r.build())
	r.win.Show()
	r.load()
}

// build construit la fenêtre.
func (r *restoreView) build() fyne.CanvasObject {
	t := r.u.t
	return page(
		pageHeader(t("restore.title"), t("restore.subtitle")),
		container.NewBorder(nil, r.actionsCard(), nil, nil,
			container.NewBorder(nil, nil, r.backupsCard(), nil, r.contentsCard())),
	)
}

// backupsCard est la liste des sauvegardes disponibles (EF-90).
func (r *restoreView) backupsCard() fyne.CanvasObject {
	t := r.u.t
	r.backups = widget.NewList(
		func() int { return len(r.archives) },
		func() fyne.CanvasObject {
			when := newText("", theme.SizeNameText, theme.ColorNameForeground, fyne.TextStyle{Bold: true})
			when.truncate = true
			detail := newText("", sizeSmall, colorMuted, fyne.TextStyle{})
			detail.truncate = true
			return container.NewBorder(nil, nil, newBubble(theme.HistoryIcon(), toneInfo, 34), nil,
				container.NewVBox(layout.NewSpacer(), when, detail, layout.NewSpacer()))
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			archive := r.archives[id]
			row := item.(*fyne.Container)
			lines := row.Objects[0].(*fyne.Container)
			lines.Objects[1].(*text).SetText(format.When(t, archive.Start.Time, time.Now()))
			lines.Objects[2].(*text).SetText(r.describe(archive))
			tone := toneInfo
			if run, ok := r.runs[archive.Name]; ok && run.Status == history.StatusWarning {
				tone = toneWarning
			}
			row.Objects[1].(*bubble).Set(theme.HistoryIcon(), tone)
		},
	)
	r.backups.OnSelected = func(id widget.ListItemID) { r.choose(r.archives[id]) }

	r.count = newBadge("", toneInfo)
	r.backupsAt = container.NewStack(r.backups)
	title := container.NewHBox(
		newText(strings.ToUpper(t("restore.backups")), sizeSmall, colorMuted, fyne.TextStyle{Bold: true}),
		container.NewCenter(r.count))
	width := canvas.NewRectangle(color.Transparent)
	width.SetMinSize(fyne.NewSize(backupsWidth, 0))
	return container.NewStack(width, card(container.NewBorder(
		container.NewVBox(title, widget.NewSeparator()), nil, nil, nil, r.backupsAt)))
}

// describe résume une sauvegarde : son ancienneté, et ce que l'historique
// de ce poste en a retenu.
func (r *restoreView) describe(archive borg.Archive) string {
	t := r.u.t
	ago := format.Ago(t, archive.Start.Time, time.Now())
	run, ok := r.runs[archive.Name]
	if !ok {
		return ago
	}
	return t("restore.backup_detail", map[string]any{
		"Ago": ago, "Files": run.Files, "Size": format.Size(run.OriginalSize),
	})
}

// contentsCard montre le contenu de la sauvegarde choisie (EF-91, EF-92).
func (r *restoreView) contentsCard() fyne.CanvasObject {
	t := r.u.t
	r.up = widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() { r.open(parentDir(r.dir)) })
	r.up.Importance = widget.LowImportance
	r.up.Disable()
	r.location = newText(t("restore.pick_backup"), theme.SizeNameText, theme.ColorNameForeground, fyne.TextStyle{Bold: true})
	r.location.truncate = true

	r.search = widget.NewEntry()
	r.search.SetPlaceHolder(t("restore.search"))
	r.search.ActionItem = widget.NewIcon(theme.SearchIcon())
	r.search.OnChanged = func(term string) { r.find(term) }
	r.search.Disable()

	r.contents = widget.NewList(
		func() int { return len(r.entries) },
		func() fyne.CanvasObject { return newEntryRow() },
		func(id widget.ListItemID, item fyne.CanvasObject) { r.fillEntry(item.(*entryRow), r.entries[id]) },
	)
	r.contents.OnSelected = func(id widget.ListItemID) {
		r.contents.Unselect(id)
		entry := r.entries[id]
		if entry.Dir {
			r.search.SetText("")
			r.open(entry.Path)
			return
		}
		r.toggle(entry, !r.isSelected(entry.Path))
	}
	r.status = container.NewCenter(muted(t("restore.pick_backup_hint")))

	header := container.NewBorder(nil, nil, r.up,
		container.NewGridWrap(fyne.NewSize(260, r.search.MinSize().Height), r.search),
		container.NewPadded(r.location))
	return card(container.NewBorder(
		container.NewVBox(header, widget.NewSeparator()), nil, nil, nil,
		container.NewStack(r.contents, r.status),
	))
}

// entryRow est une ligne du contenu : la case de sélection, l'icône, le
// nom, sa taille et sa date.
type entryRow struct {
	widget.BaseWidget
	check   *widget.Check
	icon    *bubble
	name    *text
	detail  *text
	size    *text
	content fyne.CanvasObject
}

func newEntryRow() *entryRow {
	r := &entryRow{
		check:  widget.NewCheck("", nil),
		icon:   newBubble(theme.FileIcon(), toneNeutral, 28),
		name:   newText("", theme.SizeNameText, theme.ColorNameForeground, fyne.TextStyle{}),
		detail: newText("", sizeSmall, colorMuted, fyne.TextStyle{}),
		size:   newText("", sizeSmall, colorMuted, fyne.TextStyle{}),
	}
	r.name.truncate = true
	r.detail.truncate = true
	r.content = container.NewBorder(nil, nil,
		container.NewHBox(r.check, r.icon), container.NewCenter(r.size),
		container.NewVBox(layout.NewSpacer(), r.name, r.detail, layout.NewSpacer()))
	r.ExtendBaseWidget(r)
	return r
}

func (r *entryRow) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(r.content)
}

// fillEntry remplit une ligne du contenu.
func (r *restoreView) fillEntry(row *entryRow, entry history.Entry) {
	t := r.u.t
	row.check.OnChanged = nil
	row.check.SetChecked(r.isSelected(entry.Path))
	row.check.OnChanged = func(on bool) { r.toggle(entry, on) }
	if entry.Dir {
		row.icon.Set(theme.FolderIcon(), toneInfo)
	} else {
		row.icon.Set(theme.FileIcon(), toneNeutral)
	}
	row.name.SetText(entry.Name())
	switch {
	case r.searched != "":
		// Un résultat de recherche dit où il se trouve.
		row.detail.SetText(r.native(entry.Parent()))
	case !entry.Modified.IsZero():
		row.detail.SetText(t("restore.modified", map[string]any{"Date": entry.Modified.Local().Format("2006-01-02 15:04")}))
	default:
		row.detail.SetText("")
	}
	row.size.SetText(format.Size(entry.Size))
	// La taille change de largeur avec son texte : la ligne se remet en
	// page.
	row.content.Refresh()
}

// actionsCard porte la sélection, la destination et les boutons.
func (r *restoreView) actionsCard() fyne.CanvasObject {
	t := r.u.t
	r.summary = newText(t("restore.nothing_selected"), theme.SizeNameText, theme.ColorNameForeground, fyne.TextStyle{Bold: true})
	r.summary.truncate = true

	newFolder, original := t("restore.to_new_folder"), t("restore.to_original")
	r.target = widget.NewRadioGroup([]string{newFolder, original}, func(string) { r.refreshActions() })
	r.target.Horizontal = true
	r.target.Required = true

	r.folderLabel = newText("", sizeSmall, colorMuted, fyne.TextStyle{Monospace: true})
	r.folderLabel.truncate = true
	r.change = widget.NewButtonWithIcon(t("restore.change_folder"), theme.FolderOpenIcon(), r.chooseParent)
	r.change.Importance = widget.LowImportance

	r.restoreSelection = widget.NewButtonWithIcon(t("restore.restore_selection"), theme.DownloadIcon(), func() { r.confirm(false) })
	r.restoreSelection.Importance = widget.HighImportance
	r.restoreAll = widget.NewButtonWithIcon(t("restore.restore_all"), theme.DownloadIcon(), func() { r.confirm(true) })
	r.cancel = widget.NewButtonWithIcon(t("backup_screen.cancel"), theme.CancelIcon(), func() {
		if r.stop != nil {
			r.cancelled = true
			r.cancel.Disable()
			r.stop()
		}
	})
	r.cancel.Importance = widget.DangerImportance
	r.cancel.Hide()
	r.progress = widget.NewProgressBar()
	r.progress.Hide()
	r.result = container.NewVBox()

	destination := container.NewBorder(nil, nil, nil, r.change, container.NewPadded(r.folderLabel))
	buttons := container.NewHBox(r.cancel, r.restoreAll, r.restoreSelection)
	// Le dossier neuf est présélectionné (EF-95). Le choix déclenche la mise
	// à jour des boutons : il vient une fois ceux-ci construits.
	r.target.SetSelected(newFolder)
	return container.NewVBox(spacer(4), card(container.NewVBox(
		container.NewBorder(nil, nil, nil, buttons, container.NewVBox(layout.NewSpacer(), r.summary, layout.NewSpacer())),
		widget.NewSeparator(),
		container.NewBorder(nil, nil, caption(strings.ToUpper(t("restore.where"))), nil, r.target),
		destination,
		r.progress,
		r.result,
	)))
}

// load lit la liste des sauvegardes, et ce que l'historique en sait.
func (r *restoreView) load() {
	t := r.u.t
	r.setBackupsStatus(container.NewCenter(container.NewVBox(
		widget.NewProgressBarInfinite(), muted(t("restore.loading_backups")))))
	var (
		list *borg.ArchiveList
		runs map[string]history.Run
		err  error
	)
	async(func() {
		if r.profile, err = r.u.st.Profile(); err != nil {
			return
		}
		if r.profile.Encryption.Encrypted() {
			if _, err = r.u.st.Secrets().Get(r.profile.Name); errors.Is(err, secret.ErrNotFound) {
				return
			}
		}
		if r.runner, err = r.u.st.Runner(); err != nil {
			return
		}
		if r.env, err = r.u.st.Environment(r.profile); err != nil {
			return
		}
		if list, _, err = borg.List(context.Background(), r.runner, r.env); err != nil {
			return
		}
		runs = r.knownRuns()
	}, func() {
		switch {
		case errors.Is(err, secret.ErrNotFound):
			r.setBackupsStatus(r.u.explanation("backup_screen.passphrase_missing", nil, ""))
			return
		case err != nil:
			retry := widget.NewButtonWithIcon(t("restore.retry"), theme.ViewRefreshIcon(), r.load)
			r.setBackupsStatus(container.NewVBox(r.u.explanation(errorKey(err), nil, err.Error()), container.NewHBox(retry)))
			return
		}
		r.archives, r.runs = list.Archives, runs
		sort.SliceStable(r.archives, func(i, j int) bool {
			return r.archives[i].Start.After(r.archives[j].Start.Time)
		})
		r.count.Set(fmt.Sprint(len(r.archives)), toneInfo)
		if len(r.archives) == 0 {
			r.setBackupsStatus(container.NewCenter(muted(t("restore.no_backups"))))
			return
		}
		r.setBackupsStatus(nil)
		r.backups.Refresh()
		// La plus récente est la plus souvent cherchée : elle est ouverte
		// d'emblée.
		r.backups.Select(0)
	})
}

// knownRuns relit l'historique de ce poste, par nom de sauvegarde. Un
// historique illisible prive seulement la liste de ses détails.
func (r *restoreView) knownRuns() map[string]history.Run {
	runs := map[string]history.Run{}
	store, err := r.u.st.History()
	if err != nil {
		return runs
	}
	defer store.Close()
	recent, err := store.Recent(backgroundContext(), r.profile.Name, 1000)
	if err != nil {
		return runs
	}
	for _, run := range recent {
		if run.Archive != "" {
			runs[run.Archive] = run
		}
	}
	return runs
}

// setBackupsStatus remplace la liste des sauvegardes par un message, ou la
// rétablit si status est nil.
func (r *restoreView) setBackupsStatus(status fyne.CanvasObject) {
	r.backupsAt.RemoveAll()
	if status == nil {
		r.backupsAt.Add(r.backups)
		return
	}
	r.backupsAt.Add(container.NewPadded(status))
}

// choose ouvre une sauvegarde : son catalogue est relevé au premier accès,
// puis relu du cache (EF-93).
func (r *restoreView) choose(archive borg.Archive) {
	t := r.u.t
	r.archive = &archive
	r.selected = map[string]history.Entry{}
	r.dir, r.searched = "", ""
	r.search.SetText("")
	r.search.Disable()
	r.entries = nil
	r.contents.Refresh()
	r.location.SetText(format.When(t, archive.Start.Time, time.Now()))
	r.folder = t("restore.folder_name", map[string]any{
		"Date": archive.Start.Local().Format("2006-01-02 15-04"),
	})
	r.refreshActions()
	r.showStatus(container.NewVBox(widget.NewProgressBarInfinite(), muted(t("restore.loading_contents"))))

	r.generation++
	generation := r.generation
	var err error
	async(func() {
		var store *history.Store
		if store, err = r.u.st.History(); err != nil {
			return
		}
		defer store.Close()
		_, err = core.OpenCatalog(context.Background(), r.runner, r.env, store, archive, time.Now())
	}, func() {
		if generation != r.generation {
			return
		}
		if err != nil {
			r.showStatus(r.u.explanation(errorKey(err), nil, err.Error()))
			return
		}
		r.search.Enable()
		r.open("")
	})
}

// open affiche le contenu d'un dossier de la sauvegarde.
func (r *restoreView) open(dir string) {
	r.dir, r.searched = dir, ""
	r.query(func(store *history.Store) ([]history.Entry, error) {
		return store.Children(backgroundContext(), r.archive.ID, dir)
	})
}

// find cherche un nom dans toute la sauvegarde (EF-92).
func (r *restoreView) find(term string) {
	if r.archive == nil {
		return
	}
	if strings.TrimSpace(term) == "" {
		r.open(r.dir)
		return
	}
	r.searched = term
	r.query(func(store *history.Store) ([]history.Entry, error) {
		return store.Search(backgroundContext(), r.archive.ID, term, searchLimit)
	})
}

// query lit des entrées du catalogue hors du fil de l'interface, puis les
// affiche.
func (r *restoreView) query(read func(*history.Store) ([]history.Entry, error)) {
	t := r.u.t
	r.generation++
	generation := r.generation
	var (
		entries []history.Entry
		err     error
	)
	async(func() {
		var store *history.Store
		if store, err = r.u.st.History(); err != nil {
			return
		}
		defer store.Close()
		entries, err = read(store)
	}, func() {
		if generation != r.generation {
			return
		}
		if err != nil {
			r.showStatus(r.u.explanation("error.unknown", nil, err.Error()))
			return
		}
		r.entries = entries
		r.contents.ScrollToTop()
		r.contents.Refresh()
		switch {
		case r.searched != "":
			r.location.SetText(t("restore.results", map[string]any{"Count": len(entries), "Term": r.searched}))
		case r.dir == "":
			r.location.SetText(t("restore.root"))
		default:
			r.location.SetText(r.native(r.dir))
		}
		if r.dir == "" || r.searched != "" {
			r.up.Disable()
		} else {
			r.up.Enable()
		}
		switch {
		case len(entries) > 0:
			r.showStatus(nil)
		case r.searched != "":
			r.showStatus(muted(t("restore.no_results")))
		default:
			r.showStatus(muted(t("restore.empty_folder")))
		}
	})
}

// showStatus affiche un message à la place du contenu, ou le retire.
func (r *restoreView) showStatus(status fyne.CanvasObject) {
	r.status.RemoveAll()
	if status == nil {
		r.status.Hide()
		return
	}
	r.status.Add(status)
	r.status.Show()
}

// native traduit un chemin de la sauvegarde en chemin du poste, tel que
// l'utilisateur le connaît.
func (r *restoreView) native(path string) string {
	if path == "" {
		return r.u.t("restore.root")
	}
	if native, _, err := r.runner.Origin(path); err == nil {
		return native
	}
	return path
}

// parentDir est le dossier parent d'un chemin de la sauvegarde.
func parentDir(path string) string {
	return history.Entry{Path: path}.Parent()
}

// isSelected indique qu'un élément, ou l'un des dossiers qui le contiennent,
// est choisi.
func (r *restoreView) isSelected(path string) bool {
	for p := path; p != ""; p = parentDir(p) {
		if _, ok := r.selected[p]; ok {
			return true
		}
	}
	return false
}

// toggle choisit ou écarte un élément.
func (r *restoreView) toggle(entry history.Entry, on bool) {
	if on {
		r.selected[entry.Path] = entry
	} else {
		delete(r.selected, entry.Path)
	}
	r.contents.Refresh()
	r.refreshActions()
}

// selection retourne les chemins choisis, sans ceux déjà couverts par un
// dossier choisi.
func (r *restoreView) selection() []string {
	var paths []string
	for path := range r.selected {
		if !r.isSelected(parentDir(path)) || parentDir(path) == "" {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
}

// refreshActions met à jour le résumé de la sélection, la destination et
// les boutons.
func (r *restoreView) refreshActions() {
	t := r.u.t
	paths := r.selection()
	var size int64
	for _, path := range paths {
		size += r.selected[path].Size
	}
	if len(paths) == 0 {
		r.summary.SetText(t("restore.nothing_selected"))
	} else {
		r.summary.SetText(t("restore.selected", map[string]any{"Count": len(paths), "Size": format.Size(size)}))
	}

	inPlace := r.inPlace()
	if inPlace {
		r.folderLabel.SetText(t("restore.original_hint"))
		r.change.Hide()
	} else {
		r.folderLabel.SetText(filepath.Join(r.parent, r.folder))
		r.change.Show()
	}

	running := r.stop != nil
	ready := r.archive != nil && !running
	setEnabled(r.restoreSelection, ready && len(paths) > 0)
	// Tout rendre à l'emplacement d'origine écraserait d'un coup tout ce qui
	// a changé depuis : seule une sélection y est restaurée.
	setEnabled(r.restoreAll, ready && !inPlace)
}

// inPlace indique une restauration à l'emplacement d'origine.
func (r *restoreView) inPlace() bool {
	return r.target.Selected == r.u.t("restore.to_original")
}

func setEnabled(button *widget.Button, enabled bool) {
	if enabled {
		button.Enable()
	} else {
		button.Disable()
	}
}

// chooseParent change le dossier où le dossier neuf est créé.
func (r *restoreView) chooseParent() {
	dialog.ShowFolderOpen(func(folder fyne.ListableURI, err error) {
		if err != nil || folder == nil {
			return
		}
		r.parent = folder.Path()
		r.refreshActions()
	}, r.win)
}

// confirm demande confirmation d'une restauration à l'emplacement
// d'origine, en énonçant ce qui sera écrasé (EF-96) ; un dossier neuf ne
// risque rien et part aussitôt.
func (r *restoreView) confirm(all bool) {
	var paths []string
	if !all {
		paths = r.selection()
	}
	if !r.inPlace() {
		r.run(paths, filepath.Join(r.parent, r.folder))
		return
	}

	t := r.u.t
	existing, err := core.Overwritten(r.runner, paths)
	if err != nil {
		r.showResult(r.u.explanation("error.unknown", nil, err.Error()))
		return
	}
	date := r.archive.Start.Local().Format("2006-01-02 15:04")
	var message string
	if len(existing) == 0 {
		message = t("restore.confirm_none", map[string]any{"Date": date})
	} else {
		const shown = 8
		lines := existing
		if len(lines) > shown {
			lines = lines[:shown]
		}
		message = t("restore.confirm_overwrite", map[string]any{"Date": date, "Count": len(existing)}) +
			"\n\n" + strings.Join(lines, "\n")
		if more := len(existing) - len(lines); more > 0 {
			message += "\n" + t("restore.confirm_more", map[string]any{"Count": more})
		}
	}
	body := widget.NewLabel(message)
	body.Wrapping = fyne.TextWrapWord
	confirm := dialog.NewCustomConfirm(t("restore.confirm_title"), t("restore.confirm_replace"), t("restore.confirm_cancel"),
		container.NewVScroll(body), func(ok bool) {
			if ok {
				r.run(paths, "")
			}
		}, r.win)
	if len(existing) == 0 {
		confirm.Resize(fyne.NewSize(520, 220))
	} else {
		confirm.Resize(fyne.NewSize(620, 380))
	}
	confirm.Show()
}

// run restaure, hors du fil de l'interface.
func (r *restoreView) run(paths []string, destination string) {
	ctx, stop := context.WithCancel(context.Background())
	r.stop, r.cancelled = stop, false
	r.result.RemoveAll()
	r.progress.SetValue(0)
	r.progress.Show()
	r.cancel.Enable()
	r.cancel.Show()
	r.refreshActions()

	inPlace := destination == ""
	var (
		result *borg.Result
		err    error
	)
	async(func() {
		defer stop()
		result, err = core.Restore(ctx, r.runner, core.RestoreRequest{
			Env:         r.env,
			Archive:     r.archive.Name,
			Paths:       paths,
			Destination: destination,
			InPlace:     inPlace,
			OnEvent:     r.progressHandler(),
		})
	}, func() {
		r.stop = nil
		r.progress.Hide()
		r.cancel.Hide()
		r.refreshActions()
		switch {
		case r.cancelled:
			r.showResult(r.u.explanation("restore.cancelled", nil, ""))
		case errors.Is(err, core.ErrDestinationNotEmpty):
			r.showResult(r.u.explanation("restore.folder_taken", map[string]any{"Path": destination}, ""))
		case err != nil:
			r.showResult(r.u.explanation(errorKey(err), nil, err.Error()))
		default:
			r.showDone(result, destination)
		}
	})
}

// progressHandler reçoit l'avancement de l'extraction, hors du fil de
// l'interface.
func (r *restoreView) progressHandler() func(borg.Event) {
	var last time.Time
	return func(event borg.Event) {
		percent, ok := event.Percent()
		if event.Kind != borg.EventProgressPercent || !ok || time.Since(last) < progressInterval {
			return
		}
		last = time.Now()
		onUI(func() { r.progress.SetValue(percent / 100) })
	}
}

// showDone annonce la fin de la restauration, avec de quoi ouvrir le
// dossier restauré.
func (r *restoreView) showDone(result *borg.Result, destination string) {
	t := r.u.t
	var content []fyne.CanvasObject
	if destination == "" {
		content = append(content, r.u.explanation("restore.done_original", nil, ""))
	} else {
		content = append(content, r.u.explanation("restore.done", map[string]any{"Path": destination}, ""))
		open := widget.NewButtonWithIcon(t("restore.open_folder"), theme.FolderOpenIcon(), func() {
			r.u.app.OpenURL(fileURL(destination))
		})
		content = append(content, container.NewHBox(open))
	}
	if result != nil && result.Status == borg.StatusWarning {
		var detail strings.Builder
		for _, warning := range result.Warnings() {
			detail.WriteString(warning.Text + "\n")
		}
		content = append(content, r.u.explanation("restore.warnings",
			map[string]any{"Count": len(result.Warnings())}, detail.String()))
	}
	r.showResult(container.NewVBox(content...))
	if destination != "" {
		// Le dossier neuf suivant ne doit pas buter sur celui-ci.
		r.folder = t("restore.folder_name", map[string]any{"Date": time.Now().Format("2006-01-02 15-04-05")})
		r.refreshActions()
	}
}

// showResult affiche l'issue de la restauration.
func (r *restoreView) showResult(content fyne.CanvasObject) {
	r.result.RemoveAll()
	r.result.Add(content)
}

// fileURL désigne un dossier local, pour l'ouvrir dans le gestionnaire de
// fichiers du système.
func fileURL(path string) *url.URL {
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		// C:/Users/marc devient /C:/Users/marc, forme attendue d'une URL.
		slashed = "/" + slashed
	}
	return &url.URL{Scheme: "file", Path: slashed}
}
