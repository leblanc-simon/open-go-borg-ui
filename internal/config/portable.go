package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/pelletier/go-toml/v2"
)

// maxPortableSize borne la taille d'un fichier importé : une configuration
// légitime pèse quelques kilo-octets.
const maxPortableSize = 1 << 20

// ErrNotPortable signale un fichier importé inexploitable.
var ErrNotPortable = errors.New("config: fichier d'import invalide")

// Portable retourne la part d'un profil qui se transporte d'un poste à
// l'autre (EF-13).
//
// Aucun secret n'y figure, puisqu'aucun ne vit dans la configuration. Ce qui
// est propre au poste en est retiré : son sous-compte — un poste, un
// sous-compte (DT-08) —, une clé SSH particulière, et l'adresse complète d'une
// destination SSH quelconque, qui désigne ce sous-compte.
func Portable(profile Profile) Profile {
	portable := profile
	portable.Destination.User = ""
	portable.Destination.SSHKey = ""
	if portable.Destination.Kind == KindSSH {
		portable.Destination.Repo = ""
	}
	portable.Sources = append([]string(nil), profile.Sources...)
	portable.Excludes = append([]string(nil), profile.Excludes...)
	return portable
}

// Export sérialise un profil transportable, précédé d'un en-tête qui dit ce
// que le fichier contient et n'en contient pas.
func Export(profile Profile, header string) ([]byte, error) {
	data, err := toml.Marshal(&Config{Profiles: []Profile{Portable(profile)}})
	if err != nil {
		return nil, fmt.Errorf("config: encodage: %w", err)
	}
	var b bytes.Buffer
	for _, line := range bytes.Split([]byte(header), []byte("\n")) {
		b.WriteString("# ")
		b.Write(line)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	b.Write(data)
	return b.Bytes(), nil
}

// Import relit un profil exporté. name choisit le profil si le fichier en
// contient plusieurs ; vide, le premier. Le résultat est transportable même si
// le fichier ne l'était pas : ce qui est propre à l'autre poste n'est jamais
// repris.
func Import(r io.Reader, name string) (Profile, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxPortableSize+1))
	if err != nil {
		return Profile{}, err
	}
	if len(data) > maxPortableSize {
		return Profile{}, fmt.Errorf("%w: fichier trop volumineux", ErrNotPortable)
	}
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Profile{}, fmt.Errorf("%w: %v", ErrNotPortable, err)
	}
	profile, err := cfg.Profile(name)
	if err != nil {
		return Profile{}, fmt.Errorf("%w: %v", ErrNotPortable, err)
	}
	switch profile.Destination.Kind {
	case KindHetzner, KindSSH:
	default:
		return Profile{}, fmt.Errorf("%w: type de destination %q", ErrNotPortable, profile.Destination.Kind)
	}
	switch profile.Encryption {
	case EncryptionRepokey, EncryptionNone:
	default:
		return Profile{}, fmt.Errorf("%w: chiffrement %q", ErrNotPortable, profile.Encryption)
	}
	return Portable(*profile), nil
}
