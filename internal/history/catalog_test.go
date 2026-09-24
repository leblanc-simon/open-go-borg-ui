package history

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func openCatalog(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func names(entries []Entry) []string {
	var out []string
	for _, e := range entries {
		out = append(out, e.Path)
	}
	return out
}

// TestCatalogue vérifie l'enregistrement du contenu d'une sauvegarde : les
// dossiers manquants reconstitués, les tailles cumulées, l'ordre de
// présentation, et un catalogue propre à chaque sauvegarde.
func TestCatalogue(t *testing.T) {
	store := openCatalog(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 24, 22, 0, 0, 0, time.Local)

	if _, ok, err := store.Catalog(ctx, "a1"); ok || err != nil {
		t.Fatalf("catalogue avant relevé: %v, %v", ok, err)
	}

	catalog, err := store.SaveCatalog(ctx, "a1", []Entry{
		{Path: "home/marc/Documents", Dir: true},
		{Path: "home/marc/Documents/lettre.odt", Size: 2048, Modified: now},
		{Path: "home/marc/Documents/Factures", Dir: true},
		{Path: "home/marc/Documents/Factures/Été 2026.pdf", Size: 1000},
		{Path: "home/marc/Documents/archive.zip", Size: 10},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Files != 3 || catalog.Size != 3058 {
		t.Errorf("résumé: %+v", catalog)
	}
	if got, ok, _ := store.Catalog(ctx, "a1"); !ok || got.Files != 3 || !got.Cached.Equal(now) {
		t.Errorf("résumé relu: %+v, %v", got, ok)
	}

	root, _ := store.Children(ctx, "a1", "")
	if len(root) != 1 || root[0].Path != "home" || !root[0].Dir || root[0].Size != 3058 {
		t.Errorf("racine reconstituée: %+v", root)
	}
	docs, _ := store.Children(ctx, "a1", "home/marc/Documents/")
	want := []string{"home/marc/Documents/Factures", "home/marc/Documents/archive.zip", "home/marc/Documents/lettre.odt"}
	if got := names(docs); len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("contenu du dossier: %v, attendu %v", got, want)
	}
	if docs[0].Size != 1000 || !docs[2].Modified.Equal(now) {
		t.Errorf("taille du dossier ou date: %+v", docs)
	}

	if other, _ := store.Children(ctx, "a2", ""); len(other) != 0 {
		t.Errorf("une autre sauvegarde ne voit pas ce catalogue: %v", names(other))
	}
}

// TestRechercheCatalogue vérifie la recherche par nom : sans casse, accents
// compris, et sans que les jokers de LIKE s'appliquent (EF-92).
func TestRechercheCatalogue(t *testing.T) {
	store := openCatalog(t)
	ctx := context.Background()
	store.SaveCatalog(ctx, "a1", []Entry{
		{Path: "c/Users/marc/ÉTÉ 2026.pdf", Size: 1},
		{Path: "c/Users/marc/100%_sûr.txt", Size: 1},
		{Path: "c/Users/marc/100x-sur.txt", Size: 1},
	}, time.Now())

	if got := names(must(store.Search(ctx, "a1", "été", 10))); len(got) != 1 || got[0] != "c/Users/marc/ÉTÉ 2026.pdf" {
		t.Errorf("recherche accentuée: %v", got)
	}
	if got := names(must(store.Search(ctx, "a1", "100%_", 10))); len(got) != 1 {
		t.Errorf("jokers pris à la lettre: %v", got)
	}
	if got := must(store.Search(ctx, "a1", "  ", 10)); len(got) != 0 {
		t.Errorf("recherche vide: %v", got)
	}
	if got := must(store.Search(ctx, "a1", "c", 1)); len(got) != 1 {
		t.Errorf("limite: %d résultats", len(got))
	}
}

func must(entries []Entry, err error) []Entry {
	if err != nil {
		panic(err)
	}
	return entries
}
