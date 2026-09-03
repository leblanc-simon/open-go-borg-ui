package borgruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// ProgressFunc reçoit l'avancement du téléchargement. total vaut 0 tant que la
// taille est inconnue.
type ProgressFunc func(downloaded, total int64)

// downloadTimeout borne la durée totale d'un téléchargement. Le runtime pèse
// une centaine de mégaoctets : une heure couvre une liaison très lente sans
// laisser l'application attendre indéfiniment.
const downloadTimeout = time.Hour

// download récupère l'archive du runtime dans dest.
//
// Le transfert se fait dans un fichier « .part » repris à l'octet près en cas
// de nouvelle tentative : un premier lancement qui échoue à 90 % sur une
// liaison lente ne doit pas tout recommencer (EF-02).
//
// Le client HTTP par défaut suit les variables de proxy du système (EF-07).
func download(ctx context.Context, spec Spec, dest string, progress ProgressFunc) error {
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	part := dest + ".part"
	resumeAt := int64(0)
	if info, err := os.Stat(part); err == nil {
		resumeAt = info.Size()
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.URL, nil)
	if err != nil {
		return fmt.Errorf("runtime: requête invalide: %w", err)
	}
	if resumeAt > 0 {
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-", resumeAt))
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("runtime: téléchargement: %w", err)
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusOK:
		// Le serveur ignore la reprise : le transfert repart de zéro.
		resumeAt = 0
	case http.StatusPartialContent:
	default:
		return fmt.Errorf("runtime: téléchargement: réponse %s", response.Status)
	}

	flags := os.O_CREATE | os.O_WRONLY
	if resumeAt > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	file, err := os.OpenFile(part, flags, 0o600)
	if err != nil {
		return fmt.Errorf("runtime: écriture de %s: %w", part, err)
	}

	total := spec.Size
	if response.ContentLength > 0 {
		total = resumeAt + response.ContentLength
	}

	written, err := copyWithProgress(file, response.Body, resumeAt, total, progress)
	if closeErr := file.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		// Le fichier partiel est conservé : la tentative suivante le reprend.
		return fmt.Errorf("runtime: téléchargement interrompu à %d octets: %w", written, err)
	}

	if err := os.Rename(part, dest); err != nil {
		return fmt.Errorf("runtime: finalisation du téléchargement: %w", err)
	}
	return nil
}

// copyWithProgress recopie le flux en signalant l'avancement.
func copyWithProgress(dst io.Writer, src io.Reader, offset, total int64, progress ProgressFunc) (int64, error) {
	buffer := make([]byte, 256*1024)
	written := offset
	lastReport := time.Now()

	for {
		n, err := src.Read(buffer)
		if n > 0 {
			if _, writeErr := dst.Write(buffer[:n]); writeErr != nil {
				return written, writeErr
			}
			written += int64(n)
			// Un rapport toutes les 200 ms suffit à animer une barre de
			// progression sans inonder l'appelant.
			if progress != nil && time.Since(lastReport) > 200*time.Millisecond {
				progress(written, total)
				lastReport = time.Now()
			}
		}
		if errors.Is(err, io.EOF) {
			if progress != nil {
				progress(written, total)
			}
			return written, nil
		}
		if err != nil {
			return written, err
		}
	}
}
