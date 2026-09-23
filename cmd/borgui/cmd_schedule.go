package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"leblanc.io/open-go-borg-ui/internal/config"
	"leblanc.io/open-go-borg-ui/internal/schedule"
)

// newScheduler construit l'ordonnanceur du système. Les tests le remplacent
// pour ne jamais toucher à celui du poste.
var newScheduler = schedule.New

// commandSchedule règle et installe la planification du profil (EF-60,
// EF-61).
//
//	borgui schedule [status]
//	borgui schedule daily <HH:MM>
//	borgui schedule weekly <jour> <HH:MM>
//	borgui schedule manual
func (a *app) commandSchedule(ctx context.Context, args []string) (int, error) {
	action, rest := subcommand(args)

	var change func(*config.Schedule) bool
	switch {
	case action == "" || action == "status":
		return a.scheduleStatus(ctx)
	case action == "manual" && len(rest) == 0:
		change = func(s *config.Schedule) bool { s.Kind = "manual"; return true }
	case action == "daily" && len(rest) == 1:
		change = func(s *config.Schedule) bool { s.Kind, s.At, s.Day = "daily", rest[0], ""; return true }
	case action == "weekly" && len(rest) == 2:
		change = func(s *config.Schedule) bool { s.Kind, s.Day, s.At = "weekly", rest[0], rest[1]; return true }
	default:
		return exitError, fmt.Errorf("%s", a.T("schedule.usage"))
	}

	cfg, err := config.Load(a.ConfigPath)
	if err != nil {
		return exitError, err
	}
	profile, err := cfg.Profile(a.ProfileName)
	if err != nil {
		return exitError, err
	}
	updated := profile.Schedule
	change(&updated)
	// Le réglage est vérifié avant d'être enregistré : une heure mal saisie
	// ne doit pas remplacer une planification qui fonctionne.
	if _, err := schedule.FromConfig(updated); err != nil {
		return exitError, fmt.Errorf("%s", a.T("schedule.invalid"))
	}
	profile.Schedule = updated
	if err := config.Save(a.ConfigPath, cfg); err != nil {
		return exitError, err
	}
	return a.applySchedule(ctx, profile)
}

// installSchedule est le mode « --install-schedule <profil> » (AR-03) : il
// applique la planification enregistrée dans la configuration.
func (a *app) installSchedule(ctx context.Context) (int, error) {
	profile, err := a.Profile()
	if err != nil {
		return exitError, err
	}
	return a.applySchedule(ctx, profile)
}

// applySchedule met la tâche du système en accord avec le profil : installée
// ou remplacée si une fréquence est choisie, retirée en planification
// manuelle.
func (a *app) applySchedule(ctx context.Context, profile *config.Profile) (int, error) {
	plan, err := schedule.FromConfig(profile.Schedule)
	if err != nil {
		return exitError, fmt.Errorf("%s", a.T("schedule.invalid"))
	}
	scheduler, err := newScheduler()
	if err != nil {
		return exitError, err
	}

	name := config.SafeName(profile.Name)
	if plan.Frequency == schedule.Manual {
		if err := scheduler.Remove(ctx, name); err != nil {
			return exitError, err
		}
		fmt.Println(a.T("schedule.removed"))
		return exitSuccess, nil
	}

	task, err := a.scheduledTask(profile)
	if err != nil {
		return exitError, err
	}
	if err := scheduler.Install(ctx, task, plan); err != nil {
		return exitError, err
	}
	fmt.Println(a.T("schedule.installed", map[string]any{"Plan": a.describePlan(plan)}))
	return exitSuccess, nil
}

// scheduledTask décrit ce que l'ordonnanceur doit lancer : cet exécutable, en
// mode « --run <profil> » (EF-63), avec la configuration en cours si elle
// n'est pas à son emplacement standard.
func (a *app) scheduledTask(profile *config.Profile) (schedule.Task, error) {
	executable, err := os.Executable()
	if err != nil {
		return schedule.Task{}, err
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}

	var args []string
	if a.ConfigPath != "" {
		path, err := filepath.Abs(a.ConfigPath)
		if err != nil {
			return schedule.Task{}, err
		}
		args = append(args, "--config", path)
	}
	args = append(args, "--run", profile.Name)

	return schedule.Task{
		Name:        config.SafeName(profile.Name),
		Description: a.T("schedule.task_description", map[string]any{"Profile": profile.Name}),
		Executable:  executable,
		Args:        args,
	}, nil
}

// scheduleStatus affiche la planification enregistrée et l'état de la tâche.
func (a *app) scheduleStatus(ctx context.Context) (int, error) {
	profile, err := a.Profile()
	if err != nil {
		return exitError, err
	}
	plan, err := schedule.FromConfig(profile.Schedule)
	if err != nil {
		return exitError, fmt.Errorf("%s", a.T("schedule.invalid"))
	}
	fmt.Println(a.T("schedule.current", map[string]any{"Plan": a.describePlan(plan)}))
	if plan.Frequency == schedule.Manual {
		return exitSuccess, nil
	}

	scheduler, err := newScheduler()
	if err != nil {
		return exitError, err
	}
	installed, err := scheduler.Installed(ctx, config.SafeName(profile.Name))
	if err != nil {
		return exitError, err
	}
	if !installed {
		// Réglée mais absente du système : configuration recopiée d'un
		// autre poste, ou tâche supprimée à la main.
		fmt.Println(a.T("schedule.not_installed"))
		return exitWarning, nil
	}
	fmt.Println(a.T("schedule.log", map[string]any{"Path": a.logPath()}))
	return exitSuccess, nil
}

// describePlan rend une planification en langage courant.
func (a *app) describePlan(plan schedule.Plan) string {
	data := map[string]any{"At": fmt.Sprintf("%02d:%02d", plan.Hour, plan.Minute)}
	var text string
	switch plan.Frequency {
	case schedule.Daily:
		text = a.T("schedule.daily", data)
	case schedule.Weekly:
		data["Day"] = a.T(schedule.DayKey(plan.Day))
		text = a.T("schedule.weekly", data)
	default:
		return a.T("schedule.manual")
	}
	if plan.CatchUp {
		text += a.T("schedule.catch_up")
	}
	return text
}
