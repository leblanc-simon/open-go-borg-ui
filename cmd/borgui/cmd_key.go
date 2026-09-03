package main

import (
	"fmt"

	"leblanc.io/open-go-borg-ui/internal/config"
	"leblanc.io/open-go-borg-ui/internal/probe"
)

// commandKey affiche la clé publique de l'application.
func (a *app) commandKey(args []string) (int, error) {
	action, _ := subcommand(args)
	if action != "" && action != "show" {
		return exitError, fmt.Errorf("%s", a.T("key.usage"))
	}

	path, err := config.SSHKeyPath()
	if err != nil {
		return exitError, err
	}
	if _, err := probe.EnsureKey(path); err != nil {
		return exitError, err
	}
	public, err := probe.PublicKey(path)
	if err != nil {
		return exitError, err
	}

	fmt.Println(a.T("key.intro"))
	fmt.Println()
	fmt.Print(public)
	fmt.Println()
	// La clé ne suffit pas : sans « SSH support » ni « External reachability »,
	// le port 23 reste muet et le diagnostic échoue à l'étape suivante (EF-24).
	fmt.Println(a.T("key.hetzner_steps"))
	fmt.Println(a.T("key.location", map[string]any{"Path": path}))
	return exitSuccess, nil
}
