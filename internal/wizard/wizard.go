// Package wizard tient l'état de l'assistant de premier lancement (EF-10,
// EF-11).
//
// Une question par écran : bienvenue, moteur, destination, chiffrement,
// dossiers, planification, clé de secours, première sauvegarde. L'état est
// enregistré à chaque étape : un assistant interrompu — fenêtre fermée,
// poste éteint — reprend là où il s'était arrêté. Aucun secret n'y figure :
// la passphrase part directement dans le trousseau.
package wizard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"leblanc.io/open-go-borg-ui/internal/config"
)

// Step est une étape de l'assistant.
type Step string

const (
	StepWelcome     Step = "welcome"
	StepEngine      Step = "engine"
	StepDestination Step = "destination"
	StepEncryption  Step = "encryption"
	StepFolders     Step = "folders"
	StepSchedule    Step = "schedule"
	StepRecoveryKey Step = "recovery_key"
	StepFinish      Step = "finish"
)

// order est l'ordre des étapes.
var order = []Step{
	StepWelcome, StepEngine, StepDestination, StepEncryption,
	StepFolders, StepSchedule, StepRecoveryKey, StepFinish,
}

// State est l'avancement de l'assistant.
type State struct {
	Step Step `json:"step"`
	// Profile est le profil en préparation. Il n'est écrit dans la
	// configuration qu'à la fin : jusque-là, le poste reste « non
	// configuré ».
	Profile config.Profile `json:"profile"`
	// Imported indique un profil importé d'un autre poste (EF-13) : seuls
	// le sous-compte et, le cas échéant, la passphrase restent à fournir.
	Imported bool `json:"imported,omitempty"`
	// DestinationChecked : la connexion à la destination a abouti.
	DestinationChecked bool `json:"destination_checked,omitempty"`
	// RepositoryExists : la destination contenait déjà des sauvegardes, et
	// son mode de chiffrement a été lu plutôt que demandé (EF-34).
	RepositoryExists bool `json:"repository_exists,omitempty"`
	// RepositoryReady : la destination est créée, ou existante et lisible.
	RepositoryReady bool `json:"repository_ready,omitempty"`
	// KeyConfirmed : l'utilisateur a confirmé avoir mis la clé de secours à
	// l'abri (EF-35).
	KeyConfirmed bool `json:"key_confirmed,omitempty"`
}

// New commence un assistant, avec le profil recommandé : chiffré (PA-04),
// conservation 7/4/6, rattrapage des exécutions manquées.
func New(hostname string) *State {
	profile := config.Default("poste")
	profile.Destination.Repo = hostname
	profile.Schedule = config.Schedule{Kind: "daily", At: "12:30", CatchUpIfMissed: true}
	return &State{Step: StepWelcome, Profile: profile}
}

// skipped indique si une étape n'a pas lieu d'être.
func (s *State) skipped(step Step) bool {
	switch step {
	case StepEncryption:
		// Une destination existante non chiffrée n'a ni mode à choisir
		// (EF-34) ni passphrase à saisir.
		return s.RepositoryExists && !s.Profile.Encryption.Encrypted()
	case StepRecoveryKey:
		// Sans chiffrement, il n'y a pas de clé de secours (EF-39).
		return !s.Profile.Encryption.Encrypted()
	}
	return false
}

// index retourne la position d'une étape.
func index(step Step) int {
	for i, s := range order {
		if s == step {
			return i
		}
	}
	return 0
}

// Next passe à l'étape suivante qui a lieu d'être.
func (s *State) Next() {
	for i := index(s.Step) + 1; i < len(order); i++ {
		if !s.skipped(order[i]) {
			s.Step = order[i]
			return
		}
	}
}

// Previous revient à l'étape précédente qui a lieu d'être.
func (s *State) Previous() {
	for i := index(s.Step) - 1; i >= 0; i-- {
		if !s.skipped(order[i]) {
			s.Step = order[i]
			return
		}
	}
}

// Position retourne le rang de l'étape courante et le nombre d'étapes, pour
// l'indication « étape 3 sur 8 ».
func (s *State) Position() (current, total int) {
	for _, step := range order {
		if s.skipped(step) {
			continue
		}
		total++
		if step == s.Step {
			current = total
		}
	}
	return current, total
}

// Path retourne le fichier de l'assistant dans le dossier d'état.
func Path(stateDir string) string { return filepath.Join(stateDir, "wizard.json") }

// Load relit l'assistant en cours, nil s'il n'y en a pas.
func Load(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("wizard: %w", err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		// Un fichier illisible ne doit pas bloquer le premier lancement :
		// l'assistant repart de zéro.
		return nil, nil
	}
	if index(state.Step) == 0 && state.Step != StepWelcome {
		state.Step = StepWelcome
	}
	return &state, nil
}

// Save enregistre l'assistant. Le fichier est écrit à côté puis renommé : un
// arrêt brutal pendant l'écriture laisse l'ancien état, jamais un fichier
// tronqué.
func (s *State) Save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("wizard: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("wizard: %w", err)
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return fmt.Errorf("wizard: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("wizard: %w", err)
	}
	return nil
}

// Clear retire l'assistant terminé.
func Clear(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("wizard: %w", err)
	}
	return nil
}
