package lock

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// lockFile pose un verrou exclusif non bloquant sur le premier octet.
func lockFile(file *os.File) error {
	var overlapped windows.Overlapped
	err := windows.LockFileEx(windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, &overlapped)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return ErrBusy
	}
	if err != nil {
		return fmt.Errorf("lock: %w", err)
	}
	return nil
}

// unlockFile lève le verrou.
func unlockFile(file *os.File) {
	var overlapped windows.Overlapped
	windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
}
