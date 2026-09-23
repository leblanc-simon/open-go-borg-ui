package statusfile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"leblanc.io/open-go-borg-ui/internal/probe"
)

// DefaultDir est le répertoire du fichier d'état dans le sous-compte.
const DefaultDir = "status"

// Session est une connexion SFTP au sous-compte du poste.
type Session struct {
	SFTP *sftp.Client
	ssh  *ssh.Client
}

// Dial ouvre la session, avec la clé de l'application et l'empreinte épinglée
// de la destination, exactement comme le diagnostic de connexion.
func Dial(ctx context.Context, params probe.Params) (*Session, error) {
	client, _, err := probe.Dial(ctx, params)
	if err != nil {
		return nil, err
	}
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("statusfile: ouverture SFTP: %w", err)
	}
	return &Session{SFTP: sftpClient, ssh: client}, nil
}

// Close ferme la session.
func (s *Session) Close() error {
	err := s.SFTP.Close()
	if s.ssh != nil {
		if sshErr := s.ssh.Close(); err == nil {
			err = sshErr
		}
	}
	return err
}

// Target désigne le sous-compte du poste et le répertoire du fichier d'état.
type Target struct {
	Params probe.Params
	Dir    string
}

// PublishTo ouvre une session, dépose l'état et la referme.
func PublishTo(ctx context.Context, target Target, status Status) error {
	session, err := Dial(ctx, target.Params)
	if err != nil {
		return err
	}
	defer session.Close()
	return Publish(session.SFTP, target.Dir, status)
}

// ReadFrom ouvre une session, relit le fichier d'état du poste et la
// referme.
func ReadFrom(ctx context.Context, target Target, hostname string, now time.Time) (Status, error) {
	session, err := Dial(ctx, target.Params)
	if err != nil {
		return Status{}, err
	}
	defer session.Close()
	return Read(session.SFTP, target.Dir, hostname, now)
}

// Publish dépose le fichier d'état du poste dans dir.
//
// Le fichier est écrit sous un nom temporaire puis renommé : une lecture au
// même moment voit l'ancien état ou le nouveau, jamais un fichier tronqué.
func Publish(client *sftp.Client, dir string, status Status) error {
	data, err := Encode(status)
	if err != nil {
		return fmt.Errorf("statusfile: %w", err)
	}
	if err := client.MkdirAll(dir); err != nil {
		return fmt.Errorf("statusfile: création de %s: %w", dir, err)
	}

	name := FileName(status.Hostname)
	final := path.Join(dir, name)
	temporary := path.Join(dir, "."+name+".tmp")

	file, err := client.Create(temporary)
	if err != nil {
		return fmt.Errorf("statusfile: écriture de %s: %w", temporary, err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		client.Remove(temporary)
		return fmt.Errorf("statusfile: écriture de %s: %w", temporary, err)
	}
	if err := file.Close(); err != nil {
		client.Remove(temporary)
		return fmt.Errorf("statusfile: écriture de %s: %w", temporary, err)
	}

	// Le renommage POSIX remplace la cible d'un seul geste. Un serveur qui
	// ne le connaît pas n'accepte de renommer que vers un nom libre.
	if err := client.PosixRename(temporary, final); err != nil {
		client.Remove(final)
		if err := client.Rename(temporary, final); err != nil {
			client.Remove(temporary)
			return fmt.Errorf("statusfile: publication de %s: %w", final, err)
		}
	}
	return nil
}

// ErrNotPublished signale qu'aucun état n'a encore été déposé.
var ErrNotPublished = errors.New("statusfile: aucun état publié")

// Read relit le fichier d'état du poste, sans jamais lire au-delà de la
// taille admise : la taille annoncée par le serveur n'engage pas le contenu.
func Read(client *sftp.Client, dir, hostname string, now time.Time) (Status, error) {
	name := FileName(hostname)
	file, err := client.Open(path.Join(dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return Status{}, ErrNotPublished
	}
	if err != nil {
		return Status{}, fmt.Errorf("statusfile: lecture de %s: %w", name, err)
	}
	defer file.Close()
	// Un lien ou un dossier à la place du fichier n'est pas un état.
	if info, err := file.Stat(); err == nil && !info.Mode().IsRegular() {
		return Status{}, fmt.Errorf("%w: %s n'est pas un fichier", ErrInvalid, name)
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxFileSize+1))
	if err != nil {
		return Status{}, fmt.Errorf("statusfile: lecture de %s: %w", name, err)
	}
	return Parse(data, name, now)
}
