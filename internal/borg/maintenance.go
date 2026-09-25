package borg

import (
	"context"
	"errors"
	"strconv"
	"strings"
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

// PrunePlan est le sort des sauvegardes selon une rotation : celles qui
// restent et celles qui partent. C'est ce que l'utilisateur voit avant de
// confirmer un changement de conservation (EF-72).
type PrunePlan struct {
	Kept   []string
	Pruned []string
}

// ParsePruneList lit les messages de « borg prune --list ». Borg 1.4 écrit
// une ligne par sauvegarde : un libellé aligné sur 40 colonnes, puis le nom.
//
//	Keeping archive (rule: daily #1):        poste-2026-09-24T22:00:00 Thu, …
//	Would prune:                             poste-2026-09-01T22:00:00 Mon, …
//	Pruning archive (2/5):                   poste-2026-09-01T22:00:00 Mon, …
//
// Le libellé est reconnu à son début et le nom pris juste après lui, plutôt
// qu'à la 41e colonne : un libellé plus long que prévu repousse l'alignement.
func ParsePruneList(messages []Message) PrunePlan {
	var plan PrunePlan
	for _, m := range messages {
		text := m.Text
		var rest string
		var pruned bool
		switch {
		case strings.HasPrefix(text, "Would prune:"):
			rest, pruned = strings.TrimPrefix(text, "Would prune:"), true
		case strings.HasPrefix(text, "Pruning archive ("), strings.HasPrefix(text, "Keeping archive ("):
			end := strings.Index(text, "):")
			if end < 0 {
				continue
			}
			rest, pruned = text[end+2:], strings.HasPrefix(text, "Pruning")
		case strings.HasPrefix(text, "Keeping checkpoint archive:"):
			rest = strings.TrimPrefix(text, "Keeping checkpoint archive:")
		default:
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		if pruned {
			plan.Pruned = append(plan.Pruned, fields[0])
		} else {
			plan.Kept = append(plan.Kept, fields[0])
		}
	}
	return plan
}

// Check contrôle l'intégrité de la destination, sans relire les sauvegardes
// elles-mêmes : « --repository-only » vérifie les segments côté serveur,
// sans les faire transiter (EF-87). Borg y conserve des sommes de contrôle
// même sans chiffrement ; en mode non chiffré, seule la corruption
// accidentelle se détecte, pas une altération malveillante (addendum §3).
//
// Le code de retour 1 signale des anomalies trouvées : ce n'est pas une
// erreur d'exécution, le résultat se lit dans Result.Status. Seul un
// contrôle qui n'a pas pu se dérouler retourne une erreur.
func Check(ctx context.Context, runner Runner, env Environment, onEvent func(Event)) (*Result, error) {
	result, err := runner.Run(ctx, Command{
		Name:    "check",
		Flags:   []string{"--repository-only", "--progress"},
		Env:     env,
		LogJSON: true,
		OnEvent: onEvent,
	})
	if err != nil {
		return nil, err
	}
	if result.Status == StatusError {
		return result, failure(result, "check")
	}
	return result, nil
}
