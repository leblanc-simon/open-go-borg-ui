//go:build !windows

package gui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"

	"leblanc.io/open-go-borg-ui/internal/config"
	"leblanc.io/open-go-borg-ui/internal/probe"
	"leblanc.io/open-go-borg-ui/internal/secret"
	"leblanc.io/open-go-borg-ui/internal/wizard"
)

// TestPlanificationDepuisLesReglages vérifie EF-60 : la planification
// enregistrée depuis les Réglages installe la tâche, et le passage en
// manuel la retire.
func TestPlanificationDepuisLesReglages(t *testing.T) {
	units := fakeScheduler(t)
	st := poste(t, "exit 0", true)
	u, _, tr := open(t, st)
	s := u.settings
	timer := filepath.Join(units, "borgui-poste.timer")

	s.plan = config.Schedule{Kind: "daily", At: "21:15", CatchUpIfMissed: true}
	s.applySchedule()
	waitFor(t, "l'installation", func() bool { return len(s.planResult.Objects) > 0 })
	if _, err := os.Stat(timer); err != nil {
		t.Fatalf("tâche non installée: %v\n%s", err, texts(s.planResult))
	}
	if profile, _ := st.Profile(); profile.Schedule.At != "21:15" {
		t.Errorf("planification enregistrée: %+v", profile.Schedule)
	}
	if u.backup.plan.Text != tr("backup_screen.plan_daily", map[string]any{"At": "21:15"}) {
		t.Errorf("écran Sauvegarde non mis à jour: %q", u.backup.plan.Text)
	}

	s.plan.Kind = "manual"
	s.applySchedule()
	waitFor(t, "le retrait", func() bool {
		_, err := os.Stat(timer)
		return errors.Is(err, os.ErrNotExist)
	})
}

// reconfiguring ouvre un changement de destination, avec ou sans nouvelle
// clé, sur un poste configuré dont la clé actuelle existe.
func reconfiguring(t *testing.T, newKey bool) (*ui, string) {
	t.Helper()
	keyring.MockInit()
	st := poste(t, "exit 0", true)
	current, _ := config.SSHKeyPath()
	if _, err := probe.EnsureKey(current); err != nil {
		t.Fatal(err)
	}
	u, _, _ := open(t, st)
	profile, _ := st.Profile()
	if err := u.reconfigure(wizard.ForDestination(*profile, newKey)); err != nil {
		t.Fatal(err)
	}
	if u.wizard == nil || u.wizard.state.Step != wizard.StepDestination {
		t.Fatal("la reconfiguration doit ouvrir l'assistant sur la destination")
	}
	return u, current
}

// TestAbandonDeReconfiguration vérifie qu'abandonner laisse le poste tel
// qu'il était, et efface ce qui avait été préparé.
func TestAbandonDeReconfiguration(t *testing.T) {
	u, current := reconfiguring(t, true)
	st, w := u.st, u.wizard
	st.Secrets().Set("poste", "mot de passe en service")
	st.Secrets().Set(w.state.SecretName(), "mot de passe préparé")
	fresh := w.state.Profile.Destination.SSHKey
	if fresh == "" || fresh == current {
		t.Fatalf("nouvelle clé: %q", fresh)
	}
	waitFor(t, "la nouvelle clé", func() bool { _, err := os.Stat(fresh); return err == nil })

	w.discard()
	u.wizard = nil
	u.showMain()

	if got, _ := st.Secrets().Get("poste"); got != "mot de passe en service" {
		t.Errorf("mot de passe en service: %q", got)
	}
	if _, err := st.Secrets().Get("poste.pending"); !errors.Is(err, secret.ErrNotFound) {
		t.Errorf("mot de passe préparé non effacé: %v", err)
	}
	if _, err := os.Stat(fresh); !errors.Is(err, os.ErrNotExist) {
		t.Error("la clé préparée doit être effacée")
	}
	if _, err := os.Stat(current); err != nil {
		t.Errorf("la clé en service doit rester: %v", err)
	}
	if profile, _ := st.Profile(); profile.Destination.Repo != "ssh://u123456@127.0.0.1:9/./poste" {
		t.Errorf("configuration modifiée: %+v", profile.Destination)
	}
}

// TestValidationDeReconfiguration vérifie la mise en service : le mot de
// passe préparé remplace l'ancien, la configuration est remplacée, et
// l'ancienne clé, remplacée par une nouvelle, est effacée du poste.
func TestValidationDeReconfiguration(t *testing.T) {
	u, current := reconfiguring(t, true)
	st, w := u.st, u.wizard
	st.Secrets().Set("poste", "ancien")
	st.Secrets().Set(w.state.SecretName(), "nouveau")
	w.state.Profile.Destination.Repo = "ssh://u654321@127.0.0.1:9/./ailleurs"
	w.state.Profile.Encryption = config.EncryptionRepokey
	fresh := w.state.Profile.Destination.SSHKey

	if err := w.commitReconfiguration(); err != nil {
		t.Fatal(err)
	}
	profile, _ := st.Profile()
	if profile.Destination.Repo != "ssh://u654321@127.0.0.1:9/./ailleurs" || profile.Destination.SSHKey != fresh {
		t.Errorf("configuration: %+v", profile.Destination)
	}
	if len(profile.Sources) != 1 {
		t.Errorf("les dossiers doivent être gardés: %v", profile.Sources)
	}
	if got, _ := st.Secrets().Get("poste"); got != "nouveau" {
		t.Errorf("mot de passe en service: %q", got)
	}
	if _, err := st.Secrets().Get("poste.pending"); !errors.Is(err, secret.ErrNotFound) {
		t.Errorf("mot de passe provisoire non effacé: %v", err)
	}
	if _, err := os.Stat(current); !errors.Is(err, os.ErrNotExist) {
		t.Error("l'ancienne clé doit être effacée du poste")
	}
}

// TestValidationEnGardantLaCle vérifie qu'en gardant la clé, rien n'est
// effacé, et qu'une destination non chiffrée n'a plus de mot de passe.
func TestValidationEnGardantLaCle(t *testing.T) {
	u, current := reconfiguring(t, false)
	st, w := u.st, u.wizard
	st.Secrets().Set("poste", "ancien")
	w.state.Profile.Encryption = config.EncryptionNone

	if err := w.commitReconfiguration(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(current); err != nil {
		t.Errorf("la clé gardée doit rester: %v", err)
	}
	if profile, _ := st.Profile(); profile.Destination.SSHKey != "" {
		t.Errorf("clé de la configuration: %q", profile.Destination.SSHKey)
	}
	if _, err := st.Secrets().Get("poste"); !errors.Is(err, secret.ErrNotFound) {
		t.Errorf("une destination non chiffrée n'a plus de mot de passe: %v", err)
	}
}
