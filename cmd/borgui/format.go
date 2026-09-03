package main

import (
	"fmt"
	"time"
)

// units énumère les multiples binaires, symboles internationaux et donc non
// traduits.
var units = []string{"o", "Kio", "Mio", "Gio", "Tio"}

// formatSize rend une taille lisible.
func formatSize(bytes int64) string {
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

// formatDuration rend une durée dans la langue courante. Le découpage en
// heures, minutes et secondes vient du catalogue : sa forme varie d'une langue
// à l'autre et n'a pas sa place dans le code.
func (a *app) formatDuration(d time.Duration) string {
	seconds := int(d.Round(time.Second).Seconds())
	switch {
	case seconds >= 3600:
		return a.T("duration.hours_minutes", map[string]any{
			"Hours":   seconds / 3600,
			"Minutes": (seconds % 3600) / 60,
		})
	case seconds >= 60:
		return a.T("duration.minutes_seconds", map[string]any{
			"Minutes": seconds / 60,
			"Seconds": seconds % 60,
		})
	default:
		return a.T("duration.seconds", map[string]any{"Seconds": seconds})
	}
}

// formatRate rend un débit moyen. Le second retour vaut false quand la durée
// est trop courte pour qu'une moyenne ait un sens.
func formatRate(bytes int64, d time.Duration) (string, bool) {
	if d < time.Second || bytes <= 0 {
		return "", false
	}
	return formatSize(int64(float64(bytes)/d.Seconds())) + "/s", true
}
