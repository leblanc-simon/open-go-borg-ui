package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/core"
)

// commandRestore restaure une sauvegarde dans un dossier neuf.
//
// La destination n'est jamais l'emplacement d'origine (EF-95) : une
// restauration ne doit pas pouvoir écraser ce qu'elle est censée réparer. Un
// dossier qui existe déjà et n'est pas vide est refusé.
//
//	borgui restore [--to <dossier>] [<sauvegarde> [<chemin>…]]
//
// Sans nom, la sauvegarde la plus récente est restaurée. Les chemins sont ceux
// que liste la sauvegarde : la lettre de lecteur en première composante sous
// Windows (c/Users/marc/Documents), le chemin sans « / » initial sous Linux.
func (a *app) commandRestore(ctx context.Context, args []string) (int, error) {
	var destination string
	flags := flag.NewFlagSet("restore", flag.ContinueOnError)
	flags.StringVar(&destination, "to", "", "dossier où restaurer, qui doit être neuf ou vide")
	if err := flags.Parse(args); err != nil {
		return exitError, err
	}
	rest := flags.Args()

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

	var name string
	var paths []string
	if len(rest) > 0 {
		name, paths = rest[0], rest[1:]
	} else {
		list, _, err := borg.List(ctx, runner, env)
		if err != nil {
			return exitError, err
		}
		if len(list.Archives) == 0 {
			return exitError, fmt.Errorf("%s", a.T("archives.empty"))
		}
		name = latest(list.Archives).Name
	}

	if destination == "" {
		// Les deux-points de l'horodatage sont interdits dans un nom de
		// fichier Windows.
		destination = "borgui-" + strings.ReplaceAll(name, ":", "-")
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return exitError, err
	}
	if err := core.PrepareDestination(destination); err != nil {
		if errors.Is(err, core.ErrDestinationNotEmpty) {
			return exitError, fmt.Errorf("%s", a.T("restore.not_empty", map[string]any{"Path": destination}))
		}
		return exitError, err
	}

	fmt.Println(a.T("restore.starting", map[string]any{"Name": name, "Path": destination}))

	started := time.Now()
	result, err := borg.Extract(ctx, runner, borg.ExtractOptions{
		Env:         env,
		Archive:     name,
		Paths:       paths,
		Destination: destination,
		OnEvent:     a.restoreProgress(),
	})
	progressDone()
	if err != nil {
		return exitError, err
	}

	fmt.Println(a.T("restore.finished", map[string]any{
		"Path":     destination,
		"Duration": a.formatDuration(time.Since(started)),
	}))
	if result.Status == borg.StatusWarning {
		warnings := result.Warnings()
		fmt.Println(a.T("restore.warnings", map[string]any{"Count": len(warnings)}))
		for _, warning := range warnings {
			fmt.Println(a.T("cli.details", map[string]any{"Message": warning.Text}))
		}
		return exitWarning, nil
	}
	return exitSuccess, nil
}

// latest retourne la sauvegarde la plus récente. Borg les liste dans l'ordre
// chronologique, mais l'ordre n'est pas une garantie sur laquelle fonder le
// choix de ce qui sera restauré.
func latest(archives []borg.Archive) borg.Archive {
	best := archives[0]
	for _, archive := range archives[1:] {
		if archive.Start.After(best.Start.Time) {
			best = archive
		}
	}
	return best
}

// restoreProgress affiche l'avancement de l'extraction.
func (a *app) restoreProgress() func(borg.Event) {
	lastReport := time.Now()
	return func(event borg.Event) {
		switch event.Kind {
		case borg.EventProgressPercent:
			percent, ok := event.Percent()
			if !ok || event.Finished || time.Since(lastReport) < 200*time.Millisecond {
				return
			}
			lastReport = time.Now()
			progressLine(a.T("restore.progress", map[string]any{
				"Percent": fmt.Sprintf("%.0f", percent),
			}))
		case borg.EventLog:
			if interactive() && (event.Level == "WARNING" || event.Level == "ERROR") {
				notice(a.T("cli.details", map[string]any{"Message": event.Message}))
			}
		}
	}
}
