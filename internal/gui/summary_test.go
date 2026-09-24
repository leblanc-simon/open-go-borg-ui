package gui

import (
	"testing"
	"time"

	"leblanc.io/open-go-borg-ui/internal/history"
)

var now = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

// run construit une exécution terminée il y a age.
func run(status history.Status, age time.Duration) history.Run {
	r := history.Run{Status: status, Started: now.Add(-age - time.Minute)}
	if status != history.StatusRunning {
		r.Finished = now.Add(-age)
	}
	return r
}

// TestSynthese fixe l'indicateur dominant de l'écran État (EF-80, EF-85).
func TestSynthese(t *testing.T) {
	h := time.Hour
	cases := []struct {
		name string
		runs []history.Run
		ind  Indicator
		key  string
	}{
		{"jamais", nil, IndicatorOrange, "home.never"},
		{"réussie hier", []history.Run{run(history.StatusSuccess, 20*h)}, IndicatorGreen, "home.ok"},
		{"avertissements", []history.Run{run(history.StatusWarning, h)}, IndicatorOrange, "home.warning"},
		{"échec après réussite", []history.Run{run(history.StatusError, h), run(history.StatusSuccess, 2*h)}, IndicatorRed, "home.failed"},
		{"annulée", []history.Run{run(history.StatusCancelled, h)}, IndicatorRed, "home.failed"},
		{"trois jours sans réussite", []history.Run{run(history.StatusSuccess, 72*h)}, IndicatorOrange, "home.stale"},
		{"interrompue après réussite", []history.Run{run(history.StatusRunning, h), run(history.StatusSuccess, 3*h)}, IndicatorOrange, "home.interrupted"},
		{"interrompue sans passé", []history.Run{run(history.StatusRunning, h)}, IndicatorOrange, "home.interrupted"},
		{"ancienne et échouée", []history.Run{run(history.StatusError, 100*h)}, IndicatorRed, "home.failed"},
	}
	for _, c := range cases {
		s := Summarize(c.runs, now)
		if s.Indicator != c.ind || s.Key != c.key {
			t.Errorf("%s: %v %s, attendu %v %s", c.name, s.Indicator, s.Key, c.ind, c.key)
		}
	}
	if s := Summarize([]history.Run{run(history.StatusSuccess, 72*h)}, now); s.Data["Days"] != 3 {
		t.Errorf("ancienneté: %v", s.Data)
	}
}

// TestSyntheseVerification vérifie qu'une vérification de restauration en
// échec fait passer au orange un état vert, et seulement lui.
func TestSyntheseVerification(t *testing.T) {
	failed := history.RestoreCheck{Outcome: history.OutcomeFailed}
	green := Summarize([]history.Run{run(history.StatusSuccess, time.Hour)}, now)

	if got := green.WithRestoreCheck(failed, true); got.Indicator != IndicatorOrange || got.Key != "home.verify_failed" {
		t.Errorf("vérification en échec: %+v", got)
	}
	if got := green.WithRestoreCheck(history.RestoreCheck{Outcome: history.OutcomeIdentical}, true); got.Indicator != IndicatorGreen {
		t.Errorf("vérification réussie: %+v", got)
	}
	if got := green.WithRestoreCheck(history.RestoreCheck{}, false); got.Indicator != IndicatorGreen {
		t.Errorf("jamais vérifiée: %+v", got)
	}
	red := Summarize([]history.Run{run(history.StatusError, time.Hour)}, now)
	if got := red.WithRestoreCheck(failed, true); got.Indicator != IndicatorRed || got.Key != "home.failed" {
		t.Errorf("un état rouge garde sa phrase: %+v", got)
	}
}
