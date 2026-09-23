//go:build !windows

package fsperm

import "os"

// restrict ramène le mode à 0600.
func restrict(path string) error { return os.Chmod(path, 0o600) }
