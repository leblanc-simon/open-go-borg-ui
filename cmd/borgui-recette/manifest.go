package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// entry est une ligne du manifeste.
//
// Le format est du texte tabulé, une entrée par ligne, triée par chemin :
//
//	<type>	<taille>	<sha256>	<chemin>
//
// type vaut f (fichier), d (dossier) ou l (lien, la cible tenant lieu
// d'empreinte). Le chemin est relatif à la racine, en barres obliques et sans
// aucune normalisation Unicode : la forme exacte des noms fait partie de ce
// que la restauration doit préserver.
type entry struct {
	kind   string
	size   int64
	digest string
	path   string
}

// String rend la ligne du manifeste.
func (e entry) String() string {
	return e.kind + "\t" + strconv.FormatInt(e.size, 10) + "\t" + e.digest + "\t" + e.path
}

// commandManifest établit le manifeste d'un dossier.
func commandManifest(t translate, args []string) (int, error) {
	var output string
	flags := flag.NewFlagSet("manifest", flag.ContinueOnError)
	flags.StringVar(&output, "o", "", "fichier où écrire le manifeste")
	positional, err := parse(flags, args)
	if err != nil || len(positional) != 1 {
		return exitError, usageError{}
	}

	entries, err := scan(t, positional[0])
	if err != nil {
		return exitError, err
	}

	var out io.Writer = os.Stdout
	if output != "" {
		file, err := os.Create(output)
		if err != nil {
			return exitError, err
		}
		defer file.Close()
		out = file
	}
	writer := bufio.NewWriter(out)
	for _, e := range entries {
		fmt.Fprintln(writer, e.String())
	}
	if err := writer.Flush(); err != nil {
		return exitError, err
	}
	if output != "" {
		fmt.Fprintln(os.Stderr, t("recette.manifest_written", map[string]any{
			"Count": len(entries), "Path": output,
		}))
	}
	return exitSuccess, nil
}

// scan parcourt un dossier et retourne ses entrées triées.
func scan(t translate, dir string) ([]entry, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s", t("recette.not_a_directory", map[string]any{"Path": root}))
	}

	var entries []entry
	var hashed int64
	lastReport := time.Now()
	reported := false

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)

		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			entries = append(entries, entry{kind: "l", digest: filepath.ToSlash(target), path: relative})
		case d.IsDir():
			entries = append(entries, entry{kind: "d", digest: "-", path: relative})
		case d.Type().IsRegular():
			size, digest, err := hashFile(path)
			if err != nil {
				return err
			}
			entries = append(entries, entry{kind: "f", size: size, digest: digest, path: relative})
			hashed += size
			if time.Since(lastReport) > time.Second {
				lastReport = time.Now()
				reported = true
				fmt.Fprint(os.Stderr, "\r"+t("recette.scanning", map[string]any{
					"Count": len(entries), "Size": hashed >> 20,
				}))
			}
		}
		return nil
	})
	if reported {
		fmt.Fprint(os.Stderr, "\r\033[K")
	}
	if err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	return entries, nil
}

// hashFile calcule l'empreinte SHA-256 d'un fichier.
func hashFile(path string) (int64, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer file.Close()
	digest := sha256.New()
	size, err := io.Copy(digest, file)
	if err != nil {
		return 0, "", err
	}
	return size, hex.EncodeToString(digest.Sum(nil)), nil
}

// readManifest relit un manifeste.
func readManifest(t translate, path string) ([]entry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entries []entry
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSuffix(scanner.Text(), "\r")
		if text == "" {
			continue
		}
		fields := strings.SplitN(text, "\t", 4)
		if len(fields) != 4 {
			return nil, fmt.Errorf("%s", t("recette.malformed", map[string]any{"Path": path, "Line": line}))
		}
		size, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s", t("recette.malformed", map[string]any{"Path": path, "Line": line}))
		}
		entries = append(entries, entry{kind: fields[0], size: size, digest: fields[2], path: fields[3]})
	}
	return entries, scanner.Err()
}
