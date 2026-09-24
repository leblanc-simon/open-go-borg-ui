package core

import (
	"context"
	"strings"
	"testing"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/config"
)

// pruneRunner répond à « prune » par une liste prédéfinie.
type pruneRunner struct {
	messages []borg.Message
	calls    []string
}

func (r *pruneRunner) Run(_ context.Context, cmd borg.Command) (*borg.Result, error) {
	r.calls = append(r.calls, cmd.Name+" "+strings.Join(cmd.Flags, " "))
	return &borg.Result{Status: borg.StatusSuccess, Messages: r.messages}, nil
}
func (r *pruneRunner) Version(context.Context) (string, error)    { return "1.4.5", nil }
func (r *pruneRunner) Executable() string                         { return "borg" }
func (r *pruneRunner) Origin(path string) (string, string, error) { return "/" + path, "/", nil }

// TestApercuConservation vérifie que l'aperçu ne supprime rien et rend la
// liste de ce qui partirait.
func TestApercuConservation(t *testing.T) {
	runner := &pruneRunner{messages: []borg.Message{
		{Text: "Keeping archive (rule: daily #1):        poste-2026-09-24T22:00:00 Thu"},
		{Text: "Would prune:                             poste-2026-09-01T22:00:00 Mon"},
	}}
	plan, err := PreviewRetention(context.Background(), runner, borg.Environment{}, config.Retention{Daily: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Pruned) != 1 || plan.Pruned[0] != "poste-2026-09-01T22:00:00" || len(plan.Kept) != 1 {
		t.Errorf("aperçu: %+v", plan)
	}
	if len(runner.calls) != 1 || !strings.Contains(runner.calls[0], "--dry-run") || !strings.Contains(runner.calls[0], "--list") {
		t.Errorf("appels: %v", runner.calls)
	}
}

// TestApercuSansRegle vérifie qu'une conservation vide ne sollicite pas Borg
// et ne supprime rien.
func TestApercuSansRegle(t *testing.T) {
	runner := &pruneRunner{}
	plan, err := PreviewRetention(context.Background(), runner, borg.Environment{}, config.Retention{})
	if err != nil || len(plan.Pruned) != 0 || len(runner.calls) != 0 {
		t.Errorf("aperçu %+v, erreur %v, appels %v", plan, err, runner.calls)
	}
}

// TestHorizon vérifie que la règle la plus longue fixe l'horizon.
func TestHorizon(t *testing.T) {
	cases := []struct {
		r     config.Retention
		unit  HorizonUnit
		count int
	}{
		{config.Retention{Daily: 7, Weekly: 4, Monthly: 6}, HorizonMonths, 6},
		{config.Retention{Daily: 7, Weekly: 4}, HorizonWeeks, 4},
		{config.Retention{Daily: 14}, HorizonDays, 14},
		{config.Retention{}, HorizonAll, 0},
	}
	for _, c := range cases {
		if unit, count := RetentionHorizon(c.r); unit != c.unit || count != c.count {
			t.Errorf("%+v: %v %d", c.r, unit, count)
		}
	}
}
