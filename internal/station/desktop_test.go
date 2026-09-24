//go:build !windows

package station

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBureau vérifie la lecture du Bureau localisé, et le repli sur le
// dossier personnel.
func TestBureau(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	if got := DesktopDir(); got != home {
		t.Errorf("sans Bureau: %q, attendu le dossier personnel", got)
	}

	os.MkdirAll(filepath.Join(home, ".config"), 0o700)
	os.MkdirAll(filepath.Join(home, "Bureau"), 0o700)
	os.WriteFile(filepath.Join(home, ".config", "user-dirs.dirs"),
		[]byte("# généré\nXDG_DESKTOP_DIR=\"$HOME/Bureau\"\n"), 0o600)
	if got := DesktopDir(); got != filepath.Join(home, "Bureau") {
		t.Errorf("Bureau localisé: %q", got)
	}
}
