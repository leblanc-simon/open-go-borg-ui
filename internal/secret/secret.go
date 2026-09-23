// Package secret conserve la passphrase du dépôt.
//
// La passphrase vit dans le trousseau du système : Credential Manager sous
// Windows, Secret Service sous Linux. Quand aucun trousseau n'est disponible —
// session sans agent, poste minimal — un repli sur fichier en permissions
// restreintes évite de bloquer l'utilisateur, mais l'application doit le
// signaler visiblement (EF-36).
//
// La passphrase n'est jamais transmise à Borg par l'environnement ni par une
// ligne de commande : Borg l'obtient en exécutant BORG_PASSCOMMAND, qui
// rappelle l'application (SEC-05).
package secret

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"

	"leblanc.io/open-go-borg-ui/internal/fsperm"
)

// service identifie l'application dans le trousseau.
const service = "borgui"

// ErrNotFound signale l'absence de passphrase enregistrée pour ce profil.
var ErrNotFound = errors.New("secret: aucune passphrase enregistrée")

// Store lit et écrit la passphrase d'un profil.
type Store struct {
	// fallbackDir accueille le fichier de repli quand le trousseau est
	// indisponible.
	fallbackDir string
}

// NewStore construit le magasin. fallbackDir est le dossier des données
// locales de l'application.
func NewStore(fallbackDir string) *Store { return &Store{fallbackDir: fallbackDir} }

// Set enregistre la passphrase du profil. Le premier retour indique que le
// trousseau n'a pas pu être utilisé et que le repli sur fichier a servi :
// l'appelant doit en avertir l'utilisateur.
func (s *Store) Set(profile, passphrase string) (fallback bool, err error) {
	if err := keyring.Set(service, profile, passphrase); err == nil {
		// Le fichier de repli d'une installation antérieure n'a plus lieu
		// d'être : le laisser reviendrait à conserver le secret en clair.
		_ = os.Remove(s.fallbackPath(profile))
		return false, nil
	}

	if err := s.writeFallback(profile, passphrase); err != nil {
		return false, err
	}
	return true, nil
}

// Get retourne la passphrase du profil.
func (s *Store) Get(profile string) (string, error) {
	passphrase, err := keyring.Get(service, profile)
	if err == nil {
		return passphrase, nil
	}
	if data, fallbackErr := s.readFallback(profile); fallbackErr == nil {
		return data, nil
	}
	return "", ErrNotFound
}

// Delete efface la passphrase du profil des deux emplacements.
func (s *Store) Delete(profile string) error {
	keyringErr := keyring.Delete(service, profile)
	fileErr := os.Remove(s.fallbackPath(profile))

	if keyringErr != nil && !errors.Is(keyringErr, keyring.ErrNotFound) &&
		fileErr != nil && !errors.Is(fileErr, os.ErrNotExist) {
		return fmt.Errorf("secret: suppression: %w", keyringErr)
	}
	return nil
}

// fallbackPath retourne le chemin du fichier de repli du profil.
func (s *Store) fallbackPath(profile string) string {
	return filepath.Join(s.fallbackDir, "passphrase-"+sanitize(profile))
}

// writeFallback écrit la passphrase en permissions restreintes.
func (s *Store) writeFallback(profile, passphrase string) error {
	if s.fallbackDir == "" {
		return errors.New("secret: aucun emplacement de repli configuré")
	}
	if err := os.MkdirAll(s.fallbackDir, 0o700); err != nil {
		return fmt.Errorf("secret: création du dossier: %w", err)
	}
	if err := fsperm.WritePrivate(s.fallbackPath(profile), []byte(passphrase)); err != nil {
		return fmt.Errorf("secret: écriture du repli: %w", err)
	}
	return nil
}

// readFallback lit le fichier de repli.
func (s *Store) readFallback(profile string) (string, error) {
	data, err := os.ReadFile(s.fallbackPath(profile))
	if err != nil {
		return "", ErrNotFound
	}
	return strings.TrimRight(string(data), "\r\n"), nil
}

// sanitize réduit un nom de profil à un nom de fichier sûr.
func sanitize(profile string) string {
	var builder strings.Builder
	for _, r := range profile {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
	}
	if builder.Len() == 0 {
		return "default"
	}
	return builder.String()
}
