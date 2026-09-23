package main

import (
	"context"
	"errors"
	"fmt"
	"leblanc.io/open-go-borg-ui/internal/station"
	"time"

	"leblanc.io/open-go-borg-ui/internal/statusfile"
)

// stateTimeout borne la relecture de l'état sur la destination.
const stateTimeout = 30 * time.Second

// commandState relit l'état que le poste a déposé sur sa destination et
// l'apprécie (EF-85).
func (a *app) commandState(ctx context.Context, _ []string) (int, error) {
	profile, err := a.Profile()
	if err != nil {
		return exitError, err
	}
	target, err := a.StateTarget(profile)
	if err != nil {
		return exitError, err
	}

	ctx, cancel := context.WithTimeout(ctx, stateTimeout)
	defer cancel()
	now := time.Now()
	status, err := statusfile.ReadFrom(ctx, target, station.Hostname(), now)
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
