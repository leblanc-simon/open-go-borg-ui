package main

import (
	"os"
	"path/filepath"
	"testing"
)

// identity rend la clé elle-même : les tests portent sur les verdicts, pas
// sur les libellés.
func identity(key string, _ ...any) string { return key }

// generated crée un jeu de recette et retourne sa racine et son manifeste.
func generated(t *testing.T) (string, []entry) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "donnees")
	if _, err := generate(root, 1<<20); err != nil {
		t.Fatalf("génération: %v", err)
	}
	entries, err := scan(identity, root)
	if err != nil {
		t.Fatalf("manifeste: %v", err)
	}
	return root, entries
}

// TestJeuDeRecette vérifie que le jeu contient bien les cas difficiles qu'il
// annonce, et qu'il est reproductible d'une génération à l'autre : le même
// manifeste doit pouvoir servir sur deux machines.
func TestJeuDeRecette(t *testing.T) {
	_, first := generated(t)
	_, second := generated(t)
	if report := compare(first, second); !report.ok() {
		t.Errorf("deux générations diffèrent: %+v", report)
	}

	var long, empty, emptyDirs bool
	digests := map[string]int{}
	for _, e := range first {
		if len(e.path) > 260 {
			long = true
		}
		if e.kind == "f" && e.size == 0 {
			empty = true
		}
		if e.path == "vides/dossier-vide" && e.kind == "d" {
			emptyDirs = true
		}
		if e.kind == "f" {
			digests[e.digest]++
		}
	}
	if !long || !empty || !emptyDirs {
		t.Errorf("cas manquant: chemin long %v, fichier vide %v, dossier vide %v", long, empty, emptyDirs)
	}
	duplicated := false
	for _, n := range digests {
		if n > 1 {
			duplicated = true
		}
	}
	if !duplicated {
		t.Error("le jeu doit contenir un doublon pour exercer la déduplication")
	}
}

// TestComparaisonEcarts vérifie que chaque sorte d'écart est détectée.
func TestComparaisonEcarts(t *testing.T) {
	root, expected := generated(t)

	// Un octet modifié, un fichier perdu, un fichier apparu.
	target := filepath.Join(root, "binaires", "aléatoire-1.bin")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 0xFF
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "vides", "fichier-vide.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "intrus.txt"), []byte("?"), 0o644); err != nil {
		t.Fatal(err)
	}

	actual, err := scan(identity, root)
	if err != nil {
		t.Fatal(err)
	}
	report := compare(expected, actual)
	if len(report.different) != 1 || report.different[0].path != "binaires/aléatoire-1.bin" {
		t.Errorf("fichier modifié non détecté: %+v", report.different)
	}
	if len(report.missing) != 1 || report.missing[0].path != "vides/fichier-vide.txt" {
		t.Errorf("fichier perdu non détecté: %+v", report.missing)
	}
	if len(report.extra) != 1 || report.extra[0].path != "intrus.txt" {
		t.Errorf("fichier apparu non détecté: %+v", report.extra)
	}
}

// TestManifesteRelu vérifie qu'un manifeste écrit puis relu ne perd rien,
// noms Unicode et chemins longs compris.
func TestManifesteRelu(t *testing.T) {
	root, _ := generated(t)
	manifest := filepath.Join(t.TempDir(), "manifeste.tsv")

	if code, err := commandManifest(identity, []string{root, "-o", manifest}); code != exitSuccess || err != nil {
		t.Fatalf("manifest: code %d, erreur %v", code, err)
	}
	if code, err := commandCompare(identity, []string{manifest, root}); code != exitSuccess || err != nil {
		t.Fatalf("compare: code %d, erreur %v", code, err)
	}
}
