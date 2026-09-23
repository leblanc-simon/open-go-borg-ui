package config

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// TestExportImport vérifie l'aller-retour d'un profil entre deux postes : les
// réglages suivent, ce qui est propre au poste d'origine reste en arrière.
func TestExportImport(t *testing.T) {
	origin := Default("bureau")
	origin.Destination.User = "u123456-sub1"
	origin.Destination.Repo = "bureau"
	origin.Destination.SSHKey = "/home/marc/.ssh/cle"
	origin.Sources = []string{"/home/marc/Documents"}
	origin.Excludes = []string{"**/node_modules"}
	origin.Retention = Retention{Daily: 14, Monthly: 12}
	origin.Schedule = Schedule{Kind: "daily", At: "21:00", CatchUpIfMissed: true}

	data, err := Export(origin, "Configuration exportée\nsans secret")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.HasPrefix(text, "# Configuration exportée\n# sans secret\n") {
		t.Errorf("en-tête:\n%s", text)
	}
	if strings.Contains(text, "u123456-sub1") || strings.Contains(text, "/home/marc/.ssh/cle") {
		t.Errorf("l'export contient ce qui est propre au poste:\n%s", text)
	}

	imported, err := Import(bytes.NewReader(data), "")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if imported.Name != "bureau" || imported.Destination.Repo != "bureau" ||
		imported.Retention != origin.Retention || imported.Schedule != origin.Schedule ||
		imported.Excludes[0] != "**/node_modules" || imported.Encryption != EncryptionRepokey {
		t.Errorf("profil importé: %+v", imported)
	}
	if imported.Destination.User != "" {
		t.Error("le sous-compte doit être redemandé")
	}
}

// TestImportNonTransportable vérifie qu'un fichier qui n'a pas été produit par
// l'export — configuration complète recopiée à la main — perd quand même ce
// qui appartient à l'autre poste.
func TestImportNonTransportable(t *testing.T) {
	src := `[[profile]]
name = "poste"
encryption = "none"
[profile.destination]
kind = "ssh"
repo = "ssh://marc@nas:22/./sauvegardes"
ssh_key = "C:\\Users\\marc\\cle"
`
	imported, err := Import(strings.NewReader(src), "")
	if err != nil {
		t.Fatal(err)
	}
	if imported.Destination.Repo != "" || imported.Destination.SSHKey != "" {
		t.Errorf("profil importé: %+v", imported.Destination)
	}
}

// TestImportInvalide vérifie les fichiers refusés.
func TestImportInvalide(t *testing.T) {
	cases := map[string]string{
		"pas du TOML":          "<html>",
		"sans profil":          "",
		"destination inconnue": "[[profile]]\nname = \"p\"\nencryption = \"none\"\n[profile.destination]\nkind = \"ftp\"\n",
		"chiffrement inconnu":  "[[profile]]\nname = \"p\"\nencryption = \"keyfile\"\n[profile.destination]\nkind = \"hetzner\"\n",
		"démesuré":             strings.Repeat("#", maxPortableSize+1),
	}
	for name, src := range cases {
		if _, err := Import(strings.NewReader(src), ""); !errors.Is(err, ErrNotPortable) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
