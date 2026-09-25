package history

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Les vérifications de restauration prouvent qu'une sauvegarde se relit :
// une fois par mois, un fichier tiré au hasard est extrait puis comparé à
// l'original (EF-99). C'est le seul élément qui distingue une sauvegarde
// présumée fonctionnelle d'une sauvegarde vérifiée (addendum §5.4).

// Outcome est l'issue d'une vérification de restauration.
type Outcome string

const (
	// OutcomeIdentical : le fichier extrait est identique à l'original.
	OutcomeIdentical Outcome = "identical"
	// OutcomeExtracted : le fichier a été extrait, mais l'original a disparu
	// ou changé depuis la sauvegarde ; il n'y avait rien à comparer.
	OutcomeExtracted Outcome = "extracted"
	// OutcomeFailed : l'extraction a échoué, ou son contenu diffère d'un
	// original resté inchangé.
	OutcomeFailed Outcome = "failed"
)

// RestoreCheck est une vérification de restauration.
type RestoreCheck struct {
	ID      int64
	Profile string
	Checked time.Time
	// Archive est le nom de la sauvegarde vérifiée.
	Archive string
	// Path est le fichier tiré, tel qu'il figure dans la sauvegarde ;
	// Native, le même sur le poste.
	Path    string
	Native  string
	Outcome Outcome
	// Reason est la clé de traduction de la phrase qui explique l'issue :
	// identique, original absent ou modifié, contenu différent. Jamais un
	// libellé : la vérification se relit dans la langue du moment.
	Reason string
	// Detail garde le message brut d'un échec, pour le dépliant « Détails ».
	Detail string
}

// AddRestoreCheck consigne une vérification.
func (s *Store) AddRestoreCheck(ctx context.Context, check RestoreCheck) (int64, error) {
	result, err := s.db.ExecContext(ctx, `INSERT INTO restore_checks
		(profile, checked, archive, path, native, outcome, reason, detail) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		check.Profile, check.Checked.Unix(), check.Archive, check.Path, check.Native,
		string(check.Outcome), check.Reason, check.Detail)
	if err != nil {
		return 0, fmt.Errorf("historique: vérification: %w", err)
	}
	return result.LastInsertId()
}

// LastRestoreCheck retourne la dernière vérification du profil, et false
// s'il n'y en a encore aucune.
func (s *Store) LastRestoreCheck(ctx context.Context, profile string) (RestoreCheck, bool, error) {
	var check RestoreCheck
	var checked int64
	var outcome string
	err := s.db.QueryRowContext(ctx, `SELECT id, profile, checked, archive, path, native, outcome, reason, detail
		FROM restore_checks WHERE profile = ? ORDER BY checked DESC, id DESC LIMIT 1`, profile,
	).Scan(&check.ID, &check.Profile, &checked, &check.Archive, &check.Path, &check.Native, &outcome, &check.Reason, &check.Detail)
	if errors.Is(err, sql.ErrNoRows) {
		return RestoreCheck{}, false, nil
	}
	if err != nil {
		return RestoreCheck{}, false, fmt.Errorf("historique: vérification: %w", err)
	}
	check.Checked = time.Unix(checked, 0)
	check.Outcome = Outcome(outcome)
	return check, true, nil
}

// RandomFile tire au hasard un fichier non vide d'une sauvegarde, d'au plus
// maxSize octets : la vérification ne doit pas transférer des gigaoctets.
// Il retourne false quand la sauvegarde n'en contient aucun.
func (s *Store) RandomFile(ctx context.Context, archive string, maxSize int64) (Entry, bool, error) {
	var e Entry
	var modified int64
	err := s.db.QueryRowContext(ctx, `SELECT path, dir, size, modified FROM catalog_entries
		WHERE archive = ? AND dir = 0 AND size > 0 AND size <= ? ORDER BY random() LIMIT 1`,
		archive, maxSize,
	).Scan(&e.Path, &e.Dir, &e.Size, &modified)
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, false, nil
	}
	if err != nil {
		return Entry{}, false, fmt.Errorf("catalogue: %w", err)
	}
	if modified != 0 {
		e.Modified = time.Unix(modified, 0)
	}
	return e, true, nil
}

// RepositoryCheck est un contrôle d'intégrité de la destination (EF-87).
type RepositoryCheck struct {
	ID      int64
	Profile string
	Checked time.Time
	// Healthy : aucune anomalie trouvée.
	Healthy  bool
	Duration time.Duration
	// Detail rassemble ce que Borg a signalé, pour le dépliant « Détails ».
	Detail string
}

// AddRepositoryCheck consigne un contrôle de la destination.
func (s *Store) AddRepositoryCheck(ctx context.Context, check RepositoryCheck) (int64, error) {
	result, err := s.db.ExecContext(ctx, `INSERT INTO repository_checks
		(profile, checked, healthy, duration, detail) VALUES (?, ?, ?, ?, ?)`,
		check.Profile, check.Checked.Unix(), check.Healthy, int64(check.Duration/time.Second), check.Detail)
	if err != nil {
		return 0, fmt.Errorf("historique: contrôle: %w", err)
	}
	return result.LastInsertId()
}

// LastRepositoryCheck retourne le dernier contrôle de la destination du
// profil, et false s'il n'y en a encore aucun.
func (s *Store) LastRepositoryCheck(ctx context.Context, profile string) (RepositoryCheck, bool, error) {
	var check RepositoryCheck
	var checked, seconds int64
	err := s.db.QueryRowContext(ctx, `SELECT id, profile, checked, healthy, duration, detail
		FROM repository_checks WHERE profile = ? ORDER BY checked DESC, id DESC LIMIT 1`, profile,
	).Scan(&check.ID, &check.Profile, &checked, &check.Healthy, &seconds, &check.Detail)
	if errors.Is(err, sql.ErrNoRows) {
		return RepositoryCheck{}, false, nil
	}
	if err != nil {
		return RepositoryCheck{}, false, fmt.Errorf("historique: contrôle: %w", err)
	}
	check.Checked = time.Unix(checked, 0)
	check.Duration = time.Duration(seconds) * time.Second
	return check, true, nil
}
