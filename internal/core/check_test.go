package core

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/config"
	"leblanc.io/open-go-borg-ui/internal/history"
)

// checkRunner répond au contrôle par l'issue donnée, et par un succès au
// reste.
type checkRunner struct {
	fakeRunner
	status borg.Status
}

func (r *checkRunner) Run(ctx context.Context, cmd borg.Command) (*borg.Result, error) {
	r.names = append(r.names, cmd.Name)
	switch cmd.Name {
	case "check":
		return &borg.Result{Status: r.status, Messages: []borg.Message{
			{Level: "INFO", Text: "Starting repository check"},
			{Level: "ERROR", Text: "segment 12: checksum mismatch"},
		}}, nil
	case "create":
		return &borg.Result{Status: borg.StatusSuccess, Stdout: []byte(statsJSON)}, nil
	}
	return &borg.Result{Status: borg.StatusSuccess}, nil
}

func checkStore(t *testing.T) *history.Store {
	t.Helper()
	store, err := history.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// TestControleDeLaDestination vérifie les issues d'un contrôle : sain,
// anomalies consignées avec leur détail, contrôle impossible non consigné.
func TestControleDeLaDestination(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name     string
		status   borg.Status
		healthy  bool
		recorded bool
	}{
		{"saine", borg.StatusSuccess, true, true},
		{"anomalies", borg.StatusWarning, false, true},
		{"impossible", borg.StatusError, false, false},
	} {
		store := checkStore(t)
		check, err := CheckRepository(ctx, &checkRunner{status: c.status}, borg.Environment{}, store, "poste", time.Now, nil)
		last, recorded, _ := store.LastRepositoryCheck(ctx, "poste")
		if recorded != c.recorded || (c.recorded && (err != nil || check.Healthy != c.healthy || last.ID != check.ID)) {
			t.Errorf("%s: %+v, erreur %v, consigné %v", c.name, check, err, recorded)
		}
		if c.name == "anomalies" && last.Detail != "segment 12: checksum mismatch" {
			t.Errorf("détail des anomalies: %q", last.Detail)
		}
		if !c.recorded && err == nil {
			t.Errorf("%s: un contrôle impossible doit être une erreur", c.name)
		}
	}
}

// TestControleDu vérifie le rythme mensuel du contrôle.
func TestControleDu(t *testing.T) {
	store := checkStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.Local)
	if due, _ := CheckDue(ctx, store, "poste", now); !due {
		t.Error("jamais contrôlée : le contrôle est dû")
	}
	store.AddRepositoryCheck(ctx, history.RepositoryCheck{Profile: "poste", Checked: now.AddDate(0, 0, -3), Healthy: true})
	if due, _ := CheckDue(ctx, store, "poste", now); due {
		t.Error("contrôlée il y a trois jours : rien n'est dû")
	}
	if due, _ := CheckDue(ctx, store, "poste", now.AddDate(0, 0, 28)); !due {
		t.Error("contrôlée il y a trente et un jours : le contrôle est dû")
	}
}

// TestSauvegardeControleUneFoisParMois vérifie l'accroche à la sauvegarde.
func TestSauvegardeControleUneFoisParMois(t *testing.T) {
	store := checkStore(t)
	runner := &checkRunner{status: borg.StatusSuccess}
	service := &Backup{Runner: runner, History: store, CheckRepositories: true,
		ScanCloud: func(context.Context, []string) ([]string, error) { return nil, nil }}
	profile := config.Default("poste")
	profile.Sources = []string{t.TempDir()}
	var phases []Phase
	req := BackupRequest{Profile: &profile, OnPhase: func(p Phase) { phases = append(phases, p) }}

	report, err := service.Run(context.Background(), req)
	if err != nil || report.RepositoryCheck == nil || !report.RepositoryCheck.Healthy {
		t.Fatalf("première sauvegarde: %+v, %v, %v", report.RepositoryCheck, report.CheckErr, err)
	}
	if phases[len(phases)-1] != PhaseCheck {
		t.Errorf("étapes annoncées: %v", phases)
	}
	report, _ = service.Run(context.Background(), req)
	if report.RepositoryCheck != nil {
		t.Error("un seul contrôle par mois")
	}
}
