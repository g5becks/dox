package runlock_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/runlock"
)

func openRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

func TestSecondAcquireWaitsThenSucceeds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first, err := runlock.Acquire(t.Context(), openRoot(t, dir), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan time.Time, 1)
	go func() {
		time.Sleep(200 * time.Millisecond)
		released <- time.Now()
		_ = first.Release()
	}()
	second, err := runlock.Acquire(t.Context(), openRoot(t, dir), 5*time.Second)
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	defer second.Release()
	if acquired := time.Now(); acquired.Before(<-released) {
		t.Fatal("second lock acquired before the first was released")
	}
}

func TestSecondAcquireTimesOutClearly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first, err := runlock.Acquire(t.Context(), openRoot(t, dir), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	start := time.Now()
	_, err = runlock.Acquire(t.Context(), openRoot(t, dir), 150*time.Millisecond)
	detail, ok := errors.AsType[*errs.Error](err)
	if !ok || detail.Code != errs.Internal || !strings.Contains(err.Error(), "another dox sync or clean") ||
		!strings.Contains(detail.Hint, "--lock-timeout") {
		t.Fatalf("Acquire() error = %v, want a clear busy error", err)
	}
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("waited %v, want about the timeout", elapsed)
	}
}

func TestAcquireHonorsCancellation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first, err := runlock.Acquire(t.Context(), openRoot(t, dir), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if _, waitErr := runlock.Acquire(
		ctx,
		openRoot(t, dir),
		time.Minute,
	); !errors.Is(
		waitErr,
		context.DeadlineExceeded,
	) {
		t.Fatalf("Acquire() error = %v, want the context error", waitErr)
	}
}

// TestLockExcludesOtherProcesses holds the lock in a child process and checks
// this process cannot take it until the child exits.
//
//nolint:paralleltest // Spawns a helper process via the test binary.
func TestLockExcludesOtherProcesses(t *testing.T) {
	if dir := os.Getenv("RUNLOCK_HOLD_DIR"); dir != "" {
		root, err := os.OpenRoot(dir)
		if err != nil {
			os.Exit(2)
		}
		lock, err := runlock.Acquire(context.Background(), root, time.Second)
		if err != nil {
			os.Exit(3)
		}
		_, _ = os.Stdout.WriteString("locked\n")
		time.Sleep(400 * time.Millisecond)
		_ = lock.Release()
		os.Exit(0)
	}
	dir := t.TempDir()
	child := exec.Command(os.Args[0], "-test.run=^TestLockExcludesOtherProcesses$")
	child.Env = append(os.Environ(), "RUNLOCK_HOLD_DIR="+dir)
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if startErr := child.Start(); startErr != nil {
		t.Fatal(startErr)
	}
	buffer := make([]byte, len("locked\n"))
	if _, readErr := stdout.Read(buffer); readErr != nil || string(buffer) != "locked\n" {
		t.Fatalf("child did not report the lock: %q, %v", buffer, readErr)
	}
	if _, busyErr := runlock.Acquire(t.Context(), openRoot(t, dir), 100*time.Millisecond); busyErr == nil {
		t.Fatal("acquired the lock while another process held it")
	}
	lock, err := runlock.Acquire(t.Context(), openRoot(t, dir), 5*time.Second)
	if err != nil {
		t.Fatalf("lock not available after the child released it: %v", err)
	}
	_ = lock.Release()
	if waitErr := child.Wait(); waitErr != nil {
		t.Fatalf("child: %v", waitErr)
	}
}
