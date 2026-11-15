package source_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/lockfile"
	"github.com/g5becks/dox/internal/source"
)

const unchangedTree = `{
  "sha": "tree-same",
  "truncated": false,
  "tree": [
    {"path":"a.md","type":"blob","sha":"sha-a"},
    {"path":"skip.md","type":"blob","sha":"sha-skip"},
    {"path":"b.txt","type":"blob","sha":"sha-b"}
  ]
}`

func configChangedSource(t *testing.T, cfg config.Source) source.Source {
	t.Helper()
	cfg.Repo = "acme/widgets"
	cfg.Path = "docs"
	client := source.NewMockGitHubClient(t, map[string]source.MockHTTPResponse{
		"/repos/acme/widgets/git/trees/main:docs?recursive=1": {Body: unchangedTree},
	})
	return source.TestableGitHubSource(t, "widgets", cfg, client, "main")
}

// An unchanged upstream tree is skipped only while the configuration is the
// one recorded in the lock (dox-9l5.2).
func TestUnchangedTreeIsSkippedWhileConfigIsUnchanged(t *testing.T) {
	t.Parallel()
	src := configChangedSource(t, config.Source{Patterns: []string{"**/*.md"}, Exclude: []string{"skip.md"}})
	prev := &lockfile.LockEntry{
		Type:    "github",
		TreeSHA: "tree-same",
		Files:   map[string]string{"a.md": "sha-a", "skip.md": "sha-skip"},
	}

	result, err := src.Sync(t.Context(), t.TempDir(), prev, source.SyncOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Skipped || result.Downloaded != 0 || result.Deleted != 0 {
		t.Fatalf("result = %+v, want skipped", result)
	}
}

func TestAddedExcludeDeletesNewlyExcludedFiles(t *testing.T) {
	t.Parallel()
	src := configChangedSource(t, config.Source{Patterns: []string{"**/*.md"}, Exclude: []string{"skip.md"}})
	prev := &lockfile.LockEntry{
		Type:    "github",
		TreeSHA: "tree-same",
		Files:   map[string]string{"a.md": "sha-a", "skip.md": "sha-skip"},
	}

	dryRun, err := src.Sync(t.Context(), t.TempDir(), prev, source.SyncOptions{DryRun: true, ConfigChanged: true})
	if err != nil {
		t.Fatal(err)
	}
	if dryRun.Skipped || dryRun.Downloaded != 0 || dryRun.Deleted != 1 {
		t.Fatalf("dry run = %+v, want 0 downloads and 1 deletion", dryRun)
	}

	dest := t.TempDir()
	for _, name := range []string{"a.md", "skip.md"} {
		if writeErr := os.WriteFile(filepath.Join(dest, name), []byte("# "+name+"\n"), 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	result, err := src.Sync(t.Context(), dest, prev, source.SyncOptions{ConfigChanged: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Deleted != 1 || result.Downloaded != 0 {
		t.Fatalf("result = %+v, want 1 deletion and no downloads", result)
	}
	if _, statErr := os.Stat(filepath.Join(dest, "skip.md")); !os.IsNotExist(statErr) {
		t.Fatalf("skip.md still present after its exclude was added (stat err %v)", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dest, "a.md")); statErr != nil {
		t.Fatalf("a.md removed: %v", statErr)
	}
	if _, kept := result.LockEntry.Files["skip.md"]; kept {
		t.Fatalf("lock still lists skip.md: %v", result.LockEntry.Files)
	}
}

func TestRemovedExcludeAndWidenedPatternsDownloadNewFiles(t *testing.T) {
	t.Parallel()
	prev := &lockfile.LockEntry{Type: "github", TreeSHA: "tree-same", Files: map[string]string{"a.md": "sha-a"}}

	removed := configChangedSource(t, config.Source{Patterns: []string{"**/*.md"}})
	result, err := removed.Sync(t.Context(), t.TempDir(), prev, source.SyncOptions{DryRun: true, ConfigChanged: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Downloaded != 1 || result.Deleted != 0 {
		t.Fatalf("removed exclude: %+v, want skip.md downloaded only", result)
	}

	widened := configChangedSource(
		t,
		config.Source{Patterns: []string{"**/*.md", "**/*.txt"}, Exclude: []string{"skip.md"}},
	)
	result, err = widened.Sync(t.Context(), t.TempDir(), prev, source.SyncOptions{DryRun: true, ConfigChanged: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Downloaded != 1 || result.Deleted != 0 {
		t.Fatalf("widened patterns: %+v, want b.txt downloaded only", result)
	}

	narrowed := configChangedSource(t, config.Source{Patterns: []string{"**/*.txt"}})
	result, err = narrowed.Sync(t.Context(), t.TempDir(), prev, source.SyncOptions{DryRun: true, ConfigChanged: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Downloaded != 1 || result.Deleted != 1 {
		t.Fatalf("narrowed patterns: %+v, want b.txt downloaded and a.md deleted", result)
	}
}

// A URL source whose config changed is fetched without the previous
// validators, which describe another configuration.
func TestURLConfigChangedIgnoresPreviousValidators(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write([]byte("guide\n"))
	}))
	t.Cleanup(server.Close)

	src, err := source.NewWithHTTPClient(
		"docs",
		config.Source{Type: "url", URL: server.URL + "/guide.txt", Filename: "renamed.txt"},
		"",
		server.Client(),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	prev := &lockfile.LockEntry{Type: "url", ETag: `"v1"`, Files: map[string]string{"guide.txt": "sha256:old"}}

	unchanged, err := src.Sync(t.Context(), t.TempDir(), prev, source.SyncOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !unchanged.Skipped {
		t.Fatalf("without a config change: %+v, want skipped (304)", unchanged)
	}
	changed, err := src.Sync(
		t.Context(),
		t.TempDir(),
		prev,
		source.SyncOptions{DryRun: true, Check: true, ConfigChanged: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Skipped || changed.Downloaded != 1 {
		t.Fatalf("with a config change: %+v, want a download", changed)
	}
}
