// Package config lit et écrit la configuration de l'application.
//
// Le fichier est du TOML volontairement lisible et modifiable à la main
// (AR-06) : pour un logiciel de sauvegarde, pouvoir inspecter et corriger la
// configuration avec un éditeur de texte est une garantie, pas un défaut.
//
// Aucun secret n'y figure : la passphrase vit dans le trousseau du système et
// la clé SSH dans un fichier séparé.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// Kind désigne le type de destination.
type Kind string

const (
	// KindHetzner déduit l'hôte, le port et le chemin du seul sous-compte.
	KindHetzner Kind = "hetzner"
	// KindSSH accepte une URL de dépôt complète.
	KindSSH Kind = "ssh"
)

// Encryption est le mode de chiffrement du dépôt. Il est figé à la création et
// ne peut jamais être modifié ensuite (EF-32).
type Encryption string

const (
	// EncryptionRepokey chiffre le dépôt et exige une passphrase.
	EncryptionRepokey Encryption = "repokey-blake2"
	// EncryptionNone n'utilise aucun secret.
	EncryptionNone Encryption = "none"
)

// BorgMode traduit le mode de chiffrement tel que Borg l'attend sur la ligne
// de commande de « borg init ».
func (e Encryption) BorgMode() string { return string(e) }

// Encrypted indique si le mode exige une passphrase.
func (e Encryption) Encrypted() bool { return e != EncryptionNone }

// Config est le contenu complet du fichier.
type Config struct {
	Profiles []Profile `toml:"profile"`
}

// Profile décrit une destination et ce qui y est sauvegardé. Un poste n'a
// qu'un profil ; le format en accepte plusieurs pour ne pas figer le fichier.
type Profile struct {
	Name string `toml:"name"`

	Destination Destination `toml:"destination"`

	// Encryption est le mode constaté du dépôt. Il est renseigné à la création
	// et rafraîchi depuis « borg info --json » ; il n'est jamais redemandé à
	// l'utilisateur (EF-34).
	Encryption Encryption `toml:"encryption"`

	Sources  []string `toml:"sources"`
	Excludes []string `toml:"excludes,omitempty"`

	ExcludeCaches bool `toml:"exclude_caches"`
	OneFileSystem bool `toml:"one_file_system"`

	// IncludeCloudPlaceholders inclut les fichiers « à la demande » des
	// services de stockage en ligne. Faux par défaut : les lire déclenche
	// leur téléchargement complet (addendum §6.1).
	IncludeCloudPlaceholders bool `toml:"include_cloud_placeholders"`

	// Compression est la valeur transmise à Borg (lz4, zstd,3, zstd,9).
	Compression string `toml:"compression"`

	Retention Retention `toml:"retention"`
	Schedule  Schedule  `toml:"schedule"`
}

// Destination décrit où sont déposées les sauvegardes.
type Destination struct {
	Kind Kind `toml:"kind"`

	// User est le sous-compte Hetzner (uXXXXXX), inutilisé en mode ssh.
	User string `toml:"user"`
	// Repo est le nom du dépôt en mode hetzner, l'URL complète en mode ssh.
	Repo string `toml:"repo"`
	// RemotePath est transmis à toutes les commandes Borg (EF-22).
	RemotePath string `toml:"remote_path"`

	// SSHKey remplace la clé par défaut de l'application quand il est
	// renseigné.
	SSHKey string `toml:"ssh_key,omitempty"`

	// UploadRateLimit borne le débit montant en kio/s, 0 pour illimité.
	UploadRateLimit int `toml:"upload_ratelimit,omitempty"`
}

// Retention exprime la profondeur d'historique conservée.
type Retention struct {
	Daily   int `toml:"daily"`
	Weekly  int `toml:"weekly"`
	Monthly int `toml:"monthly"`
}

