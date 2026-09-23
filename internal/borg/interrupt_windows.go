package borg

import (
	"os/exec"
	"strconv"
)

// prepareProcess n'a rien à préparer sous Windows.
func prepareProcess(*exec.Cmd) {}

// interrupt arrête Borg et tout son arbre de processus.
//
// Sous Cygwin, Borg est lancé par le bash du runtime, et « exec » y crée un
// nouveau processus Windows : tuer le processus lancé ne tuerait que bash, et
// Borg continuerait en arrière-plan. taskkill /T arrête l'arbre entier.
// Windows n'offre pas d'équivalent utile du signal d'interruption : le dépôt
// peut rester verrouillé, ce que l'application sait détecter et lever
// (EF-58).
func interrupt(cmd *exec.Cmd) error {
	kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
	if err := kill.Run(); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
