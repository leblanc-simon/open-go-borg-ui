package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestURLHetzner couvre la déduction de l'adresse d'une Storage Box : seul le
// sous-compte est demandé à l'utilisateur, le reste s'en déduit (EF-21).
func TestURLHetzner(t *testing.T) {
	destination := Destination{Kind: KindHetzner, User: "u123456", Repo: "poste-marc"}

	url, err := destination.RepositoryURL()
	if err != nil {
		t.Fatalf("RepositoryURL a échoué: %v", err)
	}

	want := "ssh://u123456@u123456.your-storagebox.de:23/./poste-marc"
	if url != want {
		t.Errorf("RepositoryURL = %q, attendu %q", url, want)
	}
	// Le chemin relatif est significatif : seul /home est inscriptible sur une
	// Storage Box, et l'oublier est une cause classique d'échec.
	if !strings.Contains(url, "/./") {
		t.Error("l'adresse doit désigner un chemin relatif")
	}
}

// TestURLIncomplete vérifie qu'une destination incomplète est refusée avant
// tout appel réseau.
func TestURLIncomplete(t *testing.T) {
	cases := map[string]Destination{
		"sans sous-compte": {Kind: KindHetzner, Repo: "poste"},
		"sans nom":         {Kind: KindHetzner, User: "u1"},
		"sans adresse":     {Kind: KindSSH},
		"type inconnu":     {Kind: Kind("ftp"), Repo: "poste"},
	}
	for name, destination := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := destination.RepositoryURL(); err == nil {
				t.Error("une erreur était attendue")
			}
		})
	}
}

// TestEcritureRelecture vérifie qu'une configuration écrite se relit à
// l'identique : le fichier reste la référence, y compris après modification à
// la main.
func TestEcritureRelecture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	profile := Default("Poste de Marc")
	profile.Destination.User = "u123456"
	profile.Destination.Repo = "poste-marc"
	profile.Sources = []string{"/home/marc/Documents", "/home/marc/Projets"}
	profile.Excludes = []string{"**/node_modules", "**/*.iso"}

	if err := Save(path, &Config{Profiles: []Profile{profile}}); err != nil {
		t.Fatalf("Save a échoué: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load a échoué: %v", err)
	}
	got, err := loaded.Profile("Poste de Marc")
	if err != nil {
		t.Fatalf("Profile a échoué: %v", err)
	}

	if got.Destination.RemotePath != DefaultRemotePath {
		t.Errorf("remote_path = %q, attendu %q", got.Destination.RemotePath, DefaultRemotePath)
	}
	if got.Retention != (Retention{Daily: 7, Weekly: 4, Monthly: 6}) {
		t.Errorf("rétention = %+v, attendue 7/4/6", got.Retention)
	}
	if len(got.Excludes) != 2 || got.Excludes[0] != "**/node_modules" {
		t.Errorf("exclusions perdues: %+v", got.Excludes)
	}
	if !got.ExcludeCaches {
		t.Error("le respect de CACHEDIR.TAG doit rester actif par défaut")
	}
}

// TestProfilParDefaut vérifie la sélection du profil.
func TestProfilParDefaut(t *testing.T) {
	cfg := &Config{Profiles: []Profile{Default("premier"), Default("second")}}

	first, err := cfg.Profile("")
	if err != nil || first.Name != "premier" {
		t.Errorf("le premier profil doit être choisi par défaut: %v %v", first, err)
	}
	if _, err := cfg.Profile("absent"); err == nil {
		t.Error("un profil inexistant doit être signalé")
	}
}

// TestChiffrement vérifie la lecture des deux seuls modes exposés.
func TestChiffrement(t *testing.T) {
	if !EncryptionRepokey.Encrypted() {
		t.Error("le mode chiffré exige une passphrase")
	}
	if EncryptionNone.Encrypted() {
		t.Error("le mode non chiffré n'utilise aucun secret")
	}
	if EncryptionRepokey.BorgMode() != "repokey-blake2" {
		t.Errorf("mode Borg inattendu: %s", EncryptionRepokey.BorgMode())
	}
}
