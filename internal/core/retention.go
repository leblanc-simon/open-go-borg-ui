package core

import (
	"context"
	"errors"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/config"
)

// PreviewRetention montre, sans rien supprimer, ce qu'une conservation ferait
// des sauvegardes existantes (EF-72). Une conservation vide garde tout : rien
// n'est supprimé, et Borg n'est pas sollicité.
func PreviewRetention(ctx context.Context, runner borg.Runner, env borg.Environment, retention config.Retention) (borg.PrunePlan, error) {
	result, err := borg.Prune(ctx, runner, borg.PruneOptions{
		Env:     env,
		Daily:   retention.Daily,
		Weekly:  retention.Weekly,
		Monthly: retention.Monthly,
		DryRun:  true,
		List:    true,
	})
	if errors.Is(err, borg.ErrNoRetention) {
		return borg.PrunePlan{}, nil
	}
	if err != nil {
		return borg.PrunePlan{}, err
	}
	return borg.ParsePruneList(result.Messages), nil
}

// HorizonUnit est l'unité de la profondeur d'historique.
type HorizonUnit int

const (
	// HorizonAll : aucune règle, rien n'est jamais supprimé.
	HorizonAll HorizonUnit = iota
	HorizonDays
	HorizonWeeks
	HorizonMonths
)

// RetentionHorizon dit jusqu'où la conservation permet de revenir en arrière,
// pour la phrase en langage courant d'EF-71 : c'est la règle la plus longue
// qui fixe l'horizon.
func RetentionHorizon(retention config.Retention) (HorizonUnit, int) {
	switch {
	case retention.Monthly > 0:
		return HorizonMonths, retention.Monthly
	case retention.Weekly > 0:
		return HorizonWeeks, retention.Weekly
	case retention.Daily > 0:
		return HorizonDays, retention.Daily
	default:
		return HorizonAll, 0
	}
}
