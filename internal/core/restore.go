package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/history"
)

// ErrDestinationNotEmpty signale un dossier de restauration déjà occupé : une
// restauration dans un dossier neuf ne mélange jamais ses fichiers à
// d'autres (EF-95).
var ErrDestinationNotEmpty = errors.New("core: le dossier de restauration n'est pas vide")

// OpenCatalog retourne le catalogue d'une sauvegarde, en le relevant auprès
// de Borg au premier accès seulement : une sauvegarde est immuable, son
// catalogue ne se périme jamais (EF-93).
func OpenCatalog(ctx context.Context, runner borg.Runner, env borg.Environment, store *history.Store, archive borg.Archive, now time.Time) (history.Catalog, error) {
	if catalog, ok, err := store.Catalog(ctx, archive.ID); err != nil || ok {
		return catalog, err
	}
	items, _, err := borg.ListContents(ctx, runner, env, archive.Name)
	if err != nil {
		return history.Catalog{}, err
	}
	entries := make([]history.Entry, 0, len(items))
	for _, item := range items {
		entries = append(entries, history.Entry{
			Path:     item.Path,
			Dir:      item.Dir(),
			Size:     item.Size,
			Modified: item.Mtime.Time,
		})
	}
	return store.SaveCatalog(ctx, archive.ID, entries, now)
}

// PrepareDestination crée le dossier de restauration, ou vérifie qu'il est
// vide.
func PrepareDestination(path string) error {
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.MkdirAll(path, 0o700)
	}
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("%w: %s", ErrDestinationNotEmpty, path)
	}
	return nil
}

// Overwritten retourne, en chemins du poste, ce qu'une restauration à
// l'emplacement d'origine écraserait : les éléments choisis qui y existent
// encore. C'est ce que la confirmation énonce (EF-96).
func Overwritten(runner borg.Runner, paths []string) ([]string, error) {
	var existing []string
	for _, path := range paths {
		native, _, err := runner.Origin(path)
		if err != nil {
			return nil, err
		}
		if _, err := os.Lstat(native); err == nil {
			existing = append(existing, native)
		}
	}
	return existing, nil
}

// RestoreRequest décrit une restauration.
type RestoreRequest struct {
	Env     borg.Environment
	Archive string
	// Paths sont les chemins choisis, tels qu'ils figurent dans la
	// sauvegarde. Vide, tout est restauré (EF-94).
	Paths []string
	// Destination est le dossier neuf où restaurer (EF-95). Ignorée à
	// l'emplacement d'origine.
	Destination string
	// InPlace rend chaque élément à son emplacement d'origine, en écrasant
	// ce qui s'y trouve (EF-96).
	InPlace bool
	OnEvent func(borg.Event)
}

// Restore restaure une sauvegarde, en tout ou partie.
func Restore(ctx context.Context, runner borg.Runner, req RestoreRequest) (*borg.Result, error) {
	if req.InPlace && len(req.Paths) == 0 {
		// Rendre toute une sauvegarde à ses emplacements écraserait d'un
		// coup tout ce qui a changé depuis : l'interface ne le propose pas.
		return nil, errors.New("core: une restauration à l'emplacement d'origine désigne ses éléments")
	}
	if !req.InPlace {
		if err := PrepareDestination(req.Destination); err != nil {
			return nil, err
		}
	}
	return borg.Extract(ctx, runner, borg.ExtractOptions{
		Env:         req.Env,
		Archive:     req.Archive,
		Paths:       req.Paths,
		Destination: req.Destination,
		InPlace:     req.InPlace,
		OnEvent:     req.OnEvent,
	})
}
