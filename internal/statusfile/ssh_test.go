package statusfile

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"leblanc.io/open-go-borg-ui/internal/probe"
)

// sshServer démarre un serveur SSH local qui n'accepte que la clé publique
// donnée et ne sert que le sous-système SFTP, sur un système de fichiers en
// mémoire partagé par toutes les connexions. Il retourne son adresse.
func sshServer(t *testing.T, authorized ssh.PublicKey) (host string, port int) {
	t.Helper()
	_, hostKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(hostKey)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if string(key.Marshal()) == string(authorized.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("clé refusée")
		},
	}
	config.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	handlers := sftp.InMemHandler()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go serveSSH(conn, config, handlers)
		}
	}()

	address := listener.Addr().(*net.TCPAddr)
	return "127.0.0.1", address.Port
}

// serveSSH sert une connexion : une session, un sous-système SFTP.
func serveSSH(conn net.Conn, config *ssh.ServerConfig, handlers sftp.Handlers) {
	_, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		conn.Close()
		return
	}
	go ssh.DiscardRequests(requests)
	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "")
			continue
		}
		channel, channelRequests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go func() {
			for request := range channelRequests {
				ok := request.Type == "subsystem" && string(request.Payload[4:]) == "sftp"
				request.Reply(ok, nil)
				if ok {
					server := sftp.NewRequestServer(channel, handlers)
					server.Serve()
					server.Close()
				}
			}
		}()
	}
}

// TestSessionSSH vérifie la liaison complète : clé de l'application,
// épinglage de l'empreinte au premier appariement, refus d'un serveur inconnu
// ensuite, publication puis relecture à travers SSH et SFTP.
func TestSessionSSH(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	signer, err := probe.EnsureKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	host, port := sshServer(t, signer.PublicKey())

	params := probe.Params{
		Host: host, Port: port, User: "u123456",
		KeyPath: keyPath, KnownHostsPath: filepath.Join(dir, "known_hosts"),
		Timeout: 5 * time.Second,
	}
	target := Target{Params: params, Dir: DefaultDir}
	ctx := context.Background()

	// Sans épinglage autorisé, un serveur inconnu est refusé.
	if err := PublishTo(ctx, target, valid()); !errors.Is(err, probe.ErrHostKeyUnknown) {
		t.Fatalf("serveur inconnu: %v, attendu ErrHostKeyUnknown", err)
	}

	pinning := target
	pinning.Params.AllowPinning = true
	if err := PublishTo(ctx, pinning, valid()); err != nil {
		t.Fatalf("premier appariement: %v", err)
	}

	// L'empreinte est désormais connue : plus besoin d'autorisation.
	got, err := ReadFrom(ctx, target, "poste-marc", now)
	if err != nil || got.Hostname != "poste-marc" {
		t.Errorf("ReadFrom: %+v, erreur %v", got, err)
	}

	// Un second serveur, présenté sous une adresse dont l'empreinte épinglée
	// est celle du premier : c'est la situation d'un serveur substitué.
	otherHost, otherPort := sshServer(t, signer.PublicKey())
	if err := pinAs(params.KnownHostsPath, port, otherPort); err != nil {
		t.Fatal(err)
	}
	impostor := target
	impostor.Params.Host, impostor.Params.Port = otherHost, otherPort
	if _, err := ReadFrom(ctx, impostor, "poste-marc", now); !errors.Is(err, probe.ErrHostKeyChanged) {
		t.Errorf("serveur substitué: %v, attendu ErrHostKeyChanged", err)
	}
}

// pinAs recopie l'empreinte épinglée pour le port from sous le port to.
func pinAs(knownHosts string, from, to int) error {
	data, err := os.ReadFile(knownHosts)
	if err != nil {
		return err
	}
	line := strings.TrimSpace(string(data))
	line = strings.Replace(line, "]:"+strconv.Itoa(from)+" ", "]:"+strconv.Itoa(to)+" ", 1)
	return os.WriteFile(knownHosts, []byte(string(data)+line+"\n"), 0o600)
}
