package borg

import (
	"context"
	"errors"
	"strconv"
)

// ErrNoRetention signale une rétention vide. Borg refuserait la rotation ; le
// cœur ne doit de toute façon jamais la lancer sans règle, de peur qu'un
// réglage manquant soit un jour interprété comme « ne rien garder ».
var ErrNoRetention = errors.New("borg: aucune règle de conservation")

// PruneOptions décrit une rotation.
type PruneOptions struct {
	Env Environment
	// Daily, Weekly et Monthly sont les nombres de sauvegardes quotidiennes,
	// hebdomadaires et mensuelles à conserver (EF-70).
	Daily, Weekly, Monthly int
	// DryRun simule la rotation sans rien supprimer : c'est l'aperçu montré
	// avant toute modification de la conservation (EF-72).
	DryRun bool
	// List demande le sort de chaque sauvegarde dans les messages.
	List bool
	// OnEvent reçoit la progression.
	OnEvent func(Event)
}

// Prune applique la rotation aux sauvegardes de ce poste.
//
// Seules les sauvegardes nommées d'après le poste sont concernées : même sur
// une destination qui ne devrait servir qu'à lui, une sauvegarde déposée par
// un autre outil ne disparaît pas par effet de bord.
func Prune(ctx context.Context, runner Runner, opts PruneOptions) (*Result, error) {
	if opts.Daily <= 0 && opts.Weekly <= 0 && opts.Monthly <= 0 {
		return nil, ErrNoRetention
	}
	flags := []string{"--glob-archives", "{hostname}-*"}
	for _, rule := range []struct {
		flag  string
		count int
	}{
		{"--keep-daily", opts.Daily},
		{"--keep-weekly", opts.Weekly},
		{"--keep-monthly", opts.Monthly},
	} {
		if rule.count > 0 {
			flags = append(flags, rule.flag, strconv.Itoa(rule.count))
		}
	}
	if opts.DryRun {
		flags = append(flags, "--dry-run")
	}
	if opts.List {
		flags = append(flags, "--list")
	}

	result, err := runner.Run(ctx, Command{
		Name:    "prune",
		Flags:   flags,
		Env:     opts.Env,
		LogJSON: true,
		OnEvent: opts.OnEvent,
	})
	if err != nil {
		return nil, err
	}
	if result.Status == StatusError {
		return result, failure(result, "prune")
	}
	return result, nil
}

// Compact récupère l'espace libéré par la rotation. Depuis Borg 1.2, la
// suppression d'une sauvegarde ne libère rien tant que le dépôt n'est pas
// compacté (EF-55).
func Compact(ctx context.Context, runner Runner, env Environment) (*Result, error) {
	result, err := runner.Run(ctx, Command{Name: "compact", Env: env, LogJSON: true})
	if err != nil {
		return nil, err
	}
	if result.Status == StatusError {
		return result, failure(result, "compact")
	}
	return result, nil
}

// BreakLock lève le verrou laissé sur la destination par une exécution
// interrompue brutalement (EF-58).
//
// À ne proposer que lorsqu'aucune exécution de ce poste n'est en cours : le
// verrou local l'établit (EF-57), et la destination n'est jamais partagée
// entre postes.
func BreakLock(ctx context.Context, runner Runner, env Environment) (*Result, error) {
	result, err := runner.Run(ctx, Command{Name: "break-lock", Env: env, LogJSON: true})
	if err != nil {
		return nil, err
	}
	if result.Status == StatusError {
		return result, failure(result, "break-lock")
	}
	return result, nil
}
