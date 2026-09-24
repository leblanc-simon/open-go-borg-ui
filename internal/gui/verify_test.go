//go:build !windows

package gui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"
)

// verifyScript simule une destination dont la sauvegarde la plus récente
// contient un seul fichier, que l'extraction restitue.
const verifyScript = `
case "$*" in
*--json-lines*)
  echo '{"type": "-", "path": "home/marc/notes.txt", "size": 8, "mtime": "2026-09-03T22:14:07.000000"}' ;;
*" list "*)
  echo '{"archives": [{"name": "poste-2026-09-24T22:00:00", "id": "a1b2", "start": "2026-09-24T22:00:00.000000"}], "encryption": {"mode": "none"}}' ;;
*extract*)
  mkdir -p home/marc && printf 'bonjour\n' > home/marc/notes.txt ;;
esac
exit 0
`

// TestVerificationDepuisLEcranEtat vérifie EF-99 sur l'écran État : sans
// vérification, il annonce la prochaine ; « Vérifier maintenant » en mène
// une et en affiche l'issue.
func TestVerificationDepuisLEcranEtat(t *testing.T) {
	u, _, tr := open(t, poste(t, verifyScript, true))
	h := u.home
	waitFor(t, "l'état initial", func() bool { return h.verifyWhen.Text == tr("verify.never_title") })

	test.Tap(h.verifyButton)
	waitFor(t, "la vérification", func() bool { return strings.HasPrefix(h.verifyWhen.Text, "Vérifiée le") })
	// L'original n'existe pas sur le poste de test : le fichier a été
	// extrait, sans comparaison possible.
	if want := tr("verify.original_missing", map[string]any{"Path": "/home/marc/notes.txt"}); h.verifyText.Text != want {
		t.Errorf("issue affichée: %q, attendu %q", h.verifyText.Text, want)
	}
	if h.verifyBadge.label != strings.ToUpper(tr("verify.badge_extracted")) {
		t.Errorf("pastille: %q", h.verifyBadge.label)
	}
}
