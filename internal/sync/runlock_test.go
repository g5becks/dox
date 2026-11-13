package sync_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/runlock"
	"github.com/g5becks/dox/internal/sync"
)

func lockedProject(t *testing.T) (*config.Config, *runlock.Lock) {
	t.Helper()
	project := t.TempDir()
	output := filepath.Join(project, ".dox")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("guide\n"))
	}))
	t.Cleanup(server.Close)
	cfg := &config.Config{
		ConfigDir: project,
		Output:    output,
		Sources: map[string]config.Source{
			"docs": {Type: "url", URL: server.URL + "/guide.txt", Filename: "guide.txt"},
		},
	}
	// A first sync creates and marks the output; then another "process" holds the lock.
	if _, err := sync.Run(t.Context(), cfg, sync.Options{}); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(output)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	held, err := runlock.Acquire(t.Context(), root, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Release() })
	return cfg, held
}

func TestSyncFailsClearlyWhileAnotherRunHoldsTheOutput(t *testing.T) {
	t.Parallel()
	cfg, _ := lockedProject(t)
	_, err := sync.Run(t.Context(), cfg, sync.Options{LockTimeout: 150 * time.Millisecond})
	detail, ok := errors.AsType[*errs.Error](err)
	if !ok || detail.Code != errs.Internal || !strings.Contains(err.Error(), "another dox sync or clean") {
		t.Fatalf("Run() error = %v, want a lock timeout", err)
	}
	// Dry runs and --check write nothing, so they do not need the lock.
	if _, checkErr := sync.Run(
		t.Context(),
		cfg,
		sync.Options{Check: true, LockTimeout: 50 * time.Millisecond},
	); checkErr != nil {
		t.Fatalf("--check blocked by the run lock: %v", checkErr)
	}
}

func TestSyncCleanWaitsForTheLockBeforeCleaning(t *testing.T) {
	t.Parallel()
	cfg, held := lockedProject(t)
	guide := filepath.Join(cfg.Output, "docs", "guide.txt")
	done := make(chan error, 1)
	go func() {
		_, err := sync.Run(t.Context(), cfg, sync.Options{Clean: true, LockTimeout: 10 * time.Second})
		done <- err
	}()
	time.Sleep(200 * time.Millisecond)
	// While the other run holds the lock, --clean must not have removed anything.
	if _, err := os.Stat(guide); err != nil {
		t.Fatalf("output cleaned while another run held the lock: %v", err)
	}
	select {
	case err := <-done:
		t.Fatalf("sync finished while the lock was held: %v", err)
	default:
	}
	if err := held.Release(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("sync after release: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("sync did not proceed after the lock was released")
	}
	if _, err := os.Stat(filepath.Join(cfg.Output, runlock.FileName)); err != nil {
		t.Fatalf("clean removed the run lock file: %v", err)
	}
}
