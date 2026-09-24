package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"leblanc.io/open-go-borg-ui/internal/station"
)

// screen désigne un écran de la fenêtre principale (EI-01).
type screen int

const (
	screenHome screen = iota
	screenBackup
	screenDestination
)

// sidebarWidth est la largeur de la barre latérale.
const sidebarWidth = 224

// shell est le cadre de la fenêtre principale : la barre latérale à gauche,
// l'écran choisi à droite. Les écrans restent construits quand ils sont
// masqués : une sauvegarde en cours continue de s'afficher au retour.
type shell struct {
	u       *ui
	items   []*navItem
	screens []fyne.CanvasObject
	current screen

	content fyne.CanvasObject
}

// entry décrit une entrée du menu.
type entry struct {
	icon    fyne.Resource
	label   string
	content fyne.CanvasObject
}

func newShell(u *ui, entries []entry) *shell {
	s := &shell{u: u}
	menu := container.NewVBox()
	for i, e := range entries {
		index := screen(i)
		item := newNavItem(e.icon, e.label, func() { s.show(index) })
		s.items = append(s.items, item)
		s.screens = append(s.screens, e.content)
		menu.Add(item)
	}
	stack := container.NewStack(s.screens...)

	s.content = container.NewBorder(nil, nil, s.sidebar(menu), nil, stack)
	s.show(screenHome)
	return s
}

// sidebar construit la barre latérale : l'application, le menu, puis en bas
// le poste et le choix d'apparence.
func (s *shell) sidebar(menu fyne.CanvasObject) fyne.CanvasObject {
	t := s.u.t

	brand := container.NewHBox(logo(44), container.NewVBox(
		layout.NewSpacer(),
		newText(t("window.title"), theme.SizeNameSubHeadingText, theme.ColorNameForeground, fyne.TextStyle{Bold: true}),
		newText(t("sidebar.tagline"), sizeSmall, colorMuted, fyne.TextStyle{Bold: true}),
		layout.NewSpacer(),
	))

	appearance := widget.NewButtonWithIcon(t("theme."+string(s.u.themeMode())), theme.ColorPaletteIcon(), nil)
	appearance.Importance = widget.LowImportance
	appearance.Alignment = widget.ButtonAlignLeading
	appearance.OnTapped = func() {
		appearance.SetText(t("theme." + string(s.u.cycleTheme())))
	}

	// La restauration est une fenêtre à part (EI-01) : son entrée l'ouvre
	// sans quitter l'écran affiché.
	restore := newNavItem(theme.DownloadIcon(), t("sidebar.restore"), s.u.openRestore)
	top := container.NewVBox(brand, spacer(18), container.NewPadded(caption(t("sidebar.section"))), menu,
		spacer(10), container.NewPadded(caption(t("sidebar.recover"))), restore)
	bottom := container.NewVBox(
		widget.NewSeparator(),
		container.NewPadded(container.NewVBox(caption(t("sidebar.computer")), muted(station.Hostname()))),
		appearance,
	)

	background := newSurface(colorSidebar, "")
	background.radius = 0
	background.min = fyne.NewSize(sidebarWidth, 0)
	return container.NewBorder(nil, nil, nil, widget.NewSeparator(), container.NewStack(
		background,
		container.New(layout.NewCustomPaddedLayout(18, 12, 12, 12), container.NewBorder(top, bottom, nil, nil)),
	))
}

// show affiche un écran et surligne son entrée.
func (s *shell) show(target screen) {
	s.current = target
	for i, item := range s.items {
		item.SetSelected(screen(i) == target)
		if screen(i) == target {
			s.screens[i].Show()
		} else {
			s.screens[i].Hide()
		}
	}
	if target == screenHome && s.u.home != nil {
		s.u.home.refresh()
	}
}
