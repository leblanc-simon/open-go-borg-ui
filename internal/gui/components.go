package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Les composants de ce fichier sont des widgets plutôt que de simples objets
// du canevas : Fyne repeint les widgets quand le thème change, et leurs
// couleurs suivent ainsi le passage du clair au sombre.

// tone est la couleur sémantique d'une pastille, d'une bulle ou d'un texte.
type tone int

const (
	toneNeutral tone = iota
	toneInfo
	toneSuccess
	toneWarning
	toneError
)

// colorName retourne la couleur du thème associée au ton.
func (t tone) colorName() fyne.ThemeColorName {
	switch t {
	case toneInfo:
		return theme.ColorNamePrimary
	case toneSuccess:
		return theme.ColorNameSuccess
	case toneWarning:
		return theme.ColorNameWarning
	case toneError:
		return theme.ColorNameError
	default:
		return colorMuted
	}
}

// soft retourne la couleur du ton, atténuée pour servir de fond.
func (t tone) soft() color.Color {
	c := color.NRGBAModel.Convert(theme.Color(t.colorName())).(color.NRGBA)
	c.A = 0x2a
	return c
}

// surface est un fond arrondi : celui d'une carte, d'une pastille, d'une
// entrée de menu.
type surface struct {
	widget.BaseWidget
	fill   func() color.Color
	stroke func() color.Color
	// radius est le rayon des coins ; négatif, celui des cartes du thème.
	radius float32
	min    fyne.Size
}

// newSurface crée un fond aux couleurs nommées du thème ; stroke peut être
// vide.
func newSurface(fill, stroke fyne.ThemeColorName) *surface {
	s := &surface{radius: -1, fill: func() color.Color { return theme.Color(fill) }}
	if stroke != "" {
		s.stroke = func() color.Color { return theme.Color(stroke) }
	}
	s.ExtendBaseWidget(s)
	return s
}

func (s *surface) CreateRenderer() fyne.WidgetRenderer {
	r := &surfaceRenderer{s: s, rect: canvas.NewRectangle(color.Transparent)}
	r.Refresh()
	return r
}

type surfaceRenderer struct {
	s    *surface
	rect *canvas.Rectangle
}

func (r *surfaceRenderer) Layout(size fyne.Size)        { r.rect.Resize(size) }
func (r *surfaceRenderer) MinSize() fyne.Size           { return r.s.min }
func (r *surfaceRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.rect} }
func (r *surfaceRenderer) Destroy()                     {}

func (r *surfaceRenderer) Refresh() {
	r.rect.FillColor = color.Transparent
	if r.s.fill != nil {
		r.rect.FillColor = r.s.fill()
	}
	r.rect.StrokeColor, r.rect.StrokeWidth = color.Transparent, 0
	if r.s.stroke != nil {
		r.rect.StrokeColor, r.rect.StrokeWidth = r.s.stroke(), 1
	}
	r.rect.CornerRadius = r.s.radius
	if r.s.radius < 0 {
		r.rect.CornerRadius = theme.Size(sizeCardRadius)
	}
	r.rect.Refresh()
}

// text est un texte d'une ligne dont la taille et la couleur viennent du
// thème : grand chiffre, intitulé en capitales, légende atténuée.
type text struct {
	widget.BaseWidget
	Text  string
	color fyne.ThemeColorName
	size  fyne.ThemeSizeName
	style fyne.TextStyle
	// fit réduit la taille du texte quand la place manque : un grand
	// chiffre reste entier.
	fit bool
	// truncate abrège le texte d'une ellipse quand la place manque.
	truncate bool
}

func newText(value string, size fyne.ThemeSizeName, color fyne.ThemeColorName, style fyne.TextStyle) *text {
	t := &text{Text: value, size: size, color: color, style: style}
	t.ExtendBaseWidget(t)
	return t
}

// SetText remplace le texte.
func (t *text) SetText(value string) {
	t.Text = value
	t.Refresh()
}

// SetColor remplace la couleur.
func (t *text) SetColor(color fyne.ThemeColorName) {
	t.color = color
	t.Refresh()
}

func (t *text) CreateRenderer() fyne.WidgetRenderer {
	r := &textRenderer{t: t, text: canvas.NewText("", color.Transparent)}
	r.Refresh()
	return r
}

type textRenderer struct {
	t    *text
	text *canvas.Text
}

// minFitSize est la taille en deçà de laquelle un texte ajustable ne se
// réduit plus.
const minFitSize = 12

func (r *textRenderer) Layout(size fyne.Size) {
	full := theme.Size(r.t.size)
	r.text.Text, r.text.TextSize = r.t.Text, full
	natural := fyne.MeasureText(r.t.Text, full, r.t.style).Width
	if natural > size.Width && size.Width > 0 {
		switch {
		case r.t.fit:
			r.text.TextSize = max(minFitSize, full*size.Width/natural)
		case r.t.truncate:
			r.text.Text = ellipsize(r.t.Text, full, r.t.style, size.Width)
		}
	}
	r.text.Resize(size)
	r.text.Refresh()
}

