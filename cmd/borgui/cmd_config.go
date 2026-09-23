package main

import (
	"flag"
	"fmt"
	"os"

	"golang.org/x/term"

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
	case "export":
		return a.configExport(rest)
	case "import":
		return a.configImport(rest)
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

	path := a.ConfigPath
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
	path := a.ConfigPath
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

// configExport écrit la configuration transportable du profil (EF-13).
func (a *app) configExport(args []string) (int, error) {
	var output string
	flags := flag.NewFlagSet("config export", flag.ContinueOnError)
	flags.StringVar(&output, "o", "", "fichier où écrire l'export")
	if err := flags.Parse(args); err != nil {
		return exitError, err
	}
	profile, err := a.Profile()
	if err != nil {
		return exitError, err
	}
	data, err := config.Export(*profile, a.T("config.export_header"))
	if err != nil {
		return exitError, err
	}
	if output == "" {
		os.Stdout.Write(data)
		return exitSuccess, nil
	}
	if err := os.WriteFile(output, data, 0o644); err != nil {
		return exitError, err
	}
	fmt.Println(a.T("config.exported", map[string]any{"Path": output}))
	return exitSuccess, nil
}

// configImport crée la configuration du poste à partir d'un export, en ne
// demandant que ce qui lui est propre : son sous-compte, et la passphrase si
// la destination est chiffrée (EF-101).
func (a *app) configImport(args []string) (int, error) {
	var user, repository string
	flags := flag.NewFlagSet("config import", flag.ContinueOnError)
	flags.StringVar(&user, "user", "", "sous-compte Hetzner de ce poste (uXXXXXX)")
	flags.StringVar(&repository, "repo", "", "URL complète de la destination, pour une destination SSH")
	positional, err := parseInterleaved(flags, args)
	if err != nil || len(positional) != 1 {
		return exitError, fmt.Errorf("%s", a.T("config.import_usage"))
	}

	path := a.ConfigPath
	if path == "" {
		if path, err = config.DefaultPath(); err != nil {
			return exitError, err
		}
	}
	if _, err := os.Stat(path); err == nil {
		return exitError, fmt.Errorf("%s", a.T("config.already_exists", map[string]any{"Path": path}))
	}

	file, err := os.Open(positional[0])
	if err != nil {
		return exitError, err
	}
	profile, err := config.Import(file, a.ProfileName)
	file.Close()
	if err != nil {
		return exitError, fmt.Errorf("%s", a.T("config.import_invalid", map[string]any{"Message": err.Error()}))
	}

	switch profile.Destination.Kind {
	case config.KindHetzner:
		if user == "" {
			return exitError, fmt.Errorf("%s", a.T("config.import_user_required"))
		}
		profile.Destination.User = user
	case config.KindSSH:
		if repository == "" {
			return exitError, fmt.Errorf("%s", a.T("config.import_repo_required"))
		}
		profile.Destination.Repo = repository
	}

	if err := config.Save(path, &config.Config{Profiles: []config.Profile{profile}}); err != nil {
		return exitError, err
	}
	fmt.Println(a.T("config.written", map[string]any{"Path": path}))
	a.ProfileName = profile.Name

	// Les dossiers viennent d'un autre poste : ceux qui n'existent pas ici
	// sont signalés, sans être retirés — c'est à l'utilisateur d'en décider.
	for _, source := range profile.Sources {
		if _, err := os.Stat(source); err != nil {
			fmt.Println(a.T("config.import_missing_source", map[string]any{"Path": source}))
		}
	}

	if profile.Encryption.Encrypted() {
		if term.IsTerminal(int(os.Stdin.Fd())) {
			if code, err := a.passphraseSet(); err != nil {
				return code, err
			}
		} else {
			fmt.Println(a.T("config.import_passphrase_later"))
		}
	}
	fmt.Println(a.T("config.import_next_steps"))
	return exitSuccess, nil
}
