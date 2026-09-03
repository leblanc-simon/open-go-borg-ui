//go:build !windows

package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// defaultOneFileSystem : ne pas franchir les points de montage est le
// comportement attendu sous Linux, où /home peut voisiner des montages
// réseau ou temporaires (EF-47).
const defaultOneFileSystem = true

// stateRoot suit la spécification XDG.
func stateRoot() (string, error) {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: dossier personnel introuvable: %w", err)
	}
	return filepath.Join(home, ".local", "share"), nil
}
