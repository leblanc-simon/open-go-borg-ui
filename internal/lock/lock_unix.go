//go:build !windows

package lock

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// lockFile pose un verrou exclusif non bloquant.
func lockFile(file *os.File) error {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return ErrBusy
	}
	if err != nil {
		return fmt.Errorf("lock: %w", err)
	}
	return nil
}

// unlockFile lève le verrou.
func unlockFile(file *os.File) {
	syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
