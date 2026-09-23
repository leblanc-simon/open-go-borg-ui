package core

import (
	"context"
	"errors"
	"time"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/cloudfiles"
	"leblanc.io/open-go-borg-ui/internal/config"
	"leblanc.io/open-go-borg-ui/internal/history"
)

// Phase est l'étape en cours d'une sauvegarde, pour l'affichage.
type Phase int

const (
	// PhaseCloudScan : repérage des fichiers à la demande, avant Borg. Il
	// peut prendre quelques secondes sur un dossier synchronisé volumineux.
	PhaseCloudScan Phase = iota
	// PhaseBackup : Borg est à l'œuvre.
	PhaseBackup
)

// Backup exécute les sauvegardes d'un poste.
type Backup struct {
	Runner borg.Runner
	// History reçoit chaque exécution. Nil, rien n'est consigné.
	History *history.Store
	// ScanCloud repère les fichiers à la demande. Nil, cloudfiles.Scan.
	ScanCloud func(context.Context, []string) ([]string, error)
	// Now donne l'heure. Nil, time.Now.
	Now func() time.Time
}

// BackupRequest décrit une sauvegarde à exécuter.
type BackupRequest struct {
	Profile *config.Profile
	Env     borg.Environment
	// DryRun parcourt les dossiers sans rien écrire ; l'exécution n'est pas
	// consignée, puisqu'aucune sauvegarde n'en résulte.
	DryRun bool
	// OnPhase et OnEvent rendent compte de l'avancement. Peuvent être nil.
	OnPhase func(Phase)
	OnEvent func(borg.Event)
}

// BackupReport est l'issue d'une sauvegarde.
type BackupReport struct {
	// Run est l'exécution telle qu'elle est consignée.
	Run history.Run
	// Stats et Result sont ceux de Borg, quand il a été lancé.
	Stats  *borg.CreateStats
	Result *borg.Result
	// CloudSkipped liste les fichiers à la demande écartés.
	CloudSkipped []string
	// HistoryErr signale un historique qui n'a pas pu être tenu. Il
	// n'empêche jamais une sauvegarde : l'historique est un témoin, pas une
	// condition.
	HistoryErr error
}

// Run exécute la sauvegarde et la consigne.
//
// L'erreur retournée est celle de la sauvegarde elle-même ; le rapport est
// toujours renseigné, y compris en cas d'échec, pour que l'appelant puisse
// l'afficher.
func (b *Backup) Run(ctx context.Context, req BackupRequest) (*BackupReport, error) {
	now := b.Now
	if now == nil {
		now = time.Now
	}
	scanCloud := b.ScanCloud
	if scanCloud == nil {
		scanCloud = cloudfiles.Scan
	}
	phase := func(p Phase) {
		if req.OnPhase != nil {
			req.OnPhase(p)
		}
	}

	report := &BackupReport{Run: history.Run{
		Profile: req.Profile.Name,
		Started: now(),
		Status:  history.StatusRunning,
	}}
	record := b.History != nil && !req.DryRun
	if record {
		id, err := b.History.Begin(ctx, report.Run.Profile, report.Run.Started)
		if err != nil {
			report.HistoryErr = err
			record = false
		}
		report.Run.ID = id
	}

	err := b.run(ctx, req, report, scanCloud, phase)

	report.Run.Finished = now()
	classify(ctx, report, err)
	if record {
		// L'exécution annulée a un contexte annulé : la consigner exige un
		// contexte qui ne l'est pas.
		if finishErr := b.History.Finish(context.WithoutCancel(ctx), report.Run); finishErr != nil {
			report.HistoryErr = finishErr
		}
	}
	return report, err
}

// run fait le travail proprement dit.
func (b *Backup) run(ctx context.Context, req BackupRequest, report *BackupReport,
	scanCloud func(context.Context, []string) ([]string, error), phase func(Phase)) error {
	profile := req.Profile

	if !profile.IncludeCloudPlaceholders {
		phase(PhaseCloudScan)
		skipped, err := scanCloud(ctx, profile.Sources)
		if err != nil {
			return err
		}
		report.CloudSkipped = skipped
		report.Run.CloudSkipped = len(skipped)
	}

	phase(PhaseBackup)
	stats, result, err := borg.Create(ctx, b.Runner, borg.CreateOptions{
		Env:           req.Env,
		Sources:       profile.Sources,
		Excludes:      profile.Excludes,
		ExcludePaths:  report.CloudSkipped,
		ExcludeCaches: profile.ExcludeCaches,
		OneFileSystem: profile.OneFileSystem,
		Compression:   profile.Compression,
		DryRun:        req.DryRun,
		OnEvent:       req.OnEvent,
	})
	report.Stats, report.Result = stats, result
	if stats != nil {
		report.Run.Archive = stats.Archive.Name
		report.Run.Files = stats.Archive.Stats.NFiles
		report.Run.OriginalSize = stats.Archive.Stats.OriginalSize
		report.Run.DeduplicatedSize = stats.Archive.Stats.DeduplicatedSize
	}
	report.Run.Warnings = len(result.Warnings())
	return err
}

// classify fixe le statut consigné de l'exécution.
func classify(ctx context.Context, report *BackupReport, err error) {
	switch {
	case err == nil && report.Result != nil && report.Result.Status == borg.StatusWarning:
		report.Run.Status = history.StatusWarning
	case err == nil:
		report.Run.Status = history.StatusSuccess
	case errors.Is(err, context.Canceled) || ctx.Err() != nil:
		report.Run.Status = history.StatusCancelled
	default:
		report.Run.Status = history.StatusError
		report.Run.ErrorKey = borg.FailureUnknown.TranslationKey()
		if failure, failed := report.Result.Diagnose(); failed {
			report.Run.ErrorKey = failure.TranslationKey()
		}
		report.Run.Detail = err.Error()
	}
}
