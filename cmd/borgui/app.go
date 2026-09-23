package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/borgruntime"
	"leblanc.io/open-go-borg-ui/internal/config"
	"leblanc.io/open-go-borg-ui/internal/history"
	"leblanc.io/open-go-borg-ui/internal/i18n"
	"leblanc.io/open-go-borg-ui/internal/secret"
)

// app rassemble ce dont toutes les commandes ont besoin : les traductions, la
// configuration et les emplacements de l'application.
type app struct {
	loc *i18n.Localizer

	configPath  string
	profileName string

	stateDir string
}

// newApp construit le contexte applicatif.
func newApp(loc *i18n.Localizer, configPath, profileName string) (*app, error) {
	stateDir, err := config.StateDir()
	if err != nil {
		return nil, err
	}
	return &app{loc: loc, configPath: configPath, profileName: profileName, stateDir: stateDir}, nil
}

// T traduit une clé.
func (a *app) T(key string, data ...any) string { return a.loc.T(key, data...) }

// profile charge le profil demandé.
func (a *app) profile() (*config.Profile, error) {
	cfg, err := config.Load(a.configPath)
	if err != nil {
		return nil, err
	}
	return cfg.Profile(a.profileName)
}

// runtimeManager construit le gestionnaire du moteur de sauvegarde.
func (a *app) runtimeManager() *borgruntime.Manager {
	return borgruntime.NewManager(filepath.Join(a.stateDir, "runtime"), borgruntime.Pinned)
}

// historyStore ouvre l'historique des exécutions du poste.
func (a *app) historyStore() (*history.Store, error) {
	path, err := config.HistoryPath()
	if err != nil {
		return nil, err
	}
	return history.Open(path)
}

// secrets construit le magasin de passphrases.
func (a *app) secrets() *secret.Store { return secret.NewStore(a.stateDir) }

// runner construit le pilote de Borg de la plateforme.
//
// Sous Windows, le moteur est le runtime installé ; sous Linux, le Borg de la
// distribution. Dans les deux cas, l'absence de moteur est une situation
// prévue, que l'appelant traduit en action concrète (EF-08).
func (a *app) runner() (borg.Runner, error) {
	engine := ""
	if isWindows() {
		installed, err := a.runtimeManager().Installed()
		if err != nil {
			return nil, err
		}
		engine = installed
	}
	return borg.New(engine)
}

// environment construit l'environnement d'exécution de Borg pour ce profil.
func (a *app) environment(profile *config.Profile) (borg.Environment, error) {
	repository, err := profile.Destination.RepositoryURL()
	if err != nil {
		return borg.Environment{}, err
	}

	keyPath, err := a.sshKeyPath(profile)
	if err != nil {
		return borg.Environment{}, err
	}
	knownHosts, err := config.KnownHostsPath()
	if err != nil {
		return borg.Environment{}, err
	}
	executable, err := os.Executable()
	if err != nil {
		return borg.Environment{}, fmt.Errorf("chemin de l'application: %w", err)
	}

	_, port := hostPort(repository)

	env := borg.Environment{
		Repository: repository,
		RemotePath: profile.Destination.RemotePath,
		SSHKey:     keyPath,
		KnownHosts: knownHosts,
		Port:       port,
		Encrypted:  profile.Encryption.Encrypted(),
		// Borg rappelle l'application pour obtenir la passphrase : elle ne
		// figure ni dans l'environnement, ni dans une ligne de commande.
		PassCommandExe:  executable,
		PassCommandArgs: []string{"--print-passphrase", profile.Name},
		BaseDir:         filepath.Join(a.stateDir, "borg"),
		UploadRateLimit: profile.Destination.UploadRateLimit,
	}
	return env, nil
}

// sshKeyPath retourne la clé du profil, ou celle de l'application.
func (a *app) sshKeyPath(profile *config.Profile) (string, error) {
	if profile.Destination.SSHKey != "" {
		return profile.Destination.SSHKey, nil
	}
	return config.SSHKeyPath()
}

// hostPort extrait l'hôte et le port d'une URL de dépôt. Une Storage Box
// répond sur le port 23 ; un dépôt SSH quelconque suit la convention SSH.
func hostPort(repository string) (string, int) {
	parsed, err := url.Parse(repository)
	if err != nil || parsed.Host == "" {
		return "", 22
	}
	host := parsed.Hostname()
	port := 22
	if raw := parsed.Port(); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil {
			port = value
		}
	}
	return host, port
}

// sshUser extrait le compte d'une URL de dépôt.
func sshUser(repository string) string {
	parsed, err := url.Parse(repository)
	if err != nil || parsed.User == nil {
		return ""
	}
	return parsed.User.Username()
}

// isWindows indique si l'application tourne sous Windows.
func isWindows() bool { return strings.EqualFold(osName(), "windows") }
