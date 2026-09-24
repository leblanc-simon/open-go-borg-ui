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

// Ago rend l'ancienneté d'un instant passé, à l'instant now : « il y a
// 9 h ». Au-delà d'une semaine, la date elle-même parle mieux.
func Ago(t Translate, then, now time.Time) string {
	elapsed := now.Sub(then)
	switch {
	case elapsed < time.Minute:
		return t("relative.just_now")
	case elapsed < time.Hour:
		return t("relative.minutes", map[string]any{"Count": int(elapsed / time.Minute)})
	case elapsed < 24*time.Hour:
		return t("relative.hours", map[string]any{"Count": int(elapsed / time.Hour)})
	case elapsed < 48*time.Hour:
		return t("relative.yesterday")
	case elapsed < 7*24*time.Hour:
		return t("relative.days", map[string]any{"Count": int(elapsed / (24 * time.Hour))})
	default:
		return When(t, then, now)
	}
}

// When rend un instant proche par rapport au jour de now : « aujourd'hui,
// 22:00 », « demain, 22:00 », sinon la date. La forme de la date vient du
// catalogue.
func When(t Translate, at, now time.Time) string {
	at, now = at.Local(), now.Local()
	data := map[string]any{"Time": at.Format("15:04")}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, at.Location()); {
	case day.Equal(today):
		return t("relative.today", data)
	case day.Equal(today.AddDate(0, 0, 1)):
		return t("relative.tomorrow", data)
	case day.Equal(today.AddDate(0, 0, -1)):
		return t("relative.yesterday_at", data)
	default:
		data["Date"] = at.Format(t("relative.date_layout"))
		return t("relative.date", data)
	}
}
