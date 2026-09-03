package borgruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTelechargementAvecReprise vérifie qu'un transfert interrompu repart où
// il s'était arrêté : sur une liaison lente, recommencer une centaine de
// mégaoctets décourage l'utilisateur au premier lancement (EF-02).
func TestTelechargementAvecReprise(t *testing.T) {
	content := strings.Repeat("runtime", 4096)

	var ranges []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Range")
		ranges = append(ranges, header)

		if header == "" {
			// Première tentative : la connexion se coupe en cours de route.
			w.Header().Set("Content-Length", "1000")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(content[:1000]))
			return
		}

		var offset int
		if err := parseRange(header, &offset); err != nil {
			t.Errorf("en-tête Range illisible: %s", header)
		}
		w.Header().Set("Content-Range", "bytes")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte(content[offset:]))
	}))
	defer server.Close()

	digest := sha256.Sum256([]byte(content))
	spec := Spec{
		Version: "test",
		URL:     server.URL + "/runtime.zip",
		SHA256:  hex.EncodeToString(digest[:]),
	}

	dest := filepath.Join(t.TempDir(), "runtime.zip")

	// Première passe : le serveur n'envoie qu'un fragment, le fichier partiel
	// est conservé.
	if err := download(context.Background(), spec, dest, nil); err != nil {
		t.Fatalf("premier téléchargement: %v", err)
	}
	// Le fragment reçu a été considéré comme complet faute d'indication
	// contraire : la vérification d'empreinte l'écarte.
	manager := NewManager(filepath.Dir(dest), spec)
	if err := manager.verifyChecksum(dest); err == nil {
		t.Fatal("un fichier tronqué doit être rejeté par l'empreinte")
	}

	// Deuxième passe, à partir du fichier partiel renommé en .part.
	if err := os.Rename(dest, dest+".part"); err != nil {
		t.Fatalf("préparation de la reprise: %v", err)
	}
	if err := download(context.Background(), spec, dest, nil); err != nil {
		t.Fatalf("reprise du téléchargement: %v", err)
	}
	if err := manager.verifyChecksum(dest); err != nil {
		t.Errorf("le fichier repris devrait être complet: %v", err)
	}

	if len(ranges) != 2 || ranges[0] != "" || ranges[1] != "bytes=1000-" {
		t.Errorf("en-têtes Range = %v, attendus [\"\", \"bytes=1000-\"]", ranges)
	}
}

// TestProgressionTelechargement vérifie que l'avancement est rapporté.
func TestProgressionTelechargement(t *testing.T) {
	content := strings.Repeat("x", 512*1024)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(content))
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "runtime.zip")
	var last int64
	err := download(context.Background(), Spec{URL: server.URL}, dest, func(downloaded, _ int64) {
		last = downloaded
	})
	if err != nil {
		t.Fatalf("download a échoué: %v", err)
	}
	if last != int64(len(content)) {
		t.Errorf("dernier avancement rapporté = %d, attendu %d", last, len(content))
	}
}

// parseRange isole la borne inférieure d'un en-tête « bytes=N- ».
func parseRange(header string, offset *int) error {
	trimmed := strings.TrimSuffix(strings.TrimPrefix(header, "bytes="), "-")
	value := 0
	for _, r := range trimmed {
		if r < '0' || r > '9' {
			return errBadRange
		}
		value = value*10 + int(r-'0')
	}
	*offset = value
	return nil
}

// errBadRange signale un en-tête Range inattendu dans les tests.
var errBadRange = errorString("en-tête Range inattendu")

// errorString est une erreur constante de test.
type errorString string

func (e errorString) Error() string { return string(e) }
