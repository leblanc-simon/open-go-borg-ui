package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// appDir est le nom du dossier de l'application dans les emplacements standard.
const appDir = "borgui"

// DefaultPath retourne l'emplacement standard du fichier de configuration :
// ~/.config/borgui/config.toml sous Linux, %APPDATA%\borgui\config.toml sous
// Windows (AR-06).
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: dossier de configuration introuvable: %w", err)
	}
	return filepath.Join(dir, appDir, "config.toml"), nil
}

// StateDir retourne le dossier des données locales : historique SQLite,
// journaux d'exécution, clé SSH, known_hosts.
func StateDir() (string, error) {
	dir, err := stateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appDir), nil
}

// SSHKeyPath retourne l'emplacement par défaut de la clé SSH dédiée à
// l'application. Elle n'est jamais partagée avec la clé personnelle de
// l'utilisateur (SEC-03).
func SSHKeyPath() (string, error) {
	dir, err := StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "id_ed25519"), nil
}

// KnownHostsPath retourne le fichier known_hosts propre à l'application, qui
// épingle l'empreinte du serveur au premier appariement (SEC-04).
func KnownHostsPath() (string, error) {
	dir, err := StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "known_hosts"), nil
}

// HistoryPath retourne la base SQLite de l'historique des exécutions.
func HistoryPath() (string, error) {
	dir, err := StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "history.db"), nil
}
