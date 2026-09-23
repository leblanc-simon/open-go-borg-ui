//go:build !windows

// Ces tests exercent la chaîne complète — configuration, environnement, ligne
// de commande, codes de sortie — en substituant à Borg un script qui consigne
// ce qu'il reçoit. Ils vérifient donc ce que l'application demande réellement
// au moteur, sans qu'aucune installation de Borg soit nécessaire.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// poste prépare un poste de test : dossier personnel isolé, faux moteur dans
// le PATH, configuration écrite. Il retourne le fichier où le faux moteur
// consigne ses appels.
func poste(t *testing.T, script string, options ...string) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)

	journal := filepath.Join(home, "appels.txt")
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("création de %s: %v", binDir, err)
	}
	faux := "#!/bin/sh\n" +
		"{ echo \"ARGS $*\"; echo \"REPO $BORG_REPO\"; echo \"RSH $BORG_RSH\"; " +
		"echo \"UNENCRYPTED $BORG_UNKNOWN_UNENCRYPTED_REPO_ACCESS_IS_OK\"; " +
		"echo \"PASSCOMMAND $BORG_PASSCOMMAND\"; } >> " + journal + "\n" + script
	if err := os.WriteFile(filepath.Join(binDir, "borg"), []byte(faux), 0o755); err != nil {
		t.Fatalf("écriture du faux moteur: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	args := append([]string{"config", "init", "--user", "u123456", "--repo", "poste-marc"}, options...)
	if code := run(args); code != exitSuccess {
		t.Fatalf("config init a retourné %d", code)
	}
	return journal
}

// lireJournal retourne ce que le faux moteur a consigné. Un journal absent
// signifie qu'il n'a jamais été appelé, ce qui est parfois le résultat
// attendu.
func lireJournal(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatalf("lecture du journal: %v", err)
	}
	return string(data)
}

// TestPreparationDestinationNonChiffree vérifie la commande envoyée au moteur
// et, surtout, la variable sans laquelle une sauvegarde planifiée sur une
// destination non chiffrée resterait bloquée sur une question (EF-38).
func TestPreparationDestinationNonChiffree(t *testing.T) {
	journal := poste(t, "exit 0", "--clear")

	if code := run([]string{"repository", "init"}); code != exitSuccess {
		t.Fatalf("repository init a retourné %d", code)
	}

	appels := lireJournal(t, journal)
	for _, want := range []string{
		"--encryption=none",
		"--remote-path=borg-1.4",
		"REPO ssh://u123456@u123456.your-storagebox.de:23/./poste-marc",
		"UNENCRYPTED yes",
		"-p 23",
		"StrictHostKeyChecking=yes",
	} {
		if !strings.Contains(appels, want) {
			t.Errorf("appel attendu absent : %q\n%s", want, appels)
		}
	}
	if strings.Contains(appels, "PASSCOMMAND /") {
		t.Error("aucune commande de mot de passe ne doit être définie sans chiffrement")
	}
}

// TestPreparationSansMotDePasse vérifie qu'une destination chiffrée sans mot
// de passe enregistré est refusée avant d'appeler le moteur, plutôt que de le
// laisser attendre une saisie.
func TestPreparationSansMotDePasse(t *testing.T) {
	journal := poste(t, "exit 0")

	if code := run([]string{"repository", "init"}); code != exitError {
		t.Fatalf("repository init a retourné %d, attendu %d", code, exitError)
	}
	if strings.Contains(lireJournal(t, journal), "ARGS init") {
		t.Error("le moteur ne devrait pas être appelé sans mot de passe disponible")
	}
}

