package station

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DesktopDir retourne le Bureau de l'utilisateur, où la restauration crée
// par défaut son dossier neuf (addendum §5.3). Faute de Bureau, c'est le
// dossier personnel.
//
// Sous Linux, le nom du Bureau dépend de la langue (« Bureau », « Desktop »)
// et se lit dans user-dirs.dirs, comme le fait xdg-user-dir.
func DesktopDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	candidates := []string{filepath.Join(home, "Desktop")}
	if runtime.GOOS != "windows" {
		if dir := xdgDesktop(home); dir != "" {
			candidates = append([]string{dir}, candidates...)
		}
	}
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	return home
}

// xdgDesktop lit XDG_DESKTOP_DIR dans user-dirs.dirs.
func xdgDesktop(home string) string {
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	file, err := os.Open(filepath.Join(config, "user-dirs.dirs"))
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		value, ok := strings.CutPrefix(line, "XDG_DESKTOP_DIR=")
		if !ok {
			continue
		}
		value = strings.Trim(value, `"`)
		value = strings.Replace(value, "$HOME", home, 1)
		if !filepath.IsAbs(value) {
			return ""
		}
		return filepath.Clean(value)
	}
	return ""
}
