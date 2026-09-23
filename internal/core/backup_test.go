package core

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/config"
	"leblanc.io/open-go-borg-ui/internal/history"
	"leblanc.io/open-go-borg-ui/internal/lock"
)

// fakeRunner rejoue une issue de Borg pour « create », répond par un succès
// aux autres commandes sauf celles de failing, et retient tout ce qu'il reçoit.
type fakeRunner struct {
	result *borg.Result
	err    error
	// block attend l'annulation du contexte avant de répondre.
	block bool
	// failing liste les sous-commandes qui échouent.
	failing  map[string]bool
	received borg.Command
	names    []string
}

func (f *fakeRunner) Run(ctx context.Context, cmd borg.Command) (*borg.Result, error) {
	f.names = append(f.names, cmd.Name)
	if cmd.Name != "create" {
		if f.failing[cmd.Name] {
			return &borg.Result{Status: borg.StatusError, ExitCode: 2}, nil
		}
		return &borg.Result{Status: borg.StatusSuccess}, nil
	}
	f.received = cmd
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.result, f.err
}
func (f *fakeRunner) Version(context.Context) (string, error) { return "1.4.5", nil }
func (f *fakeRunner) Executable() string                      { return "borg" }

// statsJSON est la sortie de « borg create --json » d'une sauvegarde réussie.
const statsJSON = `{"archive":{"name":"poste-2026-09-23T22:00:00","stats":{"nfiles":42,"original_size":1048576,"deduplicated_size":4096}}}`

// setup construit le service avec un historique neuf et une heure qui avance
// d'une minute à chaque lecture.
func setup(t *testing.T, runner *fakeRunner, cloud []string) (*Backup, *history.Store, *int) {
	t.Helper()
	store, err := history.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	clock := time.Date(2026, 9, 23, 22, 0, 0, 0, time.Local)
	scans := 0
	return &Backup{
		Runner:  runner,
		History: store,
		ScanCloud: func(context.Context, []string) ([]string, error) {
			scans++
			return cloud, nil
		},
		Now: func() time.Time { clock = clock.Add(time.Minute); return clock },
	}, store, &scans
}

// profile est le profil de test.
func profile() *config.Profile {
	p := config.Default("poste")
	p.Sources = []string{`C:\Users\marc\Documents`, `C:\Users\marc\OneDrive`}
	return &p
}

// lastRun relit la dernière exécution consignée.
func lastRun(t *testing.T, store *history.Store) history.Run {
	t.Helper()
	runs, err := store.Recent(context.Background(), "poste", 1)
	if err != nil || len(runs) != 1 {
		t.Fatalf("historique: %d exécutions, erreur %v", len(runs), err)
	}
	return runs[0]
}

