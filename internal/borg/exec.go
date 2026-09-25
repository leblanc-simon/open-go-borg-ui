package borg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// maxLineSize borne la taille d'une ligne de journal. Les événements
// « --log-json » embarquent le chemin du fichier courant : un mégaoctet couvre
// largement les chemins les plus longs sans risquer de tronquer un événement.
const maxLineSize = 1 << 20

// interruptGrace laisse à Borg le temps de relâcher son verrou après une
// demande d'annulation, avant que le processus ne soit tué.
const interruptGrace = 10 * time.Second

// invocation est une commande système prête à être lancée, telle que la
// construit un Runner.
type invocation struct {
	// Path est l'exécutable à lancer : borg lui-même, ou le shell qui
	// l'enveloppe sous Cygwin.
	Path string
	// Args sont ses arguments, sans le nom du programme.
	Args []string
	// Dir est le répertoire courant du processus, dans la forme native du
	// système. Vide, le processus hérite de celui de l'application.
	Dir string
	// Env est l'environnement complet du processus.
	Env []string
	// Display est la représentation lisible de la commande pour le journal,
	// dépourvue de tout secret.
	Display string
}

// run exécute l'invocation et collecte son résultat.
//
// Le processus est lancé sans entrée standard : aucune question de Borg ne peut
// suspendre une exécution planifiée. Une erreur n'est retournée que si Borg n'a
// pas pu être lancé ; un code de retour non nul se lit dans Result.
func run(ctx context.Context, inv invocation, onEvent func(Event)) (*Result, error) {
	cmd := exec.CommandContext(ctx, inv.Path, inv.Args...)
	cmd.Dir = inv.Dir
	cmd.Env = inv.Env
	cmd.Stdin = nil

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	// La sortie d'erreur porte les événements de « --log-json », une ligne
	// chacun. Elle est confiée à exec plutôt que lue par un tube : Wait
	// n'est alors rendu qu'une fois tout recopié. Lue par un tube, elle
	// pouvait perdre ses dernières lignes — celles qui expliquent un échec —,
	// Wait fermant le tube dès la fin du processus.
	var (
		mu       sync.Mutex
		messages []Message
	)
	events := &lineWriter{max: maxLineSize, line: func(line []byte) {
		event, ok := parseEvent(line)
		if !ok {
			return
		}
		if event.Kind == EventLog && event.Message != "" {
			mu.Lock()
			messages = append(messages, Message{
				Level: event.Level,
				MsgID: event.MsgID,
				Text:  event.Message,
			})
			mu.Unlock()
		}
		if onEvent != nil {
			onEvent(event)
		}
	}}
	cmd.Stderr = events

	// Une annulation demande d'abord poliment l'arrêt : Borg relâche alors son
	// verrou et laisse le dépôt utilisable, là où un processus tué impose un
	// « break-lock » à la prochaine exécution.
	prepareProcess(cmd)
	cmd.Cancel = func() error { return interrupt(cmd) }
	cmd.WaitDelay = interruptGrace

	started := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("borg: lancement de %s: %w", inv.Path, err)
	}

	waitErr := cmd.Wait()
	// Une dernière ligne sans saut de ligne final est aussi un événement.
	events.flush()

	mu.Lock()
	collected := messages
	mu.Unlock()
	result := &Result{
		Stdout:      stdout.Bytes(),
		Messages:    collected,
		Duration:    time.Since(started),
		CommandLine: inv.Display,
	}

	switch {
	case waitErr == nil || errors.Is(waitErr, exec.ErrWaitDelay):
		// ErrWaitDelay : Borg a réussi, mais un processus qu'il a lancé —
		// ssh — gardait sa sortie d'erreur ouverte au-delà du délai de
		// grâce. Wait l'a refermée ; un code de retour non nul aurait été
		// rapporté, lui, par une ExitError.
		result.ExitCode = 0
	default:
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			return nil, fmt.Errorf("borg: exécution: %w", waitErr)
		}
		result.ExitCode = exitErr.ExitCode()
		if result.ExitCode < 0 {
			// Processus tué : un contexte annulé en est la cause attendue.
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			result.ExitCode = 2
		}
	}
	result.Status = statusFromExitCode(result.ExitCode)
	return result, nil
}

// output lance une commande courte et retourne sa sortie standard. Sert aux
// interrogations sans progression, comme « borg --version ».
func output(ctx context.Context, inv invocation) (string, error) {
	result, err := run(ctx, inv, nil)
	if err != nil {
		return "", err
	}
	if result.Status == StatusError {
		return "", fmt.Errorf("borg: %s a échoué (code %d): %s",
			inv.Display, result.ExitCode, joinMessages(result.Messages))
	}
	return strings.TrimSpace(string(result.Stdout)), nil
}

// joinMessages concatène les messages de journal pour un diagnostic court.
func joinMessages(messages []Message) string {
	texts := make([]string, 0, len(messages))
	for _, m := range messages {
		texts = append(texts, m.Text)
	}
	return strings.Join(texts, " / ")
}

// lineWriter découpe en lignes ce qu'on lui écrit, et les transmet une à une.
// exec l'alimente depuis une seule goroutine. Une ligne plus longue que max
// est écartée plutôt que de faire grossir la mémoire sans limite.
type lineWriter struct {
	max     int
	line    func([]byte)
	pending []byte
	// overflow : la ligne en cours a dépassé max, son reste est ignoré
	// jusqu'au prochain saut de ligne.
	overflow bool
}

func (w *lineWriter) Write(data []byte) (int, error) {
	written := len(data)
	for len(data) > 0 {
		end := bytes.IndexByte(data, '\n')
		if end < 0 {
			w.append(data)
			break
		}
		w.append(data[:end])
		if !w.overflow {
			w.line(bytes.TrimRight(w.pending, "\r"))
		}
		w.pending, w.overflow = w.pending[:0], false
		data = data[end+1:]
	}
	return written, nil
}

// append ajoute un morceau à la ligne en cours, dans la limite de max.
func (w *lineWriter) append(part []byte) {
	if w.overflow {
		return
	}
	if len(w.pending)+len(part) > w.max {
		w.pending, w.overflow = w.pending[:0], true
		return
	}
	w.pending = append(w.pending, part...)
}

// flush transmet la dernière ligne, restée sans saut de ligne.
func (w *lineWriter) flush() {
	if len(w.pending) > 0 && !w.overflow {
		w.line(bytes.TrimRight(w.pending, "\r"))
	}
	w.pending, w.overflow = w.pending[:0], false
}
