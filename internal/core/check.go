package core

import (
	"context"
	"strings"
	"time"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/history"
	"leblanc.io/open-go-borg-ui/internal/lock"
)

// CheckInterval est l'intervalle entre deux contrôles de la destination
// (EF-87).
const CheckInterval = 30 * 24 * time.Hour

// CheckDue indique qu'un contrôle de la destination est dû : jamais fait, ou
// fait il y a plus d'un mois.
func CheckDue(ctx context.Context, store *history.Store, profile string, now time.Time) (bool, error) {
	last, ok, err := store.LastRepositoryCheck(ctx, profile)
	if err != nil || !ok {
		return err == nil, err
	}
	return now.Sub(last.Checked) >= CheckInterval, nil
}

// CheckRepository contrôle l'intégrité de la destination et consigne
// l'issue (EF-87).
//
// Seul un contrôle mené à son terme est consigné, sain ou non. Un contrôle
// qui n'a pas pu se dérouler — destination injoignable, annulation — ne dit
// rien de la destination : il est retourné en erreur, et sera retenté.
func CheckRepository(ctx context.Context, runner borg.Runner, env borg.Environment, store *history.Store,
	profile string, clock func() time.Time, onEvent func(borg.Event)) (history.RepositoryCheck, error) {
	started := clock()
	result, err := borg.Check(ctx, runner, env, onEvent)
	if ctx.Err() != nil {
		return history.RepositoryCheck{}, ctx.Err()
	}
	if err != nil {
		return history.RepositoryCheck{}, err
	}

	check := history.RepositoryCheck{
		Profile:  profile,
		Checked:  started,
		Healthy:  result.Status == borg.StatusSuccess,
		Duration: clock().Sub(started),
	}
	if !check.Healthy {
		var detail []string
		for _, message := range result.Warnings() {
			detail = append(detail, message.Text)
		}
		check.Detail = strings.Join(detail, "\n")
	}
	if check.ID, err = store.AddRepositoryCheck(ctx, check); err != nil {
		return check, err
	}
	return check, nil
}

// CheckNow contrôle la destination à la demande — bouton de l'écran État ou
// mode « --check » de l'exécutable. Le verrou de la destination est pris :
// une sauvegarde qui démarrerait entre-temps attend son tour (EF-57).
func CheckNow(ctx context.Context, runner borg.Runner, env borg.Environment, store *history.Store,
	profile, lockDir string, clock func() time.Time, onEvent func(borg.Event)) (history.RepositoryCheck, error) {
	if lockDir != "" {
		held, err := lock.Acquire(lock.PathFor(lockDir, env.Repository))
		if err != nil {
			return history.RepositoryCheck{}, err
		}
		defer held.Release()
	}
	return CheckRepository(ctx, runner, env, store, profile, clock, onEvent)
}
