package main

import (
	"strings"

	"leblanc.io/open-go-borg-ui/internal/i18n"
	"leblanc.io/open-go-borg-ui/internal/station"
)

// app rassemble ce dont toutes les commandes ont besoin : les traductions et
// le poste, qui sait où vivent sa configuration et ses données et comment se
// construisent ses services.
type app struct {
	loc *i18n.Localizer
	*station.Station
}

// newApp construit le contexte applicatif.
func newApp(loc *i18n.Localizer, configPath, profileName string) (*app, error) {
	st, err := station.New(configPath, profileName)
	if err != nil {
		return nil, err
	}
	return &app{loc: loc, Station: st}, nil
}

// T traduit une clé.
func (a *app) T(key string, data ...any) string { return a.loc.T(key, data...) }

// isWindows indique si l'application tourne sous Windows.
func isWindows() bool { return strings.EqualFold(osName(), "windows") }
