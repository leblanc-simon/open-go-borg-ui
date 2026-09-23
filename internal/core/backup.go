package core

import (
	"context"
	"errors"
	"time"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/cloudfiles"
	"leblanc.io/open-go-borg-ui/internal/config"
	"leblanc.io/open-go-borg-ui/internal/history"
	"leblanc.io/open-go-borg-ui/internal/lock"
	"leblanc.io/open-go-borg-ui/internal/statusfile"
)

// Phase est l'étape en cours d'une sauvegarde, pour l'affichage.
type Phase int

const (
	// PhaseCloudScan : repérage des fichiers à la demande, avant Borg. Il
	// peut prendre quelques secondes sur un dossier synchronisé volumineux.
	PhaseCloudScan Phase = iota
	// PhaseBackup : Borg est à l'œuvre.
	PhaseBackup
	// PhasePrune : application de la conservation (EF-55).
	PhasePrune
	// PhaseCompact : récupération de l'espace libéré.
	PhaseCompact
)

// Clés de diagnostic propres au cœur, sous la racine transverse des erreurs.
const (
	// ErrorKeyAlreadyRunning : une autre exécution tient le verrou local.
	ErrorKeyAlreadyRunning = "error.already_running"
	// ErrorKeyMaintenance : la sauvegarde a réussi, mais la conservation ou
	// la récupération d'espace a échoué.
	ErrorKeyMaintenance = "error.maintenance"
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
	// LockDir accueille les verrous locaux (EF-57). Vide, aucun verrou n'est
	// pris — réservé aux tests.
	LockDir string

	// Publish dépose l'état du poste dans son sous-compte (EF-83). Nil,
	// rien n'est publié.
	Publish func(context.Context, statusfile.Status) error
	// Hostname est le nom court du poste, celui des sauvegardes.
	Hostname string
	// NextRun donne la prochaine exécution planifiée, zéro si aucune.
	NextRun func() time.Time
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
	// MaintenanceErr signale l'échec de la conservation ou de la
	// récupération d'espace, après une sauvegarde réussie.
	MaintenanceErr error
	// PublishErr signale un état qui n'a pas pu être déposé. Comme
	// l'historique, il n'affecte pas la sauvegarde.
	PublishErr error
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

	var err error
	if b.LockDir != "" && !req.DryRun {
		var held *lock.Lock
		held, err = lock.Acquire(lock.PathFor(b.LockDir, req.Env.Repository))
		if err == nil {
			defer held.Release()
		}
	}
	if err == nil {
		err = b.run(ctx, req, report, scanCloud, phase)
	}
	if err == nil && !req.DryRun {
		report.MaintenanceErr = b.maintain(ctx, req, phase)
	}

	report.Run.Finished = now()
	classify(ctx, report, err)
	if record {
		// L'exécution annulée a un contexte annulé : la consigner exige un
		// contexte qui ne l'est pas.
		if finishErr := b.History.Finish(context.WithoutCancel(ctx), report.Run); finishErr != nil {
			report.HistoryErr = finishErr
		}
	}
	if b.Publish != nil && !req.DryRun && report.Run.ErrorKey != ErrorKeyAlreadyRunning {
		// Un refus pour exécution concurrente ne dit rien de l'état de la
		// destination : le publier remplacerait un bon état par un faux
		// échec. Une annulation, elle, se publie : le poste passerait
		// sinon pour à jour.
		status := b.status(context.WithoutCancel(ctx), req, report)
		report.PublishErr = b.Publish(context.WithoutCancel(ctx), status)
	}
	return report, err
}

// status construit l'état publié du poste.
func (b *Backup) status(ctx context.Context, req BackupRequest, report *BackupReport) statusfile.Status {
	run := report.Run
	status := statusfile.Status{
		Hostname:         b.Hostname,
		Started:          run.Started,
		Finished:         run.Finished,
		Result:           statusfile.Result(run.Status),
		ErrorKey:         run.ErrorKey,
		Files:            run.Files,
		OriginalSize:     run.OriginalSize,
		DeduplicatedSize: run.DeduplicatedSize,
		Encryption:       req.Profile.Encryption.BorgMode(),
	}
	if b.NextRun != nil {
		status.NextRun = b.NextRun()
	}

	switch run.Status {
	case history.StatusSuccess, history.StatusWarning:
		status.LastSuccess = run.Finished
		// La destination vient de répondre : l'interroger sur l'espace
		// occupé ne coûte qu'un aller-retour. Un échec laisse la taille
		// inconnue, sans plus.
		if info, _, err := borg.Info(ctx, b.Runner, req.Env); err == nil {
			status.RepositorySize = info.Cache.Stats.UniqueCSize
			if info.Encryption.Mode != "" {
				status.Encryption = info.Encryption.Mode
			}
		}
	default:
		if b.History != nil {
			status.LastSuccess, _ = b.History.LastSuccess(ctx, run.Profile)
		}
	}
	return status
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

// maintain applique la conservation puis récupère l'espace (EF-55).
func (b *Backup) maintain(ctx context.Context, req BackupRequest, phase func(Phase)) error {
	retention := req.Profile.Retention
	phase(PhasePrune)
	_, err := borg.Prune(ctx, b.Runner, borg.PruneOptions{
		Env:     req.Env,
		Daily:   retention.Daily,
		Weekly:  retention.Weekly,
		Monthly: retention.Monthly,
	})
	if errors.Is(err, borg.ErrNoRetention) {
		// Sans règle, on garde tout : rien à supprimer, rien à compacter.
		return nil
	}
	if err != nil {
		return err
	}
	phase(PhaseCompact)
	_, err = borg.Compact(ctx, b.Runner, req.Env)
	return err
}

// classify fixe le statut consigné de l'exécution.
func classify(ctx context.Context, report *BackupReport, err error) {
	if err == nil && report.MaintenanceErr != nil {
		// La sauvegarde existe et reste exploitable : c'est un
		// avertissement, pas un échec. Une annulation pendant la
		// maintenance reste une annulation.
		if ctx.Err() != nil {
			report.Run.Status = history.StatusCancelled
			return
		}
		report.Run.Status = history.StatusWarning
		report.Run.ErrorKey = ErrorKeyMaintenance
		report.Run.Detail = report.MaintenanceErr.Error()
		return
	}
	switch {
	case err == nil && report.Result != nil && report.Result.Status == borg.StatusWarning:
		report.Run.Status = history.StatusWarning
	case err == nil:
		report.Run.Status = history.StatusSuccess
	case errors.Is(err, context.Canceled) || ctx.Err() != nil:
		report.Run.Status = history.StatusCancelled
	case errors.Is(err, lock.ErrBusy):
		report.Run.Status = history.StatusError
		report.Run.ErrorKey = ErrorKeyAlreadyRunning
	default:
		report.Run.Status = history.StatusError
		report.Run.ErrorKey = borg.FailureUnknown.TranslationKey()
		if failure, failed := report.Result.Diagnose(); failed {
			report.Run.ErrorKey = failure.TranslationKey()
		}
		report.Run.Detail = err.Error()
	}
}
