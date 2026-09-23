// Commande borgui-recette outille la recette de la v0.1 (TR-01 à TR-04).
//
// Elle ne touche ni à Borg ni à la destination : elle prépare des données à
// sauvegarder et vérifie ce qui en revient. Trois sous-commandes :
//
//	borgui-recette generate <dossier> [--size <Mio>]
//	borgui-recette manifest <dossier> [-o <fichier>]
//	borgui-recette compare <manifeste> <dossier>
//
// Le manifeste est établi sur les données d'origine, avant la sauvegarde ;
// compare le confronte au dossier restauré, sur la même machine ou sur une
// autre. C'est ce qui prouve qu'un fichier restauré est identique bit pour bit
// à l'original (TR-04), y compris quand la restauration a été faite par le
// borg officiel d'une machine Linux (TR-01, TR-02).
//
// L'outil est en Go pur, sans CGO : il se compile pour toutes les plateformes
// depuis n'importe laquelle.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"leblanc.io/open-go-borg-ui/internal/i18n"
)

// Codes de sortie : une différence constatée n'est pas une erreur d'exécution.
const (
	exitSuccess    = 0
	exitDifference = 1
	exitError      = 2
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run exécute la sous-commande et retourne le code de sortie.
func run(args []string) int {
	var language string
	flags := flag.NewFlagSet("borgui-recette", flag.ContinueOnError)
	flags.StringVar(&language, "lang", "", "langue des messages")
	flags.Usage = func() {}
	if err := flags.Parse(args); err != nil {
		return exitError
	}

	loc, err := i18n.Load(language)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}
	t := func(key string, data ...any) string { return loc.T(key, data...) }

	rest := flags.Args()
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, t("recette.usage"))
		return exitError
	}

	var code int
	switch rest[0] {
	case "generate":
		code, err = commandGenerate(t, rest[1:])
	case "manifest":
		code, err = commandManifest(t, rest[1:])
	case "compare":
		code, err = commandCompare(t, rest[1:])
	default:
		fmt.Fprintln(os.Stderr, t("recette.usage"))
		return exitError
	}
	if err != nil {
		var usage usageError
		if errors.As(err, &usage) {
			fmt.Fprintln(os.Stderr, t("recette.usage"))
			return exitError
		}
		fmt.Fprintln(os.Stderr, t("cli.error", map[string]any{"Message": err.Error()}))
	}
	return code
}

// translate résout une clé du catalogue.
type translate func(key string, data ...any) string

// usageError signale des arguments incorrects.
type usageError struct{}

func (usageError) Error() string { return "usage" }

// parse analyse les options d'une sous-commande. Les options peuvent suivre
// les arguments positionnels, ce que le paquet flag n'admet pas seul.
func parse(flags *flag.FlagSet, args []string) ([]string, error) {
	flags.Usage = func() {}
	var positional []string
	for {
		if err := flags.Parse(args); err != nil {
			return nil, usageError{}
		}
		args = flags.Args()
		if len(args) == 0 {
			return positional, nil
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
}
