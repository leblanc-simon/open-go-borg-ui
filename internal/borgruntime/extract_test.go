package borgruntime

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestExtractionArbreCygwin reproduit la forme de l'archive livrée : des noms
// préfixés de « ./ », des dossiers vides, des liens symboliques absolus et
// relatifs, des liens durs.
func TestExtractionArbreCygwin(t *testing.T) {
	archive, _ := makeArchive(t,
		dir("./"),
		dir("./bin/"),
		dir("./tmp/"),
		dir("./etc/alternatives/"),
		file("./bin/gawk-5.4.0.exe", "vrai awk"),
		file("./bin/python3.12.exe", "vrai python"),
		symlink("./bin/awk", "gawk.exe"),
		symlink("./bin/python3", "/etc/alternatives/python3"),
		symlink("./etc/alternatives/python3", "/usr/bin/python3.12"),
		hardlink("./bin/gawk.exe", "./bin/gawk-5.4.0.exe"),
	)

	dest := t.TempDir()
	if err := extract(context.Background(), archive, dest); err != nil {
		t.Fatalf("extraction: %v", err)
	}

	// /tmp est vide dans l'arbre livré, et son absence se voit à chaque
	// commande : l'entrée de dossier doit être honorée.
	if info, err := os.Stat(filepath.Join(dest, "tmp")); err != nil || !info.IsDir() {
		t.Errorf("le dossier vide tmp n'a pas été créé: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dest, "bin", "gawk.exe"))
	if err != nil || string(content) != "vrai awk" {
		t.Errorf("lien dur gawk.exe: contenu %q, erreur %v", content, err)
	}

	for name, target := range map[string]string{
		"bin/awk":                  "gawk.exe",
		"bin/python3":              "/etc/alternatives/python3",
		"etc/alternatives/python3": "/usr/bin/python3.12",
	} {
		full := filepath.Join(dest, filepath.FromSlash(name))
		info, err := os.Lstat(full)
		if err != nil {
			t.Errorf("lien %s absent: %v", name, err)
			continue
		}
		// Jamais un lien natif : il demanderait un privilège sous Windows,
		// et pourrait désigner un chemin hors du dossier d'installation.
		if !info.Mode().IsRegular() {
			t.Errorf("lien %s: mode %v, attendu un fichier ordinaire", name, info.Mode())
		}
		got, _ := os.ReadFile(full)
		if !bytes.Equal(got, cygwinSymlinkContent(target)) {
			t.Errorf("lien %s: contenu % x", name, got)
		}
	}
}

// TestFormatLienCygwin fixe le format relevé sur un arbre écrit par Cygwin :
// /bin/python3 y occupe 64 octets pour la cible /etc/alternatives/python3.
func TestFormatLienCygwin(t *testing.T) {
	got := cygwinSymlinkContent("/etc/alternatives/python3")
	if len(got) != 64 {
		t.Errorf("taille %d, attendue 64", len(got))
	}
	if !bytes.HasPrefix(got, []byte("!<symlink>\xff\xfe/\x00e\x00t\x00c\x00")) {
		t.Errorf("début inattendu: % x", got[:20])
	}
	if !bytes.HasSuffix(got, []byte("3\x00\x00\x00")) {
		t.Errorf("fin inattendue: % x", got[len(got)-4:])
	}

	// Une cible non ASCII est la raison d'écrire la forme UTF-16.
	accented := cygwinSymlinkContent("é")
	if !bytes.Equal(accented, []byte("!<symlink>\xff\xfe\xe9\x00\x00\x00")) {
		t.Errorf("cible accentuée: % x", accented)
	}
}

// TestEntreesHostiles vérifie que ni un nom ni la cible d'un lien dur ne
// permettent d'écrire ou de lire hors du dossier d'extraction.
func TestEntreesHostiles(t *testing.T) {
	cases := map[string][]entry{
		"remontée":              {file("../evasion.txt", "x")},
		"remontée masquée":      {file("./bin/../../evasion.txt", "x")},
		"chemin absolu":         {file("/evasion.txt", "x")},
		"lien dur hors dossier": {hardlink("./bin/passwd", "../../etc/passwd")},
		"lien dur absolu":       {hardlink("./bin/passwd", "/etc/passwd")},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			archive, _ := makeArchive(t, entries...)
			parent := t.TempDir()
			dest := filepath.Join(parent, "runtime")
			if err := os.Mkdir(dest, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := extract(context.Background(), archive, dest); err == nil {
				t.Fatal("l'entrée aurait dû être refusée")
			}
			if _, err := os.Stat(filepath.Join(parent, "evasion.txt")); err == nil {
				t.Error("un fichier a été écrit hors du dossier d'extraction")
			}
		})
	}
}

// TestLienSymboliqueNeSuitRien vérifie qu'un lien suivi d'un fichier du même
// nom n'écrit pas à travers le lien : les liens étant des fichiers ordinaires,
// l'écriture remplace le lien au lieu de viser sa cible.
func TestLienSymboliqueNeSuitRien(t *testing.T) {
	outside := t.TempDir()
	archive, _ := makeArchive(t,
		symlink("./piege", outside+"/victime"),
		file("./piege", "écrasé"),
	)
	dest := t.TempDir()
	if err := extract(context.Background(), archive, dest); err != nil {
		t.Fatalf("extraction: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "victime")); err == nil {
		t.Error("l'écriture a suivi le lien hors du dossier d'extraction")
	}
}

// TestArchivePubliee installe l'archive réelle du runtime, désignée par
// BORGUI_RUNTIME_ARCHIVE, avec l'empreinte compilée dans le binaire. Elle
// pèse 60 Mo et n'est pas dans le dépôt : le test est ignoré sans elle.
//
//	BORGUI_RUNTIME_ARCHIVE=build/runtime/dist/borgui-runtime-1.4.5-cygwin.1.tar.gz \
//	    go test ./internal/borgruntime -run TestArchivePubliee -v
func TestArchivePubliee(t *testing.T) {
	archive := os.Getenv("BORGUI_RUNTIME_ARCHIVE")
	if archive == "" {
		t.Skip("BORGUI_RUNTIME_ARCHIVE non défini")
	}
	if !filepath.IsAbs(archive) {
		// go test s'exécute dans le dossier du paquet.
		archive = filepath.Join("..", "..", archive)
	}

	manager := NewManager(t.TempDir(), Pinned)
	root, err := manager.InstallFromArchive(context.Background(), archive)
	if err != nil {
		t.Fatalf("installation: %v", err)
	}

	for _, name := range []string{"bin/bash.exe", "bin/cygwin1.dll", "bin/borg"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(name))); err != nil {
			t.Errorf("%s absent: %v", name, err)
		}
	}
	if info, err := os.Stat(filepath.Join(root, "tmp")); err != nil || !info.IsDir() {
		t.Errorf("tmp absent: %v", err)
	}

	// /bin/python3 traverse /etc/alternatives : s'il est écrit correctement,
	// les autres liens le sont aussi.
	got, err := os.ReadFile(filepath.Join(root, "bin", "python3"))
	if err != nil {
		t.Fatalf("lien python3: %v", err)
	}
	if !bytes.Equal(got, cygwinSymlinkContent("/etc/alternatives/python3")) {
		t.Errorf("lien python3: contenu % x", got)
	}

	var files, links int
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		files++
		head := make([]byte, len(cygwinSymlinkMagic))
		if f, err := os.Open(path); err == nil {
			n, _ := f.Read(head)
			f.Close()
			if string(head[:n]) == cygwinSymlinkMagic {
				links++
			}
		}
		return nil
	})
	t.Logf("%d fichiers dont %d liens Cygwin, dans %s", files, links, root)
}
