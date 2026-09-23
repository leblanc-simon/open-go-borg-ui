// Package statusfile publie l'état du poste sur sa destination (EF-83).
//
// À la fin de chaque exécution, le poste dépose status/<poste>.json par SFTP
// dans son propre sous-compte, à côté de son dépôt. Aucun autre poste ne le
// lit : l'état d'un poste n'est pas visible du reste du parc (addendum §8).
//
// Le fichier vit hors du poste : il est relu comme une donnée non fiable
// (EF-86).
package statusfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"leblanc.io/open-go-borg-ui/internal/config"
)

// FormatVersion est la version du format des fichiers d'état. Un format plus
// récent est ignoré plutôt que relu de travers.
const FormatVersion = 1

// MaxFileSize borne la taille d'un fichier d'état relu. Un fichier légitime
// pèse quelques centaines d'octets.
const MaxFileSize = 64 << 10

// StaleAfter est l'ancienneté au-delà de laquelle un poste est signalé
// (EF-85).
const StaleAfter = 48 * time.Hour

// Result est l'issue de la dernière exécution d'un poste.
type Result string

const (
	ResultSuccess   Result = "success"
	ResultWarning   Result = "warning"
	ResultError     Result = "error"
	ResultCancelled Result = "cancelled"
)

// Status est le contenu d'un fichier d'état.
type Status struct {
	Version  int    `json:"version"`
	Hostname string `json:"hostname"`

	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	Result   Result    `json:"result"`
	// ErrorKey est la clé de traduction du diagnostic d'échec : chaque poste
	// l'affiche dans sa propre langue.
	ErrorKey string `json:"error_key,omitempty"`

	Files            int64 `json:"files"`
	OriginalSize     int64 `json:"original_size"`
	DeduplicatedSize int64 `json:"deduplicated_size"`
	// RepositorySize est l'espace occupé sur la destination, 0 si inconnu.
	RepositorySize int64 `json:"repository_size"`

	// Encryption est le mode de chiffrement constaté de la destination.
	Encryption string `json:"encryption"`

	// LastSuccess est la date de la dernière sauvegarde exploitable,
	// réussie ou terminée avec des avertissements. Zéro si aucune.
	LastSuccess time.Time `json:"last_success,omitzero"`
	// NextRun est la prochaine exécution planifiée, zéro en planification
	// manuelle.
	NextRun time.Time `json:"next_run,omitzero"`
}

// FileName retourne le nom du fichier d'état d'un poste, dérivé de son nom
// par la même réduction que partout ailleurs. Un fichier relu dont le contenu
// décrit un autre poste que celui de son nom est écarté.
func FileName(hostname string) string { return config.SafeName(hostname) + ".json" }

// Hostname retourne le nom court du poste, celui que Borg substitue à
// {hostname} dans le nom des sauvegardes.
func Hostname(full string) string {
	short, _, _ := strings.Cut(full, ".")
	return short
}

// Encode produit le fichier d'état.
func Encode(status Status) ([]byte, error) {
	status.Version = FormatVersion
	return json.MarshalIndent(status, "", "  ")
}

// Erreurs de relecture, toutes causes d'un fichier écarté.
var (
	ErrTooLarge     = errors.New("statusfile: fichier d'état trop volumineux")
	ErrUnsupported  = errors.New("statusfile: format de fichier d'état inconnu")
	ErrInvalid      = errors.New("statusfile: fichier d'état invalide")
	ErrNameMismatch = errors.New("statusfile: le fichier ne porte pas le nom du poste qu'il décrit")
)

var (
	// encryptionPattern admet les modes de Borg, sans rien d'autre.
	encryptionPattern = regexp.MustCompile(`^[a-z0-9-]{0,32}$`)
	// errorKeyPattern n'admet que des clés de la racine error : une clé
	// arbitraire ferait afficher n'importe quel libellé du catalogue.
	errorKeyPattern = regexp.MustCompile(`^error\.[a-z_]{1,48}$`)
)

// Parse relit un fichier d'état comme une donnée non fiable (EF-86).
//
// fileName est le nom sous lequel le fichier a été trouvé ; now borne les
// dates, qu'un fichier altéré placerait dans le futur pour paraître à jour
// indéfiniment.
func Parse(data []byte, fileName string, now time.Time) (Status, error) {
	var status Status
	if len(data) > MaxFileSize {
		return status, ErrTooLarge
	}
	if err := json.Unmarshal(data, &status); err != nil {
		return status, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if status.Version != FormatVersion {
		return status, fmt.Errorf("%w: version %d", ErrUnsupported, status.Version)
	}

	if !displayable(status.Hostname, 64) {
		return status, fmt.Errorf("%w: nom de poste", ErrInvalid)
	}
	if FileName(status.Hostname) != fileName {
		return status, ErrNameMismatch
	}
	switch status.Result {
	case ResultSuccess, ResultWarning, ResultError, ResultCancelled:
	default:
		return status, fmt.Errorf("%w: résultat %q", ErrInvalid, status.Result)
	}
	if status.Files < 0 || status.OriginalSize < 0 || status.DeduplicatedSize < 0 || status.RepositorySize < 0 {
		return status, fmt.Errorf("%w: valeur négative", ErrInvalid)
	}
	if !encryptionPattern.MatchString(status.Encryption) {
		return status, fmt.Errorf("%w: chiffrement", ErrInvalid)
	}

	// Une horloge de poste peut dériver ; une journée d'avance ne peut
	// s'expliquer ainsi.
	horizon := now.Add(24 * time.Hour)
	if status.Finished.IsZero() || status.Finished.After(horizon) || status.Started.After(status.Finished) {
		return status, fmt.Errorf("%w: dates", ErrInvalid)
	}
	if status.LastSuccess.After(horizon) {
		return status, fmt.Errorf("%w: dates", ErrInvalid)
	}

	if status.ErrorKey != "" && !errorKeyPattern.MatchString(status.ErrorKey) {
		status.ErrorKey = ""
	}
	return status, nil
}

// displayable vérifie qu'un texte relu peut être affiché tel quel : non vide,
// borné, sans caractère de contrôle ni de mise en forme — ces derniers
// comprennent les inversions de sens d'écriture, qui feraient lire à l'écran
// autre chose que ce qui est écrit.
func displayable(text string, maxRunes int) bool {
	if text == "" || len([]rune(text)) > maxRunes {
		return false
	}
	for _, r := range text {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == unicode.ReplacementChar {
			return false
		}
	}
	return true
}

// Health est l'appréciation du poste, pour l'indicateur de l'écran État.
type Health int

const (
	// HealthOK : dernière exécution réussie, et récente.
	HealthOK Health = iota
	// HealthWarning : terminée avec des avertissements.
	HealthWarning
	// HealthStale : pas de mise à jour depuis plus de StaleAfter (EF-85).
	HealthStale
	// HealthFailed : dernière exécution en échec ou annulée.
	HealthFailed
)

// Assess apprécie l'état du poste à l'instant now. days est l'ancienneté du
// dernier fichier, en jours entiers.
func Assess(status Status, now time.Time) (health Health, days int) {
	age := now.Sub(status.Finished)
	days = int(age / (24 * time.Hour))
	if age > StaleAfter {
		return HealthStale, days
	}
	switch status.Result {
	case ResultSuccess:
		return HealthOK, days
	case ResultWarning:
		return HealthWarning, days
	default:
		return HealthFailed, days
	}
}
