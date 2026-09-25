package main

import (
	"os"
	"syscall"
)

// attachParentProcess désigne, pour AttachConsole, la console du processus
// parent.
const attachParentProcess = ^uint32(0)

// attachConsole rend la ligne de commande utilisable sous Windows.
//
// L'exécutable publié est compilé en application graphique (-H windowsgui) :
// sans cela, une fenêtre de console s'ouvrirait derrière l'interface, et à
// chaque sauvegarde planifiée. En contrepartie, il ne reçoit aucune console :
// lancé depuis cmd ou PowerShell avec une commande, il s'attache à celle qui
// l'a lancé pour y écrire.
//
// Une sortie déjà valide — redirection vers un fichier, tube de
// BORG_PASSCOMMAND — est laissée telle quelle.
func attachConsole() {
	if validHandle(os.Stdout) {
		return
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	if attached, _, _ := kernel32.NewProc("AttachConsole").Call(uintptr(attachParentProcess)); attached == 0 {
		// Aucune console parente : tâche planifiée, raccourci.
		return
	}
	if out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = out, out
		stderr = out
	}
	if in, err := os.OpenFile("CONIN$", os.O_RDONLY, 0); err == nil {
		os.Stdin = in
	}
}

// validHandle indique qu'un fichier standard désigne une sortie réelle.
func validHandle(file *os.File) bool {
	if file == nil {
		return false
	}
	handle := syscall.Handle(file.Fd())
	if handle == 0 || handle == syscall.InvalidHandle {
		return false
	}
	_, err := syscall.GetFileType(handle)
	return err == nil
}
