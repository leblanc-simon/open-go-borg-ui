//go:build !windows

package gui

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/zalando/go-keyring"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"leblanc.io/open-go-borg-ui/internal/config"
	"leblanc.io/open-go-borg-ui/internal/history"
	"leblanc.io/open-go-borg-ui/internal/schedule"
	"leblanc.io/open-go-borg-ui/internal/wizard"
)

// wizardScript simule Borg d'une destination d'abord vide : « init » la
// crée, et « info » la trouve ensuite.
const wizardScript = `
case "$*" in
*--version*) echo "borg 1.4.5" ;;
*" init "*) touch "$HOME/.destination" ;;
*" info "*)
  if [ -f "$HOME/.destination" ]; then
    echo '{"encryption":{"mode":"repokey-blake2"},"cache":{"stats":{"unique_csize":4096}}}'
  else
    echo '{"type":"log_message","levelname":"ERROR","msgid":"Repository.DoesNotExist","message":"Repository does not exist."}' >&2
    exit 2
  fi ;;
*" key "*) echo "BORG PAPER KEY v1 — clé de secours de test" ;;
*" create "*) echo '{"archive":{"name":"poste-2026-09-24T12:00:00","stats":{"nfiles":7,"original_size":2048,"deduplicated_size":1024}}}' ;;
esac
exit 0
`

// sshServer démarre un serveur SSH local qui accepte toute clé et répond à la
// demande de version du moteur, comme une Storage Box. Son empreinte est
// épinglée d'avance dans known_hosts.
func sshServer(t *testing.T, knownHostsPath string) int {
	t.Helper()
	_, hostKey, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(hostKey)
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) { return nil, nil }}
	cfg.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go serveVersion(conn, cfg)
		}
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	os.MkdirAll(filepath.Dir(knownHostsPath), 0o700)
	line := knownhosts.Line([]string{knownhosts.Normalize("127.0.0.1:" + strconv.Itoa(port))}, signer.PublicKey())
	if err := os.WriteFile(knownHostsPath, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return port
}

// serveVersion répond aux commandes exécutées par la version du moteur.
func serveVersion(conn net.Conn, cfg *ssh.ServerConfig) {
	_, channels, requests, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(requests)
	for newChannel := range channels {
		channel, reqs, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go func() {
			for req := range reqs {
				req.Reply(req.Type == "exec", nil)
				if req.Type == "exec" {
					channel.Write([]byte("borg 1.4.5\n"))
					status := make([]byte, 4)
					binary.BigEndian.PutUint32(status, 0)
					channel.SendRequest("exit-status", false, status)
					channel.Close()
				}
			}
		}()
	}
}

// fakeScheduler remplace l'ordonnanceur du système.
func fakeScheduler(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	previous := newScheduler
	newScheduler = func() (schedule.Scheduler, error) {
		return schedule.NewSystemd(dir, func(context.Context, string, ...string) ([]byte, error) { return nil, nil }), nil
	}
	t.Cleanup(func() { newScheduler = previous })
	return dir
}

// find cherche dans l'arbre un widget qui satisfait match.
func find[T fyne.CanvasObject](root fyne.CanvasObject, match func(T) bool) T {
	var zero T
	var walk func(fyne.CanvasObject) (T, bool)
	walk = func(o fyne.CanvasObject) (T, bool) {
		if w, ok := o.(T); ok && match(w) {
			return w, true
		}
		var children []fyne.CanvasObject
		switch c := o.(type) {
		case *fyne.Container:
			children = c.Objects
		case *container.Scroll:
			children = []fyne.CanvasObject{c.Content}
		case *widget.Form:
			for _, item := range c.Items {
				children = append(children, item.Widget)
			}
		}
		for _, child := range children {
			if found, ok := walk(child); ok {
				return found, true
			}
		}
		return zero, false
	}
	found, _ := walk(root)
	return found
}

// button retrouve un bouton de l'étape par son libellé.
func button(t *testing.T, w *wizardView, label string) *widget.Button {
	t.Helper()
	b := find(w.body, func(b *widget.Button) bool { return b.Text == label })
	if b == nil {
		t.Fatalf("bouton « %s » absent de l'étape %s", label, w.state.Step)
	}
	return b
}

// advance attend que l'étape soit satisfaite, puis passe à la suivante.
func advance(t *testing.T, w *wizardView, want wizard.Step) {
	t.Helper()
	waitFor(t, "l'étape "+string(w.state.Step), func() bool { return !w.next.Disabled() })
	test.Tap(w.next)
	if w.state.Step != want {
		t.Fatalf("étape %s, attendue %s", w.state.Step, want)
	}
}

