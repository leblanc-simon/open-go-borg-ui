//go:build !windows

package borg

import (
	"os/exec"
	"syscall"
)

// prepareProcess place Borg dans son propre groupe de processus, pour que
// l'interruption atteigne aussi ses enfants — ssh en tête.
func prepareProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// interrupt demande l'arrêt de Borg et de ses enfants, comme un Ctrl-C au
// terminal. Le signal d'interruption laisse à Borg le temps de relâcher le
// verrou du dépôt ; un enfant qui garderait les sorties ouvertes ne retient
// plus la fin de l'exécution.
func interrupt(cmd *exec.Cmd) error {
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
}