func (r *textRenderer) MinSize() fyne.Size {
	full := theme.Size(r.t.size)
	natural := fyne.MeasureText(r.t.Text, full, r.t.style)
	switch {
	case r.t.fit:
		natural.Width = fyne.MeasureText(r.t.Text, minFitSize, r.t.style).Width
	case r.t.truncate:
		natural.Width = 0
	}
	return natural
}

// ellipsize abrège value pour qu'il tienne dans width, ellipse comprise.
func ellipsize(value string, size float32, style fyne.TextStyle, width float32) string {
	runes := []rune(value)
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]
		candidate := string(runes) + "…"
		if fyne.MeasureText(candidate, size, style).Width <= width {
			return candidate
		}
	}
	return ""
}
func (r *textRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.text} }
func (r *textRenderer) Destroy()                     {}

func (r *textRenderer) Refresh() {
	r.text.Color = theme.Color(r.t.color)
	r.text.TextStyle = r.t.style
	r.Layout(r.t.Size())
}

// badge est une pastille : un état en capitales, sur un fond de sa couleur.
type badge struct {
	widget.BaseWidget
	label string
	tone  tone
}

func newBadge(label string, t tone) *badge {
	b := &badge{label: label, tone: t}
	b.ExtendBaseWidget(b)
	return b
}

// Set remplace le texte et la couleur de la pastille.
func (b *badge) Set(label string, t tone) {
	b.label, b.tone = label, t
	b.Refresh()
}

func (b *badge) CreateRenderer() fyne.WidgetRenderer {
	r := &badgeRenderer{b: b, rect: canvas.NewRectangle(color.Transparent), text: canvas.NewText("", color.Transparent)}
	r.Refresh()
	return r
}

type badgeRenderer struct {
	b    *badge
	rect *canvas.Rectangle
	text *canvas.Text
}

// badgePadding est la marge intérieure d'une pastille.
var badgePadding = fyne.NewSize(8, 3)

func (r *badgeRenderer) Layout(size fyne.Size) {
	r.rect.Resize(size)
	textSize := r.text.MinSize()
	r.text.Move(fyne.NewPos((size.Width-textSize.Width)/2, (size.Height-textSize.Height)/2))
	r.text.Resize(textSize)
}

func (r *badgeRenderer) MinSize() fyne.Size {
	return r.text.MinSize().Add(badgePadding).Add(badgePadding)
}

func (r *badgeRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.rect, r.text} }
func (r *badgeRenderer) Destroy()                     {}

func (r *badgeRenderer) Refresh() {
	r.rect.FillColor = r.b.tone.soft()
	r.rect.CornerRadius = 6
	r.text.Text = r.b.label
	r.text.Color = theme.Color(r.b.tone.colorName())
	r.text.TextSize = theme.Size(sizeSmall)
	r.text.TextStyle = fyne.TextStyle{Bold: true}
	r.rect.Refresh()
	r.text.Refresh()
}

// bubble est une icône dans un disque de sa couleur.
type bubble struct {
	widget.BaseWidget
	disc    *surface
	icon    *widget.Icon
	tone    tone
	content fyne.CanvasObject
}

func newBubble(resource fyne.Resource, t tone, diameter float32) *bubble {
	b := &bubble{tone: t}
	b.disc = &surface{radius: diameter / 2, min: fyne.NewSquareSize(diameter)}
	b.disc.fill = func() color.Color { return b.tone.soft() }
	b.disc.ExtendBaseWidget(b.disc)
	b.icon = widget.NewIcon(theme.NewColoredResource(resource, t.colorName()))
	b.content = container.NewStack(b.disc, container.NewCenter(b.icon))
	b.ExtendBaseWidget(b)
	return b
}

func (b *bubble) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(b.content)
}

// Set remplace l'icône et sa couleur.
func (b *bubble) Set(resource fyne.Resource, t tone) {
	b.tone = t
	b.icon.SetResource(theme.NewColoredResource(resource, t.colorName()))
	b.disc.Refresh()
}

// card pose un contenu sur une carte : fond légèrement plus clair que la
// page, coins arrondis, fine bordure.
func card(content fyne.CanvasObject) fyne.CanvasObject {
	return container.NewStack(
		newSurface(colorCard, colorCardBorder),
		container.New(layout.NewCustomPaddedLayout(16, 16, 18, 18), content),
	)
}

// codeBlock présente un texte technique à copier — une clé, une adresse —
// en police à chasse fixe, dans un encart.
func codeBlock(label *widget.Label) fyne.CanvasObject {
	label.Wrapping = fyne.TextWrapBreak
	label.TextStyle = fyne.TextStyle{Monospace: true}
	back := newSurface(colorInset, colorCardBorder)
	back.radius = 8
	return container.NewStack(back, container.NewPadded(label))
}

