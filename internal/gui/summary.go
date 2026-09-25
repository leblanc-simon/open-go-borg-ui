package gui

import (
	"time"

	"leblanc.io/open-go-borg-ui/internal/history"
	"leblanc.io/open-go-borg-ui/internal/statusfile"
)

// Indicator est la couleur de l'indicateur dominant de l'écran État (EF-80).
type Indicator int

const (
	IndicatorGreen Indicator = iota
	IndicatorOrange
	IndicatorRed
)

// Summary est ce que l'écran État dit en une phrase : une couleur, une clé de
// traduction et ses paramètres. Jamais un code d'erreur brut (EF-80).
type Summary struct {
	Indicator Indicator
	Key       string
	Data      map[string]any
	// Last est l'exécution décrite en détail, nil s'il n'y en a aucune.
	Last *history.Run
	// LastSuccess est la dernière sauvegarde exploitable, nil si aucune.
	LastSuccess *history.Run
}

// Summarize résume l'historique, du plus récent au plus ancien, à l'instant
// now.
//
// La dernière exécution terminée fixe le ton : un échec est rouge même si une
// sauvegarde réussie le précède de peu, car c'est la prochaine qui compte.
// Faute d'échec, c'est l'ancienneté de la dernière sauvegarde exploitable qui
// décide (EF-85), puis ses éventuels avertissements.
func Summarize(runs []history.Run, now time.Time) Summary {
	var last, lastSuccess *history.Run
	interrupted := false
	for i := range runs {
		run := &runs[i]
		if run.Status == history.StatusRunning {
			// Une exécution jamais terminée, la plus récente : interrompue
			// par une extinction ou un plantage, ou en cours ailleurs.
			if last == nil && i == 0 {
				interrupted = true
			}
			continue
		}
		if last == nil {
			last = run
		}
		if lastSuccess == nil && (run.Status == history.StatusSuccess || run.Status == history.StatusWarning) {
			lastSuccess = run
		}
	}

	summary := Summary{Last: last, LastSuccess: lastSuccess}
	switch {
	case last == nil && !interrupted:
		summary.Indicator, summary.Key = IndicatorOrange, "home.never"
	case last != nil && (last.Status == history.StatusError || last.Status == history.StatusCancelled):
		summary.Indicator, summary.Key = IndicatorRed, "home.failed"
	case lastSuccess == nil:
		summary.Indicator, summary.Key = IndicatorOrange, "home.interrupted"
	case now.Sub(lastSuccess.Finished) > statusfile.StaleAfter:
		summary.Indicator, summary.Key = IndicatorOrange, "home.stale"
		summary.Data = map[string]any{"Days": int(now.Sub(lastSuccess.Finished) / (24 * time.Hour))}
	case interrupted:
		summary.Indicator, summary.Key = IndicatorOrange, "home.interrupted"
	case last.Status == history.StatusWarning:
		summary.Indicator, summary.Key = IndicatorOrange, "home.warning"
	default:
		summary.Indicator, summary.Key = IndicatorGreen, "home.ok"
	}
	return summary
}

// WithRestoreCheck tient compte de la dernière vérification de
// restauration (EF-99) : une vérification en échec signale une sauvegarde
// qui ne se relit pas, et fait passer au orange un état qui serait vert.
// Un état déjà orange ou rouge garde sa phrase : elle dit ce qu'il y a de
// plus urgent.
func (s Summary) WithRestoreCheck(check history.RestoreCheck, ok bool) Summary {
	if !ok || check.Outcome != history.OutcomeFailed || s.Indicator != IndicatorGreen {
		return s
	}
	s.Indicator, s.Key, s.Data = IndicatorOrange, "home.verify_failed", nil
	return s
}

// WithRepositoryCheck tient compte du dernier contrôle de la destination
// (EF-87) : des anomalies trouvées font passer au orange un état qui serait
// vert. Un état déjà orange ou rouge garde sa phrase.
func (s Summary) WithRepositoryCheck(check history.RepositoryCheck, ok bool) Summary {
	if !ok || check.Healthy || s.Indicator != IndicatorGreen {
		return s
	}
	s.Indicator, s.Key, s.Data = IndicatorOrange, "home.check_failed", nil
	return s
}
