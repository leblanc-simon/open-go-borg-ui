package borg

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// recorder retient la commande reçue et répond par un succès.
type recorder struct{ cmd Command }

func (r *recorder) Run(_ context.Context, cmd Command) (*Result, error) {
	r.cmd = cmd
	return &Result{Status: StatusSuccess}, nil
}
func (r *recorder) Version(context.Context) (string, error)    { return "1.4.5", nil }
func (r *recorder) Executable() string                         { return "borg" }
func (r *recorder) Origin(path string) (string, string, error) { return "/" + path, "/", nil }

// TestRotation vérifie les règles transmises, et la restriction aux
// sauvegardes du poste.
func TestRotation(t *testing.T) {
	runner := &recorder{}
	if _, err := Prune(context.Background(), runner, PruneOptions{Daily: 7, Weekly: 4, Monthly: 6, DryRun: true, List: true}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.cmd.Flags, " ")
	want := "--glob-archives {hostname}-* --keep-daily 7 --keep-weekly 4 --keep-monthly 6 --dry-run --list"
	if runner.cmd.Name != "prune" || got != want {
		t.Errorf("%s %s\nattendu prune %s", runner.cmd.Name, got, want)
	}
}

// TestRotationSansRegle vérifie qu'une rétention vide ne lance rien : elle ne
// doit jamais pouvoir être lue comme « ne rien garder ».
func TestRotationSansRegle(t *testing.T) {
	runner := &recorder{}
	if _, err := Prune(context.Background(), runner, PruneOptions{}); !errors.Is(err, ErrNoRetention) {
		t.Errorf("erreur %v, attendu ErrNoRetention", err)
	}
	if runner.cmd.Name != "" {
		t.Error("le moteur ne doit pas être appelé")
	}
}

// TestRotationPartielle vérifie qu'une règle à zéro est omise plutôt que
// transmise, Borg lisant --keep-daily 0 comme une règle.
func TestRotationPartielle(t *testing.T) {
	runner := &recorder{}
	if _, err := Prune(context.Background(), runner, PruneOptions{Monthly: 12}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(runner.cmd.Flags, " "); got != "--glob-archives {hostname}-* --keep-monthly 12" {
		t.Errorf("options: %s", got)
	}
}

// TestListeRotation vérifie la lecture de « prune --list » dans ses quatre
// formes, alignement de Borg compris.
func TestListeRotation(t *testing.T) {
	line := func(label, name string) Message {
		return Message{Level: "INFO", Text: fmt.Sprintf("%-40s %-36s Thu, 2026-09-24 22:00:00 [0123abcd]", label, name)}
	}
	plan := ParsePruneList([]Message{
		{Level: "INFO", Text: "Synchronizing chunks cache..."},
		line("Keeping archive (rule: daily #1):", "poste-2026-09-24T22:00:00"),
		line("Keeping archive (rule: monthly #12):", "poste-2025-10-31T22:00:00"),
		line("Keeping checkpoint archive:", "poste-2026-09-23T22:00:00.checkpoint"),
		line("Would prune:", "poste-2026-09-01T22:00:00"),
		line("Pruning archive (2/5):", "poste-2026-08-31T22:00:00"),
	})
	kept := strings.Join(plan.Kept, " ")
	pruned := strings.Join(plan.Pruned, " ")
	if kept != "poste-2026-09-24T22:00:00 poste-2025-10-31T22:00:00 poste-2026-09-23T22:00:00.checkpoint" {
		t.Errorf("conservées: %s", kept)
	}
	if pruned != "poste-2026-09-01T22:00:00 poste-2026-08-31T22:00:00" {
		t.Errorf("supprimées: %s", pruned)
	}
}
