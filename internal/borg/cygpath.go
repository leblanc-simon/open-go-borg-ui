package borg

import (
	"fmt"
	"strings"
	"unicode"
)

// La convention de chemins Windows est figée et très coûteuse à changer une
// fois des dépôts en production (§7 de l'addendum).
//
// « borg create » s'exécute avec le répertoire courant sur /cygdrive et reçoit
// des chemins relatifs préfixés de la lettre de lecteur :
//
//	C:\Users\marc\Documents  ->  c/Users/marc/Documents
//
// L'archive contient donc « c/Users/marc/Documents/… ». La lettre de lecteur
// est préservée, ce qui lève toute ambiguïté sur un poste multi-disques, et
// aucun préfixe « cygdrive » ne pollue l'archive : elle reste lisible par un
// Borg officiel sous Linux. La restauration se fait depuis la racine du
// lecteur avec « --strip-components 1 ».

// cygdriveRoot est le point de montage des lecteurs dans l'espace de noms
// Cygwin, et le répertoire courant des sauvegardes.
const cygdriveRoot = "/cygdrive"

// toCygwinPath traduit un chemin Windows absolu en chemin Cygwin absolu. Sert
// aux chemins de service — clé SSH, known_hosts, fichier d'exclusions — que
// Borg ouvre lui-même, jamais aux chemins stockés dans une archive.
func toCygwinPath(path string) string {
	path = stripLongPathPrefix(path)
	if drive, rest, ok := splitDrive(path); ok {
		return cygdriveRoot + "/" + drive + joinRest(rest)
	}
	return strings.ReplaceAll(path, `\`, "/")
}

// toDriveRelative traduit un chemin Windows absolu dans la forme stockée en
// archive : lettre de lecteur en première composante, sans préfixe.
func toDriveRelative(path string) (string, error) {
	path = stripLongPathPrefix(path)
	if strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//") {
		return "", fmt.Errorf("borg: chemin réseau non pris en charge: %s", path)
	}
	drive, rest, ok := splitDrive(path)
	if !ok {
		return "", fmt.Errorf("borg: chemin absolu avec lettre de lecteur attendu: %s", path)
	}
	return drive + joinRest(rest), nil
}

// driveLetter retourne la lettre de lecteur d'un chemin Windows absolu, en
// minuscule.
func driveLetter(path string) (string, error) {
	drive, _, ok := splitDrive(stripLongPathPrefix(path))
	if !ok {
		return "", fmt.Errorf("borg: chemin absolu avec lettre de lecteur attendu: %s", path)
	}
	return drive, nil
}

// driveRoot retourne la racine du lecteur, répertoire courant d'une extraction.
func driveRoot(drive string) string {
	return strings.ToUpper(drive) + `:\`
}

// splitDrive isole la lettre de lecteur du reste du chemin.
func splitDrive(path string) (drive, rest string, ok bool) {
	if len(path) < 2 || path[1] != ':' {
		return "", "", false
	}
	letter := rune(path[0])
	if !unicode.IsLetter(letter) {
		return "", "", false
	}
	return strings.ToLower(string(letter)), path[2:], true
}

// joinRest normalise la partie du chemin qui suit la lettre de lecteur : des
// barres obliques, aucun séparateur redondant, aucune barre finale.
func joinRest(rest string) string {
	rest = strings.ReplaceAll(rest, `\`, "/")
	segments := make([]string, 0, 8)
	for _, segment := range strings.Split(rest, "/") {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	if len(segments) == 0 {
		return ""
	}
	return "/" + strings.Join(segments, "/")
}

// stripLongPathPrefix retire le préfixe des chemins longs de Windows. Les
// dossiers profonds en dépassent régulièrement la limite historique de 260
// caractères, et le préfixe n'a aucun sens pour Cygwin.
func stripLongPathPrefix(path string) string {
	for _, prefix := range []string{`\\?\UNC\`, `\\?\`} {
		if strings.HasPrefix(path, prefix) {
			trimmed := strings.TrimPrefix(path, prefix)
			if prefix == `\\?\UNC\` {
				return `\\` + trimmed
			}
			return trimmed
		}
	}
	return path
}

// fromDriveRelative traduit un chemin d'archive, lettre de lecteur en
// première composante, en chemin Windows absolu : c/Users/marc devient
// C:\Users\marc. Il retourne aussi la racine du lecteur, d'où l'extraire.
func fromDriveRelative(archivePath string) (native, root string, err error) {
	segments := strings.Split(strings.Trim(archivePath, "/"), "/")
	drive := segments[0]
	if len(drive) != 1 || !unicode.IsLetter(rune(drive[0])) {
		return "", "", fmt.Errorf("borg: chemin d'archive sans lettre de lecteur: %s", archivePath)
	}
	root = driveRoot(drive)
	return root + strings.Join(segments[1:], `\`), root, nil
}
