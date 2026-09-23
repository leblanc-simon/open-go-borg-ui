package lock

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestExclusion vérifie qu'un second preneur est refusé tant que le premier
// tient le verrou, puis accepté après sa libération.
func TestExclusion(t *testing.T) {
	path := PathFor(t.TempDir(), "ssh://u1@u1.your-storagebox.de:23/./poste")

	first, err := Acquire(path)
	if err != nil {
		t.Fatalf("premier verrou: %v", err)
	}
	if _, err := Acquire(path); !errors.Is(err, ErrBusy) {
		t.Fatalf("second verrou: %v, attendu ErrBusy", err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	again, err := Acquire(path)
	if err != nil {
		t.Fatalf("verrou après libération: %v", err)
	}
	again.Release()
}

// TestDestinationsDistinctes vérifie que deux destinations n'ont pas le même
// verrou, et qu'une même destination a toujours le même.
func TestDestinationsDistinctes(t *testing.T) {
	dir := t.TempDir()
	a := PathFor(dir, "ssh://u1@u1.your-storagebox.de:23/./poste")
	b := PathFor(dir, "ssh://u2@u2.your-storagebox.de:23/./poste")
	if a == b {
		t.Error("deux destinations partagent un verrou")
	}
	if a != PathFor(dir, "ssh://u1@u1.your-storagebox.de:23/./poste") {
		t.Error("une destination doit toujours avoir le même verrou")
	}
}

// TestLiberationALaMort vérifie le point qui justifie un verrou du système :
// un processus tué sans rien libérer ne laisse pas de verrou derrière lui.
func TestLiberationALaMort(t *testing.T) {
	if os.Getenv("BORGUI_LOCK_HOLDER") != "" {
		if _, err := Acquire(os.Getenv("BORGUI_LOCK_HOLDER")); err != nil {
			os.Exit(3)
		}
		os.Stdout.WriteString("tenu\n")
		select {} // attend d'être tué
	}

	path := filepath.Join(t.TempDir(), "poste.lock")
	holder := exec.Command(os.Args[0], "-test.run=TestLiberationALaMort")
	holder.Env = append(os.Environ(), "BORGUI_LOCK_HOLDER="+path)
	out, err := holder.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 5)
	if _, err := out.Read(buffer); err != nil {
		t.Fatalf("le processus témoin n'a pas pris le verrou: %v", err)
	}
	if _, err := Acquire(path); !errors.Is(err, ErrBusy) {
		t.Fatalf("verrou tenu par un autre processus: %v, attendu ErrBusy", err)
	}

	holder.Process.Kill()
	holder.Wait()

	after, err := Acquire(path)
	if err != nil {
		t.Fatalf("verrou non libéré par la mort du processus: %v", err)
	}
	after.Release()
}
