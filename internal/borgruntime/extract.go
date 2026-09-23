package borgruntime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// extract décompresse l'archive .tar.gz du runtime dans dest.
//
// Le runtime est livré en tar et non en ZIP parce qu'un arbre Cygwin repose
// sur des centaines de liens symboliques, des dossiers vides (/tmp) et des
// liens durs, que ZIP ne transporte pas.
//
// Chaque entrée est vérifiée avant écriture : une archive dont un nom
// remonterait hors du dossier de destination est rejetée. L'archive est certes
// vérifiée par empreinte, mais la vérification ne dit rien de son contenu.
func extract(ctx context.Context, archive, dest string) error {
	file, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("runtime: ouverture de l'archive: %w", err)
	}
	defer file.Close()

	compressed, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("runtime: décompression de l'archive: %w", err)
	}
	defer compressed.Close()

	reader := tar.NewReader(compressed)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("runtime: lecture de l'archive: %w", err)
		}
		if err := extractEntry(reader, header, dest); err != nil {
			return err
		}
	}
}

// extractEntry écrit une entrée de l'archive.
//
// Les droits POSIX portés par l'archive n'ont pas de sens sous Windows : seul
// le bit d'exécution est conservé, et le propriétaire garde toujours le droit
// d'écriture, faute de quoi Windows marquerait le fichier en lecture seule et
// la désinstallation du runtime échouerait.
func extractEntry(reader io.Reader, header *tar.Header, dest string) error {
	target, err := safeJoin(dest, header.Name)
	if err != nil {
		return err
	}
	if target == "" {
		// Entrée « ./ » : la racine de l'archive, c'est-à-dire dest.
		return nil
	}

	switch header.Typeflag {
	case tar.TypeDir:
		// Les dossiers vides comptent : sans /tmp, bash ouvre chaque
		// commande par un avertissement et Borg n'a pas de dossier de
		// travail.
		if err := os.MkdirAll(target, 0o700); err != nil {
			return fmt.Errorf("runtime: création de %s: %w", target, err)
		}
		return nil

	case tar.TypeReg:
		if err := makeParent(target); err != nil {
			return err
		}
		mode := os.FileMode(header.Mode).Perm()&0o755 | 0o600
		return writeFile(target, reader, mode)

	case tar.TypeSymlink:
		if err := makeParent(target); err != nil {
			return err
		}
		if err := writeCygwinSymlink(target, header.Linkname); err != nil {
			return fmt.Errorf("runtime: lien %s: %w", header.Name, err)
		}
		return nil

	case tar.TypeLink:
		// La cible d'un lien dur est un nom de l'archive, soumis à la même
		// vérification que les autres.
		source, err := safeJoin(dest, header.Linkname)
		if err != nil {
			return err
		}
		if source == "" {
			return fmt.Errorf("runtime: lien dur %s vers la racine de l'archive", header.Name)
		}
		if err := makeParent(target); err != nil {
			return err
		}
		return hardLink(source, target)

	default:
		// Périphériques, FIFO, extensions propres à GNU tar : rien de tout
		// cela ne figure dans le runtime ni n'a de sens sous Windows.
		return fmt.Errorf("runtime: type d'entrée non pris en charge pour %s: %q", header.Name, header.Typeflag)
	}
}

// makeParent crée le dossier qui accueille target.
func makeParent(target string) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("runtime: création de %s: %w", dir, err)
	}
	return nil
}

// writeFile écrit le contenu d'une entrée dans target.
func writeFile(target string, content io.Reader, mode os.FileMode) error {
	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("runtime: écriture de %s: %w", target, err)
	}
	if _, err := io.Copy(file, content); err != nil {
		file.Close()
		return fmt.Errorf("runtime: écriture de %s: %w", target, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("runtime: écriture de %s: %w", target, err)
	}
	return nil
}

// hardLink recrée un lien dur, par exemple bin/gawk.exe vers
// bin/gawk-5.4.0.exe. NTFS les accepte sans privilège ; à défaut — volume
// FAT, partage réseau — une copie a le même effet pour Cygwin.
func hardLink(source, target string) error {
	if err := os.Link(source, target); err == nil {
		return nil
	}
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("runtime: lien dur vers %s: %w", source, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("runtime: lien dur vers %s, qui n'est pas un fichier", source)
	}
	file, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("runtime: lien dur vers %s: %w", source, err)
	}
	defer file.Close()
	return writeFile(target, file, info.Mode().Perm())
}

// cygwinSymlinkMagic ouvre tout fichier que Cygwin interprète comme un lien
// symbolique, à condition qu'il porte aussi l'attribut « système ».
const cygwinSymlinkMagic = "!<symlink>"

// cygwinSymlinkContent produit le contenu d'un lien Cygwin dans sa forme
// courante : la signature, une marque d'ordre UTF-16LE, la cible en UTF-16LE,
// puis une terminaison nulle. C'est la forme qu'écrit l'appel symlink() de
// Cygwin, et la seule qui accepte une cible non ASCII.
//
// La cible est reprise telle quel : c'est déjà un chemin POSIX, absolu
// (/etc/alternatives/python3) ou relatif (gawk.exe), que Cygwin résout dans
// son propre espace de noms.
func cygwinSymlinkContent(linkname string) []byte {
	var b bytes.Buffer
	b.WriteString(cygwinSymlinkMagic)
	b.Write([]byte{0xFF, 0xFE})
	for _, unit := range utf16.Encode([]rune(linkname)) {
		b.WriteByte(byte(unit))
		b.WriteByte(byte(unit >> 8))
	}
	b.Write([]byte{0x00, 0x00})
	return b.Bytes()
}

// writeCygwinSymlink écrit un lien symbolique tel que Cygwin le représente :
// un fichier ordinaire marqué « système ».
//
// De vrais liens NTFS demanderaient le privilège SeCreateSymbolicLinkPrivilege,
// donc des droits d'administrateur ou le mode développeur, alors que le runtime
// s'installe sans aucun des deux (TR-20). Hors de Windows, le même fichier est
// écrit sans attribut : c'est ce qui permet d'éprouver l'extraction dans les
// tests, et cela évite de créer un vrai lien qui pointerait hors du dossier.
func writeCygwinSymlink(target, linkname string) error {
	if linkname == "" {
		return errors.New("cible vide")
	}
	if err := os.WriteFile(target, cygwinSymlinkContent(linkname), 0o644); err != nil {
		return err
	}
	return markSystem(target)
}

// safeJoin construit le chemin de destination et refuse toute sortie du
// dossier d'extraction. Il retourne une chaîne vide pour la racine de
// l'archive elle-même (« ./ »).
func safeJoin(dest, name string) (string, error) {
	cleaned := path.Clean(strings.ReplaceAll(name, `\`, "/"))
	if cleaned == "." {
		return "", nil
	}
	if path.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, "../") ||
		filepath.VolumeName(filepath.FromSlash(cleaned)) != "" {
		return "", fmt.Errorf("runtime: entrée d'archive hors du dossier d'extraction: %s", name)
	}
	target := filepath.Join(dest, filepath.FromSlash(cleaned))
	if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
		return "", fmt.Errorf("runtime: entrée d'archive hors du dossier d'extraction: %s", name)
	}
	return target, nil
}
