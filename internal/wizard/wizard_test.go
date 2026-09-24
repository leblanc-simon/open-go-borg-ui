package wizard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"leblanc.io/open-go-borg-ui/internal/config"
)

// walk parcourt l'assistant jusqu'au bout et retourne les étapes vues.
func walk(s *State) string {
	seen := []string{string(s.Step)}
	for s.Step != StepFinish {
		s.Next()
		seen = append(seen, string(s.Step))
	}
	return strings.Join(seen, " ")
}

// TestParcours vérifie les étapes selon la destination (EF-10, EF-34, EF-39).
func TestParcours(t *testing.T) {
	all := "welcome engine destination encryption folders schedule recovery_key finish"

	chiffree := New("poste")
	if got := walk(chiffree); got != all {
		t.Errorf("destination neuve chiffrée: %s", got)
	}

	claire := New("poste")
	claire.Profile.Encryption = config.EncryptionNone
	if got := walk(claire); got != "welcome engine destination encryption folders schedule finish" {
		t.Errorf("destination neuve non chiffrée: %s", got)
	}

	existanteClaire := New("poste")
	existanteClaire.RepositoryExists = true
	existanteClaire.Profile.Encryption = config.EncryptionNone
	if got := walk(existanteClaire); got != "welcome engine destination folders schedule finish" {
		t.Errorf("destination existante non chiffrée: %s", got)
	}

	// Une destination existante chiffrée garde l'étape du chiffrement : le
	// mode n'y est pas redemandé, mais la passphrase doit être saisie.
	existanteChiffree := New("poste")
	existanteChiffree.RepositoryExists = true
	if got := walk(existanteChiffree); got != all {
		t.Errorf("destination existante chiffrée: %s", got)
	}
}

// TestRetour vérifie le retour en arrière, étapes sautées comprises.
func TestRetour(t *testing.T) {
	s := New("poste")
	s.RepositoryExists = true
	s.Profile.Encryption = config.EncryptionNone
	s.Step = StepFolders
	s.Previous()
	if s.Step != StepDestination {
		t.Errorf("retour depuis les dossiers: %s", s.Step)
	}
	if current, total := s.Position(); current != 3 || total != 6 {
		t.Errorf("position %d sur %d", current, total)
	}
}

// TestReprise vérifie qu'un assistant interrompu reprend où il s'était
// arrêté (EF-11), sans aucun secret dans son fichier.
func TestReprise(t *testing.T) {
	path := Path(t.TempDir())
	if state, err := Load(path); state != nil || err != nil {
		t.Fatalf("sans assistant: %v, %v", state, err)
	}

	s := New("poste")
	s.Step = StepFolders
	s.Profile.Destination.User = "u123456"
	s.Profile.Sources = []string{"/home/marc/Documents"}
	s.RepositoryReady = true
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}

	resumed, err := Load(path)
	if err != nil || resumed == nil {
		t.Fatalf("reprise: %v", err)
	}
	if resumed.Step != StepFolders || resumed.Profile.Destination.User != "u123456" ||
		!resumed.RepositoryReady || resumed.Profile.Sources[0] != "/home/marc/Documents" {
		t.Errorf("état repris: %+v", resumed)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(strings.ToLower(string(data)), "passphrase") {
		t.Errorf("le fichier de l'assistant ne doit contenir aucun secret:\n%s", data)
	}

	if err := Clear(path); err != nil {
		t.Fatal(err)
	}
	if state, _ := Load(path); state != nil {
		t.Error("l'assistant terminé doit disparaître")
	}
}

// TestFichierAbime vérifie qu'un fichier illisible fait repartir de zéro
// plutôt que bloquer le premier lancement.
func TestFichierAbime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wizard.json")
	os.WriteFile(path, []byte("{tronqué"), 0o600)
	if state, err := Load(path); state != nil || err != nil {
		t.Errorf("fichier abîmé: %v, %v", state, err)
	}
}

// TestChangementDeDestination vérifie qu'un changement de destination ne
// redemande que la destination, le chiffrement et la clé de secours, et
// garde tout le reste du profil.
func TestChangementDeDestination(t *testing.T) {
	current := config.Default("poste")
	current.Encryption = config.EncryptionNone
	current.Sources = []string{"/home/marc/Documents"}
	s := ForDestination(current, true)

	got := walk(s)
	if got != "destination encryption recovery_key finish" {
		t.Errorf("étapes: %s", got)
	}
	s.Step = StepDestination
	if current, total := s.Position(); current != 1 || total != 4 {
		t.Errorf("position: %d sur %d", current, total)
	}
	if s.Profile.Encryption != config.EncryptionRepokey || len(s.Profile.Sources) != 1 || !s.NewKey {
		t.Errorf("profil repris: %+v", s)
	}
	if s.SecretName() != "poste.pending" {
		t.Errorf("mot de passe provisoire: %q", s.SecretName())
	}
}

// TestImportDepuisLesReglages vérifie qu'une configuration importée repasse
// par les dossiers et la planification, qui viennent d'un autre poste.
func TestImportDepuisLesReglages(t *testing.T) {
	s := ForImport(config.Default("poste"), false)
	if got := walk(s); got != "destination encryption folders schedule recovery_key finish" {
		t.Errorf("étapes: %s", got)
	}
	if !s.Imported {
		t.Error("un import ne redemande que le sous-compte (EF-101)")
	}
	if first := New("poste"); first.SecretName() != "poste" {
		t.Errorf("premier lancement: %q", first.SecretName())
	}
}