// TestSauvegardeReussie vérifie qu'une sauvegarde écarte les fichiers à la
// demande, transmet leurs chemins au Runner et consigne son résultat.
func TestSauvegardeReussie(t *testing.T) {
	runner := &fakeRunner{result: &borg.Result{Status: borg.StatusSuccess, Stdout: []byte(statsJSON)}}
	cloud := []string{`C:\Users\marc\OneDrive\a.docx`, `C:\Users\marc\OneDrive\b.pdf`}
	service, store, _ := setup(t, runner, cloud)

	report, err := service.Run(context.Background(), BackupRequest{Profile: profile()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(runner.received.ExcludePaths) != 2 {
		t.Errorf("chemins écartés transmis: %v", runner.received.ExcludePaths)
	}
	if report.HistoryErr != nil {
		t.Errorf("historique: %v", report.HistoryErr)
	}

	run := lastRun(t, store)
	if run.Status != history.StatusSuccess || run.Files != 42 || run.CloudSkipped != 2 ||
		run.Archive != "poste-2026-09-23T22:00:00" || run.Duration() != time.Minute {
		t.Errorf("exécution consignée: %+v", run)
	}
}

// TestAvertissements vérifie qu'un code 1 est consigné comme un succès avec
// avertissements, jamais comme un échec (EF-56).
func TestAvertissements(t *testing.T) {
	runner := &fakeRunner{result: &borg.Result{
		Status: borg.StatusWarning, ExitCode: 1, Stdout: []byte(statsJSON),
		Messages: []borg.Message{{Level: "WARNING", Text: "rapport.odt: fichier verrouillé"}},
	}}
	service, store, _ := setup(t, runner, nil)

	if _, err := service.Run(context.Background(), BackupRequest{Profile: profile()}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if run := lastRun(t, store); run.Status != history.StatusWarning || run.Warnings != 1 {
		t.Errorf("exécution consignée: %+v", run)
	}
}

// TestEchecDiagnostique vérifie qu'un échec est consigné avec la clé de son
// diagnostic, jamais avec un libellé.
func TestEchecDiagnostique(t *testing.T) {
	runner := &fakeRunner{result: &borg.Result{
		Status: borg.StatusError, ExitCode: 2,
		Messages: []borg.Message{{Level: "ERROR", MsgID: "LockTimeout", Text: "Failed to create/acquire the lock"}},
	}}
	service, store, _ := setup(t, runner, nil)

	if _, err := service.Run(context.Background(), BackupRequest{Profile: profile()}); err == nil {
		t.Fatal("un échec de Borg doit être retourné")
	}
	run := lastRun(t, store)
	if run.Status != history.StatusError || run.ErrorKey != "error.repository_locked" || run.Detail == "" {
		t.Errorf("exécution consignée: %+v", run)
	}
}

// TestAnnulation vérifie qu'une sauvegarde annulée est consignée comme telle,
// bien que son contexte soit annulé au moment de l'écrire.
func TestAnnulation(t *testing.T) {
	runner := &fakeRunner{block: true}
	service, store, _ := setup(t, runner, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()
	_, err := service.Run(ctx, BackupRequest{Profile: profile()})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("erreur %v, attendu context.Canceled", err)
	}
	if run := lastRun(t, store); run.Status != history.StatusCancelled || run.Finished.IsZero() {
		t.Errorf("exécution consignée: %+v", run)
	}
}

// TestFichiersEnLigneInclus vérifie qu'un profil qui demande les fichiers à
// la demande ne déclenche aucun repérage.
func TestFichiersEnLigneInclus(t *testing.T) {
	runner := &fakeRunner{result: &borg.Result{Status: borg.StatusSuccess, Stdout: []byte(statsJSON)}}
	service, _, scans := setup(t, runner, []string{`C:\Users\marc\OneDrive\a.docx`})

	p := profile()
	p.IncludeCloudPlaceholders = true
	if _, err := service.Run(context.Background(), BackupRequest{Profile: p}); err != nil {
		t.Fatal(err)
	}
	if *scans != 0 || len(runner.received.ExcludePaths) != 0 {
		t.Errorf("repérages %d, chemins écartés %v", *scans, runner.received.ExcludePaths)
	}
}

// TestSimulationNonConsignee vérifie qu'un parcours à blanc ne laisse aucune
// trace dans l'historique : aucune sauvegarde n'en résulte.
func TestSimulationNonConsignee(t *testing.T) {
	runner := &fakeRunner{result: &borg.Result{Status: borg.StatusSuccess}}
	service, store, _ := setup(t, runner, nil)

	if _, err := service.Run(context.Background(), BackupRequest{Profile: profile(), DryRun: true}); err != nil {
		t.Fatal(err)
	}
	runs, _ := store.Recent(context.Background(), "poste", 10)
	if len(runs) != 0 {
		t.Errorf("une simulation a été consignée: %+v", runs)
	}
}

// TestMaintenanceApresSauvegarde vérifie l'enchaînement de EF-55 : la
// conservation puis la récupération d'espace suivent la sauvegarde.
func TestMaintenanceApresSauvegarde(t *testing.T) {
	runner := &fakeRunner{result: &borg.Result{Status: borg.StatusSuccess, Stdout: []byte(statsJSON)}}
	service, _, _ := setup(t, runner, nil)

	if _, err := service.Run(context.Background(), BackupRequest{Profile: profile()}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(runner.names, " "); got != "create prune compact" {
		t.Errorf("commandes: %s", got)
	}
}

// TestMaintenanceEnEchec vérifie qu'une conservation en échec laisse la
// sauvegarde réussie, avec un avertissement et son diagnostic.
func TestMaintenanceEnEchec(t *testing.T) {
	runner := &fakeRunner{
		result:  &borg.Result{Status: borg.StatusSuccess, Stdout: []byte(statsJSON)},
		failing: map[string]bool{"prune": true},
	}
	service, store, _ := setup(t, runner, nil)

	report, err := service.Run(context.Background(), BackupRequest{Profile: profile()})
	if err != nil {
		t.Fatalf("la sauvegarde elle-même a réussi: %v", err)
	}
	if report.MaintenanceErr == nil {
		t.Error("l'échec de la conservation doit être rapporté")
	}
	if got := strings.Join(runner.names, " "); got != "create prune" {
		t.Errorf("commandes: %s — rien ne doit être compacté après un échec", got)
	}
	run := lastRun(t, store)
	if run.Status != history.StatusWarning || run.ErrorKey != ErrorKeyMaintenance || run.Archive == "" {
		t.Errorf("exécution consignée: %+v", run)
	}
}

// TestSansConservation vérifie qu'une rétention vide ne supprime ni ne
// compacte rien.
func TestSansConservation(t *testing.T) {
	runner := &fakeRunner{result: &borg.Result{Status: borg.StatusSuccess, Stdout: []byte(statsJSON)}}
	service, _, _ := setup(t, runner, nil)

	p := profile()
	p.Retention = config.Retention{}
	if _, err := service.Run(context.Background(), BackupRequest{Profile: p}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(runner.names, " "); got != "create" {
		t.Errorf("commandes: %s", got)
	}
}

// TestExecutionConcurrente vérifie qu'une seconde exécution sur la même
// destination est refusée sans appeler Borg, et consignée (EF-57).
func TestExecutionConcurrente(t *testing.T) {
	runner := &fakeRunner{result: &borg.Result{Status: borg.StatusSuccess, Stdout: []byte(statsJSON)}}
	service, store, _ := setup(t, runner, nil)
	service.LockDir = t.TempDir()
	env := borg.Environment{Repository: "ssh://u1@u1.your-storagebox.de:23/./poste"}

	held, err := lock.Acquire(lock.PathFor(service.LockDir, env.Repository))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()

	_, err = service.Run(context.Background(), BackupRequest{Profile: profile(), Env: env})
	if !errors.Is(err, lock.ErrBusy) {
		t.Fatalf("erreur %v, attendu lock.ErrBusy", err)
	}
	if len(runner.names) != 0 {
		t.Errorf("Borg a été appelé: %v", runner.names)
	}
	if run := lastRun(t, store); run.Status != history.StatusError || run.ErrorKey != ErrorKeyAlreadyRunning {
		t.Errorf("exécution consignée: %+v", run)
	}

	held.Release()
	if _, err := service.Run(context.Background(), BackupRequest{Profile: profile(), Env: env}); err != nil {
		t.Errorf("après libération: %v", err)
	}
}
