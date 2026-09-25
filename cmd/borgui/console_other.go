//go:build !windows

package main

// attachConsole n'a rien à faire hors de Windows : l'exécutable y hérite
// toujours du terminal qui l'a lancé.
func attachConsole() {}
