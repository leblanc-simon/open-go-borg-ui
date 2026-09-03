// Package i18n expose le catalogue de traductions de l'application.
//
// Les libellés ne sont jamais écrits en dur : le code manipule des clés en
// anglais structurées « écran.élément » (runtime.downloading, connection.ok…)
// que le catalogue résout dans la langue courante. Les segments transverses
// (erreurs, statuts, durées) vivent sous leurs propres racines.
//
// Le catalogue est embarqué dans le binaire : l'application doit rester
// utilisable sans aucun fichier annexe.
package i18n

import (
	"embed"
	"os"
	"strings"

	base "leblanc.io/open-go-base/i18n"
)

//go:embed locales/*.yaml
var localesFS embed.FS

// defaultLanguage est la langue de repli du catalogue. Le français est la
// langue par défaut de l'application (EI-06) ; l'anglais reste la langue
// pivot des clés.
const defaultLanguage = "fr"

// Localizer traduit une clé dans la langue résolue.
type Localizer = base.Localizer

// Load construit le localizer pour la langue demandée. force contient un tag
// BCP 47 explicite (option de ligne de commande) ou la chaîne vide, auquel cas
// la langue est déduite de l'environnement, puis du défaut.
func Load(force string) (*Localizer, error) {
	bundle, err := base.NewFS(localesFS, "locales", defaultLanguage)
	if err != nil {
		return nil, err
	}
	return bundle.Localizer(force, envLanguage()), nil
}

// MustLoad appelle Load et s'arrête sur erreur : un catalogue embarqué
// illisible est un défaut de construction, pas une erreur d'exécution.
func MustLoad(force string) *Localizer {
	loc, err := Load(force)
	if err != nil {
		panic(err)
	}
	return loc
}

// envLanguage traduit les variables POSIX de locale (fr_FR.UTF-8) en une liste
// de préférences comparable à un en-tête Accept-Language.
func envLanguage() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		value := os.Getenv(key)
		if value == "" || value == "C" || value == "POSIX" {
			continue
		}
		if i := strings.IndexAny(value, ".@"); i >= 0 {
			value = value[:i]
		}
		return strings.ReplaceAll(value, "_", "-")
	}
	return ""
}
