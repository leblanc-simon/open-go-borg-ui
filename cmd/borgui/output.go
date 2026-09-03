package main

import (
	"os"

	"golang.org/x/term"
)

// stderr reçoit la progression et les messages de diagnostic : la sortie
// standard reste réservée aux données que l'appelant peut vouloir rediriger.
var stderr = os.Stderr

// interactive indique si la progression peut réécrire la ligne courante. Une
// exécution planifiée écrit dans un fichier journal, où les séquences de
// contrôle du terminal n'auraient aucun sens.
func interactive() bool { return term.IsTerminal(int(stderr.Fd())) }

// progressLine rend une ligne de progression : réécrite sur place devant un
// utilisateur, tue lorsque la sortie est un journal.
func progressLine(text string) {
	if !interactive() {
		return
	}
	_, _ = stderr.WriteString("\r\033[K" + text)
}

// progressDone termine la zone de progression.
func progressDone() {
	if interactive() {
		_, _ = stderr.WriteString("\r\033[K")
	}
}

// notice écrit un message hors de la zone de progression.
func notice(text string) {
	progressDone()
	_, _ = stderr.WriteString(text + "\n")
}
