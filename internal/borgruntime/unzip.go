package borgruntime

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"context"
)

// unzip extrait l'archive dans dest.
//
// Chaque entrée est vérifiée avant écriture : une archive dont un nom
// remonterait hors du dossier de destination est rejetée. L'archive est certes
// vérifiée par empreinte, mais la vérification ne dit rien de son contenu.
func unzip(ctx context.Context, archive, dest string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("runtime: ouverture de l'archive: %w", err)
	}
	defer reader.Close()

	for _, entry := range reader.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := extractEntry(entry, dest); err != nil {
			return err
		}
	}
	return nil
}

// extractEntry écrit une entrée de l'archive.
func extractEntry(entry *zip.File, dest string) error {
	target, err := safeJoin(dest, entry.Name)
	if err != nil {
		return err
	}

	info := entry.FileInfo()
	switch {
	case info.IsDir():
		return os.MkdirAll(target, 0o700)

	case info.Mode()&os.ModeSymlink != 0:
		return extractSymlink(entry, target)

	default:
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return fmt.Errorf("runtime: création de %s: %w", filepath.Dir(target), err)
		}
		source, err := entry.Open()
		if err != nil {
			return fmt.Errorf("runtime: lecture de %s: %w", entry.Name, err)
		}
		defer source.Close()

		// Les permissions de l'archive sont conservées : le bit d'exécution
		// distingue les programmes du runtime de ses fichiers de données.
		file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			return fmt.Errorf("runtime: écriture de %s: %w", target, err)
		}
		if _, err := io.Copy(file, source); err != nil {
			file.Close()
			return fmt.Errorf("runtime: écriture de %s: %w", target, err)
		}
		return file.Close()
	}
}

// extractSymlink recrée un lien symbolique. Le runtime Cygwin en contient
// plusieurs, dont les alias de bibliothèques.
func extractSymlink(entry *zip.File, target string) error {
	source, err := entry.Open()
	if err != nil {
		return fmt.Errorf("runtime: lecture de %s: %w", entry.Name, err)
	}
	defer source.Close()

	destination, err := io.ReadAll(io.LimitReader(source, 4096))
	if err != nil {
		return fmt.Errorf("runtime: lecture de %s: %w", entry.Name, err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return fmt.Errorf("runtime: création de %s: %w", filepath.Dir(target), err)
	}
	if err := os.Symlink(string(destination), target); err != nil {
		return fmt.Errorf("runtime: lien %s: %w", entry.Name, err)
	}
	return nil
}

// safeJoin construit le chemin de destination et refuse toute sortie du
// dossier d'extraction.
func safeJoin(dest, name string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("runtime: entrée d'archive hors du dossier d'extraction: %s", name)
	}
	target := filepath.Join(dest, cleaned)
	if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
		return "", fmt.Errorf("runtime: entrée d'archive hors du dossier d'extraction: %s", name)
	}
	return target, nil
}
