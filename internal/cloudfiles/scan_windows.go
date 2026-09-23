package cloudfiles

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"syscall"
)

// scan parcourt les sources à la recherche des fichiers à la demande.
func scan(ctx context.Context, sources []string) ([]string, error) {
	var found []string
	for _, source := range sources {
		err := filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				// Un dossier illisible n'arrête pas le repérage : Borg le
				// signalera lui-même, en avertissement.
				if errors.Is(err, fs.ErrPermission) {
					return fs.SkipDir
				}
				return err
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if d.IsDir() || !d.Type().IsRegular() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if data, ok := info.Sys().(*syscall.Win32FileAttributeData); ok && isPlaceholder(data.FileAttributes) {
				found = append(found, path)
			}
			return nil
		})
		if err != nil {
			return found, err
		}
	}
	return found, nil
}
