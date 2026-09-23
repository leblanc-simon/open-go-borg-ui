// Package format rend lisibles les tailles, durées et débits, à l'identique
// dans la ligne de commande et dans l'interface.
package format

import (
	"fmt"
	"time"
)

// units énumère les multiples binaires, symboles internationaux et donc non
// traduits.
var units = []string{"o", "Kio", "Mio", "Gio", "Tio"}

// Size rend une taille lisible.
func Size(bytes int64) string {
	value := float64(bytes)
	index := 0
	for value >= 1024 && index < len(units)-1 {
		value /= 1024
		index++
	}
	if index == 0 {
		return fmt.Sprintf("%d %s", bytes, units[index])
	}
	return fmt.Sprintf("%.1f %s", value, units[index])
}

// Translate résout une clé du catalogue.
type Translate func(key string, data ...any) string

// Duration rend une durée dans la langue courante. Le découpage en heures,
// minutes et secondes vient du catalogue : sa forme varie d'une langue à
// l'autre et n'a pas sa place dans le code.
func Duration(t Translate, d time.Duration) string {
	seconds := int(d.Round(time.Second).Seconds())
	switch {
	case seconds >= 3600:
		return t("duration.hours_minutes", map[string]any{
			"Hours":   seconds / 3600,
			"Minutes": (seconds % 3600) / 60,
		})
	case seconds >= 60:
		return t("duration.minutes_seconds", map[string]any{
			"Minutes": seconds / 60,
			"Seconds": seconds % 60,
		})
	default:
		return t("duration.seconds", map[string]any{"Seconds": seconds})
	}
}

// Rate rend un débit moyen. Le second retour vaut false quand la durée est
// trop courte pour qu'une moyenne ait un sens.
func Rate(bytes int64, d time.Duration) (string, bool) {
	if d < time.Second || bytes <= 0 {
		return "", false
	}
	return Size(int64(float64(bytes)/d.Seconds())) + "/s", true
}
