package history

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

// Le catalogue garde le contenu des sauvegardes, pour la restauration
// (EF-91 à EF-93).
//
// Lister une sauvegarde de plusieurs centaines de milliers de fichiers prend
// du temps et fait transiter beaucoup de métadonnées. Le contenu est donc
// relevé une fois, au premier accès, puis relu ici. Une sauvegarde étant
// immuable, son catalogue n'est jamais invalidé : il ne peut que grandir
// (addendum §5.2).

// Entry est un fichier ou un dossier d'une sauvegarde.
type Entry struct {
	// Path est le chemin tel qu'il figure dans la sauvegarde, sans barre
	// oblique initiale ni finale.
	Path string
	Dir  bool
	// Size est la taille du fichier ; pour un dossier, celle de tout son
	// contenu.
	Size     int64
	Modified time.Time
}

// Name est le dernier élément du chemin.
func (e Entry) Name() string { return path.Base(e.Path) }

// Parent est le dossier qui contient l'entrée, "" à la racine.
func (e Entry) Parent() string { return parentOf(e.Path) }

func parentOf(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}

// Catalog résume le contenu relevé d'une sauvegarde.
type Catalog struct {
	Archive string
	Files   int64
	Size    int64
	Cached  time.Time
}

// Catalog retourne le résumé du catalogue d'une sauvegarde, et false s'il
// n'a pas encore été relevé. archive est l'identifiant de la sauvegarde,
// stable, plutôt que son nom.
func (s *Store) Catalog(ctx context.Context, archive string) (Catalog, bool, error) {
	var c Catalog
	var cached int64
	err := s.db.QueryRowContext(ctx,
		`SELECT archive, files, size, cached FROM catalogs WHERE archive = ?`, archive,
	).Scan(&c.Archive, &c.Files, &c.Size, &cached)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Catalog{}, false, nil
		}
		return Catalog{}, false, fmt.Errorf("catalogue: %w", err)
	}
	c.Cached = time.Unix(cached, 0)
	return c, true, nil
}

// SaveCatalog enregistre le contenu d'une sauvegarde.
//
// Borg ne liste pas les dossiers au-dessus de ce qui a été sauvegardé :
// « home » et « home/marc » manquent quand seul « home/marc/Documents » l'a
// été. Ils sont reconstitués, pour que l'arborescence parte de la racine.
// La taille de chaque dossier est celle de son contenu.
func (s *Store) SaveCatalog(ctx context.Context, archive string, entries []Entry, now time.Time) (Catalog, error) {
	byPath := make(map[string]*Entry, len(entries))
	for i := range entries {
		entry := entries[i]
		entry.Path = strings.Trim(entry.Path, "/")
		if entry.Path == "" {
			continue
		}
		if entry.Dir {
			entry.Size = 0
		}
		byPath[entry.Path] = &entry
	}
	catalog := Catalog{Archive: archive, Cached: now}
	for p, entry := range byPath {
		if !entry.Dir {
			catalog.Files++
			catalog.Size += entry.Size
		}
		for parent := parentOf(p); parent != ""; parent = parentOf(parent) {
			dir, ok := byPath[parent]
			if !ok {
				dir = &Entry{Path: parent, Dir: true}
				byPath[parent] = dir
			}
			if !entry.Dir {
				dir.Size += entry.Size
			}
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Catalog{}, fmt.Errorf("catalogue: %w", err)
	}
	defer tx.Rollback()

	// Un catalogue interrompu en cours d'écriture est repris de zéro.
	if _, err := tx.ExecContext(ctx, `DELETE FROM catalog_entries WHERE archive = ?`, archive); err != nil {
		return Catalog{}, fmt.Errorf("catalogue: %w", err)
	}
	insert, err := tx.PrepareContext(ctx, `INSERT INTO catalog_entries
		(archive, path, parent, name, folded, dir, size, modified) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return Catalog{}, fmt.Errorf("catalogue: %w", err)
	}
	defer insert.Close()
	for p, entry := range byPath {
		var modified int64
		if !entry.Modified.IsZero() {
			modified = entry.Modified.Unix()
		}
		name := path.Base(p)
		if _, err := insert.ExecContext(ctx, archive, p, parentOf(p), name, fold(name),
			entry.Dir, entry.Size, modified); err != nil {
			return Catalog{}, fmt.Errorf("catalogue: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT OR REPLACE INTO catalogs (archive, files, size, cached) VALUES (?, ?, ?, ?)`,
		archive, catalog.Files, catalog.Size, now.Unix()); err != nil {
		return Catalog{}, fmt.Errorf("catalogue: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Catalog{}, fmt.Errorf("catalogue: %w", err)
	}
	return catalog, nil
}

// Children retourne le contenu d'un dossier de la sauvegarde, "" pour la
// racine : les dossiers d'abord, puis par nom.
func (s *Store) Children(ctx context.Context, archive, dir string) ([]Entry, error) {
	return s.entries(ctx, `SELECT path, dir, size, modified FROM catalog_entries
		WHERE archive = ? AND parent = ?`, archive, strings.Trim(dir, "/"))
}

// Search retourne au plus limit entrées dont le nom contient term, sans
// tenir compte de la casse (EF-92).
func (s *Store) Search(ctx context.Context, archive, term string, limit int) ([]Entry, error) {
	term = fold(strings.TrimSpace(term))
	if term == "" {
		return nil, nil
	}
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(term)
	return s.entries(ctx, `SELECT path, dir, size, modified FROM catalog_entries
		WHERE archive = ? AND folded LIKE ? ESCAPE '\' LIMIT ?`, archive, "%"+escaped+"%", limit)
}

// entries exécute une requête d'entrées et les trie : dossiers d'abord, puis
// par nom.
func (s *Store) entries(ctx context.Context, query string, args ...any) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("catalogue: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		var modified int64
		if err := rows.Scan(&e.Path, &e.Dir, &e.Size, &modified); err != nil {
			return nil, fmt.Errorf("catalogue: %w", err)
		}
		if modified != 0 {
			e.Modified = time.Unix(modified, 0)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalogue: %w", err)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Dir != entries[j].Dir {
			return entries[i].Dir
		}
		return fold(entries[i].Name()) < fold(entries[j].Name())
	})
	return entries, nil
}

// fold prépare un nom pour la recherche : sans casse, lettres accentuées
// comprises, ce que LIKE de SQLite ne sait faire que pour l'ASCII.
func fold(name string) string { return strings.ToLower(name) }
