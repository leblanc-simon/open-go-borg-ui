//go:build !windows

// Ces tests substituent à Borg un script qui en reproduit le comportement
// observable : codes de retour, événements « --log-json » sur la sortie
// d'erreur, résultat JSON sur la sortie standard. Ils vérifient donc le
// pilotage sans dépendre d'une installation de Borg, et tournent sur la
// machine de développement comme en intégration continue.

package borg

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeBorg installe un script jouant le rôle de Borg et retourne un runner qui
// le pilote.
func fakeBorg(t *testing.T, script string) *NativeRunner {
	t.Helper()

	path := filepath.Join(t.TempDir(), "borg")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatalf("écriture du faux moteur: %v", err)
	}
	runner, err := NewNativeRunner(path)
	if err != nil {
		t.Fatalf("construction du runner: %v", err)
	}
	return runner
}

// TestCodeRetourAvertissement vérifie qu'un code 1 est un avertissement et non
// un échec : la sauvegarde existe, elle est simplement incomplète.
func TestCodeRetourAvertissement(t *testing.T) {
	runner := fakeBorg(t, `
echo '{"type":"log_message","levelname":"WARNING","msgid":"","message":"file changed while we backed it up"}' >&2
echo '{"archive":{"name":"poste-2026-09-03","stats":{"nfiles":12}}}'
exit 1
`)

	result, err := runner.Run(context.Background(), Command{Name: "create", LogJSON: true})
	if err != nil {
		t.Fatalf("Run a échoué: %v", err)
	}
	if result.Status != StatusWarning {
		t.Errorf("Status = %v, attendu StatusWarning", result.Status)
	}
	if got := len(result.Warnings()); got != 1 {
		t.Errorf("%d avertissement(s), attendu 1", got)
	}
	if !strings.Contains(string(result.Stdout), "poste-2026-09-03") {
		t.Error("la sortie standard doit être conservée telle quelle")
	}
}

// TestCodeRetourEchec vérifie qu'un code 2 est un échec et que le diagnostic
// reconnaît un verrou laissé par une exécution interrompue.
func TestCodeRetourEchec(t *testing.T) {
	runner := fakeBorg(t, `
echo '{"type":"log_message","levelname":"ERROR","msgid":"LockTimeout","message":"Failed to create/acquire the lock"}' >&2
exit 2
`)

	result, err := runner.Run(context.Background(), Command{Name: "create", LogJSON: true})
	if err != nil {
		t.Fatalf("Run a échoué: %v", err)
	}
	if result.Status != StatusError {
		t.Fatalf("Status = %v, attendu StatusError", result.Status)
	}
	diagnosis, ok := result.Diagnose()
	if !ok || diagnosis != FailureRepositoryLocked {
		t.Errorf("diagnostic = %v, attendu FailureRepositoryLocked", diagnosis)
	}
}

// TestProgression vérifie que les événements de progression parviennent à
// l'appelant au fil de l'eau.
func TestProgression(t *testing.T) {
	runner := fakeBorg(t, `
echo '{"type":"archive_progress","original_size":1024,"nfiles":3,"path":"/home/marc/Documents/rapport.odt"}' >&2
echo '{"type":"archive_progress","finished":true}' >&2
echo '{}'
`)

	var events []Event
	if _, err := runner.Run(context.Background(), Command{
		Name:    "create",
		LogJSON: true,
		OnEvent: func(e Event) { events = append(events, e) },
	}); err != nil {
		t.Fatalf("Run a échoué: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("%d événement(s), attendu 2", len(events))
	}
	if events[0].Kind != EventArchiveProgress || events[0].Files != 3 {
		t.Errorf("premier événement inattendu: %+v", events[0])
	}
	if events[0].Path != "/home/marc/Documents/rapport.odt" {
		t.Errorf("chemin manquant dans la progression: %+v", events[0])
	}
	if !events[1].Finished {
		t.Error("la fin de l'opération doit être signalée")
	}
}

// TestNonInteractif vérifie qu'aucune commande ne peut attendre une saisie.
// C'est la condition pour qu'une sauvegarde planifiée ne reste jamais bloquée.
func TestNonInteractif(t *testing.T) {
	runner := fakeBorg(t, `
if read ligne; then echo "saisie reçue"; exit 3; fi
exit 0
`)

	done := make(chan struct{})
	var result *Result
	go func() {
		defer close(done)
		result, _ = runner.Run(context.Background(), Command{Name: "info"})
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("la commande attend une saisie au lieu de se terminer")
	}
	if result == nil || result.ExitCode != 0 {
		t.Errorf("la lecture de l'entrée standard aurait dû échouer immédiatement: %+v", result)
	}
}

// TestAnnulation vérifie qu'une sauvegarde en cours s'arrête sur demande.
func TestAnnulation(t *testing.T) {
	// Le script s'arrête sur interruption comme le fait Borg, qui relâche
	// alors le verrou de la destination avant de rendre la main.
	runner := fakeBorg(t, `
trap 'kill $pid 2>/dev/null; exit 0' INT
sleep 30 &
pid=$!
wait $pid
`)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	started := time.Now()
	if _, err := runner.Run(ctx, Command{Name: "create"}); err == nil {
		t.Error("une annulation doit être rapportée comme telle")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("annulation trop lente: %s", elapsed)
	}
}

// TestVersion vérifie la lecture de la version du moteur.
func TestVersion(t *testing.T) {
	runner := fakeBorg(t, `echo "borg 1.4.5"`)

	version, err := runner.Version(context.Background())
	if err != nil {
		t.Fatalf("Version a échoué: %v", err)
	}
	if version != "1.4.5" {
		t.Errorf("Version = %q, attendu 1.4.5", version)
	}
}
