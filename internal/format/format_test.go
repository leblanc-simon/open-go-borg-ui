package format

import (
	"fmt"
	"testing"
	"time"
)

// echo rend la clé et ses paramètres, pour vérifier le choix de la clé
// sans dépendre du catalogue.
func echo(key string, data ...any) string {
	if key == "relative.date_layout" {
		return "02/01"
	}
	if len(data) == 0 || data[0] == nil {
		return key
	}
	return fmt.Sprintf("%s %v", key, data[0])
}

func TestAgo(t *testing.T) {
	now := time.Date(2026, 9, 24, 22, 0, 0, 0, time.Local)
	cases := []struct {
		then time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "relative.just_now"},
		{now.Add(-5 * time.Minute), "relative.minutes map[Count:5]"},
		{now.Add(-9 * time.Hour), "relative.hours map[Count:9]"},
		{now.Add(-30 * time.Hour), "relative.yesterday"},
		{now.Add(-3 * 24 * time.Hour), "relative.days map[Count:3]"},
		{now.Add(-10 * 24 * time.Hour), "relative.date map[Date:14/09 Time:22:00]"},
	}
	for _, c := range cases {
		if got := Ago(echo, c.then, now); got != c.want {
			t.Errorf("Ago(%v): %q, attendu %q", now.Sub(c.then), got, c.want)
		}
	}
}

func TestWhen(t *testing.T) {
	now := time.Date(2026, 9, 24, 9, 0, 0, 0, time.Local)
	cases := []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, 9, 24, 22, 0, 0, 0, time.Local), "relative.today map[Time:22:00]"},
		{time.Date(2026, 9, 25, 1, 30, 0, 0, time.Local), "relative.tomorrow map[Time:01:30]"},
		{time.Date(2026, 9, 23, 23, 0, 0, 0, time.Local), "relative.yesterday_at map[Time:23:00]"},
		{time.Date(2026, 9, 30, 22, 0, 0, 0, time.Local), "relative.date map[Date:30/09 Time:22:00]"},
	}
	for _, c := range cases {
		if got := When(echo, c.at, now); got != c.want {
			t.Errorf("When(%v): %q, attendu %q", c.at, got, c.want)
		}
	}
}
