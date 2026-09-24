//go:build !windows

package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/config"
	"leblanc.io/open-go-borg-ui/internal/history"
	"leblanc.io/open-go-borg-ui/internal/i18n"
)

// verifyRunner simule une destination dont la sauvegarde contient un seul
// fichier, original, relevé tel qu'il était au moment de la sauvegarde.
// L'extraction écrit restored à sa place.
type verifyRunner struct {
	fakeRunner
	original    string
	size        int64
	modified    time.Time
	restored    []byte
	extractFail bool
}

func (r *verifyRunner) Run(ctx context.Context, cmd borg.Command) (*borg.Result, error) {
	r.names = append(r.names, cmd.Name)
	archived := r.original[1:]
	switch {
	case cmd.Name == "list" && len(cmd.Flags) > 0 && cmd.Flags[0] == "--json-lines":
		line, _ := json.Marshal(map[string]any{
			"type": "-", "path": archived, "size": r.size,
			"mtime": r.modified.Format("2006-01-02T15:04:05.000000"),
		})
		return &borg.Result{Status: borg.StatusSuccess, Stdout: append(line, '\n')}, nil
	case cmd.Name == "extract":
		if r.extractFail {
			return &borg.Result{Status: borg.StatusError, ExitCode: 2}, nil
		}
		target := filepath.Join(cmd.Dir, cmd.Sources[0])
		os.MkdirAll(filepath.Dir(target), 0o700)
		return &borg.Result{Status: borg.StatusSuccess}, os.WriteFile(target, r.restored, 0o600)
	case cmd.Name == "create":
		return &borg.Result{Status: borg.StatusSuccess, Stdout: []byte(
			`{"archive":{"name":"poste-1","id":"a1","stats":{"nfiles":1,"original_size":7,"deduplicated_size":7}}}`)}, nil
	}
	return &borg.Result{Status: borg.StatusSuccess}, nil
}

// verifying prépare un original sur le disque, son relevé dans la
// sauvegarde, et un historique neuf.
func verifying(t *testing.T) (*verifyRunner, *history.Store) {
	t.Helper()
	original := filepath.Join(t.TempDir(), "lettre à Paul.odt")
	content := []byte("bonjour")
	if err := os.WriteFile(original, content, 0o600); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(original)
	store, err := history.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return &verifyRunner{original: original, size: info.Size(), modified: info.ModTime(), restored: content}, store
}

var verified = borg.Archive{Name: "poste-1", ID: "a1"}

// TestVerificationIdentique vérifie l'issue attendue : le fichier extrait
// est celui de l'original, et la vérification est consignée.
func TestVerificationIdentique(t *testing.T) {
	runner, store := verifying(t)
	check, err := VerifyRestore(context.Background(), runner, borg.Environment{}, store, "poste", verified, time.Now())
	if err != nil || check.Outcome != history.OutcomeIdentical || check.Native != runner.original {
		t.Fatalf("vérification: %+v, %v", check, err)
	}
	if last, ok, _ := store.LastRestoreCheck(context.Background(), "poste"); !ok || last.ID != check.ID {
		t.Errorf("vérification non consignée: %+v", last)
	}
}

// TestVerificationIssues vérifie les autres issues : original disparu ou
// modifié, contenu altéré, extraction impossible.
func TestVerificationIssues(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(r *verifyRunner)
		outcome history.Outcome
		reason  string
	}{
		{"original disparu", func(r *verifyRunner) { os.Remove(r.original) },
			history.OutcomeExtracted, ReasonOriginalMissing},
		{"original modifié", func(r *verifyRunner) {
			os.Chtimes(r.original, time.Now(), r.modified.Add(time.Hour))
		}, history.OutcomeExtracted, ReasonOriginalChanged},
		{"contenu altéré", func(r *verifyRunner) { r.restored = []byte("bonsoir") },
			history.OutcomeFailed, ReasonContentDiffers},
		{"taille différente", func(r *verifyRunner) { r.restored = []byte("bonjour !") },
			history.OutcomeFailed, ReasonSizeDiffers},
		{"extraction impossible", func(r *verifyRunner) { r.extractFail = true },
			history.OutcomeFailed, ReasonExtractFailed},
	}
	for _, c := range cases {
		runner, store := verifying(t)
		c.prepare(runner)
		check, err := VerifyRestore(context.Background(), runner, borg.Environment{}, store, "poste", verified, time.Now())
		if err != nil || check.Outcome != c.outcome || check.Reason != c.reason {
			t.Errorf("%s: %+v, %v", c.name, check, err)
		}
	}
}

