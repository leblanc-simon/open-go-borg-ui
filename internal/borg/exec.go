package borg

import (
	"bufio"
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

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("borg: sortie d'erreur: %w", err)
	}

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

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		messages []Message
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)
		for scanner.Scan() {
			event, ok := parseEvent(scanner.Bytes())
			if !ok {
				continue
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
		}
	}()

	waitErr := cmd.Wait()
	wg.Wait()

	result := &Result{
		Stdout:      stdout.Bytes(),
		Messages:    messages,
		Duration:    time.Since(started),
		CommandLine: inv.Display,
	}

	switch {
	case waitErr == nil:
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
