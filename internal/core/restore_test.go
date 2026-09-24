//go:build !windows

package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/history"
)

const contentsJSON = `{"type": "d", "path": "home/marc/Documents", "size": 0, "mtime": "2026-09-01T10:00:00.000000"}
{"type": "-", "path": "home/marc/Documents/lettre.odt", "size": 2048, "mtime": "2026-09-03T22:14:07.000000"}
`

// TestCatalogueReleveUneFois vérifie EF-93 : le contenu d'une sauvegarde
// est demandé à Borg au premier accès, puis relu du cache.
func TestCatalogueReleveUneFois(t *testing.T) {
	store, err := history.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runner := &fakeRunner{stdout: map[string]string{"list": contentsJSON}}
	archive := borg.Archive{Name: "poste-2026-09-24T22:00:00", ID: "4f2a"}
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		catalog, err := OpenCatalog(ctx, runner, borg.Environment{}, store, archive, time.Now())
		if err != nil || catalog.Files != 1 || catalog.Size != 2048 {
			t.Fatalf("ouverture %d: %+v, %v", i+1, catalog, err)
		}
	}
	if len(runner.names) != 1 {
		t.Errorf("Borg interrogé %d fois, attendu une seule", len(runner.names))
	}
	if root, _ := store.Children(ctx, archive.ID, ""); len(root) != 1 || root[0].Path != "home" {
		t.Errorf("arborescence: %+v", root)
	}
}

// TestRestaurationDossierNeuf vérifie EF-95 : le dossier est créé, et un
// dossier déjà occupé est refusé avant tout appel à Borg.
func TestRestaurationDossierNeuf(t *testing.T) {
	runner := &fakeRunner{}
	target := filepath.Join(t.TempDir(), "Restauration 2026-09-24")
	_, err := Restore(context.Background(), runner, RestoreRequest{
		Archive: "poste-1", Paths: []string{"home/marc/Documents"}, Destination: target,
	})
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Errorf("dossier non créé: %v", err)
	}

	os.WriteFile(filepath.Join(target, "déjà-là.txt"), nil, 0o600)
	runner.names = nil
	_, err = Restore(context.Background(), runner, RestoreRequest{Archive: "poste-1", Destination: target})
	if !errors.Is(err, ErrDestinationNotEmpty) || len(runner.names) != 0 {
		t.Errorf("dossier occupé: %v, Borg appelé %v", err, runner.names)
	}
}

// TestRestaurationEnPlace vérifie EF-96 : la confirmation énonce ce qui
// existe encore et serait écrasé, et une restauration complète en place
// n'est pas proposée.
func TestRestaurationEnPlace(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "présent.txt"), nil, 0o600)
	runner := &fakeRunner{}
	archived := func(name string) string { return filepath.ToSlash(filepath.Join(home, name))[1:] }

	existing, err := Overwritten(runner, []string{archived("présent.txt"), archived("disparu.txt")})
	if err != nil || len(existing) != 1 || existing[0] != filepath.Join(home, "présent.txt") {
		t.Errorf("écrasés: %v, %v", existing, err)
	}

	if _, err := Restore(context.Background(), runner, RestoreRequest{Archive: "poste-1", InPlace: true}); err == nil {
		t.Error("une restauration complète en place doit être refusée")
	}
}
