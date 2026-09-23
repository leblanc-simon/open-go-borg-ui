package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
)

// commandGenerate crée le jeu de données de recette dans un dossier neuf.
func commandGenerate(t translate, args []string) (int, error) {
	var sizeMiB int
	flags := flag.NewFlagSet("generate", flag.ContinueOnError)
	flags.IntVar(&sizeMiB, "size", 64, "taille du gros fichier, en Mio")
	positional, err := parse(flags, args)
	if err != nil || len(positional) != 1 || sizeMiB < 0 {
		return exitError, usageError{}
	}

	root, err := filepath.Abs(positional[0])
	if err != nil {
		return exitError, err
	}
	if entries, err := os.ReadDir(root); err == nil && len(entries) > 0 {
		return exitError, fmt.Errorf("%s", t("recette.not_empty", map[string]any{"Path": root}))
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return exitError, err
	}

	count, err := generate(root, int64(sizeMiB)<<20)
	if err != nil {
		return exitError, err
	}
	fmt.Println(t("recette.generated", map[string]any{"Count": count, "Path": root}))
	return exitSuccess, nil
}

// datasetFile décrit un fichier du jeu de recette.
type datasetFile struct {
	// path est relatif à la racine, en barres obliques.
	path string
	// size est la taille du contenu pseudo-aléatoire ; content, s'il est
	// renseigné, la remplace.
	size    int64
	content string
	// seed rend le contenu reproductible : deux fichiers de même graine sont
	// identiques, ce qui exerce la déduplication.
	seed uint64
}

// longPath construit un chemin relatif de plus de 260 caractères, la limite
// historique de Windows (MAX_PATH) que le runtime Cygwin doit franchir.
func longPath() string {
	segment := "dossier-profondément-imbriqué-pour-dépasser-max-path"
	var parts []string
	for length := 0; length < 300; length += len(segment) + 1 {
		parts = append(parts, segment)
	}
	return "chemins-longs/" + strings.Join(parts, "/") + "/fichier au bout du chemin.txt"
}

// dataset énumère les cas que la recette doit traverser (addendum §6.4) :
// accents, espaces, Unicode sous ses deux formes de normalisation, chemins de
// plus de 260 caractères, fichiers vides, doublons et un gros fichier
// incompressible. Les noms évitent les caractères interdits sous Windows.
func dataset(bigSize int64) []datasetFile {
	return []datasetFile{
		{path: "accents/Été à Noël — café crème.txt", content: "Données accentuées : àâäéèêëîïôöùûüÿç ÀÉÈ œ Œ æ\n"},
		{path: "accents/Réunion d'équipe/compte-rendu n°3.txt", content: "Compte-rendu.\n"},
		{path: "espaces/nom  avec  espaces doubles.txt", content: "espaces\n"},
		{path: "espaces/dossier avec espaces/fichier.txt", content: "espaces\n"},
		{path: "unicode/日本語のファイル.txt", content: "こんにちは\n"},
		{path: "unicode/Ελληνικά και кириллица.txt", content: "αβγ абв\n"},
		{path: "unicode/emoji 🎉 fête.txt", content: "🎉\n"},
		// « é » décomposé (e + accent combinant) : macOS et certains outils
		// écrivent cette forme, Windows la conserve telle quelle.
		{path: "unicode/nfd-été.txt", content: "forme décomposée\n"},
		{path: "unicode/nfc-été.txt", content: "forme composée\n"},
		{path: "symboles/prix 100% (TTC) [v2] {final} #1 & co; ok=oui.txt", content: "symboles\n"},
		{path: "symboles/.fichier-caché", content: "caché\n"},
		{path: "symboles/sans-extension", content: "sans extension\n"},
		{path: longPath(), content: "Ce fichier est au bout d'un chemin de plus de 260 caractères.\n"},
		{path: "vides/fichier-vide.txt", content: ""},
		{path: "binaires/aléatoire-1.bin", size: 1 << 20, seed: 1},
		{path: "binaires/aléatoire-2.bin", size: 3<<20 + 17, seed: 2},
		{path: "binaires/copie-de-aléatoire-1.bin", size: 1 << 20, seed: 1},
		{path: "binaires/gros-fichier.bin", size: bigSize, seed: 3},
	}
}

// emptyDirs sont les dossiers vides du jeu : Borg les conserve, la restauration
// doit les rendre.
var emptyDirs = []string{"vides/dossier-vide", "vides/dossier vide avec espaces"}

// generate écrit le jeu de données et retourne le nombre de fichiers créés.
func generate(root string, bigSize int64) (int, error) {
	files := dataset(bigSize)
	for _, file := range files {
		target := filepath.Join(root, filepath.FromSlash(file.path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return 0, err
		}
		if err := writeDatasetFile(target, file); err != nil {
			return 0, fmt.Errorf("%s: %w", file.path, err)
		}
	}
	for _, dir := range emptyDirs {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			return 0, err
		}
	}
	return len(files), nil
}

// writeDatasetFile écrit un fichier du jeu.
func writeDatasetFile(target string, file datasetFile) error {
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	var content io.Reader = strings.NewReader(file.content)
	if file.content == "" && file.size > 0 {
		// ChaCha8 produit un flux incompressible et reproductible : la
		// compression de Borg n'y gagne rien, ce qui rend les mesures de
		// durée et de débit représentatives du pire cas.
		var seed [32]byte
		seed[0] = byte(file.seed)
		content = io.LimitReader(rand.NewChaCha8(seed), file.size)
	}
	if _, err := io.Copy(out, content); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
