package source_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/lockfile"
	"github.com/g5becks/dox/internal/source"
	"github.com/g5becks/dox/internal/testutil/fakegitea"
)

func giteaSource(t *testing.T, f *fakegitea.Server, path, token string) source.Source {
	t.Helper()
	src, err := source.NewWithHTTPClient(
		"docs",
		config.Source{Type: "gitea", Repo: "acme/docs", Path: path, Host: "codeberg.org"},
		token,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	source.ConfigureGitHubTestRetry(src, f.URL+"/api/v1", 1, time.Millisecond, time.Second)
	return src
}

func TestGiteaDirectorySyncPaginatesAndPinsCommit(t *testing.T) {
	t.Parallel()
	for name, minFiles := range map[string]int{"raw": 1 << 30, "archive": 1} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := fakegitea.New(t, "acme", "docs", 45) // 46 entries: 3 pages of 20
			src := giteaSource(t, f, "docs", "")
			source.SetGitHubTarballMinFiles(src, minFiles)
			dest, result := syncInto(t, src)
			// 1 non-recursive root listing (folder lookup) + 3 recursive pages.
			if calls := f.TreeCalls.Load(); calls != 4 {
				t.Fatalf("tree calls = %d, want 4 (pagination)", calls)
			}
			assertGiteaSync(t, f, dest, result)
			if name == "archive" && (f.ArchiveCalls.Load() != 1 || f.RawCalls.Load() != 0) {
				t.Fatalf("archive/raw calls = %d/%d", f.ArchiveCalls.Load(), f.RawCalls.Load())
			}
		})
	}
}

func assertGiteaSync(t *testing.T, f *fakegitea.Server, dest string, result *source.SyncResult) {
	t.Helper()
	entry := result.LockEntry
	if result.Downloaded != 45 || entry.Type != "gitea" || entry.Commit != f.Commit() ||
		entry.TreeSHA != fakegitea.Subtree {
		t.Fatalf("result = %+v, lock = %+v", result, entry)
	}
	for relative, want := range map[string]string{"page-044.md": "# Page 44", "guide/page-000.md": "# Page 0"} {
		content, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(relative)))
		if err != nil || !strings.HasPrefix(string(content), want) {
			t.Fatalf("%s = %q, %v", relative, content, err)
		}
	}
	if entry.Files["guide/page-000.md"] != fakegitea.BlobSHA("docs/guide/page-000.md") {
		t.Fatalf("lock files carry wrong blob ids: %v", entry.Files)
	}
}

func TestGiteaRootPathUsesListingDigest(t *testing.T) {
	t.Parallel()
	f := fakegitea.New(t, "acme", "docs", 3)
	_, first := syncInto(t, giteaSource(t, f, "", ""))
	// Gitea reports the commit SHA as the root tree id, so the root folder is
	// identified by its listing instead.
	if !strings.HasPrefix(first.LockEntry.TreeSHA, "listing:") || first.Downloaded != 4 { // 3 docs + README
		t.Fatalf("root sync = %+v", first.LockEntry)
	}
	f.SetCommit("dddddddddddddddddddddddddddddddddddddddd")
	again, err := giteaSource(t, f, "", "").Sync(t.Context(), t.TempDir(), first.LockEntry, source.SyncOptions{})
	if err != nil || !again.Skipped {
		t.Fatalf("unchanged root after a new commit = %+v, %v; want skipped", again, err)
	}
}

func TestGiteaUnchangedFolderSkipsWithTwoCalls(t *testing.T) {
	t.Parallel()
	f := fakegitea.New(t, "acme", "docs", 30)
	_, first := syncInto(t, giteaSource(t, f, "docs", ""))
	f.SetCommit("dddddddddddddddddddddddddddddddddddddddd")
	before := f.APICalls.Load()
	result, err := giteaSource(t, f, "docs", "").
		Sync(t.Context(), t.TempDir(), first.LockEntry, source.SyncOptions{})
	if err != nil || !result.Skipped {
		t.Fatalf("Sync() = %+v, %v; want skipped", result, err)
	}
	if calls := f.APICalls.Load() - before; calls != 2 {
		t.Fatalf("API calls = %d, want 2 (commit + root tree listing)", calls)
	}
}

func TestGiteaLockedReproduces(t *testing.T) {
	t.Parallel()
	f := fakegitea.New(t, "acme", "docs", 8)
	firstDir, first := syncInto(t, giteaSource(t, f, "docs", ""))
	lockedDir := t.TempDir()
	if _, err := giteaSource(t, f, "docs", "").
		Sync(t.Context(), lockedDir, first.LockEntry, source.SyncOptions{Locked: true, Force: true}); err != nil {
		t.Fatal(err)
	}
	assertSameTree(t, firstDir, lockedDir)
}

func TestGiteaSingleFileAndToken(t *testing.T) {
	t.Parallel()
	f := fakegitea.New(t, "acme", "docs", 3)
	dest, result := syncInto(t, giteaSource(t, f, "docs/page-002.md", "gitea-test"))
	content, err := os.ReadFile(filepath.Join(dest, "page-002.md"))
	if err != nil || result.Downloaded != 1 || !strings.HasPrefix(string(content), "# Page 2") {
		t.Fatalf("single file = %+v, %q, %v", result, content, err)
	}
	if auth := f.Authorizations(); len(auth) == 0 || auth[0] != "token gitea-test" {
		t.Fatalf("Authorization headers = %v, want token scheme", auth)
	}
}

func TestGiteaMissingFolderIsNotFound(t *testing.T) {
	t.Parallel()
	f := fakegitea.New(t, "acme", "docs", 2)
	_, err := giteaSource(t, f, "missing", "").
		Sync(t.Context(), t.TempDir(), &lockfile.LockEntry{}, source.SyncOptions{})
	if detail, ok := errors.AsType[*errs.Error](err); !ok || detail.Code != errs.NotFound {
		t.Fatalf("Sync() error = %v, want NotFound", err)
	}
}

func TestGiteaRateLimitIsClassifiedWithHint(t *testing.T) {
	t.Parallel()
	f := fakegitea.New(t, "acme", "docs", 2)
	f.RateLimitAfter.Store(1)
	_, err := giteaSource(t, f, "docs", "").Sync(t.Context(), t.TempDir(), nil, source.SyncOptions{})
	detail, ok := errors.AsType[*errs.Error](err)
	if !ok || detail.Code != errs.RateLimited || !strings.Contains(detail.Hint, "GITEA_TOKEN") {
		t.Fatalf("Sync() error = %v, want RateLimited with a GITEA_TOKEN hint", err)
	}
	if calls := f.APICalls.Load(); calls != 2 {
		t.Fatalf("API calls = %d, want 2 (a long Retry-After must not be waited on)", calls)
	}
}
