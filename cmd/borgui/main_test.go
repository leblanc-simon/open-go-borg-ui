//go:build !windows

// Ces tests exercent la chaîne complète — configuration, environnement, ligne
// de commande, codes de sortie — en substituant à Borg un script qui consigne
// ce qu'il reçoit. Ils vérifient donc ce que l'application demande réellement
// au moteur, sans qu'aucune installation de Borg soit nécessaire.

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"leblanc.io/open-go-borg-ui/internal/config"
	"leblanc.io/open-go-borg-ui/internal/history"
	"leblanc.io/open-go-borg-ui/internal/schedule"
)

// poste prépare un poste de test : dossier personnel isolé, faux moteur dans
// le PATH, configuration écrite. Il retourne le fichier où le faux moteur
// consigne ses appels.
func poste(t *testing.T, script string, options ...string) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	// Les variables XDG l'emportent sur HOME : laissées telles quelles, elles
	// feraient écrire les tests dans les dossiers réels du développeur.
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))

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
//
// Tout test qui sauvegarde passe par ici : la destination y est aussi
// ramenée sur une adresse locale fermée, pour que le dépôt du fichier d'état
// qui suit chaque sauvegarde échoue aussitôt, sans jamais sortir sur le
// réseau.
func ajouterSource(t *testing.T, dossier string) {
	t.Helper()
	horsReseau(t)

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

// TestHistoriqueConsigne vérifie qu'une sauvegarde laisse une trace que la
// commande history relit, et qu'une simulation n'en laisse aucune.
func TestHistoriqueConsigne(t *testing.T) {
	poste(t, `echo '{"archive":{"name":"poste-2026-09-23T22:00:00","stats":{"nfiles":3}}}'`, "--clear")
	ajouterSource(t, filepath.Join(t.TempDir(), "Documents"))

	if code := run([]string{"backup", "--dry-run"}); code != exitSuccess {
		t.Fatalf("backup --dry-run a retourné %d", code)
	}
	if code := run([]string{"backup"}); code != exitSuccess {
		t.Fatalf("backup a retourné %d", code)
	}
	if code := run([]string{"history"}); code != exitSuccess {
		t.Fatalf("history a retourné %d", code)
	}

	path, err := config.HistoryPath()
	if err != nil {
		t.Fatal(err)
	}
	store, err := history.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runs, err := store.Recent(context.Background(), "poste", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Status != history.StatusSuccess || runs[0].Files != 3 {
		t.Errorf("historique: %+v", runs)
	}
}

// TestExecutionPlanifiee vérifie le mode de la tâche planifiée : rien sur le
// terminal, tout dans le journal du profil, et le code de sortie de Borg.
func TestExecutionPlanifiee(t *testing.T) {
	poste(t, `echo '{"archive":{"name":"poste-2026-09-23T22:00:00","stats":{"nfiles":3}}}'`, "--clear")
	ajouterSource(t, filepath.Join(t.TempDir(), "Documents"))

	stdout, stderrBefore := os.Stdout, stderr
	t.Cleanup(func() { os.Stdout, os.Stderr, stderr = stdout, stdout, stderrBefore })

	if code := run([]string{"--run", "poste"}); code != exitSuccess {
		t.Fatalf("--run a retourné %d", code)
	}
	os.Stdout, os.Stderr, stderr = stdout, stdout, stderrBefore

	stateDir, err := config.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	journal, err := os.ReadFile(filepath.Join(stateDir, "logs", "poste.log"))
	if err != nil {
		t.Fatalf("journal absent: %v", err)
	}
	for _, want := range []string{"poste", "poste-2026-09-23T22:00:00", "code 0"} {
		if !strings.Contains(string(journal), want) {
			t.Errorf("le journal ne contient pas %q:\n%s", want, journal)
		}
	}
}

// fauxOrdonnanceur remplace l'ordonnanceur du système par un systemd écrivant
// dans un dossier de test et n'exécutant aucune commande.
func fauxOrdonnanceur(t *testing.T) (dir string, commandes *[]string) {
	t.Helper()
	dir = t.TempDir()
	var calls []string
	previous := newScheduler
	newScheduler = func() (schedule.Scheduler, error) {
		return schedule.NewSystemd(dir, func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return nil, nil
		}), nil
	}
	t.Cleanup(func() { newScheduler = previous })
	return dir, &calls
}

// TestPlanification vérifie le cycle complet : réglage enregistré, tâche
// installée pour lancer « --run <profil> », puis retirée.
func TestPlanification(t *testing.T) {
	poste(t, "exit 0", "--clear")
	dir, _ := fauxOrdonnanceur(t)

	if code := run([]string{"schedule", "weekly", "friday", "21:30"}); code != exitSuccess {
		t.Fatalf("schedule weekly a retourné %d", code)
	}
	service, err := os.ReadFile(filepath.Join(dir, "borgui-poste.service"))
	if err != nil {
		t.Fatalf("service absent: %v", err)
	}
	if !strings.Contains(string(service), `"--run" "poste"`) {
		t.Errorf("le service ne lance pas --run poste:\n%s", service)
	}
	timer, _ := os.ReadFile(filepath.Join(dir, "borgui-poste.timer"))
	if !strings.Contains(string(timer), "OnCalendar=Fri *-*-* 21:30:00") || !strings.Contains(string(timer), "Persistent=true") {
		t.Errorf("timer inattendu:\n%s", timer)
	}

	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if s := cfg.Profiles[0].Schedule; s.Kind != "weekly" || s.Day != "friday" || s.At != "21:30" {
		t.Errorf("réglage enregistré: %+v", s)
	}
	if code := run([]string{"schedule"}); code != exitSuccess {
		t.Errorf("schedule status a retourné %d", code)
	}

	if code := run([]string{"schedule", "manual"}); code != exitSuccess {
		t.Fatalf("schedule manual a retourné %d", code)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("unités restantes après retrait: %v", entries)
	}
}

// TestPlanificationInvalide vérifie qu'une heure mal saisie ne remplace pas
// la planification en place.
func TestPlanificationInvalide(t *testing.T) {
	poste(t, "exit 0", "--clear")
	_, commandes := fauxOrdonnanceur(t)

	if code := run([]string{"schedule", "daily", "22:00"}); code != exitSuccess {
		t.Fatal("réglage initial refusé")
	}
	*commandes = nil
	if code := run([]string{"schedule", "daily", "25:99"}); code != exitError {
		t.Errorf("une heure invalide doit être refusée")
	}
	if len(*commandes) != 0 {
		t.Errorf("l'ordonnanceur ne doit pas être touché: %v", *commandes)
	}
	cfg, _ := config.Load("")
	if cfg.Profiles[0].Schedule.At != "22:00" {
		t.Errorf("la planification en place a été remplacée: %+v", cfg.Profiles[0].Schedule)
	}
}

// TestInstallationDepuisConfiguration vérifie le mode --install-schedule, qui
// applique ce que la configuration contient déjà.
func TestInstallationDepuisConfiguration(t *testing.T) {
	poste(t, "exit 0", "--clear")
	dir, _ := fauxOrdonnanceur(t)

	cfg, _ := config.Load("")
	cfg.Profiles[0].Schedule = config.Schedule{Kind: "daily", At: "12:15", CatchUpIfMissed: true}
	if err := config.Save("", cfg); err != nil {
		t.Fatal(err)
	}

	if code := run([]string{"--install-schedule", "poste"}); code != exitSuccess {
		t.Fatalf("--install-schedule a retourné %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "borgui-poste.timer")); err != nil {
		t.Errorf("timer absent: %v", err)
	}
}

// pruneScript simule « borg prune --list --dry-run » : une sauvegarde gardée,
// une retirée, sur la sortie d'erreur au format --log-json.
const pruneScript = `
case "$*" in
*prune*)
  echo '{"type":"log_message","levelname":"INFO","name":"borg.output.list","message":"Keeping archive (rule: daily #1):        poste-2026-09-24T22:00:00 Thu"}' >&2
  echo '{"type":"log_message","levelname":"INFO","name":"borg.output.list","message":"Would prune:                             poste-2026-09-01T22:00:00 Mon"}' >&2
  ;;
esac
exit 0
`

// TestConservationConfirmee vérifie EF-72 : l'aperçu est simulé, sans rien
// supprimer, et le réglage enregistré une fois confirmé.
func TestConservationConfirmee(t *testing.T) {
	journal := poste(t, pruneScript, "--clear")

	if code := run([]string{"retention", "set", "3", "0", "0", "--yes"}); code != exitSuccess {
		t.Fatalf("retention set a retourné %d", code)
	}
	appels := lireJournal(t, journal)
	if !strings.Contains(appels, "prune") || !strings.Contains(appels, "--dry-run") || !strings.Contains(appels, "--keep-daily 3") {
		t.Errorf("aperçu inattendu:\n%s", appels)
	}
	cfg, _ := config.Load("")
	if r := cfg.Profiles[0].Retention; r != (config.Retention{Daily: 3}) {
		t.Errorf("conservation enregistrée: %+v", r)
	}
}

// TestConservationSansConfirmation vérifie qu'un réglage qui retirerait des
// sauvegardes n'est pas enregistré sans confirmation possible.
func TestConservationSansConfirmation(t *testing.T) {
	poste(t, pruneScript, "--clear")

	if code := run([]string{"retention", "set", "3", "0", "0"}); code != exitError {
		t.Fatalf("retention set sans terminal a retourné %d, attendu %d", code, exitError)
	}
	cfg, _ := config.Load("")
	if r := cfg.Profiles[0].Retention; r != (config.Retention{Daily: 7, Weekly: 4, Monthly: 6}) {
		t.Errorf("la conservation a changé sans confirmation: %+v", r)
	}
}

// TestConservationInvalide vérifie qu'une saisie aberrante est refusée avant
// tout appel au moteur.
func TestConservationInvalide(t *testing.T) {
	journal := poste(t, pruneScript, "--clear")
	for _, args := range [][]string{{"-1", "0", "0"}, {"7", "4"}, {"sept", "4", "6"}, {"5000", "0", "0"}} {
		if code := run(append([]string{"retention", "set"}, args...)); code != exitError {
			t.Errorf("%v accepté", args)
		}
	}
	if strings.Contains(lireJournal(t, journal), "prune") {
		t.Error("le moteur ne doit pas être appelé")
	}
}

// TestExportImportEntrePostes vérifie TR-52 : la configuration d'un poste,
// exportée puis importée sur un poste neuf, n'y demande que le sous-compte.
func TestExportImportEntrePostes(t *testing.T) {
	poste(t, "exit 0", "--clear")
	cfg, _ := config.Load("")
	cfg.Profiles[0].Retention = config.Retention{Daily: 10, Monthly: 3}
	cfg.Profiles[0].Excludes = []string{"**/node_modules"}
	config.Save("", cfg)

	export := filepath.Join(t.TempDir(), "poste.toml")
	if code := run([]string{"config", "export", "-o", export}); code != exitSuccess {
		t.Fatalf("export a retourné %d", code)
	}
	data, _ := os.ReadFile(export)
	if strings.Contains(string(data), "u123456") {
		t.Errorf("l'export contient le sous-compte du poste d'origine:\n%s", data)
	}

	// Un second poste, vierge.
	autre := t.TempDir()
	t.Setenv("HOME", autre)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(autre, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(autre, ".local", "share"))

	if code := run([]string{"config", "import", export}); code != exitError {
		t.Errorf("un import sans sous-compte doit être refusé")
	}
	if code := run([]string{"config", "import", export, "--user", "u654321"}); code != exitSuccess {
		t.Fatalf("import a retourné %d", code)
	}
	imported, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	p := imported.Profiles[0]
	if p.Destination.User != "u654321" || p.Destination.Repo != "poste-marc" ||
		p.Retention != (config.Retention{Daily: 10, Monthly: 3}) || p.Excludes[0] != "**/node_modules" ||
		p.Encryption != config.EncryptionNone {
		t.Errorf("profil importé: %+v", p)
	}
	if code := run([]string{"config", "import", export, "--user", "u654321"}); code != exitError {
		t.Error("un import ne doit jamais écraser une configuration existante")
	}
}

// horsReseau ramène la destination du poste de test sur une adresse locale
// fermée.
func horsReseau(t *testing.T) {
	t.Helper()
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Profiles[0].Destination.Kind = config.KindSSH
	cfg.Profiles[0].Destination.Repo = "ssh://u123456@127.0.0.1:9/./poste-marc"
	if err := config.Save("", cfg); err != nil {
		t.Fatal(err)
	}
}

// TestControleDeLaDestination vérifie le mode « --check <profil> » (AR-03) :
// la commande envoyée au moteur, et le code de sortie selon l'issue — 0
// destination saine, 1 anomalies trouvées (EF-87).
func TestControleDeLaDestination(t *testing.T) {
	journal := poste(t, `
case "$*" in
*check*)
  if [ -f "$HOME/anomalies" ]; then
    echo '{"type":"log_message","levelname":"ERROR","message":"segment 12: checksum mismatch"}' >&2
    exit 1
  fi ;;
esac
exit 0
`, "--clear")

	if code := run([]string{"--check", "poste"}); code != exitSuccess {
		t.Fatalf("destination saine : code %d", code)
	}
	if appels := lireJournal(t, journal); !strings.Contains(appels, "check") || !strings.Contains(appels, "--repository-only") {
		t.Errorf("commande envoyée :\n%s", appels)
	}

	os.WriteFile(filepath.Join(os.Getenv("HOME"), "anomalies"), nil, 0o600)
	if code := run([]string{"check"}); code != exitWarning {
		t.Fatalf("anomalies : code %d, attendu %d", code, exitWarning)
	}

	path, err := config.HistoryPath()
	if err != nil {
		t.Fatal(err)
	}
	store, err := history.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	last, ok, _ := store.LastRepositoryCheck(context.Background(), "poste")
	if !ok || last.Healthy || last.Detail != "segment 12: checksum mismatch" {
		t.Errorf("contrôle consigné : %+v", last)
	}
}
