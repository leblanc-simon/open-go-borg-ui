// Package history conserve l'historique local des exécutions (HistoryStore).
//
// Chaque sauvegarde y est inscrite à son lancement, puis complétée à son
// terme. Une exécution restée « en cours » après la fin du processus révèle
// donc une interruption brutale — extinction, plantage — que rien d'autre ne
// laisserait voir.
//
// La base est en SQLite pur Go (modernc.org/sqlite) : aucune dépendance à CGO,
// ce qui garde cette couche testable et compilable partout. L'interface et la
// sauvegarde planifiée peuvent y écrire en même temps : le journal WAL et un
// délai d'attente sur verrou absorbent ces accès concurrents.
package history

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Status est l'issue d'une exécution, telle qu'elle est enregistrée.
type Status string

const (
	// StatusRunning : l'exécution a commencé et ne s'est pas encore
	// terminée — ou a été interrompue sans pouvoir le consigner.
	StatusRunning Status = "running"
	// StatusSuccess : terminée sans avertissement.
	StatusSuccess Status = "success"
	// StatusWarning : terminée avec des avertissements ; la sauvegarde est
	// exploitable (EF-56).
	StatusWarning Status = "warning"
	// StatusError : échec.
	StatusError Status = "error"
	// StatusCancelled : arrêtée à la demande de l'utilisateur.
	StatusCancelled Status = "cancelled"
)

// TranslationKey retourne la clé de traduction du statut.
func (s Status) TranslationKey() string { return "status." + string(s) }

// Run est une exécution.
type Run struct {
	ID       int64
	Profile  string
	Started  time.Time
	Finished time.Time // zéro tant que l'exécution est en cours
	Status   Status

	// Archive est le nom de la sauvegarde créée, s'il y en a une.
	Archive string
	// Files et les deux volumes reprennent les statistiques de Borg.
	Files            int64
	OriginalSize     int64
	DeduplicatedSize int64
	// RepositorySize est l'espace occupé sur la destination après
	// l'exécution, 0 s'il n'a pas pu être relevé (EF-81).
	RepositorySize int64
	// Warnings compte les fichiers que Borg n'a pas pu lire.
	Warnings int
	// CloudSkipped compte les fichiers à la demande écartés.
	CloudSkipped int
	// ErrorKey est la clé de traduction du diagnostic d'échec, jamais un
	// libellé : l'historique se relit dans la langue du moment.
	ErrorKey string
	// Detail conserve le message brut de l'échec, pour le dépliant
	// « Détails » (EI-04).
	Detail string
}

// Duration retourne la durée de l'exécution, nulle tant qu'elle est en cours.
func (r Run) Duration() time.Duration {
	if r.Finished.IsZero() {
		return 0
	}
	return r.Finished.Sub(r.Started)
}

// Store est l'historique d'un poste.
type Store struct {
	db *sql.DB
}

// Open ouvre la base, la crée au besoin et la met au schéma courant.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("historique: %w", err)
	}
	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("historique: %w", err)
	}
	// Une seule connexion : SQLite n'écrit qu'à un endroit à la fois, et un
	// pool ne ferait que multiplier les attentes sur verrou.
	db.SetMaxOpenConns(1)

	store := &Store{db: db}
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

// Close ferme la base.
func (s *Store) Close() error { return s.db.Close() }

// migrations sont appliquées dans l'ordre ; user_version retient la dernière
// appliquée. Une migration publiée n'est jamais modifiée : on en ajoute une.
var migrations = []string{
	`CREATE TABLE runs (
		id                INTEGER PRIMARY KEY,
		profile           TEXT    NOT NULL,
		started           INTEGER NOT NULL,
		finished          INTEGER,
		status            TEXT    NOT NULL,
		archive           TEXT    NOT NULL DEFAULT '',
		files             INTEGER NOT NULL DEFAULT 0,
		original_size     INTEGER NOT NULL DEFAULT 0,
		deduplicated_size INTEGER NOT NULL DEFAULT 0,
		warnings          INTEGER NOT NULL DEFAULT 0,
		cloud_skipped     INTEGER NOT NULL DEFAULT 0,
		error_key         TEXT    NOT NULL DEFAULT '',
		detail            TEXT    NOT NULL DEFAULT ''
	);
	CREATE INDEX runs_profile_started ON runs (profile, started DESC);`,
	`ALTER TABLE runs ADD COLUMN repository_size INTEGER NOT NULL DEFAULT 0;`,
	`CREATE TABLE catalogs (
		archive TEXT    PRIMARY KEY,
		files   INTEGER NOT NULL,
		size    INTEGER NOT NULL,
		cached  INTEGER NOT NULL
	);
	CREATE TABLE catalog_entries (
		archive  TEXT    NOT NULL,
		path     TEXT    NOT NULL,
		parent   TEXT    NOT NULL,
		name     TEXT    NOT NULL,
		folded   TEXT    NOT NULL,
		dir      INTEGER NOT NULL,
		size     INTEGER NOT NULL,
		modified INTEGER NOT NULL,
		PRIMARY KEY (archive, path)
	) WITHOUT ROWID;
	CREATE INDEX catalog_entries_parent ON catalog_entries (archive, parent);`,
	`CREATE TABLE restore_checks (
		id      INTEGER PRIMARY KEY,
		profile TEXT    NOT NULL,
		checked INTEGER NOT NULL,
		archive TEXT    NOT NULL,
		path    TEXT    NOT NULL,
		native  TEXT    NOT NULL,
		outcome TEXT    NOT NULL,
		reason  TEXT    NOT NULL DEFAULT '',
		detail  TEXT    NOT NULL DEFAULT ''
	);
	CREATE INDEX restore_checks_profile_checked ON restore_checks (profile, checked DESC);`,
}

