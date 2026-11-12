package source_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/lockfile"
	"github.com/g5becks/dox/internal/source"
	"github.com/g5becks/dox/internal/testutil/fakegitlab"
)

const gitlabProject = "group/sub/project"

func gitlabSource(t *testing.T, f *fakegitlab.Server, path, token string) source.Source {
	t.Helper()
	src, err := source.NewWithHTTPClient(
		"docs",
		config.Source{Type: "gitlab", Repo: gitlabProject, Path: path, Host: "gitlab.com"},
		token,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	source.ConfigureGitHubTestRetry(src, f.URL+"/api/v4", 1, time.Millisecond, time.Second)
	return src
}

func TestGitLabDirectorySyncPaginatesAndPinsCommit(t *testing.T) {
	t.Parallel()
	for name, minFiles := range map[string]int{"raw": 1 << 30, "archive": 1} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := fakegitlab.New(t, gitlabProject, 45) // 3 pages of 20 (+ the guide dir)
			src := gitlabSource(t, f, "docs", "")
			source.SetGitHubTarballMinFiles(src, minFiles)
			dest, result := syncInto(t, src)
			// 1 parent listing + 3 pages of the recursive listing.
			if calls := f.TreeCalls.Load(); calls != 4 {
				t.Fatalf("tree calls = %d, want 4 (pagination)", calls)
			}
			assertGitLabSync(t, f, dest, result)
			if name == "archive" && (f.ArchiveCalls.Load() != 1 || f.RawCalls.Load() != 0) {
				t.Fatalf("archive/raw calls = %d/%d", f.ArchiveCalls.Load(), f.RawCalls.Load())
			}
		})
	}
}

func assertGitLabSync(t *testing.T, f *fakegitlab.Server, dest string, result *source.SyncResult) {
	t.Helper()
	entry := result.LockEntry
	if result.Downloaded != 45 || entry.Type != "gitlab" || entry.Commit != f.Commit() ||
		entry.TreeSHA != fakegitlab.Subtree {
		t.Fatalf("result = %+v, lock = %+v", result, entry)
	}
	for relative, want := range map[string]string{"page-044.md": "# Page 44", "guide/page-000.md": "# Page 0"} {
		content, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(relative)))
		if err != nil || !strings.HasPrefix(string(content), want) {
			t.Fatalf("%s = %q, %v", relative, content, err)
		}
	}
	// Project paths travel URL-encoded, including subgroups.
	if !slices.ContainsFunc(f.EscapedPaths(), func(p string) bool {
		return strings.Contains(p, "/projects/group%2Fsub%2Fproject/")
	}) {
		t.Fatalf("project id not URL-encoded: %v", f.EscapedPaths())
	}
}

func TestGitLabUnchangedFolderSkipsWithTwoCalls(t *testing.T) {
	t.Parallel()
	f := fakegitlab.New(t, gitlabProject, 30)
	_, first := syncInto(t, gitlabSource(t, f, "docs", ""))
	f.SetCommit("dddddddddddddddddddddddddddddddddddddddd") // commit outside docs/
	before := f.APICalls.Load()
	result, err := gitlabSource(t, f, "docs", "").
		Sync(t.Context(), t.TempDir(), first.LockEntry, source.SyncOptions{})
	if err != nil || !result.Skipped {
		t.Fatalf("Sync() = %+v, %v; want skipped", result, err)
	}
	if calls := f.APICalls.Load() - before; calls != 2 {
		t.Fatalf("API calls = %d, want 2 (commit + parent listing)", calls)
	}
}

func TestGitLabLockedReproduces(t *testing.T) {
	t.Parallel()
	f := fakegitlab.New(t, gitlabProject, 8)
	firstDir, first := syncInto(t, gitlabSource(t, f, "docs", ""))
	lockedDir := t.TempDir()
	_, err := gitlabSource(t, f, "docs", "").
		Sync(t.Context(), lockedDir, first.LockEntry, source.SyncOptions{Locked: true, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	assertSameTree(t, firstDir, lockedDir)
}

func TestGitLabSingleFileAndToken(t *testing.T) {
	t.Parallel()
	f := fakegitlab.New(t, gitlabProject, 3)
	dest, result := syncInto(t, gitlabSource(t, f, "docs/page-002.md", "glpat-test"))
	if result.Downloaded != 1 {
		t.Fatalf("Downloaded = %d", result.Downloaded)
	}
	content, err := os.ReadFile(filepath.Join(dest, "page-002.md"))
	if err != nil || !strings.HasPrefix(string(content), "# Page 2") {
		t.Fatalf("page-002.md = %q, %v", content, err)
	}
	want := fakegitlab.BlobID(content)
	if result.LockEntry.Files["page-002.md"] != want {
		t.Fatalf("lock blob id = %q, want %q", result.LockEntry.Files["page-002.md"], want)
	}
	if tokens := f.Tokens(); len(tokens) == 0 || tokens[0] != "glpat-test" {
		t.Fatalf("PRIVATE-TOKEN headers = %v", tokens)
	}
}

func TestGitLabMissingFolderIsNotFound(t *testing.T) {
	t.Parallel()
	f := fakegitlab.New(t, gitlabProject, 2)
	_, err := gitlabSource(t, f, "missing", "").
		Sync(t.Context(), t.TempDir(), &lockfile.LockEntry{}, source.SyncOptions{})
	if detail, ok := errors.AsType[*errs.Error](err); !ok || detail.Code != errs.NotFound {
		t.Fatalf("Sync() error = %v, want NotFound", err)
	}
}

func TestGitLabRateLimitIsClassifiedWithHint(t *testing.T) {
	t.Parallel()
	f := fakegitlab.New(t, gitlabProject, 2)
	f.RateLimitAfter.Store(1) // the commit call succeeds; the next is throttled
	_, err := gitlabSource(t, f, "docs", "").Sync(t.Context(), t.TempDir(), nil, source.SyncOptions{})
	detail, ok := errors.AsType[*errs.Error](err)
	if !ok || detail.Code != errs.RateLimited || !strings.Contains(detail.Hint, "GITLAB_TOKEN") {
		t.Fatalf("Sync() error = %v, want RateLimited with a GITLAB_TOKEN hint", err)
	}
	if _, typed := errors.AsType[*errs.RateLimitError](err); !typed {
		t.Fatal("missing typed RateLimitError")
	}
	if calls := f.APICalls.Load(); calls != 2 {
		t.Fatalf("API calls = %d, want 2 (a long reset must not be retried)", calls)
	}
}
