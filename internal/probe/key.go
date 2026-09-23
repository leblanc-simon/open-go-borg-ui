// Package probe diagnostique la connexion à la destination sans dépendre d'un
// binaire externe : ni ssh, ni ssh-keygen, ni sftp (AR-05).
//
// Le diagnostic progresse par étapes et s'arrête à la première en échec, de
// façon que l'interface puisse afficher l'action corrective correspondante
// plutôt qu'un message de bibliothèque (EF-25).
package probe

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"

	"leblanc.io/open-go-borg-ui/internal/fsperm"
)

// EnsureKey retourne la clé SSH de l'application, en la créant si nécessaire.
//
// La clé est dédiée à l'application et n'est jamais partagée avec celle de
// l'utilisateur (SEC-03). Elle n'a pas de passphrase : une sauvegarde planifiée
// ne peut pas répondre à une invite de saisie.
func EnsureKey(path string) (ssh.Signer, error) {
	if signer, err := loadKey(path); err == nil {
		// Une clé existante peut avoir été restaurée ou copiée avec des
		// droits trop larges, que le ssh du runtime refuserait.
		if err := fsperm.Restrict(path); err != nil {
			return nil, err
		}
		return signer, nil
	} else if !os.IsNotExist(errCause(err)) {
		return nil, err
	}

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("probe: génération de la clé: %w", err)
	}

	block, err := ssh.MarshalPrivateKey(private, "borgui")
	if err != nil {
		return nil, fmt.Errorf("probe: encodage de la clé: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("probe: création du dossier de la clé: %w", err)
	}
	if err := fsperm.WritePrivate(path, pemEncode(block)); err != nil {
		return nil, fmt.Errorf("probe: écriture de la clé: %w", err)
	}

	authorized, err := ssh.NewPublicKey(public)
	if err != nil {
		return nil, fmt.Errorf("probe: dérivation de la clé publique: %w", err)
	}
	// La clé publique est écrite à côté de la privée : c'est elle que
	// l'utilisateur dépose dans la console Hetzner (EF-23).
	if err := os.WriteFile(path+".pub", ssh.MarshalAuthorizedKey(authorized), 0o644); err != nil {
		return nil, fmt.Errorf("probe: écriture de la clé publique: %w", err)
	}

	return ssh.NewSignerFromKey(private)
}

// PublicKey retourne la clé publique au format attendu par le port 23 d'une
// Storage Box, c'est-à-dire le format OpenSSH d'un fichier authorized_keys.
func PublicKey(path string) (string, error) {
	signer, err := loadKey(path)
	if err != nil {
		return "", err
	}
	return string(ssh.MarshalAuthorizedKey(signer.PublicKey())), nil
}

// loadKey lit la clé privée.
func loadKey(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("probe: lecture de la clé: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("probe: clé illisible: %w", err)
	}
	return signer, nil
}