// TestSauvegardeAvecAvertissements vérifie qu'un fichier illisible ne
// transforme pas une sauvegarde réussie en échec (EF-56).
func TestSauvegardeAvecAvertissements(t *testing.T) {
	journal := poste(t, `
echo '{"type":"log_message","levelname":"WARNING","message":"rapport.odt: fichier verrouillé"}' >&2
echo '{"archive":{"name":"poste-2026-09-03T12:00:00","stats":{"nfiles":42,"original_size":1048576,"deduplicated_size":524288}}}'
exit 1
`, "--clear")

	// Le dossier à sauvegarder est ajouté à la configuration comme
	// l'utilisateur le ferait.
	ajouterSource(t, filepath.Join(t.TempDir(), "Documents"))

	if code := run([]string{"backup"}); code != exitWarning {
		t.Fatalf("backup a retourné %d, attendu %d", code, exitWarning)
	}

	appels := lireJournal(t, journal)
	for _, want := range []string{"--stats", "--json", "--exclude-caches", "--compression=zstd,3"} {
		if !strings.Contains(appels, want) {
			t.Errorf("option attendue absente : %q\n%s", want, appels)
		}
	}
}

// TestSauvegardeSansDossier vérifie qu'une configuration sans dossier à
// sauvegarder est signalée sans appeler le moteur.
func TestSauvegardeSansDossier(t *testing.T) {
	poste(t, "exit 0", "--clear")

	if code := run([]string{"backup"}); code != exitError {
		t.Fatalf("backup a retourné %d, attendu %d", code, exitError)
	}
}

// TestCommandeInconnue vérifie qu'une commande inconnue échoue proprement.
func TestCommandeInconnue(t *testing.T) {
	poste(t, "exit 0")

	if code := run([]string{"sauvegarde-magique"}); code != exitError {
		t.Errorf("code de sortie = %d, attendu %d", code, exitError)
	}
}

// ajouterSource inscrit un dossier dans la configuration du poste de test.
func ajouterSource(t *testing.T, dossier string) {
	t.Helper()

	if err := os.MkdirAll(dossier, 0o700); err != nil {
		t.Fatalf("création de %s: %v", dossier, err)
	}
	path := filepath.Join(os.Getenv("HOME"), ".config", "borgui", "config.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lecture de la configuration: %v", err)
	}
	updated := strings.Replace(string(data), "sources = []",
		"sources = ['"+dossier+"']", 1)
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatalf("écriture de la configuration: %v", err)
	}
}

// TestRestaurationPlusRecente vérifie qu'une restauration sans nom choisit la
// sauvegarde la plus récente, quel que soit l'ordre de la liste, et qu'elle
// s'exécute dans le dossier neuf demandé.
func TestRestaurationPlusRecente(t *testing.T) {
	journal := poste(t, `
case "$*" in
*list*)
  echo '{"archives":[{"name":"poste-2026-09-03T12:00:00","start":"2026-09-03T12:00:00.000000"},{"name":"poste-2026-09-01T12:00:00","start":"2026-09-01T12:00:00.000000"}]}'
  ;;
*extract*)
  echo "PWD $(pwd)" >> "$HOME/appels.txt"
  ;;
esac
exit 0
`, "--clear")

	destination := filepath.Join(t.TempDir(), "restauration")
	if code := run([]string{"restore", "--to", destination}); code != exitSuccess {
		t.Fatalf("restore a retourné %d", code)
	}

	appels := lireJournal(t, journal)
	if !strings.Contains(appels, "::poste-2026-09-03T12:00:00") {
		t.Errorf("la sauvegarde la plus récente n'a pas été choisie\n%s", appels)
	}
	if !strings.Contains(appels, "PWD "+destination) {
		t.Errorf("l'extraction ne s'est pas faite dans %s\n%s", destination, appels)
	}
}

// TestRestaurationDossierOccupe vérifie qu'une restauration refuse un dossier
// qui contient déjà des fichiers, sans appeler le moteur (EF-95).
func TestRestaurationDossierOccupe(t *testing.T) {
	journal := poste(t, "exit 0", "--clear")

	destination := t.TempDir()
	if err := os.WriteFile(filepath.Join(destination, "rapport.odt"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"restore", "--to", destination, "poste-2026-09-03T12:00:00"}); code != exitError {
		t.Fatalf("restore a retourné %d, attendu %d", code, exitError)
	}
	if strings.Contains(lireJournal(t, journal), "extract") {
		t.Error("le moteur ne devrait pas être appelé sur un dossier occupé")
	}
}
