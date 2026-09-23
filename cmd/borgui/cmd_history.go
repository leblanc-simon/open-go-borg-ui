package main

import (
	"context"
	"flag"
	"fmt"

	"leblanc.io/open-go-borg-ui/internal/history"
)

// commandHistory affiche les dernières exécutions du poste.
func (a *app) commandHistory(ctx context.Context, args []string) (int, error) {
	var limit int
	flags := flag.NewFlagSet("history", flag.ContinueOnError)
	flags.IntVar(&limit, "n", 20, "nombre d'exécutions affichées")
	if err := flags.Parse(args); err != nil {
		return exitError, err
	}

	profile, err := a.Profile()
	if err != nil {
		return exitError, err
	}
	store, err := a.History()
	if err != nil {
		return exitError, err
	}
	defer store.Close()

	runs, err := store.Recent(ctx, profile.Name, limit)
	if err != nil {
		return exitError, err
	}
	if len(runs) == 0 {
		fmt.Println(a.T("history.empty"))
		return exitSuccess, nil
	}

	for _, run := range runs {
		fmt.Println(a.historyLine(run))
		if run.CloudSkipped > 0 {
			fmt.Println(a.T("cli.details", map[string]any{
				"Message": a.T("backup.cloud_skipped", map[string]any{"Count": run.CloudSkipped}),
			}))
		}
		if run.ErrorKey != "" {
			fmt.Println(a.T("cli.details", map[string]any{"Message": a.T(run.ErrorKey)}))
		}
	}
	return exitSuccess, nil
}

// historyLine rend une exécution sur une ligne.
func (a *app) historyLine(run history.Run) string {
	data := map[string]any{
		"Date":   run.Started.Local().Format("2006-01-02 15:04"),
		"Status": a.T(run.Status.TranslationKey()),
	}
	switch run.Status {
	case history.StatusRunning:
		// Une exécution qui ne s'est jamais terminée : en cours ailleurs,
		// ou interrompue par une extinction ou un plantage.
		return a.T("history.entry_running", data)
	case history.StatusSuccess, history.StatusWarning:
		data["Duration"] = a.formatDuration(run.Duration())
		data["Files"] = run.Files
		data["Original"] = formatSize(run.OriginalSize)
		data["Stored"] = formatSize(run.DeduplicatedSize)
		return a.T("history.entry_done", data)
	default:
		data["Duration"] = a.formatDuration(run.Duration())
		return a.T("history.entry_failed", data)
	}
}
