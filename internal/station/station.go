// Package station représente le poste : où vivent sa configuration et ses
// données, et comment se construisent les services qui agissent pour lui —
// moteur de sauvegarde, environnement de Borg, historique, verrous, état
// publié.
//
// La ligne de commande et l'interface graphique s'appuient toutes deux sur
// lui : ce qu'elles font d'une même action ne peut pas diverger.
package station

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/borgruntime"
	"leblanc.io/open-go-borg-ui/internal/config"
	"leblanc.io/open-go-borg-ui/internal/core"
	"leblanc.io/open-go-borg-ui/internal/fsperm"
	"leblanc.io/open-go-borg-ui/internal/history"
	"leblanc.io/open-go-borg-ui/internal/probe"
	"leblanc.io/open-go-borg-ui/internal/schedule"
	"leblanc.io/open-go-borg-ui/internal/secret"
	"leblanc.io/open-go-borg-ui/internal/statusfile"
)

// Station est le poste courant.
type Station struct {
	// ConfigPath est le fichier de configuration ; vide, son emplacement
	// standard.
	ConfigPath string
	// ProfileName choisit le profil ; vide, le premier.
	ProfileName string
	// StateDir accueille les données locales : historique, clé, runtime.
	StateDir string
}

// New construit le poste.
func New(configPath, profileName string) (*Station, error) {
	stateDir, err := config.StateDir()
	if err != nil {
		return nil, err
	}
	return &Station{ConfigPath: configPath, ProfileName: profileName, StateDir: stateDir}, nil
}

// Profile charge le profil du poste.
func (s *Station) Profile() (*config.Profile, error) {
	cfg, err := config.Load(s.ConfigPath)
	if err != nil {
		return nil, err
	}
	return cfg.Profile(s.ProfileName)
}

// RuntimeManager construit le gestionnaire du moteur de sauvegarde.
func (s *Station) RuntimeManager() *borgruntime.Manager {
	return borgruntime.NewManager(filepath.Join(s.StateDir, "runtime"), borgruntime.Pinned)
}

// History ouvre l'historique des exécutions du poste.
func (s *Station) History() (*history.Store, error) {
	path, err := config.HistoryPath()
	if err != nil {
		return nil, err
	}
	return history.Open(path)
}

// LockDir est le dossier des verrous locaux (EF-57).
func (s *Station) LockDir() string { return filepath.Join(s.StateDir, "locks") }

// Secrets construit le magasin de passphrases.
func (s *Station) Secrets() *secret.Store { return secret.NewStore(s.StateDir) }

// Runner construit le pilote de Borg de la plateforme.
//
// Sous Windows, le moteur est le runtime installé ; sous Linux, le Borg de la
// distribution. Dans les deux cas, l'absence de moteur est une situation
// prévue, que l'appelant traduit en action concrète (EF-08).
func (s *Station) Runner() (borg.Runner, error) {
	engine := ""
	if runtime.GOOS == "windows" {
		installed, err := s.RuntimeManager().Installed()
		if err != nil {
			return nil, err
		}
		engine = installed
	}
	return borg.New(engine)
}

// SSHKeyPath retourne la clé du profil, ou celle de l'application.
func (s *Station) SSHKeyPath(profile *config.Profile) (string, error) {
	if profile.Destination.SSHKey != "" {
		return profile.Destination.SSHKey, nil
	}
	return config.SSHKeyPath()
}

// Environment construit l'environnement d'exécution de Borg pour ce profil.
func (s *Station) Environment(profile *config.Profile) (borg.Environment, error) {
	return s.EnvironmentWithSecret(profile, profile.Name)
}

