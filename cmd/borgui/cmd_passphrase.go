package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"leblanc.io/open-go-borg-ui/internal/secret"
)

// commandPassphrase gère la passphrase du dépôt.
func (a *app) commandPassphrase(args []string) (int, error) {
	action, _ := subcommand(args)
	switch action {
	case "set":
		return a.passphraseSet()
	case "check":
		return a.passphraseCheck()
	default:
		return exitError, fmt.Errorf("%s", a.T("passphrase.usage"))
	}
}

// passphraseSet enregistre la passphrase du profil.
func (a *app) passphraseSet() (int, error) {
	profile, err := a.profile()
	if err != nil {
		return exitError, err
	}
	if !profile.Encryption.Encrypted() {
		return exitError, fmt.Errorf("%s", a.T("passphrase.not_encrypted"))
	}

	passphrase, err := a.readPassphrase()
	if err != nil {
		return exitError, err
	}
	if passphrase == "" {
		return exitError, fmt.Errorf("%s", a.T("passphrase.empty"))
	}

	fallback, err := a.secrets().Set(profile.Name, passphrase)
	if err != nil {
		return exitError, err
	}
	fmt.Println(a.T("passphrase.stored"))
	if fallback {
		// Le trousseau du système n'a pas répondu : l'utilisateur doit savoir
		// que le secret est désormais dans un fichier (EF-36).
		fmt.Println(a.T("passphrase.fallback_warning", map[string]any{"Dir": a.stateDir}))
	}
	return exitSuccess, nil
}

// passphraseCheck vérifie qu'une passphrase est disponible sans l'afficher.
func (a *app) passphraseCheck() (int, error) {
	profile, err := a.profile()
	if err != nil {
		return exitError, err
	}
	if !profile.Encryption.Encrypted() {
		fmt.Println(a.T("passphrase.not_needed"))
		return exitSuccess, nil
	}
	if _, err := a.secrets().Get(profile.Name); err != nil {
		return exitError, fmt.Errorf("%s", a.T("passphrase.missing"))
	}
	fmt.Println(a.T("passphrase.available"))
	return exitSuccess, nil
}

// readPassphrase lit la passphrase sans écho lorsque l'entrée est un terminal.
func (a *app) readPassphrase() (string, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Fprint(os.Stderr, a.T("passphrase.prompt")+" ")
		data, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	}

	// Entrée redirigée : la passphrase vient d'un script ou d'un test.
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return "", errors.New(a.T("passphrase.empty"))
	}
	return strings.TrimSpace(scanner.Text()), nil
}

// printPassphrase écrit la passphrase sur la sortie standard.
//
// C'est le mode qu'invoque BORG_PASSCOMMAND : la sortie ne doit contenir que
// la passphrase, et rien d'autre ne doit être écrit sur la sortie standard.
func (a *app) printPassphrase(profileName string) int {
	passphrase, err := a.secrets().Get(profileName)
	if err != nil {
		if errors.Is(err, secret.ErrNotFound) {
			fmt.Fprintln(os.Stderr, a.T("passphrase.missing"))
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		return exitError
	}
	fmt.Println(passphrase)
	return exitSuccess
}
