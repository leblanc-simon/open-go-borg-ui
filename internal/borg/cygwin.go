package borg

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// CygwinRunner pilote le runtime Borg téléchargé sous Windows.
//
// Le runtime est une installation Cygwin autonome : bash, Python, OpenSSH et
// le code officiel de Borg, non modifié. Les commandes sont enveloppées dans
// ce bash pour une raison précise : la convention de chemins impose que
// « borg create » s'exécute depuis /cygdrive, répertoire qui n'existe que dans
// l'espace de noms Cygwin et qu'un processus Windows ne peut donc pas prendre
// comme répertoire courant.
type CygwinRunner struct {
	// root est le dossier du runtime, racine du système de fichiers Cygwin.
	root string
	// shell est le bash.exe du runtime, dans sa forme Windows.
	shell string
	// borgPath est l'exécutable Borg dans sa forme Cygwin, relative à root.
	borgPath string
}

// shellCandidates et engineCandidates listent les emplacements admis dans le
// runtime, du plus probable au moins probable.
var (
	shellCandidates  = []string{`bin\bash.exe`, `usr\bin\bash.exe`}
	engineCandidates = []string{`bin\borg.exe`, `bin\borg`, `usr\bin\borg.exe`, `usr\bin\borg`}
)

// NewCygwinRunner vérifie qu'un runtime exploitable se trouve dans root.
func NewCygwinRunner(root string) (*CygwinRunner, error) {
	if root == "" {
		return nil, fmt.Errorf("%w: aucun runtime installé", ErrEngineMissing)
	}
	shell, err := locate(root, shellCandidates)
	if err != nil {
		return nil, err
	}
	engine, err := locate(root, engineCandidates)
	if err != nil {
		return nil, err
	}
	return newCygwinRunner(root, shell, engine), nil
}

// newCygwinRunner assemble le runner sans toucher au disque.
func newCygwinRunner(root, shell, engine string) *CygwinRunner {
	return &CygwinRunner{
		root:     root,
		shell:    shell,
		borgPath: runtimeRelativePath(root, engine),
	}
}

// locate cherche le premier candidat présent dans le runtime.
func locate(root string, candidates []string) (string, error) {
	for _, candidate := range candidates {
		full := filepath.Join(root, filepath.FromSlash(strings.ReplaceAll(candidate, `\`, "/")))
		if _, err := os.Stat(full); err == nil {
			return full, nil
		}
	}
	return "", fmt.Errorf("%w: %s introuvable dans %s", ErrEngineMissing, candidates[0], root)
}

// runtimeRelativePath traduit un chemin du runtime en chemin Cygwin absolu.
//
// La racine Cygwin d'une installation autonome est le dossier qui contient
// cygwin1.dll : les chemins internes s'y rapportent. Le découpage est fait à
// la main plutôt qu'avec filepath, dont le comportement dépend du système où
// tourne le programme et non de celui qu'il décrit.
func runtimeRelativePath(root, full string) string {
	normalize := func(s string) string { return strings.TrimRight(strings.ReplaceAll(s, `\`, "/"), "/") }

	normalizedRoot, normalizedFull := normalize(root), normalize(full)
	if rest, ok := strings.CutPrefix(normalizedFull, normalizedRoot+"/"); ok {
		return "/" + rest
	}
	// Chemin hors du runtime : seul son nom a du sens dans l'espace Cygwin.
	return "/" + path.Base(normalizedFull)
}

// Executable retourne l'exécutable Borg du runtime.
func (r *CygwinRunner) Executable() string { return r.borgPath }

// Run exécute la commande à travers le shell du runtime.
func (r *CygwinRunner) Run(ctx context.Context, cmd Command) (*Result, error) {
	inv, err := r.invocation(cmd)
	if err != nil {
		return nil, err
	}
	return run(ctx, inv, cmd.OnEvent)
}

// Version interroge le runtime.
func (r *CygwinRunner) Version(ctx context.Context) (string, error) {
	inv, err := r.invocation(Command{Name: "--version"})
	if err != nil {
		return "", err
	}
	out, err := output(ctx, inv)
	if err != nil {
		return "", err
	}
	return parseVersion(out)
}

// invocation construit l'appel au shell du runtime.
func (r *CygwinRunner) invocation(cmd Command) (invocation, error) {
	script, err := r.script(cmd)
	if err != nil {
		return invocation{}, err
	}

	env := cmd.Env.environ(toCygwinPath)
	env = append(env, "PATH="+r.binDir()+string(os.PathListSeparator)+os.Getenv("PATH"))

	return invocation{
		Path: r.shell,
		Args: []string{"-c", script},
		// Le répertoire courant côté Windows n'a pas d'importance : le script
		// se place lui-même où la convention l'exige. La racine du runtime est
		// choisie parce qu'elle existe toujours.
		Dir:     r.root,
		Env:     env,
		Display: script,
	}, nil
}

// script produit la ligne de shell exécutée dans le runtime.
func (r *CygwinRunner) script(cmd Command) (string, error) {
	workDir, sources, flags, err := r.convert(cmd)
	if err != nil {
		return "", err
	}

	converted := cmd
	converted.Flags = flags
	args := commonArgs(converted)
	args = append(args, sources...)

	quoted := make([]string, 0, len(args)+1)
	quoted = append(quoted, shellQuote(r.borgPath))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}

	// exec évite un processus de shell résident entre l'application et Borg :
	// une demande d'arrêt atteint alors directement Borg.
	return fmt.Sprintf("cd %s && exec %s", shellQuote(workDir), strings.Join(quoted, " ")), nil
}

// convert applique la convention de chemins de la commande et retourne le
// répertoire de travail Cygwin, les chemins convertis et les options ajustées.
func (r *CygwinRunner) convert(cmd Command) (workDir string, sources []string, flags []string, err error) {
	flags = cmd.Flags

	switch cmd.PathMode {
	case PathSources:
		// Depuis /cygdrive, un chemin relatif commence par la lettre de
		// lecteur : c'est elle qui est stockée dans l'archive.
		workDir = cygdriveRoot
		sources = make([]string, 0, len(cmd.Sources))
		for _, source := range cmd.Sources {
			relative, convErr := toDriveRelative(source)
			if convErr != nil {
				return "", nil, nil, convErr
			}
			sources = append(sources, relative)
		}

	case PathExtract:
		// Les chemins sont ici ceux stockés dans l'archive, déjà préfixés de
		// la lettre de lecteur : ils ne subissent aucune conversion. Seule la
		// destination en subit une, et la lettre est retirée à l'écriture.
		if cmd.Dir == "" {
			return "", nil, nil, fmt.Errorf("borg: destination d'extraction manquante")
		}
		workDir = toCygwinPath(cmd.Dir)
		sources = cmd.Sources
		flags = append(append([]string{}, flags...), "--strip-components", "1")

	default:
		workDir = "/"
		sources = cmd.Sources
		if cmd.Dir != "" {
			workDir = toCygwinPath(cmd.Dir)
		}
	}
	return workDir, sources, flags, nil
}

// binDir retourne le dossier des exécutables du runtime, ajouté au PATH pour
// que Borg trouve ssh.exe et les bibliothèques Cygwin.
func (r *CygwinRunner) binDir() string {
	return filepath.Join(r.root, "bin")
}
