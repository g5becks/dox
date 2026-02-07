package source_test

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/lockfile"
	"github.com/g5becks/dox/internal/source"
)

func TestIsSingleFilePath(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		path string
		want bool
	}{
		{path: "docs", want: false},
		{path: "docs/", want: false},
		{path: "docs/overview.md", want: true},
		{path: "docs/guide.MDX", want: true},
		{path: "docs/README.txt", want: true},
		{path: "docs/spec.rst", want: true},
		{path: "docs/weird.md/", want: false},
		{path: "docs/other.go", want: false},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()

			got := source.IsSingleFilePath(tc.path)
			if got != tc.want {
				t.Fatalf("IsSingleFilePath(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestBuildFileMapFiltersByBaseAndPatterns(t *testing.T) {
	t.Parallel()

	src := source.TestableGitHubSource(t, "widgets", config.Source{
		Repo:     "acme/widgets",
		Path:     "docs",
		Patterns: []string{"**/*.md", "**/*.txt"},
		Exclude:  []string{"**/skip.md"},
	}, source.NewMockGitHubClient(t, map[string]source.MockHTTPResponse{
		"/repos/acme/widgets/git/trees/main?recursive=1": {
			Body: `{
  "sha": "tree-sha",
  "truncated": false,
  "tree": [
    {"path":"docs/getting-started.md","type":"blob","sha":"sha-1"},
    {"path":"docs/skip.md","type":"blob","sha":"sha-2"},
    {"path":"docs/sub/notes.txt","type":"blob","sha":"sha-3"},
    {"path":"docs/ignored.go","type":"blob","sha":"sha-4"},
    {"path":"other/outside.md","type":"blob","sha":"sha-5"},
    {"path":"docs/subdir","type":"tree","sha":"sha-tree"}
  ]
}`,
		},
	}), "main")

	result, err := src.Sync(context.Background(), t.TempDir(), nil, source.SyncOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if result.Downloaded != 2 {
		t.Fatalf("Downloaded = %d, want 2", result.Downloaded)
	}

	if result.LockEntry == nil {
		t.Fatalf("LockEntry = nil, want non-nil")
	}

	if result.LockEntry.Files["getting-started.md"] != "sha-1" {
		t.Fatalf("Files[getting-started.md] = %q, want sha-1", result.LockEntry.Files["getting-started.md"])
	}

	if result.LockEntry.Files["sub/notes.txt"] != "sha-3" {
		t.Fatalf("Files[sub/notes.txt] = %q, want sha-3", result.LockEntry.Files["sub/notes.txt"])
	}
}

func TestSyncDirectoryDryRunComputesDiff(t *testing.T) {
	t.Parallel()

	src := source.TestableGitHubSource(t, "widgets", config.Source{
		Repo:     "acme/widgets",
		Path:     "docs",
		Patterns: []string{"**/*.md", "**/*.txt"},
	}, source.NewMockGitHubClient(t, map[string]source.MockHTTPResponse{
		"/repos/acme/widgets/git/trees/main?recursive=1": {
			Body: `{
  "sha": "tree-new",
  "truncated": false,
  "tree": [
    {"path":"docs/a.md","type":"blob","sha":"sha-a-new"},
    {"path":"docs/b.txt","type":"blob","sha":"sha-b"},
    {"path":"docs/ignored.go","type":"blob","sha":"sha-go"}
  ]
}`,
		},
	}), "main")

	prevLock := &lockfile.LockEntry{
		Type:    "github",
		TreeSHA: "tree-old",
		Files: map[string]string{
			"a.md":   "sha-a-old",
			"old.md": "sha-old",
		},
	}

	result, err := src.Sync(context.Background(), t.TempDir(), prevLock, source.SyncOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if result.Downloaded != 2 {
		t.Fatalf("Downloaded = %d, want 2", result.Downloaded)
	}

	if result.Deleted != 1 {
		t.Fatalf("Deleted = %d, want 1", result.Deleted)
	}

	if result.LockEntry == nil || result.LockEntry.TreeSHA != "tree-new" {
		t.Fatalf("TreeSHA = %v, want tree-new", result.LockEntry)
	}
}

func TestSyncDirectorySkipsWhenTreeSHAUnchanged(t *testing.T) {
	t.Parallel()

	src := source.TestableGitHubSource(t, "widgets", config.Source{
		Repo:     "acme/widgets",
		Path:     "docs",
		Patterns: []string{"**/*.md"},
	}, source.NewMockGitHubClient(t, map[string]source.MockHTTPResponse{
		"/repos/acme/widgets/git/trees/main?recursive=1": {
			Body: `{"sha":"tree-same","truncated":false,"tree":[]}`,
		},
	}), "main")

	prevLock := &lockfile.LockEntry{
		Type:    "github",
		TreeSHA: "tree-same",
		Files: map[string]string{
			"a.md": "sha-a",
		},
	}

	result, err := src.Sync(context.Background(), t.TempDir(), prevLock, source.SyncOptions{})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if !result.Skipped {
		t.Fatalf("Skipped = %v, want true", result.Skipped)
	}
}

func TestSyncSingleFileDownloadsChangedBlob(t *testing.T) {
	t.Parallel()

	destDir := t.TempDir()
	encoded := base64.StdEncoding.EncodeToString([]byte("hello docs"))

	src := source.TestableGitHubSource(t, "widgets", config.Source{
		Repo: "acme/widgets",
		Path: "docs/overview.md",
	}, source.NewMockGitHubClient(t, map[string]source.MockHTTPResponse{
		"/repos/acme/widgets/contents/docs/overview.md?ref=main": {
			Body: `{"type":"file","sha":"blob-1"}`,
		},
		"/repos/acme/widgets/git/blobs/blob-1": {
			Body: `{"encoding":"base64","content":"` + encoded + `"}`,
		},
	}), "main")

	prevLock := &lockfile.LockEntry{
		Type: "github",
		Files: map[string]string{
			"overview.md": "old-sha",
		},
	}

	result, err := src.Sync(context.Background(), destDir, prevLock, source.SyncOptions{})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if result.Downloaded != 1 {
		t.Fatalf("Downloaded = %d, want 1", result.Downloaded)
	}

	content, err := os.ReadFile(filepath.Join(destDir, "overview.md"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if string(content) != "hello docs" {
		t.Fatalf("file content = %q, want %q", string(content), "hello docs")
	}
}

func TestSyncSingleFileSkipsWhenSHAUnchanged(t *testing.T) {
	t.Parallel()

	destDir := t.TempDir()

	src := source.TestableGitHubSource(t, "widgets", config.Source{
		Repo: "acme/widgets",
		Path: "docs/overview.md",
	}, source.NewMockGitHubClient(t, map[string]source.MockHTTPResponse{
		"/repos/acme/widgets/contents/docs/overview.md?ref=main": {
			Body: `{"type":"file","sha":"same-sha"}`,
		},
	}), "main")

	prevLock := &lockfile.LockEntry{
		Type: "github",
		Files: map[string]string{
			"overview.md": "same-sha",
		},
	}

	result, err := src.Sync(context.Background(), destDir, prevLock, source.SyncOptions{})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if !result.Skipped {
		t.Fatalf("Skipped = %v, want true", result.Skipped)
	}

	if _, statErr := os.Stat(filepath.Join(destDir, "overview.md")); !os.IsNotExist(statErr) {
		t.Fatalf("overview.md exists unexpectedly")
	}
}
