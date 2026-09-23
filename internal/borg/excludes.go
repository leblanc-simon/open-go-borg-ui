package borg

import (
	"bufio"
	"fmt"
	"os"
)

// withExcludeFile inscrit les chemins à écarter dans un fichier d'exclusions
// et ajoute à la commande l'option qui le désigne.
//
// Un fichier plutôt que des options --exclude : un dossier synchronisé peut
// compter des dizaines de milliers de fichiers à la demande, bien au-delà de
// ce qu'une ligne de commande accepte.
//
// archivePath donne la forme que prend un chemin natif dans l'archive, à
// laquelle Borg confronte les motifs ; servicePath la forme sous laquelle Borg
// ouvre le fichier lui-même. Les deux sont propres au Runner (AR-02). La
// fonction de nettoyage retournée est toujours appelable.
func withExcludeFile(cmd Command, archivePath func(string) (string, error), servicePath func(string) string) (Command, func(), error) {
	noop := func() {}
	if len(cmd.ExcludePaths) == 0 {
		return cmd, noop, nil
	}

	patterns := make([]string, 0, len(cmd.ExcludePaths))
	for _, path := range cmd.ExcludePaths {
		archived, err := archivePath(path)
		if err != nil {
			return cmd, noop, err
		}
		// « pp: » compare un préfixe de chemin, composante par composante et
		// sans aucun caractère spécial : un nom contenant * ou [ est pris à
		// la lettre, et « a.txt » n'écarte pas « a.txt.bak ».
		patterns = append(patterns, "pp:"+archived)
	}

	file, err := os.CreateTemp("", "borgui-exclusions-*.txt")
	if err != nil {
		return cmd, noop, fmt.Errorf("borg: fichier d'exclusions: %w", err)
	}
	cleanup := func() { os.Remove(file.Name()) }

	writer := bufio.NewWriter(file)
	for _, pattern := range patterns {
		writer.WriteString(pattern)
		writer.WriteByte('\n')
	}
	if err := writer.Flush(); err != nil {
		file.Close()
		cleanup()
		return cmd, noop, fmt.Errorf("borg: fichier d'exclusions: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return cmd, noop, fmt.Errorf("borg: fichier d'exclusions: %w", err)
	}

	flags := make([]string, 0, len(cmd.Flags)+2)
	flags = append(flags, cmd.Flags...)
	flags = append(flags, "--exclude-from", servicePath(file.Name()))
	cmd.Flags = flags
	cmd.ExcludePaths = nil
	return cmd, cleanup, nil
}