// EnvironmentWithSecret construit l'environnement de Borg en lisant le mot
// de passe sous secretName plutôt que sous le nom du profil : une
// destination en cours de préparation a le sien, qui ne remplace celui de
// la destination en service qu'à la validation.
func (s *Station) EnvironmentWithSecret(profile *config.Profile, secretName string) (borg.Environment, error) {
	repository, err := profile.Destination.RepositoryURL()
	if err != nil {
		return borg.Environment{}, err
	}

	keyPath, err := s.SSHKeyPath(profile)
	if err != nil {
		return borg.Environment{}, err
	}
	knownHosts, err := config.KnownHostsPath()
	if err != nil {
		return borg.Environment{}, err
	}
	// Le ssh du runtime refuse une clé lisible par d'autres que son
	// propriétaire. Les droits sont remis en ordre avant chaque appel à
	// Borg : une clé restaurée ou copiée à la main ne doit pas faire
	// échouer une sauvegarde planifiée.
	for _, path := range []string{keyPath, knownHosts} {
		if err := fsperm.Restrict(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return borg.Environment{}, err
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return borg.Environment{}, fmt.Errorf("chemin de l'application: %w", err)
	}

	_, port := HostPort(repository)

	return borg.Environment{
		Repository: repository,
		RemotePath: profile.Destination.RemotePath,
		SSHKey:     keyPath,
		KnownHosts: knownHosts,
		Port:       port,
		Encrypted:  profile.Encryption.Encrypted(),
		// Borg rappelle l'application pour obtenir la passphrase : elle ne
		// figure ni dans l'environnement, ni dans une ligne de commande.
		PassCommandExe:  executable,
		PassCommandArgs: []string{"--print-passphrase", secretName},
		BaseDir:         filepath.Join(s.StateDir, "borg"),
		UploadRateLimit: profile.Destination.UploadRateLimit,
	}, nil
}

// stateTimeout borne un échange avec la destination pour le fichier d'état :
// il ne doit jamais retenir une sauvegarde planifiée.
const stateTimeout = 30 * time.Second

// StateTarget désigne le fichier d'état du poste : dans son propre
// sous-compte, par la connexion de la destination (EF-83, addendum §8).
func (s *Station) StateTarget(profile *config.Profile) (statusfile.Target, error) {
	repository, err := profile.Destination.RepositoryURL()
	if err != nil {
		return statusfile.Target{}, err
	}
	host, port := HostPort(repository)
	user := SSHUser(repository)
	if user == "" {
		user = profile.Destination.User
	}
	keyPath, err := s.SSHKeyPath(profile)
	if err != nil {
		return statusfile.Target{}, err
	}
	knownHosts, err := config.KnownHostsPath()
	if err != nil {
		return statusfile.Target{}, err
	}
	return statusfile.Target{
		Params: probe.Params{
			Host:           host,
			Port:           port,
			User:           user,
			KeyPath:        keyPath,
			KnownHostsPath: knownHosts,
			Timeout:        stateTimeout,
		},
		Dir: statusfile.DefaultDir,
	}, nil
}

// StatePublisher retourne la fonction qui dépose l'état du poste.
func (s *Station) StatePublisher(profile *config.Profile) func(context.Context, statusfile.Status) error {
	return func(ctx context.Context, status statusfile.Status) error {
		target, err := s.StateTarget(profile)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(ctx, stateTimeout)
		defer cancel()
		return statusfile.PublishTo(ctx, target, status)
	}
}

// BackupService assemble le service de sauvegarde du profil : verrou,
// historique, publication de l'état et prochaine exécution.
func (s *Station) BackupService(profile *config.Profile, runner borg.Runner, store *history.Store) core.Backup {
	return core.Backup{
		Runner:   runner,
		History:  store,
		LockDir:  s.LockDir(),
		Publish:  s.StatePublisher(profile),
		Hostname: Hostname(),
		NextRun:  NextRun(profile),
		// La vérification mensuelle d'une restauration suit la sauvegarde,
		// planifiée ou non : l'application n'a pas de démon (EF-99).
		VerifyRestores: true,
	}
}

// Hostname retourne le nom court du poste.
func Hostname() string {
	name, err := os.Hostname()
	if err != nil {
		return "poste"
	}
	return statusfile.Hostname(name)
}

// NextRun calcule la prochaine exécution planifiée du profil.
func NextRun(profile *config.Profile) func() time.Time {
	return func() time.Time {
		plan, err := schedule.FromConfig(profile.Schedule)
		if err != nil {
			return time.Time{}
		}
		return schedule.Next(plan, time.Now())
	}
}

// HostPort extrait l'hôte et le port d'une URL de dépôt. Une Storage Box
// répond sur le port 23 ; un dépôt SSH quelconque suit la convention SSH.
func HostPort(repository string) (string, int) {
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

// SSHUser extrait le compte d'une URL de dépôt.
func SSHUser(repository string) string {
	parsed, err := url.Parse(repository)
	if err != nil || parsed.User == nil {
		return ""
	}
	return parsed.User.Username()
}

// InstallHint retourne la commande d'installation de Borg propre à la
// distribution Linux détectée : sous Linux, le moteur vient de la
// distribution, l'application ne télécharge rien (EF-08).
func InstallHint() string {
	managers := []struct {
		binary  string
		command string
	}{
		{"apt", "sudo apt install borgbackup"},
		{"dnf", "sudo dnf install borgbackup"},
		{"zypper", "sudo zypper install borgbackup"},
		{"pacman", "sudo pacman -S borg"},
		{"apk", "sudo apk add borgbackup"},
	}
	for _, manager := range managers {
		if _, err := exec.LookPath(manager.binary); err == nil {
			return manager.command
		}
	}
	return "borgbackup"
}

// ScheduledTask décrit ce que l'ordonnanceur doit lancer : cet exécutable, en
// mode « --run <profil> » (EF-63), avec la configuration en cours si elle
// n'est pas à son emplacement standard. description est le libellé, déjà
// traduit, que montre l'ordonnanceur.
func (s *Station) ScheduledTask(profile *config.Profile, description string) (schedule.Task, error) {
	executable, err := os.Executable()
	if err != nil {
		return schedule.Task{}, err
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}

	var args []string
	if s.ConfigPath != "" {
		path, err := filepath.Abs(s.ConfigPath)
		if err != nil {
			return schedule.Task{}, err
		}
		args = append(args, "--config", path)
	}
	args = append(args, "--run", profile.Name)

	return schedule.Task{
		Name:        config.SafeName(profile.Name),
		Description: description,
		Executable:  executable,
		Args:        args,
	}, nil
}
