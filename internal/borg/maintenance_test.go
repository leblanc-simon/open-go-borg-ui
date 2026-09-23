package borg

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// recorder retient la commande reçue et répond par un succès.
type recorder struct{ cmd Command }

func (r *recorder) Run(_ context.Context, cmd Command) (*Result, error) {
	r.cmd = cmd
	return &Result{Status: StatusSuccess}, nil
}
func (r *recorder) Version(context.Context) (string, error) { return "1.4.5", nil }
func (r *recorder) Executable() string                      { return "borg" }

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
