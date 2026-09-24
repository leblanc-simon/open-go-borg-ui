package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Couleurs propres à l'application, résolues par le thème comme celles de
// Fyne : elles suivent donc le passage du clair au sombre.
const (
	colorSidebar    fyne.ThemeColorName = "borgui.sidebar"
	colorCard       fyne.ThemeColorName = "borgui.card"
	colorCardBorder fyne.ThemeColorName = "borgui.cardBorder"
	colorInset      fyne.ThemeColorName = "borgui.inset"
	colorMuted      fyne.ThemeColorName = "borgui.muted"
	colorNavActive  fyne.ThemeColorName = "borgui.navActive"
)

// Tailles propres à l'application.
const (
	sizeDisplay    fyne.ThemeSizeName = "borgui.display"
	sizeCardRadius fyne.ThemeSizeName = "borgui.cardRadius"
	sizeSmall      fyne.ThemeSizeName = "borgui.small"
)

// themeMode est le choix d'apparence de l'utilisateur.
type themeMode string

const (
	themeSystem themeMode = "system"
	themeLight  themeMode = "light"
	themeDark   themeMode = "dark"
)

// themePreference est la clé de préférence qui retient ce choix.
const themePreference = "theme.mode"

// next retourne le mode suivant, dans l'ordre où le bouton les propose.
func (m themeMode) next() themeMode {
	switch m {
	case themeSystem:
		return themeLight
	case themeLight:
		return themeDark
	default:
		return themeSystem
	}
}

// borguiTheme est le thème de l'application : fond bleu nuit, cartes
// arrondies, un seul bleu d'accent. Il suit le système, sauf si
// l'utilisateur a choisi le clair ou le sombre.
type borguiTheme struct {
	mode themeMode
}

var _ fyne.Theme = (*borguiTheme)(nil)

// palette associe une couleur à chaque nom, pour une variante.
type palette map[fyne.ThemeColorName]color.Color

func hex(value uint32) color.NRGBA {
	return color.NRGBA{R: uint8(value >> 16), G: uint8(value >> 8), B: uint8(value), A: 0xff}
}

func alpha(c color.NRGBA, a uint8) color.NRGBA {
	c.A = a
	return c
}

var darkPalette = palette{
	theme.ColorNameBackground:          hex(0x0f172a),
	theme.ColorNameForeground:          hex(0xe2e8f0),
	theme.ColorNameForegroundOnPrimary: hex(0xffffff),
	theme.ColorNamePrimary:             hex(0x3b82f6),
	theme.ColorNameButton:              hex(0x273449),
	theme.ColorNameDisabledButton:      hex(0x1e293b),
	theme.ColorNameDisabled:            hex(0x64748b),
	theme.ColorNamePlaceHolder:         hex(0x64748b),
	theme.ColorNameHover:               alpha(hex(0xffffff), 0x10),
	theme.ColorNamePressed:             alpha(hex(0xffffff), 0x20),
	theme.ColorNameFocus:               alpha(hex(0x3b82f6), 0x90),
	theme.ColorNameSelection:           alpha(hex(0x3b82f6), 0x40),
	theme.ColorNameInputBackground:     hex(0x0b1222),
	theme.ColorNameInputBorder:         hex(0x334155),
	theme.ColorNameMenuBackground:      hex(0x1e293b),
	theme.ColorNameOverlayBackground:   hex(0x1e293b),
	theme.ColorNameHeaderBackground:    hex(0x1e293b),
	theme.ColorNameSeparator:           hex(0x1e293b),
	theme.ColorNameScrollBar:           alpha(hex(0x94a3b8), 0x60),
	theme.ColorNameShadow:              alpha(hex(0x000000), 0x66),
	theme.ColorNameSuccess:             hex(0x22c55e),
	theme.ColorNameWarning:             hex(0xf59e0b),
	theme.ColorNameError:               hex(0xef4444),
	theme.ColorNameHyperlink:           hex(0x60a5fa),

	colorSidebar:    hex(0x0b1222),
	colorCard:       hex(0x1e293b),
	colorCardBorder: hex(0x2c3a50),
	colorInset:      hex(0x172033),
	colorMuted:      hex(0x94a3b8),
	colorNavActive:  alpha(hex(0x3b82f6), 0x26),
}

