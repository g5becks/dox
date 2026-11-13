// Package runlock serializes dox commands that write one output directory
// (sync, clean) with an OS advisory lock on a file inside it.
package runlock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/g5becks/dox/internal/errs"
)

// FileName is the lock file kept in the output directory.
const FileName = ".dox-run.lock"

// DefaultTimeout is how long a second invocation waits for the lock.
const DefaultTimeout = 30 * time.Second

const (
	pollInterval = 50 * time.Millisecond
	lockFileMode = 0o600
)

// errBusy reports that another process holds the lock right now.
var errBusy = errors.New("lock held by another process")

// Lock is a held advisory lock; Release it when the command finishes.
type Lock struct{ file *os.File }

// Acquire takes the exclusive lock on root's lock file, waiting up to timeout
// (DefaultTimeout when <= 0) and honoring ctx. The lock is advisory: it only
// coordinates dox processes, and the OS releases it if the process exits.
func Acquire(ctx context.Context, root *os.Root, timeout time.Duration) (*Lock, error) {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	file, err := root.OpenFile(FileName, os.O_RDWR|os.O_CREATE, lockFileMode)
	if err != nil {
		return nil, errs.Wrap(errs.Internal, fmt.Errorf("opening run lock: %w", err))
	}
	deadline := time.Now().Add(timeout)
	for {
		lockErr := tryLock(file)
		if lockErr == nil {
			return &Lock{file: file}, nil
		}
		if !errors.Is(lockErr, errBusy) {
			_ = file.Close()
			return nil, errs.Wrap(errs.Internal, fmt.Errorf("locking output directory: %w", lockErr))
		}
		if time.Now().After(deadline) {
			_ = file.Close()
			return nil, errs.New(
				errs.Internal,
				fmt.Sprintf("another dox sync or clean is using this output directory; gave up after %s", timeout),
				errs.WithHint("Wait for the other run to finish, or raise --lock-timeout"),
				errs.WithFields(map[string]any{"lock": FileName, "timeout": timeout.String()}),
			)
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// Release unlocks and closes the lock file. It is safe on a nil Lock.
func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := unlock(l.file)
	closeErr := l.file.Close()
	l.file = nil
	return errors.Join(unlockErr, closeErr)
}
