//go:build !windows

package borg

import (
	"os"
	"os/exec"
)

// interrupt demande l'arrêt du processus Borg. Le signal d'interruption lui
// laisse le temps de relâcher le verrou du dépôt.
func interrupt(cmd *exec.Cmd) error {
	return cmd.Process.Signal(os.Interrupt)
}
