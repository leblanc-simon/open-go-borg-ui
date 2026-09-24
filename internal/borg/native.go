package borg

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// NativeRunner pilote un Borg installé sur le système, sous Linux.
//
// Les chemins n'y subissent aucune traduction : ce sont ceux du système, et
// l'archive les stocke tels quels, sans leur barre oblique initiale.
type NativeRunner struct {
	executable string
}

// NewNativeRunner localise l'exécutable Borg. Un chemin vide déclenche une
// recherche dans le PATH.
func NewNativeRunner(executable string) (*NativeRunner, error) {
	if executable == "" {
		found, err := exec.LookPath("borg")
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrEngineMissing, err)
		}
		executable = found
	}
	return &NativeRunner{executable: executable}, nil
}

// Executable retourne le chemin de l'exécutable Borg utilisé.
func (r *NativeRunner) Executable() string { return r.executable }

// Run exécute la commande.
func (r *NativeRunner) Run(ctx context.Context, cmd Command) (*Result, error) {
	cmd, cleanup, err := withExcludeFile(cmd, nativeArchivePath, nativePath)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	inv, err := r.invocation(cmd)
	if err != nil {
		return nil, err
	}
	return run(ctx, inv, cmd.OnEvent)
}

// Version interroge l'exécutable.
func (r *NativeRunner) Version(ctx context.Context) (string, error) {
	out, err := output(ctx, invocation{
		Path:    r.executable,
		Args:    []string{"--version"},
		Env:     Environment{}.environ(nativePath, nil),
		Display: r.executable + " --version",
	})
	if err != nil {
		return "", err
	}
	return parseVersion(out)
}

// invocation construit la commande système.
func (r *NativeRunner) invocation(cmd Command) (invocation, error) {
	args := commonArgs(cmd)
	args = append(args, cmd.Sources...)

	return invocation{
		Path:    r.executable,
		Args:    args,
		Dir:     cmd.Dir,
		Env:     cmd.Env.environ(nativePath, nil),
		Display: r.executable + " " + strings.Join(args, " "),
	}, nil
}

// nativePath laisse les chemins inchangés.
func nativePath(path string) string { return path }

// nativeArchivePath donne la forme qu'un chemin absolu prend dans l'archive :
// Borg y retire la barre oblique initiale.
func nativeArchivePath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("borg: chemin absolu attendu: %s", path)
	}
	return strings.TrimPrefix(filepath.ToSlash(filepath.Clean(path)), "/"), nil
}

// Origin rend son « / » initial à un chemin d'archive ; l'extraction à
// l'emplacement d'origine part de la racine du système.
func (r *NativeRunner) Origin(archivePath string) (native, root string, err error) {
	clean := strings.Trim(archivePath, "/")
	if clean == "" {
		return "", "", fmt.Errorf("borg: chemin d'archive vide")
	}
	return "/" + clean, "/", nil
}
