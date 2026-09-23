package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"

	"leblanc.io/open-go-borg-ui/internal/config"
	"leblanc.io/open-go-borg-ui/internal/core"
)

// maxRetention borne chaque règle de conservation : au-delà, une saisie
// erronée plutôt qu'un choix.
const maxRetention = 1000

// maxListed borne la liste des sauvegardes montrées avant confirmation.
const maxListed = 20

// commandRetention affiche ou modifie la conservation (EF-70 à EF-72).
//
//	borgui retention
//	borgui retention set <jours> <semaines> <mois> [--yes]
func (a *app) commandRetention(ctx context.Context, args []string) (int, error) {
	action, rest := subcommand(args)
	switch action {
	case "":
		profile, err := a.profile()
		if err != nil {
			return exitError, err
		}
		fmt.Println(a.describeRetention(profile.Retention))
		return exitSuccess, nil
	case "set":
		return a.retentionSet(ctx, rest)
	default:
		return exitError, fmt.Errorf("%s", a.T("retention.usage"))
	}
}

// retentionSet montre ce que la nouvelle conservation supprimerait, demande
// confirmation, puis l'enregistre (EF-72). La suppression elle-même a lieu à
// la sauvegarde suivante.
func (a *app) retentionSet(ctx context.Context, args []string) (int, error) {
	var yes bool
	flags := flag.NewFlagSet("retention set", flag.ContinueOnError)
	flags.BoolVar(&yes, "yes", false, "confirme sans poser de question")
	positional, err := parseInterleaved(flags, args)
	if err != nil || len(positional) != 3 {
		return exitError, fmt.Errorf("%s", a.T("retention.usage"))
	}
	var values [3]int
	for i, raw := range positional {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 || value > maxRetention {
			return exitError, fmt.Errorf("%s", a.T("retention.invalid", map[string]any{"Max": maxRetention}))
		}
		values[i] = value
	}
	retention := config.Retention{Daily: values[0], Weekly: values[1], Monthly: values[2]}

	cfg, err := config.Load(a.configPath)
	if err != nil {
		return exitError, err
	}
	profile, err := cfg.Profile(a.profileName)
	if err != nil {
		return exitError, err
	}
	if err := a.ensurePassphrase(profile); err != nil {
		return exitError, err
	}
	runner, err := a.runner()
	if err != nil {
		return a.reportMissingEngine(err)
	}
	env, err := a.environment(profile)
	if err != nil {
		return exitError, err
	}

	fmt.Println(a.describeRetention(retention))
	plan, err := core.PreviewRetention(ctx, runner, env, retention)
	if err != nil {
		return exitError, err
	}
	if len(plan.Pruned) == 0 {
		fmt.Println(a.T("retention.nothing_removed"))
	} else {
		fmt.Println(a.T("retention.would_remove", map[string]any{"Count": len(plan.Pruned)}))
		for i, name := range plan.Pruned {
			if i == maxListed {
				fmt.Println(a.T("retention.more", map[string]any{"Count": len(plan.Pruned) - maxListed}))
				break
			}
			fmt.Println(a.T("cli.details", map[string]any{"Message": name}))
		}
		if !yes {
			confirmed, err := a.confirm(a.T("retention.confirm"))
			if err != nil {
				return exitError, err
			}
			if !confirmed {
				fmt.Println(a.T("retention.cancelled"))
				return exitSuccess, nil
			}
		}
	}

	profile.Retention = retention
	if err := config.Save(a.configPath, cfg); err != nil {
		return exitError, err
	}
	fmt.Println(a.T("retention.saved"))
	return exitSuccess, nil
}

// describeRetention rend la conservation et sa profondeur en langage courant
// (EF-71).
func (a *app) describeRetention(retention config.Retention) string {
	line := a.T("retention.rules", map[string]any{
		"Daily": retention.Daily, "Weekly": retention.Weekly, "Monthly": retention.Monthly,
	})
	unit, count := core.RetentionHorizon(retention)
	var horizon string
	switch unit {
	case core.HorizonMonths:
		horizon = a.T("retention.horizon_months", map[string]any{"Count": count})
	case core.HorizonWeeks:
		horizon = a.T("retention.horizon_weeks", map[string]any{"Count": count})
	case core.HorizonDays:
		horizon = a.T("retention.horizon_days", map[string]any{"Count": count})
	default:
		horizon = a.T("retention.horizon_all")
	}
	return line + "\n" + a.T("cli.details", map[string]any{"Message": horizon})
}

// confirm pose une question fermée. Sans terminal, personne ne peut
// répondre : la question est refusée plutôt que tenue pour acquise.
func (a *app) confirm(question string) (bool, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false, fmt.Errorf("%s", a.T("retention.confirm_required"))
	}
	fmt.Print(question + " ")
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "o", "oui", "y", "yes":
		return true, nil
	}
	return false, nil
}
