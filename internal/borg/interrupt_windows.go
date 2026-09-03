package borg

import "os/exec"

// interrupt arrête le processus Borg. Windows n'offre pas d'équivalent utile
// du signal d'interruption pour un processus détaché : le dépôt peut rester
// verrouillé, situation que l'application sait détecter et débloquer (EF-58).
func interrupt(cmd *exec.Cmd) error {
	return cmd.Process.Kill()
}
