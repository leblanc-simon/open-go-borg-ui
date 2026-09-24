package probe

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const testPassword = "mot-de-passe-du-sous-compte"

// passwordServer démarre un serveur SSH local qui n'accepte que le mot de
// passe de test et ne sert que le sous-système SFTP, sur un système de
// fichiers en mémoire partagé par toutes les connexions.
func passwordServer(t *testing.T) int {
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
		PasswordCallback: func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if string(password) == testPassword {
				return nil, nil
			}
			return nil, errors.New("mot de passe refusé")
		},
	}
	config.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	files := sftp.InMemHandler()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go serveSFTP(conn, config, files)
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port
}

// serveSFTP sert une connexion : une session, un sous-système SFTP.
func serveSFTP(conn net.Conn, config *ssh.ServerConfig, handlers sftp.Handlers) {
	_, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		conn.Close()
		return
	}
	go ssh.DiscardRequests(requests)
	for newChannel := range channels {
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

// readAuthorized relit le fichier authorized_keys du serveur de test.
func readAuthorized(t *testing.T, params Params) string {
	t.Helper()
	client, _, _, err := dial(context.Background(), params, []ssh.AuthMethod{ssh.Password(testPassword)})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		t.Fatal(err)
	}
	defer sftpClient.Close()
	file, err := sftpClient.Open(authorizedKeysPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// writeAuthorized dépose un fichier authorized_keys préexistant.
func writeAuthorized(t *testing.T, params Params, content string) {
	t.Helper()
	client, _, _, err := dial(context.Background(), params, []ssh.AuthMethod{ssh.Password(testPassword)})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		t.Fatal(err)
	}
	defer sftpClient.Close()
	if err := sftpClient.Mkdir(authorizedKeysDir); err != nil {
		t.Fatal(err)
	}
	file, err := sftpClient.Create(authorizedKeysPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
}

func testParams(t *testing.T, port int) Params {
	dir := t.TempDir()
	return Params{
		Host: "127.0.0.1", Port: port, User: "u123456",
		KeyPath:        filepath.Join(dir, "id_ed25519"),
		KnownHostsPath: filepath.Join(dir, "known_hosts"),
		Timeout:        5 * time.Second,
	}
}

// TestInstallKeyConserveLesClesExistantes vérifie que la clé est ajoutée à
// la suite des clés déjà autorisées, une seule fois.
func TestInstallKeyConserveLesClesExistantes(t *testing.T) {
	port := passwordServer(t)
	params := testParams(t, port)
	params.AllowPinning = true

	// Une clé d'un autre outil, sans saut de ligne final, et un bloc au
	// format RFC4716 que le port 22 d'une Storage Box attend.
	other := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl autre-outil"
	rfc := "---- BEGIN SSH2 PUBLIC KEY ----\nAAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl\n---- END SSH2 PUBLIC KEY ----\n"
	writeAuthorized(t, params, rfc+other)

	result, err := InstallKey(context.Background(), params, testPassword)
	if err != nil {
		t.Fatalf("InstallKey: %v", err)
	}
	if !result.Added || result.Fingerprint == "" {
		t.Errorf("premier dépôt: %+v", result)
	}

	public, err := PublicKey(params.KeyPath)
	if err != nil {
		t.Fatal(err)
	}
	want := rfc + other + "\n" + public
	if got := readAuthorized(t, params); got != want {
		t.Errorf("authorized_keys:\n%q\nattendu:\n%q", got, want)
	}

	// Un second dépôt ne duplique pas la clé.
	params.AllowPinning = false
	result, err = InstallKey(context.Background(), params, testPassword)
	if err != nil || result.Added {
		t.Errorf("second dépôt: %+v, erreur %v", result, err)
	}
	if got := readAuthorized(t, params); got != want {
		t.Errorf("après le second dépôt:\n%q", got)
	}
}

// TestInstallKeyCompteVide vérifie la création du dossier et du fichier.
func TestInstallKeyCompteVide(t *testing.T) {
	port := passwordServer(t)
	params := testParams(t, port)
	params.AllowPinning = true

	if _, err := InstallKey(context.Background(), params, testPassword); err != nil {
		t.Fatalf("InstallKey: %v", err)
	}
	public, err := PublicKey(params.KeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := readAuthorized(t, params); got != public {
		t.Errorf("authorized_keys: %q, attendu %q", got, public)
	}
}

// TestInstallKeyRefus vérifie les deux refus : serveur encore inconnu, puis
// mot de passe erroné une fois l'empreinte acceptée.
func TestInstallKeyRefus(t *testing.T) {
	port := passwordServer(t)
	params := testParams(t, port)

	result, err := InstallKey(context.Background(), params, testPassword)
	if !errors.Is(err, ErrHostKeyUnknown) {
		t.Fatalf("serveur inconnu: %v, attendu ErrHostKeyUnknown", err)
	}
	if !strings.HasPrefix(result.Fingerprint, "SHA256:") {
		t.Errorf("empreinte à montrer: %q", result.Fingerprint)
	}

	params.AllowPinning = true
	if _, err := InstallKey(context.Background(), params, "faux"); !errors.Is(err, ErrPasswordRejected) {
		t.Errorf("mot de passe erroné: %v, attendu ErrPasswordRejected", err)
	}
}
