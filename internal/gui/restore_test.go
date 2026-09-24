//go:build !windows

package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"

	"leblanc.io/open-go-borg-ui/internal/history"
)

// restoreScript simule une destination qui contient une sauvegarde : sa
// liste, son contenu, et l'extraction dans le répertoire courant. Chaque
// lecture du contenu est comptée.
const restoreScript = `
case "$*" in
*--json-lines*)
  echo x >> "$HOME/contents-read"
  echo '{"type": "d", "path": "home/marc/Documents", "size": 0, "mtime": "2026-09-01T10:00:00.000000"}'
  echo '{"type": "-", "path": "home/marc/Documents/Lettre à Paul.odt", "size": 2048, "mtime": "2026-09-03T22:14:07.000000"}'
  echo '{"type": "-", "path": "home/marc/Documents/budget.ods", "size": 512, "mtime": "2026-09-02T08:00:00.000000"}' ;;
*" list "*)
  echo '{"archives": [{"name": "poste-2026-09-24T22:00:00", "id": "a1b2", "start": "2026-09-24T22:00:00.000000", "time": "2026-09-24T22:03:00.000000"}], "encryption": {"mode": "none"}}' ;;
*extract*)
  echo "$*" > "$HOME/extract-args"
  mkdir -p "home/marc/Documents"
  echo restauré > "home/marc/Documents/Lettre à Paul.odt" ;;
esac
exit 0
`

// TestRestauration parcourt la fenêtre de restauration : la sauvegarde la
// plus récente s'ouvre d'elle-même, on y navigue, on y cherche, puis un
// fichier choisi est restauré dans un dossier neuf (EF-90 à EF-95).
func TestRestauration(t *testing.T) {
	st := poste(t, restoreScript, true)
	u, _, tr := open(t, st)
	home := os.Getenv("HOME")

	u.openRestore()
	r := u.restore
	waitFor(t, "le contenu de la sauvegarde", func() bool {
		return len(r.entries) == 1 && r.entries[0].Path == "home"
	})
	if r.location.Text != tr("restore.root") || !r.up.Disabled() {
		t.Errorf("racine: %q", r.location.Text)
	}

	r.open("home/marc/Documents")
	waitFor(t, "le dossier", func() bool { return len(r.entries) == 2 })
	if r.location.Text != "/home/marc/Documents" || r.up.Disabled() {
		t.Errorf("emplacement affiché: %q", r.location.Text)
	}

	// Recherche sans casse, accents compris.
	r.search.SetText("LETTRE À")
	waitFor(t, "la recherche", func() bool { return r.searched != "" && len(r.entries) == 1 })
	letter := r.entries[0]
	if letter.Name() != "Lettre à Paul.odt" {
		t.Fatalf("résultat: %+v", letter)
	}

	if !r.restoreSelection.Disabled() {
		t.Error("rien n'est choisi : « Restaurer la sélection » doit être grisé")
	}
	r.toggle(letter, true)
	if r.restoreSelection.Disabled() {
		t.Fatal("un fichier est choisi : « Restaurer la sélection » doit être actif")
	}

	r.parent = t.TempDir()
	target := filepath.Join(r.parent, r.folder)
	test.Tap(r.restoreSelection)
	waitFor(t, "la fin de la restauration", func() bool {
		return strings.Contains(texts(r.result), tr("restore.done", map[string]any{"Path": target}))
	})
	if data, err := os.ReadFile(filepath.Join(target, "home/marc/Documents/Lettre à Paul.odt")); err != nil || strings.TrimSpace(string(data)) != "restauré" {
		t.Errorf("fichier restauré: %q, %v", data, err)
	}
	args, _ := os.ReadFile(filepath.Join(home, "extract-args"))
	if !strings.Contains(string(args), "home/marc/Documents/Lettre à Paul.odt") || strings.Contains(string(args), "budget") {
		t.Errorf("seul le fichier choisi est demandé: %s", args)
	}

	// Rouvrir la sauvegarde relit le cache, sans interroger Borg (EF-93).
	r.choose(r.archives[0])
	waitFor(t, "la réouverture", func() bool { return len(r.entries) == 1 && r.searched == "" })
	if reads, _ := os.ReadFile(filepath.Join(home, "contents-read")); strings.Count(string(reads), "x") != 1 {
		t.Errorf("contenu lu %d fois auprès de Borg, attendu une seule", strings.Count(string(reads), "x"))
	}
}

// TestSelectionDossier vérifie qu'un dossier choisi couvre son contenu, qui
// n'est alors pas demandé une seconde fois.
func TestSelectionDossier(t *testing.T) {
	u, _, _ := open(t, poste(t, restoreScript, true))
	u.openRestore()
	r := u.restore
	waitFor(t, "le contenu", func() bool { return len(r.entries) == 1 })

	r.toggle(history.Entry{Path: "home/marc", Dir: true, Size: 2560}, true)
	r.toggle(history.Entry{Path: "home/marc/Documents/budget.ods", Size: 512}, true)
	if got := r.selection(); len(got) != 1 || got[0] != "home/marc" {
		t.Errorf("sélection: %v", got)
	}
	if !r.isSelected("home/marc/Documents/Lettre à Paul.odt") {
		t.Error("le contenu d'un dossier choisi doit apparaître coché")
	}
}