// TestAssistantComplet parcourt l'assistant d'un poste vierge jusqu'à la
// première sauvegarde (EF-10), destination chiffrée comprise.
func TestAssistantComplet(t *testing.T) {
	keyring.MockInit()
	st := poste(t, wizardScript, false)
	knownHosts, _ := config.KnownHostsPath()
	port := sshServer(t, knownHosts)
	units := fakeScheduler(t)

	u, _, tr := open(t, st)
	w := u.wizard
	if w == nil || w.state.Step != wizard.StepWelcome {
		t.Fatal("un poste vierge doit ouvrir l'assistant")
	}

	advance(t, w, wizard.StepEngine)
	advance(t, w, wizard.StepDestination)

	// Une destination SSH locale, servie par le serveur de test.
	w.state.Profile.Destination.Kind = config.KindSSH
	w.state.Profile.Destination.Repo = "ssh://u123456@127.0.0.1:" + strconv.Itoa(port) + "/./poste"
	w.render()
	test.Tap(button(t, w, tr("destination.test")))
	advance(t, w, wizard.StepEncryption)
	if w.state.RepositoryExists {
		t.Error("la destination de test est vide")
	}

	// Chiffrement présélectionné (PA-04) : il suffit du mot de passe.
	passwords := passwordEntries(w.body)
	if len(passwords) != 2 {
		t.Fatalf("%d champs de mot de passe", len(passwords))
	}
	for _, entry := range passwords {
		test.Type(entry, "correct horse battery staple")
	}
	test.Tap(button(t, w, tr("wizard.encryption.create")))
	advance(t, w, wizard.StepFolders)
	if secret, err := st.Secrets().Get("poste"); err != nil || secret != "correct horse battery staple" {
		t.Errorf("mot de passe au trousseau: %q, %v", secret, err)
	}

	// Le sélecteur de dossiers est natif : le dossier est inscrit
	// directement, comme il l'aurait choisi.
	w.state.Profile.Sources = []string{t.TempDir()}
	w.render()
	advance(t, w, wizard.StepSchedule)
	advance(t, w, wizard.StepRecoveryKey)

	test.Tap(button(t, w, tr("wizard.recovery_key.show")))
	check := find(w.body, func(c *widget.Check) bool { return c.Text == tr("wizard.recovery_key.confirm") })
	waitFor(t, "la clé de secours", func() bool { return !check.Disabled() })
	if !strings.Contains(texts(w.body), "BORG PAPER KEY") {
		t.Error("la clé de secours n'est pas affichée")
	}
	if !w.next.Disabled() {
		t.Error("l'étape ne doit pas pouvoir être franchie sans confirmation (EF-35)")
	}
	test.Tap(check)
	advance(t, w, wizard.StepFinish)

	test.Tap(w.next)
	if u.wizard != nil || u.shell == nil {
		t.Fatal("la fin de l'assistant doit ouvrir l'application")
	}
	cfg, err := config.Load("")
	if err != nil || cfg.Profiles[0].Encryption != config.EncryptionRepokey || len(cfg.Profiles[0].Sources) != 1 {
		t.Fatalf("configuration écrite: %+v, %v", cfg, err)
	}
	if _, err := os.Stat(filepath.Join(units, "borgui-poste.timer")); err != nil {
		t.Errorf("planification non installée: %v", err)
	}
	if _, err := os.Stat(wizard.Path(st.StateDir)); !os.IsNotExist(err) {
		t.Error("l'état de l'assistant doit disparaître une fois terminé")
	}

	waitFor(t, "la première sauvegarde", func() bool { return strings.Contains(texts(u.backup.result), "7 fichiers") })
	store, _ := st.History()
	defer store.Close()
	runs, _ := store.Recent(context.Background(), "poste", 10)
	if len(runs) != 1 || runs[0].Status != history.StatusSuccess {
		t.Errorf("historique: %+v", runs)
	}
}

// passwordEntries rassemble les champs de mot de passe visibles de l'étape.
func passwordEntries(root fyne.CanvasObject) []*widget.Entry {
	var found []*widget.Entry
	var walk func(fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if e, ok := o.(*widget.Entry); ok && e.Password {
			found = append(found, e)
		}
		if c, ok := o.(*fyne.Container); ok && c.Visible() {
			for _, child := range c.Objects {
				walk(child)
			}
		}
	}
	walk(root)
	return found
}

// TestAssistantRepris vérifie qu'un assistant interrompu reprend à l'étape où
// il s'était arrêté (EF-11).
func TestAssistantRepris(t *testing.T) {
	st := poste(t, wizardScript, false)
	u, _, _ := open(t, st)
	advance(t, u.wizard, wizard.StepEngine)
	advance(t, u.wizard, wizard.StepDestination)

	// Fenêtre fermée, application relancée.
	again, _, _ := open(t, st)
	if again.wizard == nil || again.wizard.state.Step != wizard.StepDestination {
		t.Errorf("reprise à l'étape %v", again.wizard.state.Step)
	}
}