// migrate met la base au schéma courant.
func (s *Store) migrate(ctx context.Context) error {
	// La version est relue et les migrations appliquées dans une seule
	// transaction exclusive : l'interface et une sauvegarde planifiée qui
	// ouvrent une base neuve au même instant ne migrent pas chacune de leur
	// côté. La seconde attend la première (busy_timeout), puis trouve la base
	// à jour.
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("historique: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("historique: verrouillage pour migration: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		}
	}()

	var version int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("historique: lecture de la version: %w", err)
	}
	if version > len(migrations) {
		// Une version plus récente de l'application a écrit cette base :
		// la relire avec un schéma ancien risquerait de l'abîmer.
		return fmt.Errorf("historique: base de version %d, plus récente que l'application (%d)", version, len(migrations))
	}
	for i := version; i < len(migrations); i++ {
		if _, err := conn.ExecContext(ctx, migrations[i]); err != nil {
			return fmt.Errorf("historique: migration %d: %w", i+1, err)
		}
	}
	if version < len(migrations) {
		// PRAGMA n'accepte pas de paramètre lié ; la valeur est un entier
		// calculé ici, jamais une donnée extérieure.
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", len(migrations))); err != nil {
			return fmt.Errorf("historique: migration: %w", err)
		}
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("historique: migration: %w", err)
	}
	committed = true
	return nil
}

// Begin inscrit le lancement d'une exécution et retourne son identifiant.
func (s *Store) Begin(ctx context.Context, profile string, started time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO runs (profile, started, status) VALUES (?, ?, ?)`,
		profile, started.UnixMilli(), StatusRunning)
	if err != nil {
		return 0, fmt.Errorf("historique: %w", err)
	}
	return result.LastInsertId()
}

// Finish complète une exécution.
//
// Le contexte d'une exécution annulée est lui-même annulé : l'appelant passe
// ici un contexte indépendant, faute de quoi l'annulation ne pourrait jamais
// être consignée.
func (s *Store) Finish(ctx context.Context, run Run) error {
	if run.ID == 0 {
		return errors.New("historique: exécution sans identifiant")
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE runs SET finished = ?, status = ?, archive = ?, files = ?,
			original_size = ?, deduplicated_size = ?, repository_size = ?,
			warnings = ?, cloud_skipped = ?, error_key = ?, detail = ?
		 WHERE id = ?`,
		run.Finished.UnixMilli(), run.Status, run.Archive, run.Files,
		run.OriginalSize, run.DeduplicatedSize, run.RepositorySize,
		run.Warnings, run.CloudSkipped, run.ErrorKey, run.Detail, run.ID)
	if err != nil {
		return fmt.Errorf("historique: %w", err)
	}
	return nil
}

// Recent retourne les dernières exécutions d'un profil, de la plus récente à
// la plus ancienne.
func (s *Store) Recent(ctx context.Context, profile string, limit int) ([]Run, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, profile, started, finished, status, archive, files,
			original_size, deduplicated_size, repository_size, warnings,
			cloud_skipped, error_key, detail
		 FROM runs WHERE profile = ? ORDER BY started DESC, id DESC LIMIT ?`,
		profile, limit)
	if err != nil {
		return nil, fmt.Errorf("historique: %w", err)
	}
	defer rows.Close()

	var runs []Run
	for rows.Next() {
		var (
			run      Run
			started  int64
			finished sql.NullInt64
		)
		if err := rows.Scan(&run.ID, &run.Profile, &started, &finished, &run.Status,
			&run.Archive, &run.Files, &run.OriginalSize, &run.DeduplicatedSize,
			&run.RepositorySize, &run.Warnings, &run.CloudSkipped, &run.ErrorKey, &run.Detail); err != nil {
			return nil, fmt.Errorf("historique: %w", err)
		}
		run.Started = time.UnixMilli(started)
		if finished.Valid {
			run.Finished = time.UnixMilli(finished.Int64)
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

// LastSuccess retourne le lancement de la dernière exécution exploitable du
// profil — réussie ou terminée avec des avertissements —, zéro s'il n'y en a
// aucune.
func (s *Store) LastSuccess(ctx context.Context, profile string) (time.Time, error) {
	var started sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT MAX(finished) FROM runs WHERE profile = ? AND status IN (?, ?)`,
		profile, StatusSuccess, StatusWarning).Scan(&started)
	if err != nil {
		return time.Time{}, fmt.Errorf("historique: %w", err)
	}
	if !started.Valid {
		return time.Time{}, nil
	}
	return time.UnixMilli(started.Int64), nil
}