var lightPalette = palette{
	theme.ColorNameBackground:          hex(0xf1f5f9),
	theme.ColorNameForeground:          hex(0x0f172a),
	theme.ColorNameForegroundOnPrimary: hex(0xffffff),
	theme.ColorNamePrimary:             hex(0x2563eb),
	theme.ColorNameButton:              hex(0xe2e8f0),
	theme.ColorNameDisabledButton:      hex(0xe2e8f0),
	theme.ColorNameDisabled:            hex(0x94a3b8),
	theme.ColorNamePlaceHolder:         hex(0x94a3b8),
	theme.ColorNameHover:               alpha(hex(0x0f172a), 0x0c),
	theme.ColorNamePressed:             alpha(hex(0x0f172a), 0x18),
	theme.ColorNameFocus:               alpha(hex(0x2563eb), 0x80),
	theme.ColorNameSelection:           alpha(hex(0x2563eb), 0x30),
	theme.ColorNameInputBackground:     hex(0xffffff),
	theme.ColorNameInputBorder:         hex(0xcbd5e1),
	theme.ColorNameMenuBackground:      hex(0xffffff),
	theme.ColorNameOverlayBackground:   hex(0xffffff),
	theme.ColorNameHeaderBackground:    hex(0xf8fafc),
	theme.ColorNameSeparator:           hex(0xe2e8f0),
	theme.ColorNameScrollBar:           alpha(hex(0x64748b), 0x60),
	theme.ColorNameShadow:              alpha(hex(0x0f172a), 0x22),
	theme.ColorNameSuccess:             hex(0x16a34a),
	theme.ColorNameWarning:             hex(0xd97706),
	theme.ColorNameError:               hex(0xdc2626),
	theme.ColorNameHyperlink:           hex(0x2563eb),

	colorSidebar:    hex(0xffffff),
	colorCard:       hex(0xffffff),
	colorCardBorder: hex(0xe2e8f0),
	colorInset:      hex(0xf8fafc),
	colorMuted:      hex(0x64748b),
	colorNavActive:  alpha(hex(0x2563eb), 0x1a),
}

// variant retourne la variante à peindre : celle choisie, sinon celle du
// système.
func (b *borguiTheme) variant(system fyne.ThemeVariant) fyne.ThemeVariant {
	switch b.mode {
	case themeLight:
		return theme.VariantLight
	case themeDark:
		return theme.VariantDark
	default:
		return system
	}
}

func (b *borguiTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	colors := darkPalette
	if b.variant(variant) == theme.VariantLight {
		colors = lightPalette
	}
	if c, ok := colors[name]; ok {
		return c
	}
	return theme.DefaultTheme().Color(name, b.variant(variant))
}

func (b *borguiTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

func (b *borguiTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (b *borguiTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case sizeDisplay:
		return 26
	case sizeCardRadius:
		return 12
	case sizeSmall:
		return 11
	case theme.SizeNameHeadingText:
		return 26
	case theme.SizeNameSubHeadingText:
		return 17
	case theme.SizeNameInputRadius, theme.SizeNameSelectionRadius:
		return 8
	case theme.SizeNameScrollBarRadius:
		return 4
	}
	return theme.DefaultTheme().Size(name)
}

// applyTheme installe le thème de l'application selon le choix retenu.
func (u *ui) applyTheme() {
	u.app.Settings().SetTheme(&borguiTheme{mode: u.themeMode()})
}

// themeMode retourne le choix d'apparence retenu.
func (u *ui) themeMode() themeMode {
	return themeMode(u.app.Preferences().StringWithFallback(themePreference, string(themeSystem)))
}

// cycleTheme passe au mode d'apparence suivant et le retient.
func (u *ui) cycleTheme() themeMode {
	mode := u.themeMode().next()
	u.app.Preferences().SetString(themePreference, string(mode))
	u.applyTheme()
	return mode
}
