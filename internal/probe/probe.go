package probe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"leblanc.io/open-go-borg-ui/internal/fsperm"
)

// Step identifie une étape du diagnostic. L'ordre est significatif : chaque
// étape suppose la précédente réussie, et la première en échec porte
// l'explication utile.
type Step int

const (
	// StepResolve : résolution du nom de la destination.
	StepResolve Step = iota
	// StepReach : accessibilité du port.
	StepReach
	// StepAuthenticate : acceptation de la clé SSH par le serveur.
	StepAuthenticate
	// StepEngine : présence du moteur Borg à la version demandée côté serveur.
	StepEngine
)

// TranslationKey retourne la clé de l'intitulé de l'étape.
func (s Step) TranslationKey() string {
	switch s {
	case StepResolve:
		return "connection.step_resolve"
	case StepReach:
		return "connection.step_reach"
	case StepAuthenticate:
		return "connection.step_authenticate"
	default:
		return "connection.step_engine"
	}
}

// FixKey retourne la clé de l'action corrective proposée en cas d'échec.
func (s Step) FixKey() string {
	switch s {
	case StepResolve:
		return "connection.fix_resolve"
	case StepReach:
		return "connection.fix_reach"
	case StepAuthenticate:
		return "connection.fix_authenticate"
	default:
		return "connection.fix_engine"
	}
}

// Params décrit la destination à diagnostiquer.
type Params struct {
	Host string
	Port int
	User string

	// KeyPath est la clé privée de l'application.
	KeyPath string
	// KnownHostsPath épingle l'empreinte du serveur (SEC-04).
	KnownHostsPath string
	// AllowPinning autorise l'enregistrement de l'empreinte lorsqu'elle est
	// encore inconnue. Ce n'est vrai qu'au premier appariement, à l'initiative
	// de l'utilisateur.
	AllowPinning bool

	// RemotePath est l'exécutable Borg attendu côté serveur.
	RemotePath string
	// Timeout borne chaque étape.
	Timeout time.Duration
}

// Outcome est le résultat d'une étape.
type Outcome struct {
	Step Step
	OK   bool
	// Detail complète l'intitulé traduit : version rapportée, empreinte
	// épinglée, adresse résolue. Jamais un secret.
	Detail string
	// Err porte l'erreur technique, destinée au dépliant « Détails ».
	Err error
}

// ErrHostKeyUnknown signale une empreinte de serveur encore inconnue alors que
// l'épinglage n'est pas autorisé.
var ErrHostKeyUnknown = errors.New("probe: empreinte du serveur inconnue")

// ErrHostKeyChanged signale une empreinte différente de celle épinglée. La
// connexion échoue : le serveur n'est peut-être pas celui attendu (EF-26).
var ErrHostKeyChanged = errors.New("probe: l'empreinte du serveur a changé")

// defaultTimeout borne une étape par défaut.
const defaultTimeout = 15 * time.Second

// Run enchaîne les étapes et s'arrête à la première en échec.
func Run(ctx context.Context, params Params) []Outcome {
	if params.Timeout == 0 {
		params.Timeout = defaultTimeout
	}

	outcomes := make([]Outcome, 0, 4)

	addresses, err := resolve(ctx, params)
	outcomes = append(outcomes, Outcome{
		Step:   StepResolve,
		OK:     err == nil,
		Detail: strings.Join(addresses, ", "),
		Err:    err,
	})
	if err != nil {
		return outcomes
	}

	err = reach(ctx, params)
	outcomes = append(outcomes, Outcome{Step: StepReach, OK: err == nil, Err: err})
	if err != nil {
		return outcomes
	}

	client, fingerprint, err := connect(ctx, params)
	outcomes = append(outcomes, Outcome{
		Step:   StepAuthenticate,
		OK:     err == nil,
		Detail: fingerprint,
		Err:    err,
	})
	if err != nil {
		return outcomes
	}
	defer client.Close()

	version, err := remoteVersion(client, params.RemotePath)
	outcomes = append(outcomes, Outcome{
		Step:   StepEngine,
		OK:     err == nil,
		Detail: version,
		Err:    err,
	})
	return outcomes
}

// Failed retourne la première étape en échec.
func Failed(outcomes []Outcome) (Outcome, bool) {
	for _, outcome := range outcomes {
		if !outcome.OK {
			return outcome, true
		}
	}
	return Outcome{}, false
}

// resolve vérifie que le nom de la destination est résolu.
func resolve(ctx context.Context, params Params) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, params.Timeout)
	defer cancel()

	addresses, err := net.DefaultResolver.LookupHost(ctx, params.Host)
	if err != nil {
		return nil, fmt.Errorf("probe: résolution de %s: %w", params.Host, err)
	}
	return addresses, nil
}

// reach vérifie que le port répond. Sur une Storage Box, un port 23 muet
// signale presque toujours une option « External reachability » désactivée.
func reach(ctx context.Context, params Params) error {
	dialer := net.Dialer{Timeout: params.Timeout}
	address := net.JoinHostPort(params.Host, fmt.Sprint(params.Port))

	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("probe: connexion à %s: %w", address, err)
	}
	return conn.Close()
}

