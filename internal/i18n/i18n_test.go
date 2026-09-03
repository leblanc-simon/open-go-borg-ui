package i18n

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// catalogue charge les clés d'une langue.
func catalogue(t *testing.T, language string) map[string]any {
	t.Helper()

	data, err := localesFS.ReadFile("locales/" + language + ".yaml")
	if err != nil {
		t.Fatalf("lecture du catalogue %s: %v", language, err)
	}
	var messages map[string]any
	if err := yaml.Unmarshal(data, &messages); err != nil {
		t.Fatalf("catalogue %s illisible: %v", language, err)
	}
	return messages
}

// TestCataloguesAlignes vérifie qu'aucune langue n'a pris de retard : une clé
// présente d'un seul côté produirait une interface partiellement traduite.
func TestCataloguesAlignes(t *testing.T) {
	french, english := catalogue(t, "fr"), catalogue(t, "en")

	for key := range french {
		if _, ok := english[key]; !ok {
			t.Errorf("clé %q absente du catalogue anglais", key)
		}
	}
	for key := range english {
		if _, ok := french[key]; !ok {
			t.Errorf("clé %q absente du catalogue français", key)
		}
	}
}

// clefUtilisee repère les appels de traduction dans le code source.
var clefUtilisee = regexp.MustCompile(`T\("([a-z][a-z0-9_.]*)"`)

// TestClesUtiliseesExistent vérifie que chaque clé demandée par le code figure
// au catalogue. Sans ce garde-fou, une clé mal orthographiée s'affiche telle
// quelle à l'utilisateur, sans que rien n'échoue.
func TestClesUtiliseesExistent(t *testing.T) {
	french := catalogue(t, "fr")

	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range clefUtilisee.FindAllStringSubmatch(string(source), -1) {
			key := match[1]
			if _, ok := french[key]; !ok {
				t.Errorf("%s: clé %q absente du catalogue", path, key)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("parcours des sources: %v", err)
	}
}

// TestLangueForcee vérifie la sélection explicite de la langue.
func TestLangueForcee(t *testing.T) {
	english, err := Load("en")
	if err != nil {
		t.Fatalf("Load a échoué: %v", err)
	}
	if got := english.T("status.success"); got != "Completed" {
		t.Errorf("traduction anglaise = %q", got)
	}

	french, err := Load("fr")
	if err != nil {
		t.Fatalf("Load a échoué: %v", err)
	}
	if got := french.T("status.success"); got != "Terminé" {
		t.Errorf("traduction française = %q", got)
	}
}

// TestLangueDepuisEnvironnement vérifie la lecture des variables de locale
// POSIX, dont la forme diffère d'un en-tête Accept-Language.
func TestLangueDepuisEnvironnement(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "en_US.UTF-8")

	if got := envLanguage(); got != "en-US" {
		t.Errorf("envLanguage() = %q, attendu en-US", got)
	}

	t.Setenv("LANG", "C")
	if got := envLanguage(); got != "" {
		t.Errorf("envLanguage() = %q, attendu une chaîne vide pour la locale C", got)
	}
}

// TestParametresSubstitues vérifie qu'un message paramétré rend bien ses
// valeurs, dans les deux langues.
func TestParametresSubstitues(t *testing.T) {
	for _, language := range []string{"fr", "en"} {
		loc, err := Load(language)
		if err != nil {
			t.Fatalf("Load(%s) a échoué: %v", language, err)
		}
		got := loc.T("runtime.installed", map[string]any{"Version": "1.4.5", "Path": "/usr/bin/borg"})
		if !strings.Contains(got, "1.4.5") || !strings.Contains(got, "/usr/bin/borg") {
			t.Errorf("message %s non substitué: %q", language, got)
		}
	}
}
