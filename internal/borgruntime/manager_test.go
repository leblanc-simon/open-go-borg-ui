package borgruntime

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// makeArchive écrit une archive de test et retourne son chemin et son
// empreinte.
func makeArchive(t *testing.T, entries map[string]string) (string, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "runtime.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("création de l'archive: %v", err)
	}

	writer := zip.NewWriter(file)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("entrée %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("écriture de %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("fermeture de l'archive: %v", err)
	}
	file.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lecture de l'archive: %v", err)
	}
	digest := sha256.Sum256(data)
	return path, hex.EncodeToString(digest[:])
}

// TestInstallationHorsLigne couvre la voie utilisée sur un poste sans accès
// réseau : l'archive déposée à la main est vérifiée par la même empreinte que
// celle téléchargée (EF-06).
func TestInstallationHorsLigne(t *testing.T) {
	archive, checksum := makeArchive(t, map[string]string{
		"runtime/bin/borg.exe": "faux moteur",
		"runtime/bin/bash.exe": "faux shell",
	})

	root := t.TempDir()
	manager := NewManager(root, Spec{Version: "1.4.5-test", BorgVersion: "1.4.5", SHA256: checksum})

	path, err := manager.InstallFromArchive(context.Background(), archive)
	if err != nil {
		t.Fatalf("InstallFromArchive a échoué: %v", err)
	}
	if path != filepath.Join(root, "1.4.5-test") {
		t.Errorf("installation dans %s, attendue dans %s", path, filepath.Join(root, "1.4.5-test"))
	}
	// L'archive place tout sous un dossier unique : il ne doit pas se
	// retrouver imbriqué dans le dossier de version.
	if _, err := os.Stat(filepath.Join(path, "bin", "borg.exe")); err != nil {
		t.Errorf("le moteur n'est pas à sa place: %v", err)
	}
	if _, err := manager.Installed(); err != nil {
		t.Errorf("le runtime devrait être vu comme installé: %v", err)
	}
}

// TestEmpreinteIncorrecte vérifie qu'une archive altérée est rejetée avant
// toute extraction : l'application s'apprête à exécuter ce code (SEC-01).
func TestEmpreinteIncorrecte(t *testing.T) {
	archive, _ := makeArchive(t, map[string]string{"runtime/bin/borg.exe": "faux moteur"})

	root := t.TempDir()
	manager := NewManager(root, Spec{
		Version: "1.4.5-test",
		SHA256:  "0000000000000000000000000000000000000000000000000000000000000000",
	})

	_, err := manager.InstallFromArchive(context.Background(), archive)
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("erreur = %v, attendue ErrChecksumMismatch", err)
	}
	// Rien ne doit subsister d'une installation refusée.
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Errorf("l'installation refusée a laissé des traces: %v", entries)
	}
}

// TestArchiveHorsDossier vérifie qu'une entrée cherchant à écrire hors du
// dossier d'extraction est refusée.
func TestArchiveHorsDossier(t *testing.T) {
	archive, checksum := makeArchive(t, map[string]string{
		"../evasion.txt": "contenu",
	})

	root := t.TempDir()
	manager := NewManager(root, Spec{Version: "1.4.5-test", SHA256: checksum})

	if _, err := manager.InstallFromArchive(context.Background(), archive); err == nil {
		t.Fatal("une entrée hors du dossier d'extraction doit être refusée")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "evasion.txt")); err == nil {
		t.Error("un fichier a été écrit hors du dossier d'extraction")
	}
}

// TestRuntimeNonPublie vérifie qu'une version sans adresse de téléchargement
// oriente vers la voie hors ligne au lieu d'échouer sur le réseau.
func TestRuntimeNonPublie(t *testing.T) {
	manager := NewManager(t.TempDir(), Spec{Version: "1.4.5-test"})

	if _, err := manager.Install(context.Background(), nil); !errors.Is(err, ErrNotPublished) {
		t.Errorf("erreur = %v, attendue ErrNotPublished", err)
	}
}

// TestAbsenceDeRuntime vérifie que l'absence d'installation est une situation
// nommée, que l'appelant traduit en action concrète.
func TestAbsenceDeRuntime(t *testing.T) {
	manager := NewManager(t.TempDir(), Pinned)

	if _, err := manager.Installed(); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("erreur = %v, attendue ErrNotInstalled", err)
	}
}
