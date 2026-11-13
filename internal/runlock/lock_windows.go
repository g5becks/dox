//go:build windows

package runlock

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockRange covers the whole file; LockFileEx locks byte ranges.
const lockRange = 1

func tryLock(file *os.File) error {
	overlapped := new(windows.Overlapped)
	err := windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, lockRange, 0, overlapped,
	)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return errBusy
	}
	return err
}

func unlock(file *os.File) error {
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, lockRange, 0, new(windows.Overlapped))
}
