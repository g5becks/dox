package source_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/lockfile"
	"github.com/g5becks/dox/internal/source"
	"github.com/g5becks/dox/internal/testutil/fakegithub"
)

func TestURLUsesSuppliedRootAfterRename(t *testing.T) {
	t.Parallel()
	parent, victim := t.TempDir(), t.TempDir()
	destination := filepath.Join(parent, "docs")
	if err := os.Mkdir(destination, 0750); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	moved := filepath.Join(parent, "moved")
	if renameErr := os.Rename(destination, moved); renameErr != nil {
		t.Fatal(renameErr)
	}
	if symlinkErr := os.Symlink(victim, destination); symlinkErr != nil {
		t.Fatal(symlinkErr)
	}
	src, setClient := source.TestableURLSource(t, "docs", config.Source{URL: "https://example.test/guide.txt"})
	defer src.Close()
	setClient(
		source.NewMockRestyClient(
			func(req *http.Request) *http.Response { return source.NewHTTPResponse(req, http.StatusOK, "safe", nil) },
		),
	)
	if _, syncErr := src.Sync(t.Context(), destination, nil, source.SyncOptions{Root: root}); syncErr != nil {
		t.Fatal(syncErr)
	}
	actual, err := os.ReadFile(filepath.Join(moved, "guide.txt"))
	if err != nil || string(actual) != "safe" {
		t.Fatalf("opened source root lost: %q, %v", actual, err)
	}
	entries, err := os.ReadDir(victim)
	if err != nil || len(entries) != 0 {
		t.Fatalf("victim changed: %v, %v", entries, err)
	}
}

func TestStaleLockCannotDeleteParent(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	victim := filepath.Join(parent, "sentinel")
	if err := os.WriteFile(victim, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	server := fakegithub.New(t, 1)
	src := auditGitHub(t, server)
	previous := &lockfile.LockEntry{TreeSHA: "old", Files: map[string]string{"../sentinel": "old"}}
	_, err := src.Sync(t.Context(), filepath.Join(parent, "docs"), previous, source.SyncOptions{})
	if err == nil {
		t.Fatal("stale parent path accepted")
	}
	actual, readErr := os.ReadFile(victim)
	if readErr != nil || string(actual) != "keep" {
		t.Fatalf("parent sentinel changed: %q, %v", actual, readErr)
	}
}