// Schedule décrit le déclenchement automatique (EF-61).
type Schedule struct {
	// Kind vaut manual, daily ou weekly.
	Kind string `toml:"kind"`
	// At est l'heure de déclenchement, « HH:MM ».
	At string `toml:"at,omitempty"`
	// Day est le jour d'une planification hebdomadaire (monday…sunday).
	Day string `toml:"day,omitempty"`
	// CatchUpIfMissed rattrape au démarrage suivant une exécution manquée
	// (EF-62).
	CatchUpIfMissed bool `toml:"catch_up_if_missed"`
}

// ErrNoProfile signale une configuration sans profil exploitable.
var ErrNoProfile = errors.New("config: aucun profil dans la configuration")

// Default construit un profil neuf avec les valeurs recommandées par le
// cahier des charges : rétention 7/4/6, compression équilibrée, respect de
// CACHEDIR.TAG.
func Default(name string) Profile {
	return Profile{
		Name:       name,
		Encryption: EncryptionRepokey,
		Destination: Destination{
			Kind:       KindHetzner,
			RemotePath: DefaultRemotePath,
		},
		ExcludeCaches: true,
		OneFileSystem: defaultOneFileSystem,
		Compression:   "zstd,3",
		Retention:     Retention{Daily: 7, Weekly: 4, Monthly: 6},
		Schedule:      Schedule{Kind: "manual", CatchUpIfMissed: true},
	}
}

// DefaultRemotePath est la version de Borg à invoquer côté Storage Box.
// Hetzner en installe deux et recommande de toujours l'expliciter.
const DefaultRemotePath = "borg-1.4"

// Load lit le fichier de configuration. Un chemin vide utilise l'emplacement
// standard de la plateforme.
func Load(path string) (*Config, error) {
	if path == "" {
		var err error
		if path, err = DefaultPath(); err != nil {
			return nil, err
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: lecture de %s: %w", path, err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: %s: %w", path, err)
	}
	if len(cfg.Profiles) == 0 {
		return nil, fmt.Errorf("config: %s: %w", path, ErrNoProfile)
	}
	return &cfg, nil
}

// Save écrit la configuration de façon atomique : un fichier de sauvegarde
// tronqué par une coupure de courant coûterait la configuration du poste.
func Save(path string, cfg *Config) error {
	if path == "" {
		var err error
		if path, err = DefaultPath(); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("config: création du dossier: %w", err)
	}

	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("config: encodage: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return fmt.Errorf("config: fichier temporaire: %w", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("config: écriture: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("config: synchronisation: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: fermeture: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return fmt.Errorf("config: permissions: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("config: remplacement de %s: %w", path, err)
	}
	return nil
}

// Profile retourne le profil portant ce nom, ou le premier si name est vide.
func (c *Config) Profile(name string) (*Profile, error) {
	if len(c.Profiles) == 0 {
		return nil, ErrNoProfile
	}
	if name == "" {
		return &c.Profiles[0], nil
	}
	for i := range c.Profiles {
		if c.Profiles[i].Name == name {
			return &c.Profiles[i], nil
		}
	}
	return nil, fmt.Errorf("config: profil %q introuvable", name)
}

// RepositoryURL construit l'URL du dépôt.
//
// En mode Hetzner, l'hôte, le port 23 et le chemin relatif « /./ » sont
// déduits du sous-compte (EF-21) : seul /home est inscriptible sur une Storage
// Box, d'où le chemin relatif, dont l'oubli est une cause classique d'échec.
func (d Destination) RepositoryURL() (string, error) {
	switch d.Kind {
	case KindHetzner:
		if d.User == "" {
			return "", errors.New("config: sous-compte Hetzner manquant")
		}
		if d.Repo == "" {
			return "", errors.New("config: nom du dépôt manquant")
		}
		return fmt.Sprintf("ssh://%s@%s.your-storagebox.de:23/./%s", d.User, d.User, d.Repo), nil
	case KindSSH:
		if d.Repo == "" {
			return "", errors.New("config: URL du dépôt manquante")
		}
		return d.Repo, nil
	default:
		return "", fmt.Errorf("config: type de destination inconnu %q", d.Kind)
	}
}
