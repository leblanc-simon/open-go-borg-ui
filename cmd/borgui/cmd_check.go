package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/core"
	"leblanc.io/open-go-borg-ui/internal/lock"
)

// commandCheck contrôle l'intégrité de la destination sans attendre
// l'échéance mensuelle (EF-87) : « borgui --check <profil> » (AR-03), ou
// « borgui check ».
//
// Le code de sortie suit la convention de Borg : 0 destination saine,
// 1 anomalies trouvées, 2 contrôle impossible.
func (a *app) commandCheck(ctx context.Context) (int, error) {
	profile, err := a.Profile()
	if err != nil {
		return exitError, err
	}
	if err := a.ensurePassphrase(profile); err != nil {
		return exitError, err
	}
	runner, err := a.Runner()
	if err != nil {
		return a.reportMissingEngine(err)
	}
	env, err := a.Environment(profile)
	if err != nil {
		return exitError, err
	}
	store, err := a.History()
	if err != nil {
		return exitError, err
	}
	defer store.Close()

	fmt.Println(a.T("check.starting"))
	check, err := core.CheckNow(ctx, runner, env, store, profile.Name, a.LockDir(), time.Now, a.checkProgress())
	progressDone()
	if errors.Is(err, lock.ErrBusy) {
		return exitError, fmt.Errorf("%s", a.T(core.ErrorKeyAlreadyRunning))
	}
	if err != nil {
		return exitError, err
	}

	duration := map[string]any{"Duration": a.formatDuration(check.Duration)}
	if check.Healthy {
		fmt.Println(a.T("check.healthy", duration))
		return exitSuccess, nil
	}
	fmt.Println(a.T("check.problems", duration))
	if check.Detail != "" {
		fmt.Println(a.T("cli.details", map[string]any{"Message": check.Detail}))
	}
	return exitWarning, nil
}

// checkProgress affiche l'avancement du contrôle.
func (a *app) checkProgress() func(borg.Event) {
	lastReport := time.Now()
	return func(event borg.Event) {
		percent, ok := event.Percent()
		if event.Kind != borg.EventProgressPercent || !ok || event.Finished || time.Since(lastReport) < 200*time.Millisecond {
			return
		}
		lastReport = time.Now()
		progressLine(a.T("check.progress", map[string]any{"Percent": fmt.Sprintf("%.0f", percent)}))
	}
}
