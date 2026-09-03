package main

import "runtime"

// osName retourne le système d'exploitation courant. L'indirection permet aux
// tests de vérifier le comportement propre à chaque plateforme.
func osName() string { return runtime.GOOS }
