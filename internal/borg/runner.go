package borg

import (
	"fmt"
	"regexp"
	"runtime"
	"strings"
)

// New construit le Runner de la plateforme courante.
//
// engine désigne l'exécutable Borg sous Linux, ou le dossier du runtime
// téléchargé sous Windows. Vide, chacun applique sa recherche par défaut.
func New(engine string) (Runner, error) {
	if runtime.GOOS == "windows" {
		return NewCygwinRunner(engine)
	}
	return NewNativeRunner(engine)
}

// commonArgs construit la partie de la ligne de commande indépendante de la
// plateforme.
//
// L'ordre suit la forme documentée par Borg : les options globales précèdent
// la sous-commande, les options communes la suivent.
func commonArgs(cmd Command) []string {
	args := make([]string, 0, len(cmd.Flags)+8)
	if cmd.LogJSON {
		args = append(args, "--log-json")
	}
	args = append(args, cmd.Name)

	// Hetzner installe Borg 1.2 et 1.4 côté serveur et recommande de toujours
	// désigner la version voulue, faute de quoi le dépôt peut être ouvert par
	// la mauvaise (EF-22).
	if cmd.Env.RemotePath != "" {
		args = append(args, "--remote-path="+cmd.Env.RemotePath)
	}
	if cmd.Env.UploadRateLimit > 0 {
		args = append(args, fmt.Sprintf("--upload-ratelimit=%d", cmd.Env.UploadRateLimit))
	}

	args = append(args, cmd.Flags...)
	if cmd.Target != "" {
		args = append(args, cmd.Target)
	}
	return args
}

// versionPattern isole le numéro de version dans « borg 1.4.5 ».
var versionPattern = regexp.MustCompile(`(\d+\.\d+\.\d+[^\s]*)`)

// parseVersion extrait le numéro de version de la sortie de « borg --version ».
func parseVersion(out string) (string, error) {
	match := versionPattern.FindString(strings.TrimSpace(out))
	if match == "" {
		return "", fmt.Errorf("%w: sortie inattendue %q", ErrEngineUnusable, out)
	}
	return match, nil
}