// caption est un intitulé de section, en petites capitales atténuées.
func caption(value string) *text {
	return newText(value, sizeSmall, colorMuted, fyne.TextStyle{Bold: true})
}

// abbreviated rend le texte abrégeable, pour une colonne étroite. Un texte
// abrégeable n'a plus de largeur minimale : il ne convient qu'à une place
// dont la largeur est imposée, pas au côté d'une bordure ni à une ligne.
func (t *text) abbreviated() *text {
	t.truncate = true
	return t
}

// muted est une ligne de texte atténuée.
func muted(value string) *text {
	return newText(value, theme.SizeNameText, colorMuted, fyne.TextStyle{})
}

// pageHeader est l'en-tête d'une page : titre, phrase d'explication, et à
// droite ses actions principales.
func pageHeader(title, subtitle string, actions ...fyne.CanvasObject) fyne.CanvasObject {
	heading := newText(title, theme.SizeNameHeadingText, theme.ColorNameForeground, fyne.TextStyle{Bold: true})
	// La phrase revient à la ligne plutôt que d'imposer sa largeur à la
	// fenêtre.
	explanation := widget.NewLabel(subtitle)
	explanation.Wrapping = fyne.TextWrapWord
	explanation.Importance = widget.LowImportance
	// Le titre prend la marge intérieure de l'étiquette, pour s'aligner sur
	// elle.
	inset := theme.Size(theme.SizeNameInnerPadding)
	left := container.NewVBox(container.New(layout.NewCustomPaddedLayout(0, 0, inset, 0), heading), explanation)
	if len(actions) == 0 {
		return left
	}
	return container.NewBorder(nil, nil, nil, container.NewVBox(layout.NewSpacer(), container.NewHBox(actions...), layout.NewSpacer()), left)
}

// page assemble une page : son en-tête, puis son corps, avec les marges de
// la zone de contenu.
func page(header, body fyne.CanvasObject) fyne.CanvasObject {
	return container.New(layout.NewCustomPaddedLayout(20, 16, 28, 28),
		container.NewBorder(container.NewVBox(header, spacer(8)), nil, nil, nil, body))
}

// column empile des cartes dans une colonne qui défile, avec l'écart
// qu'il faut entre elles et la marge qui garde leur bordure visible.
func column(cards ...fyne.CanvasObject) fyne.CanvasObject {
	stack := container.NewVBox()
	for i, c := range cards {
		if i > 0 {
			stack.Add(spacer(6))
		}
		stack.Add(c)
	}
	return container.NewVScroll(container.New(layout.NewCustomPaddedLayout(0, 2, 0, 2), stack))
}

// split place side à droite de main quand la place le permet, et dessous
// sinon : une page à deux colonnes reste ainsi utilisable dans une fenêtre
// étroite ou agrandie à 150 % (EI-07), sans imposer sa largeur à la
// fenêtre.
func split(main, side fyne.CanvasObject, sideWidth float32) *fyne.Container {
	return container.New(&splitLayout{side: sideWidth}, main, side)
}

// splitMainWidth est la largeur en deçà de laquelle la colonne principale
// ne partage plus la ligne.
const splitMainWidth = 380

type splitLayout struct {
	side float32
	// stacked indique la disposition retenue au dernier placement.
	stacked bool
}

func (l *splitLayout) gap() float32 { return 2 * theme.Padding() }

func (l *splitLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	main, side := objects[0], objects[1]
	sideWidth := max(l.side, side.MinSize().Width)
	mainWidth := size.Width - sideWidth - l.gap()
	l.stacked = mainWidth < max(splitMainWidth, main.MinSize().Width)
	if !l.stacked {
		main.Move(fyne.NewPos(0, 0))
		main.Resize(fyne.NewSize(mainWidth, size.Height))
		side.Move(fyne.NewPos(mainWidth+l.gap(), 0))
		side.Resize(fyne.NewSize(sideWidth, size.Height))
		return
	}
	// Empilées : la colonne principale prend au moins la moitié de la
	// hauteur — davantage si son contenu l'exige —, la secondaire le reste.
	// Une colonne qui défile n'a presque pas de hauteur minimale : sans ce
	// partage, elle disparaîtrait.
	mainHeight := max(main.MinSize().Height, (size.Height-l.gap())/2)
	sideHeight := max(0, size.Height-mainHeight-l.gap())
	main.Move(fyne.NewPos(0, 0))
	main.Resize(fyne.NewSize(size.Width, mainHeight))
	side.Move(fyne.NewPos(0, mainHeight+l.gap()))
	side.Resize(fyne.NewSize(size.Width, sideHeight))
}

