// Package schedule installe et retire la tâche planifiée d'un profil
// (EF-60 à EF-63).
//
// La planification est confiée à l'ordonnanceur du système — timer systemd
// utilisateur sous Linux, Planificateur de tâches sous Windows — qui lance
// l'exécutable en mode « --run <profil> » : aucun démon résident (AR-04),
// aucun droit d'administrateur.
//
// Les deux implémentations se contentent d'écrire des fichiers et de lancer
// des commandes : elles se compilent et se vérifient sur toutes les
// plateformes, l'exécution des commandes étant substituable.
package schedule

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"leblanc.io/open-go-borg-ui/internal/config"
)

// Frequency est la fréquence de déclenchement (EF-61).
type Frequency string

const (
	Manual Frequency = "manual"
	Daily  Frequency = "daily"
	Weekly Frequency = "weekly"
)

// ErrInvalid signale un réglage de planification incohérent.
var ErrInvalid = errors.New("schedule: planification invalide")

// Plan est une planification vérifiée.
type Plan struct {
	Frequency Frequency
	Hour      int
	Minute    int
	// Day n'a de sens qu'en planification hebdomadaire.
	Day time.Weekday
	// CatchUp rattrape au démarrage suivant une exécution manquée, poste
	// éteint à l'heure prévue (EF-62).
	CatchUp bool
}

// weekdays associe les noms de la configuration aux jours.
var weekdays = map[string]time.Weekday{
	"monday": time.Monday, "tuesday": time.Tuesday, "wednesday": time.Wednesday,
	"thursday": time.Thursday, "friday": time.Friday, "saturday": time.Saturday,
	"sunday": time.Sunday,
}

// DayName retourne le nom d'un jour tel que la configuration l'écrit.
func DayName(day time.Weekday) string { return strings.ToLower(day.String()) }

// DayKey retourne la clé de traduction d'un jour, sous la racine transverse
// weekday.
func DayKey(day time.Weekday) string { return "weekday." + DayName(day) }

// ParseDay lit un nom de jour de la configuration.
func ParseDay(name string) (time.Weekday, bool) {
	day, ok := weekdays[strings.ToLower(strings.TrimSpace(name))]
	return day, ok
}

// FromConfig vérifie le réglage d'un profil.
func FromConfig(s config.Schedule) (Plan, error) {
	plan := Plan{Frequency: Frequency(s.Kind), CatchUp: s.CatchUpIfMissed}
	switch plan.Frequency {
	case "", Manual:
		plan.Frequency = Manual
		return plan, nil
	case Daily, Weekly:
	default:
		return plan, fmt.Errorf("%w: fréquence %q", ErrInvalid, s.Kind)
	}

	at, err := time.Parse("15:04", strings.TrimSpace(s.At))
	if err != nil {
		return plan, fmt.Errorf("%w: heure %q", ErrInvalid, s.At)
	}
	plan.Hour, plan.Minute = at.Hour(), at.Minute()

	if plan.Frequency == Weekly {
		day, ok := ParseDay(s.Day)
		if !ok {
			return plan, fmt.Errorf("%w: jour %q", ErrInvalid, s.Day)
		}
		plan.Day = day
	}
	return plan, nil
}

// Task est ce que l'ordonnanceur doit lancer.
type Task struct {
	// Name identifie la tâche auprès de l'ordonnanceur. Il doit déjà être
	// réduit à des caractères sûrs.
	Name string
	// Description est le libellé affiché par l'ordonnanceur, déjà traduit.
	Description string
	// Executable est le chemin absolu de l'application.
	Executable string
	// Args sont les arguments, dont « --run <profil> ».
	Args []string
}

// Scheduler installe et retire des tâches.
type Scheduler interface {
	// Install crée ou remplace la tâche.
	Install(ctx context.Context, task Task, plan Plan) error
	// Remove retire la tâche ; retirer une tâche absente n'est pas une
	// erreur.
	Remove(ctx context.Context, name string) error
	// Installed indique si la tâche existe.
	Installed(ctx context.Context, name string) (bool, error)
}

// CommandFunc exécute une commande système et retourne sa sortie combinée.
// Les tests y substituent un enregistreur.
type CommandFunc func(ctx context.Context, name string, args ...string) ([]byte, error)

// runFunc est l'alias interne de CommandFunc.
type runFunc = CommandFunc

// runCommand est l'exécution réelle.
func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// New retourne l'ordonnanceur de la plateforme.
func New() (Scheduler, error) {
	if runtime.GOOS == "windows" {
		return NewTaskScheduler(runCommand), nil
	}
	dir, err := systemdUserDir()
	if err != nil {
		return nil, err
	}
	return NewSystemd(dir, runCommand), nil
}

// NewSystemd construit l'ordonnanceur systemd sur le dossier d'unités dir.
func NewSystemd(dir string, run CommandFunc) *Systemd { return &Systemd{Dir: dir, run: run} }

// NewTaskScheduler construit l'ordonnanceur Windows.
func NewTaskScheduler(run CommandFunc) *TaskScheduler { return &TaskScheduler{run: run} }

// Next retourne la prochaine exécution prévue strictement après now, dans le
// fuseau de now. Zéro en planification manuelle.
//
// La date est reconstruite par time.Date plutôt qu'obtenue en ajoutant des
// durées : un passage à l'heure d'été ne décale pas l'heure prévue.
func Next(plan Plan, now time.Time) time.Time {
	if plan.Frequency != Daily && plan.Frequency != Weekly {
		return time.Time{}
	}
	for offset := 0; offset <= 7; offset++ {
		day := now.AddDate(0, 0, offset)
		candidate := time.Date(day.Year(), day.Month(), day.Day(), plan.Hour, plan.Minute, 0, 0, now.Location())
		if !candidate.After(now) {
			continue
		}
		if plan.Frequency == Weekly && candidate.Weekday() != plan.Day {
			continue
		}
		return candidate
	}
	return time.Time{}
}