// Dial ouvre une session SSH authentifiée par la clé de l'application, après
// vérification de l'empreinte du serveur (SEC-04). C'est la connexion du
// diagnostic, réutilisée par les échanges SFTP : l'épinglage y est le même.
func Dial(ctx context.Context, params Params) (*ssh.Client, string, error) {
	if params.Timeout == 0 {
		params.Timeout = defaultTimeout
	}
	return connect(ctx, params)
}

// connect ouvre la session SSH avec la clé de l'application et vérifie
// l'empreinte du serveur.
func connect(ctx context.Context, params Params) (*ssh.Client, string, error) {
	signer, err := EnsureKey(params.KeyPath)
	if err != nil {
		return nil, "", err
	}
	client, fingerprint, _, err := dial(ctx, params, []ssh.AuthMethod{ssh.PublicKeys(signer)})
	return client, fingerprint, err
}

// dial ouvre la session SSH avec les méthodes d'authentification données et
// vérifie l'empreinte du serveur. verified indique que l'empreinte a été
// acceptée : un échec survenu ensuite tient à l'authentification.
func dial(ctx context.Context, params Params, auth []ssh.AuthMethod) (client *ssh.Client, fingerprint string, verified bool, err error) {
	callback := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		fingerprint = ssh.FingerprintSHA256(key)
		if err := verifyHostKey(params, hostname, remote, key); err != nil {
			return err
		}
		verified = true
		return nil
	}

	config := &ssh.ClientConfig{
		User:            params.User,
		Auth:            auth,
		HostKeyCallback: callback,
		Timeout:         params.Timeout,
	}

	dialer := net.Dialer{Timeout: params.Timeout}
	address := net.JoinHostPort(params.Host, fmt.Sprint(params.Port))
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fingerprint, false, fmt.Errorf("probe: connexion à %s: %w", address, err)
	}

	sshConn, channels, requests, err := ssh.NewClientConn(conn, address, config)
	if err != nil {
		conn.Close()
		return nil, fingerprint, verified, fmt.Errorf("probe: authentification: %w", err)
	}
	return ssh.NewClient(sshConn, channels, requests), fingerprint, verified, nil
}

// verifyHostKey confronte l'empreinte présentée au fichier known_hosts de
// l'application, et l'y inscrit au premier appariement.
func verifyHostKey(params Params, hostname string, remote net.Addr, key ssh.PublicKey) error {
	if params.KnownHostsPath == "" {
		return errors.New("probe: fichier d'empreintes non configuré")
	}
	if err := os.MkdirAll(filepath.Dir(params.KnownHostsPath), 0o700); err != nil {
		return fmt.Errorf("probe: dossier des empreintes: %w", err)
	}
	if _, err := os.Stat(params.KnownHostsPath); errors.Is(err, os.ErrNotExist) {
		if err := fsperm.WritePrivate(params.KnownHostsPath, nil); err != nil {
			return fmt.Errorf("probe: création du fichier d'empreintes: %w", err)
		}
	}

	callback, err := knownhosts.New(params.KnownHostsPath)
	if err != nil {
		return fmt.Errorf("probe: lecture des empreintes: %w", err)
	}

	err = callback(hostname, remote, key)
	if err == nil {
		return nil
	}

	var keyErr *knownhosts.KeyError
	if errors.As(err, &keyErr) && len(keyErr.Want) == 0 {
		// Hôte inconnu : c'est le premier appariement.
		if !params.AllowPinning {
			return fmt.Errorf("%w: %s", ErrHostKeyUnknown, ssh.FingerprintSHA256(key))
		}
		return pin(params.KnownHostsPath, hostname, key)
	}
	// Une empreinte connue mais différente : le serveur n'est pas celui
	// épinglé, la connexion s'arrête là.
	return fmt.Errorf("%w: %s", ErrHostKeyChanged, ssh.FingerprintSHA256(key))
}

// pin inscrit l'empreinte du serveur.
func pin(path, hostname string, key ssh.PublicKey) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("probe: écriture des empreintes: %w", err)
	}
	defer file.Close()

	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
	if _, err := fmt.Fprintln(file, line); err != nil {
		return fmt.Errorf("probe: écriture des empreintes: %w", err)
	}
	return nil
}

// remoteVersion demande sa version au Borg du serveur. Hetzner installe deux
// versions et l'application doit vérifier que celle qu'elle désigne existe
// bien (EF-25).
func remoteVersion(client *ssh.Client, remotePath string) (string, error) {
	if remotePath == "" {
		remotePath = "borg"
	}
	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("probe: ouverture de session: %w", err)
	}
	defer session.Close()

	out, err := session.Output(remotePath + " --version")
	if err != nil {
		return "", fmt.Errorf("probe: %s absent du serveur: %w", remotePath, err)
	}
	return strings.TrimSpace(string(out)), nil
}
