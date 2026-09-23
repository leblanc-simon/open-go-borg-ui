// Commande borgui configure et exécute des sauvegardes BorgBackup.
//
// Cette version est celle du jalon v0.1 : aucune interface graphique, mais
// tout le chemin critique du projet — installation du moteur, connexion à la
// destination, création du dépôt dans les deux modes de chiffrement,
// sauvegarde, énumération des archives — de façon qu'il soit éprouvé avant
// qu'une ligne d'interface ne soit écrite.
//
// Un seul exécutable rend tous les services (AR-03) : la sauvegarde planifiée
// invoquera plus tard le même binaire, sans interface.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"leblanc.io/open-go-borg-ui/internal/i18n"
)

// Codes de sortie, alignés sur ceux de Borg : une exécution terminée avec des
// avertissements se distingue d'un échec.
const (
	exitSuccess = 0
	exitWarning = 1
	exitError   = 2
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run exécute la commande demandée et retourne le code de sortie.
func run(args []string) int {
	var (
		configPath  string
		profileName string
		language    string
		passphrase  string
	)

	flags := flag.NewFlagSet("borgui", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&configPath, "config", "", "chemin du fichier de configuration")
	flags.StringVar(&profileName, "profile", "", "nom du profil")
	flags.StringVar(&language, "lang", "", "langue de l'interface")
	flags.StringVar(&passphrase, "print-passphrase", "",
		"écrit la passphrase du profil sur la sortie standard, pour BORG_PASSCOMMAND")
	flags.Usage = func() {}

	if err := flags.Parse(args); err != nil {
		return exitError
	}

	loc, err := i18n.Load(language)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}

	application, err := newApp(loc, configPath, profileName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}

	// Mode passphrase : Borg exécute l'application pour lire le secret. La
	// sortie doit contenir la passphrase et rien d'autre.
	if passphrase != "" {
		return application.printPassphrase(passphrase)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	rest := flags.Args()
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, loc.T("cli.usage"))
		return exitError
	}

	code, err := application.dispatch(ctx, rest[0], rest[1:])
	if err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, loc.T("cli.cancelled"))
			return exitError
		}
		fmt.Fprintln(os.Stderr, loc.T("cli.error", map[string]any{"Message": err.Error()}))
		return code
	}
	return code
}

// dispatch aiguille vers la commande demandée.
func (a *app) dispatch(ctx context.Context, command string, args []string) (int, error) {
	switch command {
	case "config":
		return a.commandConfig(args)
	case "key":
		return a.commandKey(args)
	case "runtime":
		return a.commandRuntime(ctx, args)
	case "connection":
		return a.commandConnection(ctx, args)
	case "repository":
		return a.commandRepository(ctx, args)
	case "passphrase":
		return a.commandPassphrase(args)
	case "backup":
		return a.commandBackup(ctx, args)
	case "archives":
		return a.commandArchives(ctx, args)
	case "restore":
		return a.commandRestore(ctx, args)
	case "help", "--help", "-h":
		fmt.Println(a.T("cli.usage"))
		return exitSuccess, nil
	default:
		fmt.Fprintln(os.Stderr, a.T("cli.usage"))
		return exitError, fmt.Errorf("%s", a.T("cli.unknown_command", map[string]any{"Command": command}))
	}
}

// subcommand extrait la sous-commande d'une liste d'arguments.
func subcommand(args []string) (string, []string) {
	if len(args) == 0 {
		return "", nil
	}
	return args[0], args[1:]
}
