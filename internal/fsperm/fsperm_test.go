//go:build !windows

package fsperm

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEcriturePrivee vérifie qu'un fichier déjà trop ouvert est ramené au
// seul propriétaire en étant réécrit : os.WriteFile seul ne change pas le
// mode d'un fichier existant.
func TestEcriturePrivee(t *testing.T) {
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, []byte("ancienne"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WritePrivate(path, []byte("clé")); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	content, _ := os.ReadFile(path)
	if info.Mode().Perm() != 0o600 || string(content) != "clé" {
		t.Errorf("mode %v, contenu %q", info.Mode().Perm(), content)
	}
}

// TestRestriction vérifie la réparation d'un fichier existant.
func TestRestriction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	os.WriteFile(path, nil, 0o750)
	if err := Restrict(path); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", info.Mode().Perm())
	}
}