// TestVerificationDue vérifie le rythme mensuel.
func TestVerificationDue(t *testing.T) {
	_, store := verifying(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.Local)
	if due, _ := VerifyDue(ctx, store, "poste", now); !due {
		t.Error("jamais vérifiée : la vérification est due")
	}
	store.AddRestoreCheck(ctx, history.RestoreCheck{Profile: "poste", Checked: now.AddDate(0, 0, -10), Outcome: history.OutcomeIdentical})
	if due, _ := VerifyDue(ctx, store, "poste", now); due {
		t.Error("vérifiée il y a dix jours : rien n'est dû")
	}
	if due, _ := VerifyDue(ctx, store, "poste", now.AddDate(0, 0, 25)); !due {
		t.Error("vérifiée il y a trente-cinq jours : la vérification est due")
	}
}

// TestSauvegardeVerifieUneFoisParMois vérifie l'accroche à la sauvegarde :
// la première vérifie, la suivante du même mois non.
func TestSauvegardeVerifieUneFoisParMois(t *testing.T) {
	runner, store := verifying(t)
	service := &Backup{Runner: runner, History: store, VerifyRestores: true}
	profile := config.Default("poste")
	profile.Sources = []string{filepath.Dir(runner.original)}
	var phases []Phase
	req := BackupRequest{Profile: &profile, OnPhase: func(p Phase) { phases = append(phases, p) }}

	report, err := service.Run(context.Background(), req)
	if err != nil || report.Verification == nil || report.Verification.Outcome != history.OutcomeIdentical {
		t.Fatalf("première sauvegarde: %+v, %v, %v", report.Verification, report.VerifyErr, err)
	}
	if phases[len(phases)-1] != PhaseVerify {
		t.Errorf("étapes annoncées: %v", phases)
	}

	report, err = service.Run(context.Background(), req)
	if err != nil || report.Verification != nil || report.VerifyErr != nil {
		t.Errorf("seconde sauvegarde du mois: %+v, %v", report.Verification, report.VerifyErr)
	}
}

// TestVerificationSansFichier vérifie qu'une sauvegarde sans fichier ne
// consigne rien : la vérification sera retentée.
func TestVerificationSansFichier(t *testing.T) {
	runner, store := verifying(t)
	runner.size = 0
	if _, err := VerifyRestore(context.Background(), runner, borg.Environment{}, store, "poste", verified, time.Now()); !errors.Is(err, ErrNothingToVerify) {
		t.Errorf("sauvegarde sans fichier: %v", err)
	}
	if _, ok, _ := store.LastRestoreCheck(context.Background(), "poste"); ok {
		t.Error("rien ne doit être consigné")
	}
}

// TestRaisonsTraduites vérifie que chaque issue de vérification a sa phrase
// dans les deux catalogues : ces clés sont calculées, le contrôle des clés
// écrites dans le code ne les voit pas.
func TestRaisonsTraduites(t *testing.T) {
	reasons := []string{ReasonIdentical, ReasonOriginalMissing, ReasonOriginalChanged, ReasonOriginalUnread,
		ReasonExtractFailed, ReasonSizeDiffers, ReasonContentDiffers}
	for _, lang := range []string{"fr", "en"} {
		loc := i18n.MustLoad(lang)
		for _, reason := range reasons {
			if got := loc.T(reason, map[string]any{"Path": "/x"}); got == reason || !strings.Contains(got, "/x") {
				t.Errorf("%s: %q traduit en %q", lang, reason, got)
			}
		}
	}
}
