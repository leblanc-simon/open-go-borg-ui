//go:build !windows

package borgruntime

// markSystem n'a d'équivalent que sous Windows, seule plateforme où le runtime
// Cygwin est installé.
func markSystem(string) error { return nil }
