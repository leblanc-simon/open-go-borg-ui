package gui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// checkPanel présente un contrôle périodique de l'écran État —
// vérification de restauration, contrôle de la destination : une pastille,
// sa date, une phrase, le détail brut replié, et un bouton pour le mener
// sans attendre son échéance.
type checkPanel struct {
	u      *ui
	badge  *badge
	when   *text
	text   *widget.Label
	detail *fyne.Container
	button *widget.Button
	// relayout remet en page ce qui contient la section, quand elle change
	// de hauteur. Peut être nil.
	relayout func()

	content *fyne.Container
}

func newCheckPanel(u *ui, title, action string, onAction func()) *checkPanel {
	p := &checkPanel{
		u:      u,
		badge:  newBadge("", toneNeutral),
		when:   newText("", theme.SizeNameText, theme.ColorNameForeground, fyne.TextStyle{Bold: true}).abbreviated(),
		text:   widget.NewLabel(""),
		detail: container.NewVBox(),
		button: widget.NewButtonWithIcon(action, theme.ConfirmIcon(), onAction),
	}
	p.text.Wrapping = fyne.TextWrapWord
	p.text.Importance = widget.LowImportance
	p.button.Importance = widget.LowImportance
	p.content = container.NewVBox(
		container.NewBorder(nil, nil, caption(strings.ToUpper(title)), container.NewCenter(p.badge)),
		p.when,
		p.text,
		p.detail,
		container.NewHBox(p.button),
	)
	return p
}

// show affiche un état : pastille, date, phrase et, s'il y en a un, détail
// brut sous « Détails ».
func (p *checkPanel) show(label string, t tone, when, message, detail string) {
	p.badge.Set(strings.ToUpper(label), t)
	p.when.SetText(when)
	p.text.SetText(message)
	p.detail.RemoveAll()
	if detail != "" {
		p.detail.Add(p.u.details(detail))
	}
	// La pastille change de largeur avec son texte, et le dépliant
	// apparaît ou disparaît : la section, et ce qui la contient, se
	// remettent en page.
	p.content.Refresh()
	if p.relayout != nil {
		p.relayout()
	}
}

// setMessage remplace la phrase seule, pour la progression.
func (p *checkPanel) setMessage(message string) {
	p.text.SetText(message)
}
