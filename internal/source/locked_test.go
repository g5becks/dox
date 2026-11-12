package source_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/lockfile"
	"github.com/g5becks/dox/internal/source"
	"github.com/g5becks/dox/internal/testutil/githubfixture"
)

func TestLockedGitHubSyncFetchesLockedCommitByteForByte(t *testing.T) {
	t.Parallel()
	f := githubfixture.New(t, 6)
	f.Latency = 0
	firstDir, first := syncInto(t, fixtureSource(t, f))

	// Upstream has moved on (new commit), but --locked must ignore it.
	var commitCalls atomic.Int64
	upstreamMoved := unrelatedCommitHandler(f)
	counting := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/commits/") {
			commitCalls.Add(1)
		}
		upstreamMoved.ServeHTTP(w, r)
	})
	lockedDir := t.TempDir()
	result, err := fixtureSource(t, counting).
		Sync(t.Context(), lockedDir, first.LockEntry, source.SyncOptions{Locked: true, Force: true})
	if err != nil {
		t.Fatalf("locked Sync() error = %v", err)
	}
	if commitCalls.Load() != 0 {
		t.Fatalf("locked sync resolved the ref %d time(s); want the lock's commit", commitCalls.Load())
	}
	if result.LockEntry.Commit != githubfixture.Commit {
		t.Fatalf("locked commit = %q, want %q", result.LockEntry.Commit, githubfixture.Commit)
	}
	assertSameTree(t, firstDir, lockedDir)
}

func TestLockedGitHubSyncWithoutCommitFails(t *testing.T) {
	t.Parallel()
	f := githubfixture.New(t, 1)
	f.Latency = 0
	_, err := fixtureSource(t, f).
		Sync(t.Context(), t.TempDir(), &lockfile.LockEntry{Type: "github"}, source.SyncOptions{Locked: true})
	if detail, ok := errors.AsType[*errs.Error](err); !ok || detail.Code != errs.Config {
		t.Fatalf("Sync() error = %v, want a config error for a lock without commit", err)
	}
}

func urlServer(t *testing.T, body *atomic.Value) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body.Load().(string))
	}))
	t.Cleanup(server.Close)
	return server.URL + "/guide.txt"
}

func TestLockedURLVerifiesContentDigest(t *testing.T) {
	t.Parallel()
	var body atomic.Value
	body.Store("version one\n")
	src := newURLTestSource(t, urlServer(t, &body))
	dest := t.TempDir()
	first, err := src.Sync(t.Context(), dest, nil, source.SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first.LockEntry.Files["guide.txt"], "sha256:") {
		t.Fatalf("lock files = %v, want a sha256 content digest", first.LockEntry.Files)
	}

	if _, lockedErr := src.Sync(
		t.Context(),
		dest,
		first.LockEntry,
		source.SyncOptions{Locked: true},
	); lockedErr != nil {
		t.Fatalf("locked sync of unchanged content: %v", lockedErr)
	}

	body.Store("version two\n")
	_, lockedErr := src.Sync(t.Context(), dest, first.LockEntry, source.SyncOptions{Locked: true})
	if lockedErr == nil || !strings.Contains(lockedErr.Error(), "changed since the lock") {
		t.Fatalf("locked sync of changed content: %v, want a digest mismatch", lockedErr)
	}
	content, readErr := os.ReadFile(filepath.Join(dest, "guide.txt"))
	if readErr != nil || string(content) != "version one\n" {
		t.Fatalf("file after refused locked sync = %q, %v; want the locked content kept", content, readErr)
	}

	_, missingErr := src.Sync(t.Context(), dest, &lockfile.LockEntry{Type: "url"}, source.SyncOptions{Locked: true})
	if detail, ok := errors.AsType[*errs.Error](missingErr); !ok || detail.Code != errs.Config {
		t.Fatalf("locked sync without digest: %v, want a config error", missingErr)
	}
}

func TestURLCheckWithoutValidatorsComparesDigest(t *testing.T) {
	t.Parallel()
	var body atomic.Value
	body.Store("stable\n")
	src := newURLTestSource(t, urlServer(t, &body))
	first, err := src.Sync(t.Context(), t.TempDir(), nil, source.SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	checkDir := t.TempDir()
	current, err := src.Sync(t.Context(), checkDir, first.LockEntry, source.SyncOptions{DryRun: true, Check: true})
	if err != nil || !current.Skipped {
		t.Fatalf("check of unchanged content = %+v, %v; want skipped", current, err)
	}
	body.Store("changed\n")
	stale, err := src.Sync(t.Context(), checkDir, first.LockEntry, source.SyncOptions{DryRun: true, Check: true})
	if err != nil || stale.Downloaded != 1 {
		t.Fatalf("check of changed content = %+v, %v; want would-download", stale, err)
	}
	if entries, _ := os.ReadDir(checkDir); len(entries) != 0 {
		t.Fatalf("check wrote %d entries; want none", len(entries))
	}
}
