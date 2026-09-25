package history

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// open ouvre un historique neuf.
func open(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "historique.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store, path
}

// TestCycleExecution vérifie qu'une exécution inscrite puis complétée se
// relit à l'identique.
func TestCycleExecution(t *testing.T) {
	store, _ := open(t)
	ctx := context.Background()
	started := time.Date(2026, 9, 23, 22, 0, 0, 0, time.Local)

	id, err := store.Begin(ctx, "poste", started)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	want := Run{
		ID: id, Profile: "poste", Started: started,
		Finished: started.Add(3*time.Minute + 250*time.Millisecond),
		Status:   StatusWarning, Archive: "poste-2026-09-23T22:00:00",
		Files: 1234, OriginalSize: 5 << 30, DeduplicatedSize: 12 << 20, RepositorySize: 40 << 30,
		Warnings: 2, CloudSkipped: 17,
	}
	if err := store.Finish(ctx, want); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	runs, err := store.Recent(ctx, "poste", 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("%d exécutions, attendu 1", len(runs))
	}
	got := runs[0]
	if !got.Started.Equal(want.Started) || !got.Finished.Equal(want.Finished) {
		t.Errorf("dates: %v → %v", got.Started, got.Finished)
	}
	got.Started, got.Finished = want.Started, want.Finished
	if got != want {
		t.Errorf("relu %+v\nattendu %+v", got, want)
	}
	if got.Duration() != 3*time.Minute+250*time.Millisecond {
		t.Errorf("durée %v", got.Duration())
	}
}

// TestExecutionInterrompue vérifie qu'une exécution jamais complétée reste
// visible comme telle : c'est la trace d'une interruption brutale.
func TestExecutionInterrompue(t *testing.T) {
	store, _ := open(t)
	ctx := context.Background()

	if _, err := store.Begin(ctx, "poste", time.Now()); err != nil {
		t.Fatal(err)
	}
	runs, err := store.Recent(ctx, "poste", 10)
	if err != nil {
		t.Fatal(err)
	}
	if runs[0].Status != StatusRunning || !runs[0].Finished.IsZero() || runs[0].Duration() != 0 {
		t.Errorf("exécution en cours mal relue: %+v", runs[0])
	}
}

// TestOrdreEtProfils vérifie le tri, la limite et le cloisonnement par profil.
func TestOrdreEtProfils(t *testing.T) {
	store, _ := open(t)
	ctx := context.Background()
	base := time.Now()

	for i := range 5 {
		if _, err := store.Begin(ctx, "poste", base.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Begin(ctx, "autre", base.Add(10*time.Hour)); err != nil {
		t.Fatal(err)
	}

	runs, err := store.Recent(ctx, "poste", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 3 {
		t.Fatalf("%d exécutions, attendu 3", len(runs))
	}
	for i := 1; i < len(runs); i++ {
		if runs[i].Started.After(runs[i-1].Started) {
			t.Error("les exécutions doivent aller de la plus récente à la plus ancienne")
		}
	}
	if runs[0].Profile != "poste" {
		t.Error("un autre profil s'est glissé dans l'historique")
	}
}

// TestReouverture vérifie que la migration n'est appliquée qu'une fois et que
// les données survivent à la fermeture.
func TestReouverture(t *testing.T) {
	store, path := open(t)
	if _, err := store.Begin(context.Background(), "poste", time.Now()); err != nil {
		t.Fatal(err)
	}
	store.Close()

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("réouverture: %v", err)
	}
	defer reopened.Close()
	runs, err := reopened.Recent(context.Background(), "poste", 10)
	if err != nil || len(runs) != 1 {
		t.Errorf("après réouverture: %d exécutions, erreur %v", len(runs), err)
	}
}

// TestBasePlusRecente vérifie qu'une base écrite par une version plus récente
// de l'application est refusée plutôt que relue de travers.
func TestBasePlusRecente(t *testing.T) {
	store, path := open(t)
	if _, err := store.db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	store.Close()

	if _, err := Open(path); err == nil {
		t.Error("une base plus récente que l'application doit être refusée")
	}
}

// TestMigrationDepuisVersion1 vérifie qu'une base créée par la première
// version du schéma gagne la nouvelle colonne et le catalogue sans perdre
// ses exécutions.
func TestMigrationDepuisVersion1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "historique.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// Ramène la base à la version 1, comme l'aurait laissée l'application
	// précédente.
	for _, statement := range []string{
		"DROP TABLE restore_checks",
		"DROP TABLE catalog_entries",
		"DROP TABLE catalogs",
		"ALTER TABLE runs DROP COLUMN repository_size",
	} {
		if _, err := store.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.db.Exec("PRAGMA user_version = 1"); err != nil {
		t.Fatal(err)
	}
	// Une exécution inscrite par l'ancienne version.
	if _, err := store.Begin(context.Background(), "poste", time.Now()); err != nil {
		t.Fatal(err)
	}
	store.Close()

	migrated, err := Open(path)
	if err != nil {
		t.Fatalf("migration: %v", err)
	}
	defer migrated.Close()
	runs, err := migrated.Recent(context.Background(), "poste", 10)
	if err != nil || len(runs) != 1 || runs[0].RepositorySize != 0 {
		t.Errorf("après migration: %+v, erreur %v", runs, err)
	}
}

// TestOuverturesSimultanees vérifie que plusieurs processus ouvrant une base
// neuve au même instant — l'interface et une sauvegarde planifiée — ne
// migrent pas chacun de leur côté.
func TestOuverturesSimultanees(t *testing.T) {
	path := filepath.Join(t.TempDir(), "historique.db")
	errs := make(chan error, 8)
	for range 8 {
		go func() {
			store, err := Open(path)
			if err == nil {
				store.Close()
			}
			errs <- err
		}()
	}
	for range 8 {
		if err := <-errs; err != nil {
			t.Errorf("ouverture concurrente: %v", err)
		}
	}

	// Le journal WAL, qui laisse lire pendant qu'un autre processus écrit,
	// est bien en place.
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var mode string
	if err := store.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Errorf("journal: %q, %v", mode, err)
	}
}
