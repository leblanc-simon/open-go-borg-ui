package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os/exec"

	"leblanc.io/open-go-borg-ui/internal/borg"
	"leblanc.io/open-go-borg-ui/internal/borgruntime"
)

// commandRuntime installe ou inspecte le moteur de sauvegarde.
func (a *app) commandRuntime(ctx context.Context, args []string) (int, error) {
	action, rest := subcommand(args)
	switch action {
	case "", "status":
		return a.runtimeStatus(ctx)
	case "install":
		return a.runtimeInstall(ctx, rest)
	default:
		return exitError, fmt.Errorf("%s", a.T("runtime.usage"))
	}
}

// runtimeStatus rend compte du moteur disponible.
func (a *app) runtimeStatus(ctx context.Context) (int, error) {
	manager := a.RuntimeManager()
	fmt.Println(a.T("runtime.pinned", map[string]any{
		"Version": manager.Spec().BorgVersion,
	}))

	runner, err := a.Runner()
	if err != nil {
		return a.reportMissingEngine(err)
	}

	version, err := runner.Version(ctx)
	if err != nil {
		return exitError, err
	}

	fmt.Println(a.T("runtime.installed", map[string]any{
		"Version": version,
		"Path":    runner.Executable(),
	}))
	if version != manager.Spec().BorgVersion {
		// Une version différente n'est pas bloquante en v0.1, mais elle doit
		// être visible : le format de dépôt et le comportement de Borg en
		// dépendent.
		fmt.Println(a.T("runtime.version_differs", map[string]any{
			"Expected": manager.Spec().BorgVersion,
			"Found":    version,
		}))
		return exitWarning, nil
	}
	return exitSuccess, nil
}

// reportMissingEngine explique l'absence de moteur et l'action à mener.
func (a *app) reportMissingEngine(err error) (int, error) {
	if !errors.Is(err, borg.ErrEngineMissing) && !errors.Is(err, borgruntime.ErrNotInstalled) {
		return exitError, err
	}
	if isWindows() {
		return exitError, fmt.Errorf("%s", a.T("runtime.missing_windows"))
	}
	// Sous Linux, Borg vient de la distribution : l'application indique la
	// commande d'installation plutôt que de télécharger quoi que ce soit
	// (EF-08).
	return exitError, fmt.Errorf("%s", a.T("runtime.missing_linux", map[string]any{
		"Command": installHint(),
	}))
}

// runtimeInstall installe le runtime sous Windows.
func (a *app) runtimeInstall(ctx context.Context, args []string) (int, error) {
	var archive string
	flags := flag.NewFlagSet("runtime install", flag.ContinueOnError)
	flags.StringVar(&archive, "archive", "",
		"archive déjà téléchargée, pour l'installation hors ligne")
	if err := flags.Parse(args); err != nil {
		return exitError, err
	}

	if !isWindows() {
		return exitError, fmt.Errorf("%s", a.T("runtime.install_linux", map[string]any{
			"Command": installHint(),
		}))
	}

	manager := a.RuntimeManager()
	var (
		path string
		err  error
	)
	if archive != "" {
		fmt.Println(a.T("runtime.installing_offline", map[string]any{"Archive": archive}))
		path, err = manager.InstallFromArchive(ctx, archive)
	} else {
		if !manager.Spec().Published() {
			// Tant que l'archive du runtime n'est pas publiée, seule la voie
			// hors ligne existe : le dire franchement vaut mieux qu'un échec
			// réseau incompréhensible.
			return exitError, fmt.Errorf("%s", a.T("runtime.not_published"))
		}
		path, err = manager.Install(ctx, a.progressBar())
	}
	if err != nil {
		if errors.Is(err, borgruntime.ErrChecksumMismatch) {
			return exitError, fmt.Errorf("%s", a.T("runtime.checksum_mismatch"))
		}
		return exitError, err
	}

	// L'installation n'est déclarée réussie qu'après vérification
	// fonctionnelle du moteur (EF-05).
	runner, err := borg.NewCygwinRunner(path)
	if err != nil {
		return exitError, err
	}
	version, err := runner.Version(ctx)
	if err != nil {
		return exitError, err
	}
	if version != manager.Spec().BorgVersion {
		return exitError, fmt.Errorf("%s", a.T("runtime.version_unexpected", map[string]any{
			"Expected": manager.Spec().BorgVersion,
			"Found":    version,
		}))
	}

	fmt.Println(a.T("runtime.installed", map[string]any{"Version": version, "Path": path}))
	return exitSuccess, nil
}

// progressBar rend l'avancement d'un téléchargement sur la sortie d'erreur.
func (a *app) progressBar() func(downloaded, total int64) {
	return func(downloaded, total int64) {
		if total > 0 {
			progressLine(a.T("runtime.downloading", map[string]any{
				"Downloaded": formatSize(downloaded),
				"Total":      formatSize(total),
				"Percent":    fmt.Sprintf("%.0f", float64(downloaded)/float64(total)*100),
			}))
			return
		}
		progressLine(a.T("runtime.downloading_unknown", map[string]any{
			"Downloaded": formatSize(downloaded),
		}))
	}
}

// installHint retourne la commande d'installation de Borg propre à la
// distribution détectée.
func installHint() string {
	managers := []struct {
		binary  string
		command string
	}{
		{"apt", "sudo apt install borgbackup"},
		{"dnf", "sudo dnf install borgbackup"},
		{"zypper", "sudo zypper install borgbackup"},
		{"pacman", "sudo pacman -S borg"},
		{"apk", "sudo apk add borgbackup"},
	}
	for _, manager := range managers {
		if _, err := exec.LookPath(manager.binary); err == nil {
			return manager.command
		}
	}
	return "borgbackup"
}
