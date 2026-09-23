// Package lock empêche deux exécutions simultanées sur la même destination
// (EF-57).
//
// Le verrou est un verrou du système sur un fichier, et non la simple
// présence d'un fichier : le système le libère de lui-même à la mort du
// processus. Une extinction brutale pendant une sauvegarde ne laisse donc
// jamais de verrou orphelin sur le poste — seul le verrou côté destination,
// que Borg pose lui-même, peut subsister, et il se lève par
// « borg break-lock » (EF-58).
package lock

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrBusy signale qu'une autre exécution tient déjà le verrou.
var ErrBusy = errors.New("lock: une exécution est déjà en cours sur cette destination")

// Lock est un verrou tenu.
type Lock struct {
	file *os.File
}

// PathFor retourne le fichier de verrou d'une destination, dans dir. Le nom
// dérive de l'adresse de la destination : deux profils qui viseraient la même
// se partagent le même verrou.
func PathFor(dir, repository string) string {
	digest := sha256.Sum256([]byte(repository))
	return filepath.Join(dir, hex.EncodeToString(digest[:8])+".lock")
}

// Acquire prend le verrou sans attendre. Il retourne ErrBusy s'il est tenu.
func Acquire(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("lock: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lock: %w", err)
	}
	if err := lockFile(file); err != nil {
		file.Close()
		return nil, err
	}
	// Le numéro de processus n'est qu'une indication pour le diagnostic :
	// c'est le verrou du système qui fait foi.
	file.Truncate(0)
	fmt.Fprintf(file, "%d\n", os.Getpid())
	return &Lock{file: file}, nil
}

// Release libère le verrou. Le fichier est conservé : le supprimer ouvrirait
// une course avec une exécution qui viendrait de l'ouvrir.
func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockFile(l.file)
	err := l.file.Close()
	l.file = nil
	return err
}
