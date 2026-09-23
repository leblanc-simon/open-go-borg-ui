package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"leblanc.io/open-go-borg-ui/internal/config"
	"leblanc.io/open-go-borg-ui/internal/probe"
	"leblanc.io/open-go-borg-ui/internal/schedule"
	"leblanc.io/open-go-borg-ui/internal/statusfile"
)

// stateTimeout borne un échange avec la destination pour le fichier d'état :
// il ne doit jamais retenir une sauvegarde planifiée.
const stateTimeout = 30 * time.Second

// stateTarget désigne le fichier d'état du poste : dans son propre
// sous-compte, par la connexion de la destination (EF-83, addendum §8).
func (a *app) stateTarget(profile *config.Profile) (statusfile.Target, error) {
	repository, err := profile.Destination.RepositoryURL()
	if err != nil {
		return statusfile.Target{}, err
	}
	host, port := hostPort(repository)
	user := sshUser(repository)
	if user == "" {
		user = profile.Destination.User
	}
	keyPath, err := a.sshKeyPath(profile)
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

// statePublisher retourne la fonction qui dépose l'état du poste.
func (a *app) statePublisher(profile *config.Profile) func(context.Context, statusfile.Status) error {
	return func(ctx context.Context, status statusfile.Status) error {
		target, err := a.stateTarget(profile)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(ctx, stateTimeout)
		defer cancel()
		return statusfile.PublishTo(ctx, target, status)
	}
}

// hostname retourne le nom court du poste.
func hostname() string {
	name, err := os.Hostname()
	if err != nil {
		return "poste"
	}
	return statusfile.Hostname(name)
}

// nextRun calcule la prochaine exécution planifiée du profil.
func nextRun(profile *config.Profile) func() time.Time {
	return func() time.Time {
		plan, err := schedule.FromConfig(profile.Schedule)
		if err != nil {
			return time.Time{}
		}
		return schedule.Next(plan, time.Now())
	}
}

// commandState relit l'état que le poste a déposé sur sa destination et
// l'apprécie (EF-85).
func (a *app) commandState(ctx context.Context, _ []string) (int, error) {
	profile, err := a.profile()
	if err != nil {
		return exitError, err
	}
	target, err := a.stateTarget(profile)
	if err != nil {
		return exitError, err
	}

	ctx, cancel := context.WithTimeout(ctx, stateTimeout)
	defer cancel()
	now := time.Now()
	status, err := statusfile.ReadFrom(ctx, target, hostname(), now)
	switch {
	case errors.Is(err, statusfile.ErrNotPublished):
		fmt.Println(a.T("state.empty"))
		return exitSuccess, nil
	case rejected(err):
		fmt.Println(a.T("state.rejected", map[string]any{"Reason": a.T(rejectionKey(err))}))
		return exitWarning, nil
	case err != nil:
		return exitError, err
	}

	health, days := statusfile.Assess(status, now)
	fmt.Println(a.stateLine(status, health, days))
	if health != statusfile.HealthOK {
		return exitWarning, nil
	}
	return exitSuccess, nil
}

// stateLine rend l'état du poste, puis ses détails.
func (a *app) stateLine(status statusfile.Status, health statusfile.Health, days int) string {
	data := map[string]any{
		"Health": a.T(healthKey(health)),
		"Date":   status.Finished.Local().Format("2006-01-02 15:04"),
		"Days":   days,
		"Size":   formatSize(status.RepositorySize),
	}
	line := a.T("state.entry", data)
	if health == statusfile.HealthStale {
		line += "\n" + a.T("cli.details", map[string]any{"Message": a.T("state.stale", data)})
	}
	if status.ErrorKey != "" {
		line += "\n" + a.T("cli.details", map[string]any{"Message": a.T(status.ErrorKey)})
	}
	if !status.NextRun.IsZero() {
		line += "\n" + a.T("cli.details", map[string]any{"Message": a.T("state.next_run", map[string]any{
			"Date": status.NextRun.Local().Format("2006-01-02 15:04"),
		})})
	}
	return line
}

// healthKey retourne la clé de traduction de l'indicateur du poste.
func healthKey(health statusfile.Health) string {
	switch health {
	case statusfile.HealthOK:
		return "state.health_ok"
	case statusfile.HealthWarning:
		return "state.health_warning"
	case statusfile.HealthStale:
		return "state.health_stale"
	default:
		return "state.health_failed"
	}
}

// rejected indique un fichier d'état trouvé mais écarté pour son contenu, par
// opposition à une destination injoignable.
func rejected(err error) bool {
	return errors.Is(err, statusfile.ErrInvalid) || errors.Is(err, statusfile.ErrTooLarge) ||
		errors.Is(err, statusfile.ErrUnsupported) || errors.Is(err, statusfile.ErrNameMismatch)
}

// rejectionKey explique pourquoi le fichier d'état a été écarté.
func rejectionKey(err error) string {
	switch {
	case errors.Is(err, statusfile.ErrNameMismatch):
		return "state.rejected_name"
	case errors.Is(err, statusfile.ErrTooLarge):
		return "state.rejected_size"
	case errors.Is(err, statusfile.ErrUnsupported):
		return "state.rejected_version"
	default:
		return "state.rejected_invalid"
	}
}
