package main

import (
	"flag"
	"fmt"
	"os"

	"leblanc.io/open-go-borg-ui/internal/config"
)

// commandConfig crée ou affiche la configuration.
func (a *app) commandConfig(args []string) (int, error) {
	action, rest := subcommand(args)
	switch action {
	case "init":
		return a.configInit(rest)
	case "show":
		return a.configShow()
	default:
		return exitError, fmt.Errorf("%s", a.T("config.usage"))
	}
}

// configInit écrit une configuration neuve.
func (a *app) configInit(args []string) (int, error) {
	var (
		name       string
		user       string
		repository string
		remote     string
		clear      bool
	)

	flags := flag.NewFlagSet("config init", flag.ContinueOnError)
	flags.StringVar(&name, "name", "poste", "nom du profil")
	flags.StringVar(&user, "user", "", "sous-compte Hetzner (uXXXXXX)")
	flags.StringVar(&repository, "repo", "", "nom du dépôt sur la destination")
	flags.StringVar(&remote, "remote-path", config.DefaultRemotePath, "moteur Borg côté serveur")
	flags.BoolVar(&clear, "clear", false, "destination non chiffrée")
	if err := flags.Parse(args); err != nil {
		return exitError, err
	}
	if user == "" || repository == "" {
		return exitError, fmt.Errorf("%s", a.T("config.missing_destination"))
	}

	path := a.configPath
	if path == "" {
		var err error
		if path, err = config.DefaultPath(); err != nil {
			return exitError, err
		}
	}
	if _, err := os.Stat(path); err == nil {
		return exitError, fmt.Errorf("%s", a.T("config.already_exists", map[string]any{"Path": path}))
	}

	profile := config.Default(name)
	profile.Destination.User = user
	profile.Destination.Repo = repository
	profile.Destination.RemotePath = remote
	if clear {
		profile.Encryption = config.EncryptionNone
	}

	if err := config.Save(path, &config.Config{Profiles: []config.Profile{profile}}); err != nil {
		return exitError, err
	}

	fmt.Println(a.T("config.written", map[string]any{"Path": path}))
	fmt.Println(a.T("config.next_step_sources"))
	return exitSuccess, nil
}

// configShow affiche l'emplacement et le contenu de la configuration.
func (a *app) configShow() (int, error) {
	path := a.configPath
	if path == "" {
		var err error
		if path, err = config.DefaultPath(); err != nil {
			return exitError, err
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return exitError, err
	}
	fmt.Println(a.T("config.location", map[string]any{"Path": path}))
	fmt.Println()
	fmt.Print(string(data))
	return exitSuccess, nil
}
