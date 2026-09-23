package schedule

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"strings"
	"unicode/utf16"
)

// TaskScheduler planifie par le Planificateur de tâches de Windows.
//
// La tâche est importée depuis une définition XML plutôt que créée par les
// options de schtasks : seule la définition complète expose
// StartWhenAvailable, qui rattrape une exécution manquée au démarrage suivant
// (EF-62). Elle s'exécute dans la session de l'utilisateur, sans mot de passe
// enregistré ni élévation.
type TaskScheduler struct {
	run runFunc
}

// taskName retourne le nom de la tâche. Elle est placée à la racine de la
// bibliothèque : créer un dossier peut exiger des droits que l'application
// n'a pas.
func taskName(name string) string { return "BorgUI-" + name }

// Install importe la définition de la tâche, en remplaçant l'existante.
func (s *TaskScheduler) Install(ctx context.Context, task Task, plan Plan) error {
	if plan.Frequency == Manual {
		return fmt.Errorf("%w: une planification manuelle ne s'installe pas", ErrInvalid)
	}
	file, err := os.CreateTemp("", "borgui-tache-*.xml")
	if err != nil {
		return fmt.Errorf("schedule: %w", err)
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(taskXML(task, plan)); err != nil {
		file.Close()
		return fmt.Errorf("schedule: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("schedule: %w", err)
	}
	_, err = s.run(ctx, "schtasks", "/Create", "/TN", taskName(task.Name), "/XML", file.Name(), "/F")
	return err
}

// Remove supprime la tâche.
func (s *TaskScheduler) Remove(ctx context.Context, name string) error {
	installed, err := s.Installed(ctx, name)
	if err != nil || !installed {
		return err
	}
	_, err = s.run(ctx, "schtasks", "/Delete", "/TN", taskName(name), "/F")
	return err
}

// Installed interroge le Planificateur. Une tâche absente fait échouer la
// requête, ce qui est la réponse attendue et non une erreur.
func (s *TaskScheduler) Installed(ctx context.Context, name string) (bool, error) {
	_, err := s.run(ctx, "schtasks", "/Query", "/TN", taskName(name))
	return err == nil, nil
}

// taskXML produit la définition de la tâche, en UTF-16 avec marque d'ordre
// des octets, seul encodage que schtasks accepte sans ambiguïté.
func taskXML(task Task, plan Plan) []byte {
	schedule := "<ScheduleByDay><DaysInterval>1</DaysInterval></ScheduleByDay>"
	if plan.Frequency == Weekly {
		schedule = "<ScheduleByWeek><WeeksInterval>1</WeeksInterval><DaysOfWeek><" +
			plan.Day.String() + " /></DaysOfWeek></ScheduleByWeek>"
	}
	arguments := make([]string, len(task.Args))
	for i, arg := range task.Args {
		arguments[i] = windowsQuote(arg)
	}

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-16"?>` + "\n")
	b.WriteString(`<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">` + "\n")
	b.WriteString("  <RegistrationInfo><Description>" + xmlText(task.Description) + "</Description></RegistrationInfo>\n")
	b.WriteString("  <Triggers>\n    <CalendarTrigger>\n")
	fmt.Fprintf(&b, "      <StartBoundary>2026-01-01T%02d:%02d:00</StartBoundary>\n", plan.Hour, plan.Minute)
	b.WriteString("      <Enabled>true</Enabled>\n      " + schedule + "\n")
	b.WriteString("    </CalendarTrigger>\n  </Triggers>\n")
	// InteractiveToken : la tâche tourne dans la session ouverte de
	// l'utilisateur, sans mot de passe stocké ; LeastPrivilege : sans
	// élévation (DT-06).
	b.WriteString("  <Principals><Principal id=\"Author\"><LogonType>InteractiveToken</LogonType>" +
		"<RunLevel>LeastPrivilege</RunLevel></Principal></Principals>\n")
	b.WriteString("  <Settings>\n")
	b.WriteString("    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>\n")
	b.WriteString("    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>\n")
	b.WriteString("    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>\n")
	fmt.Fprintf(&b, "    <StartWhenAvailable>%t</StartWhenAvailable>\n", plan.CatchUp)
	b.WriteString("    <AllowStartOnDemand>true</AllowStartOnDemand>\n")
	b.WriteString("    <Enabled>true</Enabled>\n")
	// Une sauvegarde initiale de plusieurs centaines de gigaoctets dépasse
	// largement la limite de 72 heures par défaut.
	b.WriteString("    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>\n")
	b.WriteString("    <Priority>7</Priority>\n")
	b.WriteString("  </Settings>\n")
	b.WriteString("  <Actions Context=\"Author\"><Exec><Command>" + xmlText(task.Executable) + "</Command>" +
		"<Arguments>" + xmlText(strings.Join(arguments, " ")) + "</Arguments></Exec></Actions>\n")
	b.WriteString("</Task>\n")

	return utf16LE(b.String())
}

// xmlText échappe une valeur textuelle.
func xmlText(value string) string {
	var b bytes.Buffer
	xml.EscapeText(&b, []byte(value))
	return b.String()
}

// windowsQuote cite un argument selon les règles de CommandLineToArgvW : les
// barres obliques inverses ne sont significatives que devant un guillemet.
func windowsQuote(arg string) string {
	if arg != "" && !strings.ContainsAny(arg, " \t\"") {
		return arg
	}
	var b strings.Builder
	b.WriteByte('"')
	backslashes := 0
	for _, r := range arg {
		switch r {
		case '\\':
			backslashes++
		case '"':
			b.WriteString(strings.Repeat(`\`, 2*backslashes+1))
			b.WriteRune(r)
			backslashes = 0
		default:
			b.WriteString(strings.Repeat(`\`, backslashes))
			b.WriteRune(r)
			backslashes = 0
		}
	}
	b.WriteString(strings.Repeat(`\`, 2*backslashes))
	b.WriteByte('"')
	return b.String()
}

// utf16LE encode en UTF-16 petit-boutiste avec marque d'ordre des octets.
func utf16LE(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 0, 2+2*len(units))
	out = append(out, 0xFF, 0xFE)
	for _, u := range units {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}
