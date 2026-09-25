//go:build !windows

package gui

import (
	"os"
	"path/filepath"
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
	waitFor(t, "l'état initial", func() bool { return h.verification.when.Text == tr("verify.never_title") })

	test.Tap(h.verification.button)
	waitFor(t, "la vérification", func() bool { return strings.HasPrefix(h.verification.when.Text, "Vérifiée le") })
	// L'original n'existe pas sur le poste de test : le fichier a été
	// extrait, sans comparaison possible.
	if want := tr("verify.original_missing", map[string]any{"Path": "/home/marc/notes.txt"}); h.verification.text.Text != want {
		t.Errorf("issue affichée: %q, attendu %q", h.verification.text.Text, want)
	}
	if h.verification.badge.label != strings.ToUpper(tr("verify.badge_extracted")) {
		t.Errorf("pastille: %q", h.verification.badge.label)
	}
}

// checkScript simule une destination dont le contrôle est sain, sauf si un
// fichier « anomalies » existe.
const checkScript = `
case "$*" in
*check*)
  if [ -f "$HOME/anomalies" ]; then
    echo '{"type":"log_message","levelname":"ERROR","message":"segment 12: checksum mismatch"}' >&2
    exit 1
  fi ;;
esac
exit 0
`

// TestControleDepuisLEcranEtat vérifie EF-87 sur l'écran État : « Contrôler
// maintenant » mène le contrôle et en affiche l'issue ; des anomalies font
// passer le bandeau à l'orange.
func TestControleDepuisLEcranEtat(t *testing.T) {
	u, _, tr := open(t, poste(t, checkScript, true))
	h := u.home
	waitFor(t, "l'état initial", func() bool { return h.control.when.Text == tr("check.never_title") })

	test.Tap(h.control.button)
	waitFor(t, "le contrôle", func() bool { return strings.HasPrefix(h.control.when.Text, "Contrôlée le") })
	if h.control.badge.label != strings.ToUpper(tr("check.badge_healthy")) || h.control.button.Text != tr("check.now") {
		t.Errorf("destination saine: %q, bouton %q", h.control.badge.label, h.control.button.Text)
	}

	os.WriteFile(filepath.Join(os.Getenv("HOME"), "anomalies"), nil, 0o600)
	test.Tap(h.control.button)
	waitFor(t, "les anomalies", func() bool { return strings.HasPrefix(h.control.when.Text, "Anomalies trouvées") })
	if h.control.badge.label != strings.ToUpper(tr("check.badge_problems")) {
		t.Errorf("pastille: %q", h.control.badge.label)
	}
	if !strings.Contains(texts(h.control.detail), "segment 12") {
		t.Errorf("détail des anomalies absent:\n%s", texts(h.control.detail))
	}
}
