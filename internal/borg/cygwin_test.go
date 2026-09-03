package borg

import (
	"strings"
	"testing"
)

// testCygwinRunner construit un runner sans toucher au disque : la génération
// de la ligne de commande doit être vérifiable depuis n'importe quelle
// plateforme, y compris la machine de développement Linux.
func testCygwinRunner() *CygwinRunner {
	return newCygwinRunner(`C:\Users\marc\AppData\Local\borgui\runtime\1.4.5-cygwin.1`,
		`C:\Users\marc\AppData\Local\borgui\runtime\1.4.5-cygwin.1\bin\bash.exe`,
		`C:\Users\marc\AppData\Local\borgui\runtime\1.4.5-cygwin.1\bin\borg.exe`)
}

// TestScriptSauvegarde vérifie la convention de chemins d'une sauvegarde : le
// répertoire courant est /cygdrive et les sources sont relatives, préfixées de
// la lettre de lecteur.
func TestScriptSauvegarde(t *testing.T) {
	runner := testCygwinRunner()

	script, err := runner.script(Command{
		Name:     "create",
		Flags:    []string{"--stats"},
		Target:   "::" + ArchivePattern,
		Sources:  []string{`C:\Users\marc\Documents`, `D:\Projets`},
		PathMode: PathSources,
		Env:      Environment{RemotePath: "borg-1.4"},
		LogJSON:  true,
	})
	if err != nil {
		t.Fatalf("script a échoué: %v", err)
	}

	if !strings.HasPrefix(script, "cd /cygdrive && exec ") {
		t.Errorf("le script ne se place pas dans /cygdrive: %s", script)
	}
	for _, want := range []string{
		"c/Users/marc/Documents",
		"d/Projets",
		"--remote-path=borg-1.4",
		"--log-json",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("le script ne contient pas %q: %s", want, script)
		}
	}
	if strings.Contains(script, "cygdrive/c/Users") {
		t.Errorf("les sources ne doivent pas être préfixées de cygdrive: %s", script)
	}
}

// TestScriptExtraction vérifie qu'une restauration se place dans le dossier de
// destination et retire la lettre de lecteur des chemins écrits.
func TestScriptExtraction(t *testing.T) {
	runner := testCygwinRunner()

	script, err := runner.script(Command{
		Name:     "extract",
		Target:   "::poste-2026-09-03T12:00:00",
		Sources:  []string{"c/Users/marc/Documents/rapport.odt"},
		Dir:      `C:\Users\marc\Desktop\Restauration 2026-09-03`,
		PathMode: PathExtract,
		Env:      Environment{},
	})
	if err != nil {
		t.Fatalf("script a échoué: %v", err)
	}

	if !strings.HasPrefix(script, `cd '/cygdrive/c/Users/marc/Desktop/Restauration 2026-09-03' && exec `) {
		t.Errorf("destination d'extraction incorrecte: %s", script)
	}
	if !strings.Contains(script, "--strip-components 1") {
		t.Errorf("la lettre de lecteur n'est pas retirée à l'extraction: %s", script)
	}
}

// TestScriptEchappement vérifie qu'un chemin contenant une apostrophe ne peut
// pas s'échapper de sa protection.
func TestScriptEchappement(t *testing.T) {
	runner := testCygwinRunner()

	script, err := runner.script(Command{
		Name:     "create",
		Target:   "::sauvegarde",
		Sources:  []string{`C:\Users\marc\Dossier d'Été`},
		PathMode: PathSources,
		Env:      Environment{},
	})
	if err != nil {
		t.Fatalf("script a échoué: %v", err)
	}
	if !strings.Contains(script, `'c/Users/marc/Dossier d'\''Été'`) {
		t.Errorf("apostrophe mal protégée: %s", script)
	}
}

// TestScriptRefuseCheminReseau vérifie qu'une source hors convention arrête la
// commande au lieu de produire une archive mal formée.
func TestScriptRefuseCheminReseau(t *testing.T) {
	runner := testCygwinRunner()

	if _, err := runner.script(Command{
		Name:     "create",
		Sources:  []string{`\\serveur\partage`},
		PathMode: PathSources,
	}); err == nil {
		t.Error("un chemin réseau devrait être refusé")
	}
}

// TestCheminBorgDansRuntime vérifie que l'exécutable est désigné par son
// chemin Cygwin, relatif à la racine du runtime.
func TestCheminBorgDansRuntime(t *testing.T) {
	runner := testCygwinRunner()
	if got, want := runner.Executable(), "/bin/borg.exe"; got != want {
		t.Errorf("Executable() = %q, attendu %q", got, want)
	}
}
