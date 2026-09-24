package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/core"
	"leblanc.io/open-go-borg-ui/internal/lock"
)

// commandBackup exécute une sauvegarde.
func (a *app) commandBackup(ctx context.Context, args []string) (int, error) {
	var dryRun bool
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	flags.BoolVar(&dryRun, "dry-run", false, "parcourt les dossiers sans rien écrire")
	if err := flags.Parse(args); err != nil {
		return exitError, err
	}

	profile, err := a.Profile()
	if err != nil {
		return exitError, err
	}
	if len(profile.Sources) == 0 {
		return exitError, fmt.Errorf("%s", a.T("backup.no_sources"))
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
		// Sans historique, la sauvegarde reste possible : c'est un témoin,
		// pas une condition.
		notice(a.T("history.unavailable", map[string]any{"Message": err.Error()}))
	} else {
		defer store.Close()
	}

	fmt.Println(a.T("backup.starting", map[string]any{"Count": len(profile.Sources)}))

	service := a.BackupService(profile, runner, store)
	report, err := service.Run(ctx, core.BackupRequest{
		Profile: profile,
		Env:     env,
		DryRun:  dryRun,
		OnPhase: func(phase core.Phase) {
			switch phase {
			case core.PhaseCloudScan:
				progressLine(a.T("backup.cloud_scan"))
			case core.PhasePrune:
				progressLine(a.T("backup.pruning"))
			case core.PhaseCompact:
				progressLine(a.T("backup.compacting"))
			case core.PhaseVerify:
				progressLine(a.T("backup.verifying"))
			}
		},
		OnEvent: a.backupProgress(),
	})
	progressDone()

	if report.HistoryErr != nil {
		notice(a.T("history.unavailable", map[string]any{"Message": report.HistoryErr.Error()}))
	}
	if report.PublishErr != nil {
		notice(a.T("state.publish_failed", map[string]any{"Message": report.PublishErr.Error()}))
	}
	if len(report.CloudSkipped) > 0 {
		// Signalé même en cas d'échec : c'est une information sur ce que la
		// sauvegarde couvre, indépendante de son issue.
		fmt.Println(a.T("backup.cloud_skipped", map[string]any{"Count": len(report.CloudSkipped)}))
	}
	if errors.Is(err, lock.ErrBusy) {
		return exitError, fmt.Errorf("%s", a.T(core.ErrorKeyAlreadyRunning))
	}
	if err != nil {
		return exitError, err
	}

	stats := report.Stats
	elapsed := report.Run.Duration()
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

	if check := report.Verification; check != nil {
		// La vérification mensuelle a eu lieu : son issue est rapportée,
		// sans changer celle de la sauvegarde, déjà faite (EF-99).
		fmt.Println(a.T(check.Reason, map[string]any{"Path": check.Native}))
	}
	if report.VerifyErr != nil {
		notice(a.T("verify.impossible"))
		notice(a.T("cli.details", map[string]any{"Message": report.VerifyErr.Error()}))
	}

	code := exitSuccess
	if report.MaintenanceErr != nil {
		fmt.Println(a.T(core.ErrorKeyMaintenance))
		fmt.Println(a.T("cli.details", map[string]any{"Message": report.MaintenanceErr.Error()}))
		code = exitWarning
	}

	// Un code de retour 1 signale des fichiers illisibles : la sauvegarde
	// existe et reste exploitable, elle est simplement incomplète (EF-56).
	if report.Result.Status == borg.StatusWarning {
		warnings := report.Result.Warnings()
		fmt.Println(a.T("backup.warnings", map[string]any{"Count": len(warnings)}))
		for _, warning := range warnings {
			fmt.Println(a.T("cli.details", map[string]any{"Message": warning.Text}))
		}
		return exitWarning, nil
	}
	return code, nil
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
