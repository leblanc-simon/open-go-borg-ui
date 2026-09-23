package schedule

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Systemd planifie par un timer systemd utilisateur.
//
// Le rattrapage repose sur Persistent=true : au démarrage de la session
// suivante, systemd lance l'exécution manquée. Un timer utilisateur ne tourne
// que pendant une session ouverte, sauf si « loginctl enable-linger » a été
// accordé — ce que l'application ne fait pas d'office, cela relevant de
// l'administrateur du poste.
type Systemd struct {
	// Dir est le dossier des unités utilisateur.
	Dir string
	run runFunc
}

// systemdUserDir retourne ~/.config/systemd/user, ou son équivalent XDG.
func systemdUserDir() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("schedule: dossier personnel introuvable: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "systemd", "user"), nil
}

// unit retourne le nom de base des unités d'une tâche.
func unit(name string) string { return "borgui-" + name }

// Install écrit le service et le timer, puis active le timer.
func (s *Systemd) Install(ctx context.Context, task Task, plan Plan) error {
	if plan.Frequency == Manual {
		return fmt.Errorf("%w: une planification manuelle ne s'installe pas", ErrInvalid)
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return fmt.Errorf("schedule: %w", err)
	}
	base := filepath.Join(s.Dir, unit(task.Name))
	if err := os.WriteFile(base+".service", []byte(serviceUnit(task)), 0o644); err != nil {
		return fmt.Errorf("schedule: %w", err)
	}
	if err := os.WriteFile(base+".timer", []byte(timerUnit(task, plan)), 0o644); err != nil {
		return fmt.Errorf("schedule: %w", err)
	}
	if _, err := s.run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	// --now démarre le timer sans attendre la prochaine session.
	_, err := s.run(ctx, "systemctl", "--user", "enable", "--now", unit(task.Name)+".timer")
	return err
}

// Remove désactive le timer et supprime les unités.
func (s *Systemd) Remove(ctx context.Context, name string) error {
	base := filepath.Join(s.Dir, unit(name))
	if _, err := os.Stat(base + ".timer"); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	// Un timer déjà inconnu de systemd ne doit pas empêcher le ménage.
	s.run(ctx, "systemctl", "--user", "disable", "--now", unit(name)+".timer")
	for _, suffix := range []string{".timer", ".service"} {
		if err := os.Remove(base + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("schedule: %w", err)
		}
	}
	_, err := s.run(ctx, "systemctl", "--user", "daemon-reload")
	return err
}

// Installed indique si le timer est écrit.
func (s *Systemd) Installed(_ context.Context, name string) (bool, error) {
	_, err := os.Stat(filepath.Join(s.Dir, unit(name)+".timer"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// serviceUnit produit le service lancé par le timer.
//
// La sauvegarde tourne avec une priorité processeur et disque basse : elle ne
// doit pas se faire sentir pendant que l'utilisateur travaille.
func serviceUnit(task Task) string {
	words := make([]string, 0, len(task.Args)+1)
	words = append(words, systemdQuote(task.Executable))
	for _, arg := range task.Args {
		words = append(words, systemdQuote(arg))
	}
	return "[Unit]\n" +
		"Description=" + systemdEscape(task.Description) + "\n\n" +
		"[Service]\n" +
		"Type=oneshot\n" +
		"ExecStart=" + strings.Join(words, " ") + "\n" +
		"Nice=10\n" +
		"IOSchedulingClass=idle\n"
}

// timerUnit produit le timer.
func timerUnit(task Task, plan Plan) string {
	calendar := fmt.Sprintf("*-*-* %02d:%02d:00", plan.Hour, plan.Minute)
	if plan.Frequency == Weekly {
		calendar = plan.Day.String()[:3] + " " + calendar
	}
	persistent := "false"
	if plan.CatchUp {
		persistent = "true"
	}
	return "[Unit]\n" +
		"Description=" + systemdEscape(task.Description) + "\n\n" +
		"[Timer]\n" +
		"OnCalendar=" + calendar + "\n" +
		"Persistent=" + persistent + "\n\n" +
		"[Install]\n" +
		"WantedBy=timers.target\n"
}

// systemdEscape neutralise les spécificateurs % et les retours à la ligne
// d'une valeur d'unité.
func systemdEscape(value string) string {
	value = strings.ReplaceAll(value, "%", "%%")
	return strings.NewReplacer("\n", " ", "\r", " ").Replace(value)
}

// systemdQuote cite un mot de ligne de commande systemd : guillemets doubles,
// barres obliques inverses et guillemets échappés, $ doublé pour empêcher
// toute substitution de variable, % doublé pour les spécificateurs.
func systemdQuote(word string) string {
	word = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", "$$", "%", "%%", "\n", " ").Replace(word)
	return `"` + word + `"`
}
