package borgruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var (
	// ErrNotInstalled signale l'absence de runtime exploitable.
	ErrNotInstalled = errors.New("runtime: aucun moteur installé")
	// ErrChecksumMismatch signale une archive dont l'empreinte diffère de
	// celle attendue. L'installation est alors interrompue (EF-03).
	ErrChecksumMismatch = errors.New("runtime: empreinte de l'archive incorrecte")
	// ErrNotPublished signale un runtime dont l'adresse de téléchargement
	// n'est pas renseignée dans cette version de l'application.
	ErrNotPublished = errors.New("runtime: aucune adresse de téléchargement pour ce runtime")
)

// Manager installe et retrouve les runtimes dans un dossier versionné.
//
// Le versionnement permet de mettre à jour le moteur sans casser une
// sauvegarde en cours, et de revenir en arrière si la nouvelle version pose
// problème.
type Manager struct {
	// root contient un sous-dossier par version installée.
	root string
	spec Spec
}

// NewManager construit le gestionnaire. root est le dossier des runtimes,
// typiquement %LOCALAPPDATA%\borgui\runtime.
func NewManager(root string, spec Spec) *Manager {
	return &Manager{root: root, spec: spec}
}

// Spec retourne le runtime épinglé par cette version de l'application.
func (m *Manager) Spec() Spec { return m.spec }

// Path retourne le dossier du runtime épinglé, installé ou non.
func (m *Manager) Path() string { return filepath.Join(m.root, m.spec.Version) }

// Installed retourne le dossier du runtime épinglé s'il est présent.
func (m *Manager) Installed() (string, error) {
	path := m.Path()
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("%w: %s", ErrNotInstalled, m.spec.Version)
	}
	return path, nil
}

// Versions liste les runtimes présents, du plus ancien au plus récent nom.
func (m *Manager) Versions() ([]string, error) {
	entries, err := os.ReadDir(m.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("runtime: lecture de %s: %w", m.root, err)
	}
	var versions []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			versions = append(versions, entry.Name())
		}
	}
	return versions, nil
}

// Install télécharge, vérifie et installe le runtime épinglé.
//
// Le téléchargement est repris s'il a été interrompu, et l'installation n'est
// visible qu'une fois complète : l'extraction a lieu dans un dossier
// temporaire, renommé ensuite (EF-04). Une extraction interrompue ne laisse
// donc jamais un runtime à moitié installé.
func (m *Manager) Install(ctx context.Context, progress ProgressFunc) (string, error) {
	if !m.spec.Published() {
		return "", ErrNotPublished
	}
	if path, err := m.Installed(); err == nil {
		return path, nil
	}

	archive := filepath.Join(m.root, m.spec.Version+".zip")
	if err := os.MkdirAll(m.root, 0o700); err != nil {
		return "", fmt.Errorf("runtime: création de %s: %w", m.root, err)
	}
	if err := download(ctx, m.spec, archive, progress); err != nil {
		return "", err
	}
	defer os.Remove(archive)

	return m.InstallFromArchive(ctx, archive)
}

// InstallFromArchive installe le runtime depuis une archive déjà présente.
//
// C'est la voie hors ligne (EF-06) : sur un poste sans accès réseau ou derrière
// un filtrage, l'archive est téléchargée ailleurs et déposée à la main. Elle
// est vérifiée par la même empreinte que la voie en ligne.
func (m *Manager) InstallFromArchive(ctx context.Context, archive string) (string, error) {
	if err := m.verifyChecksum(archive); err != nil {
		return "", err
	}

	target := m.Path()
	staging, err := os.MkdirTemp(m.root, ".staging-*")
	if err != nil {
		return "", fmt.Errorf("runtime: dossier temporaire: %w", err)
	}
	defer os.RemoveAll(staging)

	if err := unzip(ctx, archive, staging); err != nil {
		return "", err
	}

	root, err := singleRoot(staging)
	if err != nil {
		return "", err
	}

	if err := os.RemoveAll(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("runtime: nettoyage de %s: %w", target, err)
	}
	if err := os.Rename(root, target); err != nil {
		return "", fmt.Errorf("runtime: installation dans %s: %w", target, err)
	}
	return target, nil
}

// verifyChecksum compare l'empreinte de l'archive à celle compilée dans le
// binaire. Un écart interrompt l'installation.
func (m *Manager) verifyChecksum(archive string) error {
	if m.spec.SHA256 == "" {
		return fmt.Errorf("%w: empreinte attendue absente", ErrChecksumMismatch)
	}
	file, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("runtime: lecture de l'archive: %w", err)
	}
	defer file.Close()

	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return fmt.Errorf("runtime: lecture de l'archive: %w", err)
	}

	got := hex.EncodeToString(digest.Sum(nil))
	if !strings.EqualFold(got, m.spec.SHA256) {
		return fmt.Errorf("%w: attendue %s, obtenue %s", ErrChecksumMismatch, m.spec.SHA256, got)
	}
	return nil
}

// singleRoot retourne le dossier à installer : certaines archives placent tout
// sous un dossier unique, d'autres à leur racine.
func singleRoot(staging string) (string, error) {
	entries, err := os.ReadDir(staging)
	if err != nil {
		return "", fmt.Errorf("runtime: lecture de l'extraction: %w", err)
	}
	if len(entries) == 1 && entries[0].IsDir() {
		return filepath.Join(staging, entries[0].Name()), nil
	}
	if len(entries) == 0 {
		return "", errors.New("runtime: archive vide")
	}
	return staging, nil
}
