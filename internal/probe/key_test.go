package probe

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestGenerationDeLaCle vérifie la création de la clé dédiée à l'application :
// une clé ed25519 sans passphrase, en permissions restreintes, accompagnée de
// sa forme publique (EF-23, SEC-03).
func TestGenerationDeLaCle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cles", "id_ed25519")

	signer, err := EnsureKey(path)
	if err != nil {
		t.Fatalf("EnsureKey a échoué: %v", err)
	}
	if got := signer.PublicKey().Type(); got != "ssh-ed25519" {
		t.Errorf("type de clé = %q, attendu ssh-ed25519", got)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("clé absente: %v", err)
	}
	// Sous Windows, la clé est réservée à son propriétaire par une liste de
	// contrôle d'accès, que les bits de permission ne reflètent pas :
	// internal/fsperm l'éprouve.
	if mode := info.Mode().Perm(); runtime.GOOS != "windows" && mode != 0o600 {
		t.Errorf("permissions de la clé = %o, attendues 600", mode)
	}

	public, err := PublicKey(path)
	if err != nil {
		t.Fatalf("PublicKey a échoué: %v", err)
	}
	// C'est cette ligne que l'utilisateur colle dans la console Hetzner : le
	// port 23 attend le format OpenSSH.
	if !strings.HasPrefix(public, "ssh-ed25519 AAAA") {
		t.Errorf("clé publique inattendue: %q", public)
	}
	if _, err := os.Stat(path + ".pub"); err != nil {
		t.Errorf("la clé publique doit être écrite à côté de la privée: %v", err)
	}
}

// TestCleConservee vérifie qu'une clé existante n'est jamais remplacée : la
// regénérer invaliderait l'accès à toutes les destinations déjà configurées.
func TestCleConservee(t *testing.T) {
	path := filepath.Join(t.TempDir(), "id_ed25519")

	first, err := EnsureKey(path)
	if err != nil {
		t.Fatalf("première génération: %v", err)
	}
	second, err := EnsureKey(path)
	if err != nil {
		t.Fatalf("seconde ouverture: %v", err)
	}

	if string(first.PublicKey().Marshal()) != string(second.PublicKey().Marshal()) {
		t.Error("la clé a été régénérée alors qu'elle existait déjà")
	}
}
