package borg

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

// TestExclusionsWindows vérifie que les chemins écartés prennent la forme de
// l'archive — lettre de lecteur en tête — et que le fichier est désigné sous
// son chemin Cygwin.
func TestExclusionsWindows(t *testing.T) {
	cmd := Command{
		Name:  "create",
		Flags: []string{"--stats"},
		ExcludePaths: []string{
			`C:\Users\marc\OneDrive\Rapport [final].docx`,
			`C:\Users\marc\OneDrive\Photos\été 2026.jpg`,
		},
	}

	var servicePath string
	converted, cleanup, err := withExcludeFile(cmd, toDriveRelative, func(p string) string {
		servicePath = p
		return "/cygdrive/fichier-exclusions"
	})
	if err != nil {
		t.Fatalf("withExcludeFile: %v", err)
	}
	defer cleanup()

	flags := strings.Join(converted.Flags, " ")
	if flags != "--stats --exclude-from /cygdrive/fichier-exclusions" {
		t.Errorf("options: %q", flags)
	}
	if len(converted.ExcludePaths) != 0 {
		t.Error("les chemins doivent avoir été consommés")
	}
	if len(cmd.Flags) != 1 {
		t.Error("les options de la commande d'origine ne doivent pas être modifiées")
	}

	content, err := os.ReadFile(servicePath)
	if err != nil {
		t.Fatalf("lecture du fichier d'exclusions: %v", err)
	}
	want := "pp:c/Users/marc/OneDrive/Rapport [final].docx\npp:c/Users/marc/OneDrive/Photos/été 2026.jpg\n"
	if string(content) != want {
		t.Errorf("contenu:\n%s\nattendu:\n%s", content, want)
	}

	cleanup()
	if _, err := os.Stat(servicePath); !os.IsNotExist(err) {
		t.Error("le fichier d'exclusions doit être supprimé au nettoyage")
	}
}

// TestExclusionsLinux vérifie la forme native : sans barre oblique initiale,
// comme Borg stocke les chemins.
func TestExclusionsLinux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("convention de chemins Linux ; celle de Cygwin est éprouvée par TestExclusionsWindows")
	}
	var servicePath string
	converted, cleanup, err := withExcludeFile(Command{
		ExcludePaths: []string{"/home/marc/Nextcloud/../Nextcloud/doc.odt"},
	}, nativeArchivePath, func(p string) string { servicePath = p; return p })
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	content, _ := os.ReadFile(servicePath)
	if string(content) != "pp:home/marc/Nextcloud/doc.odt\n" {
		t.Errorf("contenu: %q", content)
	}
	if converted.Flags[len(converted.Flags)-1] != servicePath {
		t.Errorf("options: %v", converted.Flags)
	}
}

// TestSansExclusion vérifie qu'aucun fichier n'est créé sans chemin à écarter.
func TestSansExclusion(t *testing.T) {
	cmd := Command{Flags: []string{"--stats"}}
	converted, cleanup, err := withExcludeFile(cmd, toDriveRelative, toCygwinPath)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if strings.Join(converted.Flags, " ") != "--stats" {
		t.Errorf("options: %v", converted.Flags)
	}
}

// TestExclusionRelativeRefusee vérifie qu'un chemin relatif, ambigu, est
// refusé plutôt que traduit au hasard.
func TestExclusionRelativeRefusee(t *testing.T) {
	_, cleanup, err := withExcludeFile(Command{ExcludePaths: []string{`Documents\a.txt`}}, toDriveRelative, toCygwinPath)
	cleanup()
	if err == nil {
		t.Error("un chemin relatif doit être refusé")
	}
}