func (l *splitLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	// La hauteur minimale est celle de la disposition côte à côte : une
	// fois empilée, la colonne secondaire défile dans ce qui reste.
	main, side := objects[0].MinSize(), objects[1].MinSize()
	return fyne.NewSize(max(main.Width, side.Width), max(main.Height, side.Height))
}

// spacer est un espace vertical fixe.
func spacer(height float32) fyne.CanvasObject {
	s := canvas.NewRectangle(color.Transparent)
	s.SetMinSize(fyne.NewSize(0, height))
	return s
}

// navItem est une entrée de la barre latérale : icône et libellé, surlignés
// quand la page est affichée. Elle se manipule aussi au clavier (EI-07).
type navItem struct {
	widget.BaseWidget
	icon     fyne.Resource
	label    string
	selected bool
	hovered  bool
	focused  bool
	onTapped func()
}

var (
	_ fyne.Tappable      = (*navItem)(nil)
	_ fyne.Focusable     = (*navItem)(nil)
	_ desktop.Hoverable  = (*navItem)(nil)
	_ desktop.Cursorable = (*navItem)(nil)
)

func newNavItem(icon fyne.Resource, label string, tapped func()) *navItem {
	n := &navItem{icon: icon, label: label, onTapped: tapped}
	n.ExtendBaseWidget(n)
	return n
}

// SetSelected marque l'entrée de la page affichée.
func (n *navItem) SetSelected(selected bool) {
	n.selected = selected
	n.Refresh()
}

func (n *navItem) Tapped(*fyne.PointEvent) {
	if n.onTapped != nil {
		n.onTapped()
	}
}

func (n *navItem) Cursor() desktop.Cursor         { return desktop.PointerCursor }
func (n *navItem) MouseIn(*desktop.MouseEvent)    { n.hovered = true; n.Refresh() }
func (n *navItem) MouseMoved(*desktop.MouseEvent) {}
func (n *navItem) MouseOut()                      { n.hovered = false; n.Refresh() }
func (n *navItem) FocusGained()                   { n.focused = true; n.Refresh() }
func (n *navItem) FocusLost()                     { n.focused = false; n.Refresh() }
func (n *navItem) TypedRune(rune)                 {}
func (n *navItem) TypedKey(event *fyne.KeyEvent) {
	if event.Name == fyne.KeySpace || event.Name == fyne.KeyReturn || event.Name == fyne.KeyEnter {
		n.Tapped(nil)
	}
}

func (n *navItem) CreateRenderer() fyne.WidgetRenderer {
	r := &navItemRenderer{
		n:    n,
		back: canvas.NewRectangle(color.Transparent),
		icon: widget.NewIcon(n.icon),
		text: canvas.NewText(n.label, color.Transparent),
	}
	r.Refresh()
	return r
}

type navItemRenderer struct {
	n    *navItem
	back *canvas.Rectangle
	icon *widget.Icon
	text *canvas.Text
}

// navItemHeight est la hauteur d'une entrée de menu.
const navItemHeight = 40

func (r *navItemRenderer) Layout(size fyne.Size) {
	r.back.Resize(size)
	iconSize := theme.Size(theme.SizeNameInlineIcon)
	r.icon.Move(fyne.NewPos(14, (size.Height-iconSize)/2))
	r.icon.Resize(fyne.NewSquareSize(iconSize))
	textSize := r.text.MinSize()
	r.text.Move(fyne.NewPos(14+iconSize+12, (size.Height-textSize.Height)/2))
	r.text.Resize(textSize)
}

func (r *navItemRenderer) MinSize() fyne.Size {
	return fyne.NewSize(14+theme.Size(theme.SizeNameInlineIcon)+12+r.text.MinSize().Width+14, navItemHeight)
}

func (r *navItemRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.back, r.icon, r.text}
}

func (r *navItemRenderer) Destroy() {}

func (r *navItemRenderer) Refresh() {
	foreground := theme.ColorNameForeground
	switch {
	case r.n.selected:
		r.back.FillColor = theme.Color(colorNavActive)
		foreground = theme.ColorNamePrimary
	case r.n.hovered:
		r.back.FillColor = theme.Color(theme.ColorNameHover)
	default:
		r.back.FillColor = color.Transparent
	}
	r.back.StrokeColor, r.back.StrokeWidth = color.Transparent, 0
	if r.n.focused {
		r.back.StrokeColor, r.back.StrokeWidth = theme.Color(theme.ColorNameFocus), 1
	}
	r.back.CornerRadius = 8
	r.icon.SetResource(theme.NewColoredResource(r.n.icon, foreground))
	r.text.Text = r.n.label
	r.text.Color = theme.Color(foreground)
	r.text.TextStyle = fyne.TextStyle{Bold: r.n.selected}
	r.back.Refresh()
	r.text.Refresh()
}
