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
	// Reconfigure distingue une reprise de l'assistant, depuis les réglages
	// d'un poste déjà configuré, du premier lancement.
	Reconfigure Reconfiguration `json:"reconfigure,omitempty"`
	// NewKey : la reconfiguration crée une nouvelle clé de connexion plutôt
	// que de garder celle du poste.
	NewKey bool `json:"new_key,omitempty"`
}

// Reconfiguration est la raison d'une reprise de l'assistant.
type Reconfiguration string

const (
	// FirstRun : premier lancement, le poste n'est pas encore configuré.
	FirstRun Reconfiguration = ""
	// ChangeDestination : le poste change de destination ; ses dossiers et
	// sa planification restent.
	ChangeDestination Reconfiguration = "destination"
	// ImportConfig : la configuration d'un autre poste remplace celle-ci
	// (EF-101).
	ImportConfig Reconfiguration = "import"
)

// pendingSuffix distingue le mot de passe d'une destination en cours de
// préparation de celui de la destination en service.
const pendingSuffix = ".pending"

// ForDestination prépare le changement de destination d'un poste configuré.
// Le profil courant est repris tel quel — dossiers, exclusions,
// planification — et seule la destination est redemandée. Le chiffrement
// recommandé est présélectionné, comme au premier lancement (PA-04).
func ForDestination(current config.Profile, newKey bool) *State {
	s := &State{Profile: current, Reconfigure: ChangeDestination, NewKey: newKey}
	s.Profile.Encryption = config.EncryptionRepokey
	s.Step = s.first()
	return s
}

// ForImport prépare le remplacement de la configuration du poste par celle
// d'un autre (EF-101) : seuls le sous-compte et, le cas échéant, le mot de
// passe restent à fournir, puis les dossiers se vérifient, puisqu'ils
// viennent d'un autre poste.
func ForImport(imported config.Profile, newKey bool) *State {
	s := &State{Profile: imported, Imported: true, Reconfigure: ImportConfig, NewKey: newKey}
	s.Step = s.first()
	return s
}

// first retourne la première étape qui a lieu d'être.
func (s *State) first() Step {
	for _, step := range order {
		if !s.skipped(step) {
			return step
		}
	}
	return StepFinish
}

// SecretName est le nom sous lequel le mot de passe de la destination en
// préparation est rangé. Au premier lancement, c'est celui du profil. En
// reconfiguration, c'est un nom provisoire : le mot de passe de la
// destination en service reste intact jusqu'à la validation, et les
// sauvegardes planifiées continuent d'y accéder si l'utilisateur abandonne.
func (s *State) SecretName() string {
	if s.Reconfigure == FirstRun {
		return s.Profile.Name
	}
	return s.Profile.Name + pendingSuffix
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
	if s.Reconfigure != FirstRun {
		switch step {
		case StepWelcome, StepEngine:
			// Le poste est configuré : son moteur est en place.
			return true
		case StepFolders, StepSchedule:
			// Changer de destination ne touche ni aux dossiers ni à la
			// planification ; une configuration importée, si.
			return s.Reconfigure == ChangeDestination
		}
	}
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

// ReconfigurePath retourne le fichier d'une reconfiguration en cours. Il est
// distinct de celui du premier lancement : une reconfiguration interrompue
// ne se reprend pas, le poste restant configuré comme avant.
func ReconfigurePath(stateDir string) string { return filepath.Join(stateDir, "reconfigure.json") }

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
