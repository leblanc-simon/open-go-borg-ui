//go:build !windows

// Ces tests exercent les écrans à travers le pilote de test de Fyne, sans
// affichage, sur un poste simulé : dossier personnel isolé et faux moteur
// Borg dans le PATH, comme les tests de la ligne de commande.

package gui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"leblanc.io/open-go-borg-ui/internal/config"
	"leblanc.io/open-go-borg-ui/internal/history"
	"leblanc.io/open-go-borg-ui/internal/i18n"
	"leblanc.io/open-go-borg-ui/internal/station"
)

// poste prépare un poste de test. script est le corps du faux Borg ; sans
// configuration, le poste est vierge.
func poste(t *testing.T, script string, configured bool) *station.Station {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))

	bin := filepath.Join(home, "bin")
	os.MkdirAll(bin, 0o700)
	if err := os.WriteFile(filepath.Join(bin, "borg"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if configured {
		profile := config.Default("poste")
		profile.Encryption = config.EncryptionNone
		// Une adresse locale fermée : le dépôt du fichier d'état qui suit
		// chaque sauvegarde échoue aussitôt, sans jamais sortir sur le réseau.
		profile.Destination.Kind = config.KindSSH
		profile.Destination.Repo = "ssh://u123456@127.0.0.1:9/./poste"
		profile.Sources = []string{t.TempDir()}
		if err := config.Save("", &config.Config{Profiles: []config.Profile{profile}}); err != nil {
			t.Fatal(err)
		}
	}
	st, err := station.New("", "")
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// open construit la fenêtre sur le pilote de test.
func open(t *testing.T, st *station.Station) (*ui, fyne.Window, translate) {
	t.Helper()
	test.NewTempApp(t)
	loc := i18n.MustLoad("fr")
	u := &ui{}
	win := newWindow(fyne.CurrentApp(), Deps{Station: st, T: loc.T}, u)
	return u, win, loc.T
}

// translate est la signature de la traduction, pour les assertions.
type translate = func(key string, data ...any) string

// waitFor attend qu'une condition devienne vraie : les écrans travaillent
// hors du fil de l'interface.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("délai dépassé en attendant : %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// texts rassemble les textes des étiquettes d'un conteneur.
func texts(objects ...fyne.CanvasObject) string {
	var b strings.Builder
	for _, o := range objects {
		switch w := o.(type) {
		case *widget.Label:
			b.WriteString(w.Text + "\n")
		case *fyne.Container:
			b.WriteString(texts(w.Objects...))
		case *widget.Accordion:
			for _, item := range w.Items {
				b.WriteString(texts(item.Detail))
			}
		case *container.Scroll:
			b.WriteString(texts(w.Content))
		}
	}
	return b.String()
}

// TestAccueilSansConfiguration vérifie qu'un poste vierge ouvre l'écran
// d'accueil plutôt que des écrans vides.
func TestAccueilSansConfiguration(t *testing.T) {
	u, win, tr := open(t, poste(t, "exit 0", false))
	if u.home != nil {
		t.Error("aucun écran ne doit être construit sans configuration")
	}
	if !strings.Contains(texts(win.Content()), tr("welcome.no_config")) {
		t.Errorf("écran d'accueil absent:\n%s", texts(win.Content()))
	}
}

// TestEtatVierge vérifie la phrase de l'écran État sans aucune sauvegarde.
func TestEtatVierge(t *testing.T) {
	u, _, tr := open(t, poste(t, "exit 0", true))
	waitFor(t, "la synthèse", func() bool { return u.home.headline.Text == tr("home.never") })
}

// statsScript simule une sauvegarde réussie, et la relecture de la
// destination qui la suit.
const statsScript = `
case "$*" in
*create*) echo '{"archive":{"name":"poste-2026-09-24T12:00:00","stats":{"nfiles":42,"original_size":1048576,"deduplicated_size":4096}}}' ;;
*info*) echo '{"encryption":{"mode":"none"},"cache":{"stats":{"unique_csize":8192}}}' ;;
esac
exit 0
`

// TestSauvegardeDepuisLEcran vérifie EF-50 : le bouton lance la sauvegarde,
// son issue s'affiche, elle est consignée, et l'écran État passe au vert.
func TestSauvegardeDepuisLEcran(t *testing.T) {
	st := poste(t, statsScript, true)
	u, _, tr := open(t, st)

	test.Tap(u.backup.start)
	waitFor(t, "la fin de la sauvegarde", func() bool {
		return strings.Contains(texts(u.backup.result), "42 fichiers")
	})
	waitFor(t, "l'écran État", func() bool { return u.home.headline.Text == tr("home.ok") })

	store, err := st.History()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runs, _ := store.Recent(context.Background(), "poste", 10)
	if len(runs) != 1 || runs[0].Status != history.StatusSuccess || runs[0].RepositorySize != 8192 {
		t.Errorf("historique: %+v", runs)
	}
	if !strings.Contains(u.home.details.Text, "8.0 Kio") {
		t.Errorf("espace occupé absent de l'écran État:\n%s", u.home.details.Text)
	}
}

// TestAnnulation vérifie EF-52 : une sauvegarde en cours s'annule, et elle
// est consignée comme annulée.
func TestAnnulation(t *testing.T) {
	st := poste(t, `
case "$*" in
*create*) sleep 30 ;;
esac
exit 0
`, true)
	u, _, tr := open(t, st)

	test.Tap(u.backup.start)
	waitFor(t, "le démarrage", func() bool { return !u.backup.cancel.Hidden && !u.backup.cancel.Disabled() })
	time.Sleep(200 * time.Millisecond)
	test.Tap(u.backup.cancel)
	waitFor(t, "l'annulation", func() bool {
		return strings.Contains(texts(u.backup.result), tr("backup_screen.cancelled"))
	})

	store, _ := st.History()
	defer store.Close()
	runs, _ := store.Recent(context.Background(), "poste", 10)
	if len(runs) != 1 || runs[0].Status != history.StatusCancelled {
		t.Errorf("historique: %+v", runs)
	}
}
