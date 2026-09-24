package borg

import (
	"context"
	"reflect"
	"testing"
)

// TestOrigin vérifie la traduction d'un chemin d'archive en chemin du
// poste, selon la convention de chaque Runner (AR-02, EF-97).
func TestOrigin(t *testing.T) {
	native, root, err := (&NativeRunner{}).Origin("home/marc/Documents/lettre.odt")
	if err != nil || native != "/home/marc/Documents/lettre.odt" || root != "/" {
		t.Errorf("Linux: %q depuis %q, %v", native, root, err)
	}

	native, root, err = (&CygwinRunner{}).Origin("c/Users/marc/Documents/lettre.odt")
	if err != nil || native != `C:\Users\marc\Documents\lettre.odt` || root != `C:\` {
		t.Errorf("Windows: %q depuis %q, %v", native, root, err)
	}
	if native, _, _ := (&CygwinRunner{}).Origin("d"); native != `D:\` {
		t.Errorf("racine du lecteur: %q", native)
	}
	if _, _, err := (&CygwinRunner{}).Origin("Users/marc"); err == nil {
		t.Error("un chemin sans lettre de lecteur doit être refusé")
	}
}

// driveRunner retient les extractions, avec la convention de Windows.
type driveRunner struct {
	recorder
	commands []Command
	status   map[string]Status
}

func (r *driveRunner) Run(_ context.Context, cmd Command) (*Result, error) {
	r.commands = append(r.commands, cmd)
	return &Result{Status: r.status[cmd.Dir]}, nil
}

func (r *driveRunner) Origin(path string) (string, string, error) { return fromDriveRelative(path) }

// TestExtractEnPlace vérifie qu'une restauration à l'emplacement d'origine
// fait une extraction par lecteur, depuis sa racine, et qu'un
// avertissement sur l'un n'est pas effacé par le succès de l'autre.
func TestExtractEnPlace(t *testing.T) {
	runner := &driveRunner{status: map[string]Status{`C:\`: StatusWarning}}
	result, err := Extract(context.Background(), runner, ExtractOptions{
		Archive: "poste-2026-09-24T22:00:00",
		Paths:   []string{"c/Users/marc/a.txt", "d/Projets", "c/Users/marc/b.txt"},
		InPlace: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != 2 {
		t.Fatalf("%d extractions, attendu une par lecteur", len(runner.commands))
	}
	c, d := runner.commands[0], runner.commands[1]
	if c.Dir != `C:\` || !reflect.DeepEqual(c.Sources, []string{"c/Users/marc/a.txt", "c/Users/marc/b.txt"}) {
		t.Errorf("lecteur C: %q %v", c.Dir, c.Sources)
	}
	if d.Dir != `D:\` || !reflect.DeepEqual(d.Sources, []string{"d/Projets"}) {
		t.Errorf("lecteur D: %q %v", d.Dir, d.Sources)
	}
	if result.Status != StatusWarning {
		t.Errorf("issue: %v, attendu l'avertissement du lecteur C", result.Status)
	}

	if _, err := Extract(context.Background(), runner, ExtractOptions{InPlace: true}); err == nil {
		t.Error("une restauration en place sans chemin doit être refusée")
	}
}

// contentsRunner répond au listage du contenu d'une archive.
type contentsRunner struct {
	recorder
	received Command
}

func (r *contentsRunner) Run(_ context.Context, cmd Command) (*Result, error) {
	r.received = cmd
	return &Result{Status: StatusSuccess, Stdout: []byte(
		`{"type": "d", "mode": "drwxr-xr-x", "path": "home/marc/Documents", "size": 0, "mtime": "2026-09-01T10:00:00.000000"}
{"type": "-", "mode": "-rw-r--r--", "path": "home/marc/Documents/lettre.odt", "size": 2048, "mtime": "2026-09-03T22:14:07.000000"}

`)}, nil
}

// TestListContents vérifie la lecture du contenu d'une archive.
func TestListContents(t *testing.T) {
	runner := &contentsRunner{}
	items, _, err := ListContents(context.Background(), runner, Environment{}, "poste-1")
	if err != nil {
		t.Fatal(err)
	}
	if runner.received.Target != "::poste-1" || runner.received.Flags[0] != "--json-lines" {
		t.Errorf("commande: %+v", runner.received)
	}
	if len(items) != 2 || !items[0].Dir() || items[1].Dir() || items[1].Size != 2048 ||
		items[1].Mtime.Format("2006-01-02 15:04") != "2026-09-03 22:14" {
		t.Errorf("contenu: %+v", items)
	}
}
