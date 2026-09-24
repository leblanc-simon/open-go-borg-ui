package core

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/history"
	"leblanc.io/open-go-borg-ui/internal/lock"
)

// VerifyInterval est l'intervalle entre deux vérifications de restauration
// (EF-99).
const VerifyInterval = 30 * 24 * time.Hour

// VerifyMaxSize borne la taille du fichier tiré : la vérification prouve que
// la sauvegarde se relit, elle ne doit pas transférer des gigaoctets.
const VerifyMaxSize = 64 << 20

// ErrNothingToVerify signale une sauvegarde sans aucun fichier à tirer. Rien
// n'est consigné : la vérification sera retentée après la sauvegarde
// suivante.
var ErrNothingToVerify = errors.New("core: aucun fichier à vérifier dans la sauvegarde")

// VerifyDue indique qu'une vérification est due : jamais faite, ou faite il
// y a plus d'un mois.
func VerifyDue(ctx context.Context, store *history.Store, profile string, now time.Time) (bool, error) {
	last, ok, err := store.LastRestoreCheck(ctx, profile)
	if err != nil || !ok {
		return err == nil, err
	}
	return now.Sub(last.Checked) >= VerifyInterval, nil
}

// VerifyRestore tire un fichier au hasard dans une sauvegarde, l'extrait
// dans un dossier temporaire et le compare à l'original, puis consigne
// l'issue (EF-99).
//
// La comparaison n'a de sens que si l'original n'a pas bougé depuis la
// sauvegarde : même taille, même date de modification. Sinon — ou s'il a
// disparu —, l'extraction réussie est seule consignée. Un contenu différent
// d'un original inchangé est un échec : la sauvegarde ne rend pas ce qu'elle
// a reçu.
//
// L'erreur retournée signale ce qui a empêché la vérification elle-même ;
// une vérification menée à son terme, même en échec, est consignée et
// retournée sans erreur.
func VerifyRestore(ctx context.Context, runner borg.Runner, env borg.Environment, store *history.Store,
	profile string, archive borg.Archive, now time.Time) (history.RestoreCheck, error) {
	if _, err := OpenCatalog(ctx, runner, env, store, archive, now); err != nil {
		return history.RestoreCheck{}, err
	}
	entry, ok, err := store.RandomFile(ctx, archive.ID, VerifyMaxSize)
	if err != nil {
		return history.RestoreCheck{}, err
	}
	if !ok {
		return history.RestoreCheck{}, ErrNothingToVerify
	}

	check := history.RestoreCheck{Profile: profile, Checked: now, Archive: archive.Name, Path: entry.Path}
	if check.Native, _, err = runner.Origin(entry.Path); err != nil {
		return history.RestoreCheck{}, err
	}
	check.Outcome, check.Reason, check.Detail = compareRestored(ctx, runner, env, archive.Name, entry, check.Native)
	if ctx.Err() != nil {
		// Une vérification interrompue ne prouve rien, ni dans un sens ni
		// dans l'autre.
		return history.RestoreCheck{}, ctx.Err()
	}
	if check.ID, err = store.AddRestoreCheck(ctx, check); err != nil {
		return check, err
	}
	return check, nil
}

// Raisons d'une issue de vérification. Chacune est la clé d'une phrase
// complète du catalogue, qui reçoit le fichier en paramètre (Path).
const (
	ReasonIdentical       = "verify.identical"
	ReasonOriginalMissing = "verify.original_missing"
	ReasonOriginalChanged = "verify.original_changed"
	ReasonOriginalUnread  = "verify.original_unreadable"
	ReasonExtractFailed   = "verify.extract_failed"
	ReasonSizeDiffers     = "verify.size_differs"
	ReasonContentDiffers  = "verify.content_differs"
)

// compareRestored extrait le fichier et le confronte à l'original. Il
// retourne l'issue, la clé de sa raison et, pour un échec, le message brut.
func compareRestored(ctx context.Context, runner borg.Runner, env borg.Environment, archive string,
	entry history.Entry, native string) (history.Outcome, string, string) {
	dir, err := os.MkdirTemp("", "borgui-verification-")
	if err != nil {
		return history.OutcomeFailed, ReasonExtractFailed, err.Error()
	}
	defer os.RemoveAll(dir)

	if _, err := borg.Extract(ctx, runner, borg.ExtractOptions{
		Env: env, Archive: archive, Paths: []string{entry.Path}, Destination: dir,
	}); err != nil {
		return history.OutcomeFailed, ReasonExtractFailed, err.Error()
	}
	extracted, err := onlyFile(dir)
	if err != nil {
		return history.OutcomeFailed, ReasonExtractFailed, err.Error()
	}
	restored, restoredSize, err := digest(extracted)
	if err != nil {
		return history.OutcomeFailed, ReasonExtractFailed, err.Error()
	}
	if restoredSize != entry.Size {
		return history.OutcomeFailed, ReasonSizeDiffers,
			fmt.Sprintf("extrait : %d octets, sauvegardé : %d octets", restoredSize, entry.Size)
	}

	info, err := os.Stat(native)
	if err != nil {
		return history.OutcomeExtracted, ReasonOriginalMissing, ""
	}
	if info.Size() != entry.Size || (!entry.Modified.IsZero() && info.ModTime().Unix() != entry.Modified.Unix()) {
		return history.OutcomeExtracted, ReasonOriginalChanged, ""
	}
	original, _, err := digest(native)
	if err != nil {
		return history.OutcomeExtracted, ReasonOriginalUnread, err.Error()
	}
	if original != restored {
		return history.OutcomeFailed, ReasonContentDiffers, ""
	}
	return history.OutcomeIdentical, ReasonIdentical, ""
}

// onlyFile retourne l'unique fichier extrait sous dir. Le chercher plutôt
// que reconstruire son chemin laisse la convention de chemins au seul
// Runner (AR-02).
func onlyFile(dir string) (string, error) {
	var found string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			if found != "" {
				return errors.New("core: plusieurs fichiers extraits")
			}
			found = path
		}
		return nil
	})
	if err == nil && found == "" {
		err = errors.New("core: aucun fichier extrait")
	}
	return found, err
}

// digest calcule l'empreinte SHA-256 d'un fichier et sa taille.
func digest(path string) ([sha256.Size]byte, int64, error) {
	var sum [sha256.Size]byte
	file, err := os.Open(path)
	if err != nil {
		return sum, 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return sum, 0, err
	}
	copy(sum[:], hash.Sum(nil))
	return sum, size, nil
}

// VerifyLatest vérifie la sauvegarde la plus récente de la destination, à
// la demande de l'utilisateur. Le verrou de la destination est pris : une
// sauvegarde planifiée qui démarrerait entre-temps attend son tour (EF-57).
func VerifyLatest(ctx context.Context, runner borg.Runner, env borg.Environment, store *history.Store,
	profile, lockDir string, now time.Time) (history.RestoreCheck, error) {
	if lockDir != "" {
		held, err := lock.Acquire(lock.PathFor(lockDir, env.Repository))
		if err != nil {
			return history.RestoreCheck{}, err
		}
		defer held.Release()
	}
	list, _, err := borg.List(ctx, runner, env)
	if err != nil {
		return history.RestoreCheck{}, err
	}
	if len(list.Archives) == 0 {
		return history.RestoreCheck{}, ErrNothingToVerify
	}
	latest := list.Archives[0]
	for _, archive := range list.Archives[1:] {
		if archive.Start.After(latest.Start.Time) {
			latest = archive
		}
	}
	return VerifyRestore(ctx, runner, env, store, profile, latest, now)
}
