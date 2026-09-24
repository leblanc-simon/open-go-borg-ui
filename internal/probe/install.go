package probe

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// authorizedKeysDir et authorizedKeysPath sont relatifs au répertoire du
// compte : sur une Storage Box, chaque sous-compte a le sien (addendum §8).
const (
	authorizedKeysDir  = ".ssh"
	authorizedKeysPath = ".ssh/authorized_keys"
)

// ErrPasswordRejected signale un mot de passe refusé par le serveur, dont
// l'empreinte, elle, a été acceptée.
var ErrPasswordRejected = errors.New("probe: mot de passe refusé")

// InstallResult est l'issue du dépôt de la clé.
type InstallResult struct {
	// Added est faux quand la clé figurait déjà parmi les clés autorisées.
	Added bool
	// Fingerprint est l'empreinte présentée par le serveur, à montrer à
	// l'utilisateur quand elle est encore inconnue.
	Fingerprint string
}

// InstallKey dépose la clé publique de l'application parmi les clés
// autorisées du compte, en s'authentifiant une seule fois par mot de passe.
// Elle épargne la copie manuelle dans la console Hetzner (EF-23, EF-24).
//
// Les clés déjà présentes sont conservées : la clé est ajoutée en fin de
// fichier, et seulement si elle n'y figure pas. L'empreinte du serveur est
// vérifiée comme pour le diagnostic (SEC-04). Le mot de passe n'est ni
// conservé ni écrit nulle part.
func InstallKey(ctx context.Context, params Params, password string) (InstallResult, error) {
	if params.Timeout == 0 {
		params.Timeout = defaultTimeout
	}
	if _, err := EnsureKey(params.KeyPath); err != nil {
		return InstallResult{}, err
	}
	public, err := PublicKey(params.KeyPath)
	if err != nil {
		return InstallResult{}, err
	}

	// Hetzner accepte le mot de passe sous l'une ou l'autre forme selon la
	// configuration du serveur SSH.
	answer := func(_, _ string, questions []string, _ []bool) ([]string, error) {
		answers := make([]string, len(questions))
		for i := range answers {
			answers[i] = password
		}
		return answers, nil
	}
	auth := []ssh.AuthMethod{ssh.Password(password), ssh.KeyboardInteractive(answer)}

	client, fingerprint, verified, err := dial(ctx, params, auth)
	result := InstallResult{Fingerprint: fingerprint}
	if err != nil {
		if verified {
			return result, fmt.Errorf("%w: %v", ErrPasswordRejected, err)
		}
		return result, err
	}
	defer client.Close()

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return result, fmt.Errorf("probe: ouverture SFTP: %w", err)
	}
	defer sftpClient.Close()

	result.Added, err = appendAuthorizedKey(sftpClient, public)
	return result, err
}

// appendAuthorizedKey ajoute la clé en fin de fichier authorized_keys, sans
// jamais réécrire les lignes existantes.
func appendAuthorizedKey(client *sftp.Client, public string) (bool, error) {
	wanted, _, _, _, err := ssh.ParseAuthorizedKey([]byte(public))
	if err != nil {
		return false, fmt.Errorf("probe: clé publique illisible: %w", err)
	}

	if _, err := client.Stat(authorizedKeysDir); errors.Is(err, os.ErrNotExist) {
		if err := client.Mkdir(authorizedKeysDir); err != nil {
			return false, fmt.Errorf("probe: création de %s: %w", authorizedKeysDir, err)
		}
		// Un serveur OpenSSH refuse un dossier trop ouvert ; certains
		// serveurs SFTP ne gèrent pas les droits, sans conséquence alors.
		_ = client.Chmod(authorizedKeysDir, 0o700)
	} else if err != nil {
		return false, fmt.Errorf("probe: lecture de %s: %w", authorizedKeysDir, err)
	}

	file, err := client.OpenFile(authorizedKeysPath, os.O_RDWR|os.O_CREATE)
	if err != nil {
		return false, fmt.Errorf("probe: ouverture de %s: %w", authorizedKeysPath, err)
	}
	defer file.Close()

	existing, err := io.ReadAll(file)
	if err != nil {
		return false, fmt.Errorf("probe: lecture de %s: %w", authorizedKeysPath, err)
	}
	if hasKey(existing, wanted) {
		return false, nil
	}

	line := []byte(public)
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		line = append([]byte("\n"), line...)
	}
	// Écriture à la position de fin plutôt qu'en mode ajout, que tous les
	// serveurs SFTP n'honorent pas.
	if _, err := file.WriteAt(line, int64(len(existing))); err != nil {
		return false, fmt.Errorf("probe: écriture de %s: %w", authorizedKeysPath, err)
	}
	if len(existing) == 0 {
		_ = client.Chmod(authorizedKeysPath, 0o600)
	}
	return true, nil
}

// hasKey indique que la clé figure déjà dans le contenu d'un fichier
// authorized_keys, quels que soient ses options ou son commentaire.
func hasKey(content []byte, wanted ssh.PublicKey) bool {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		key, _, _, _, err := ssh.ParseAuthorizedKey(scanner.Bytes())
		if err == nil && bytes.Equal(key.Marshal(), wanted.Marshal()) {
			return true
		}
	}
	return false
}
