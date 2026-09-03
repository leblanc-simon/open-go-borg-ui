package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"leblanc.io/open-go-borg-ui/internal/borg"
)

// commandBackup exécute une sauvegarde.
func (a *app) commandBackup(ctx context.Context, args []string) (int, error) {
	var dryRun bool
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	flags.BoolVar(&dryRun, "dry-run", false, "parcourt les dossiers sans rien écrire")
	if err := flags.Parse(args); err != nil {
		return exitError, err
	}

	profile, err := a.profile()
	if err != nil {
		return exitError, err
	}
	if len(profile.Sources) == 0 {
		return exitError, fmt.Errorf("%s", a.T("backup.no_sources"))
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

	fmt.Println(a.T("backup.starting", map[string]any{"Count": len(profile.Sources)}))

	started := time.Now()
	stats, result, err := borg.Create(ctx, runner, borg.CreateOptions{
		Env:           env,
		Sources:       profile.Sources,
		Excludes:      profile.Excludes,
		ExcludeCaches: profile.ExcludeCaches,
		OneFileSystem: profile.OneFileSystem,
		Compression:   profile.Compression,
		DryRun:        dryRun,
		OnEvent:       a.backupProgress(),
	})
	progressDone()

	if err != nil {
		return exitError, err
	}

	elapsed := time.Since(started)
	summary := map[string]any{
		"Files":    stats.Archive.Stats.NFiles,
		"Original": formatSize(stats.Archive.Stats.OriginalSize),
		"Stored":   formatSize(stats.Archive.Stats.DeduplicatedSize),
		"Duration": a.formatDuration(elapsed),
	}
	// Un débit calculé sur moins d'une seconde n'apprend rien et affiche des
	// valeurs absurdes.
	if rate, ok := formatRate(stats.Archive.Stats.OriginalSize, elapsed); ok {
		summary["Rate"] = rate
		fmt.Println(a.T("backup.finished_rate", summary))
	} else {
		fmt.Println(a.T("backup.finished", summary))
	}
	if stats.Archive.Name != "" {
		fmt.Println(a.T("backup.archive_name", map[string]any{"Name": stats.Archive.Name}))
	}

	// Un code de retour 1 signale des fichiers illisibles : la sauvegarde
	// existe et reste exploitable, elle est simplement incomplète (EF-56).
	if result.Status == borg.StatusWarning {
		warnings := result.Warnings()
		fmt.Println(a.T("backup.warnings", map[string]any{"Count": len(warnings)}))
		for _, warning := range warnings {
			fmt.Println(a.T("cli.details", map[string]any{"Message": warning.Text}))
		}
		return exitWarning, nil
	}
	return exitSuccess, nil
}

// backupProgress affiche l'avancement sur la sortie d'erreur.
func (a *app) backupProgress() func(borg.Event) {
	lastReport := time.Now()
	return func(event borg.Event) {
		switch event.Kind {
		case borg.EventArchiveProgress:
			if event.Finished || time.Since(lastReport) < 200*time.Millisecond {
				return
			}
			lastReport = time.Now()
			progressLine(a.T("backup.progress", map[string]any{
				"Files": event.Files,
				"Size":  formatSize(event.OriginalSize),
				"Path":  truncatePath(event.Path, 60),
			}))
		case borg.EventLog:
			// Devant un utilisateur, les avertissements apparaissent au fil de
			// l'eau : attendre la fin d'une sauvegarde longue pour les montrer
			// n'aide personne. Dans un journal, le récapitulatif final suffit
			// et évite de tout écrire deux fois.
			if interactive() && (event.Level == "WARNING" || event.Level == "ERROR") {
				notice(a.T("cli.details", map[string]any{"Message": event.Message}))
			}
		}
	}
}

// truncatePath raccourcit un chemin par la gauche pour tenir sur une ligne.
func truncatePath(path string, width int) string {
	runes := []rune(path)
	if len(runes) <= width {
		return path
	}
	return "…" + string(runes[len(runes)-width+1:])
}
