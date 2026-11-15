package sync_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/sync"
)

// A changed source configuration makes the source stale even when upstream
// answers 304 Not Modified, and a real sync then applies it (dox-9l5.2).
func TestConfigChangeResyncsSource(t *testing.T) {
	t.Parallel()
	var full atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		full.Add(1)
		_, _ = w.Write([]byte("guide\n"))
	}))
	t.Cleanup(server.Close)

	project := t.TempDir()
	cfg := &config.Config{
		ConfigDir: project,
		Output:    filepath.Join(project, ".dox"),
		Sources: map[string]config.Source{
			"docs": {Type: "url", URL: server.URL + "/guide.txt", Filename: "guide.txt"},
		},
	}
	if _, err := sync.Run(t.Context(), cfg, sync.Options{}); err != nil {
		t.Fatal(err)
	}

	// Unchanged config: current, and a sync skips without a full download.
	if result, err := sync.Run(t.Context(), cfg, sync.Options{Check: true}); err != nil || len(result.Stale) != 0 {
		t.Fatalf("check with unchanged config = %+v, %v; want current", result, err)
	}
	before := full.Load()
	if _, err := sync.Run(t.Context(), cfg, sync.Options{}); err != nil {
		t.Fatal(err)
	}
	if full.Load() != before {
		t.Fatalf("unchanged config downloaded again")
	}

	// Rename the file: the lock's validators no longer describe this config.
	cfg.Sources["docs"] = config.Source{Type: "url", URL: server.URL + "/guide.txt", Filename: "renamed.txt"}
	result, err := sync.Run(t.Context(), cfg, sync.Options{Check: true})
	if err == nil || !slices.Equal(result.Stale, []string{"docs"}) {
		t.Fatalf("check after config change = %+v, %v; want docs stale", result, err)
	}
	if _, resyncErr := sync.Run(t.Context(), cfg, sync.Options{}); resyncErr != nil {
		t.Fatal(resyncErr)
	}
	if _, statErr := os.Stat(filepath.Join(project, ".dox", "docs", "renamed.txt")); statErr != nil {
		t.Fatalf("renamed.txt not written after the config change: %v", statErr)
	}
	if after, afterErr := sync.Run(
		t.Context(),
		cfg,
		sync.Options{Check: true},
	); afterErr != nil ||
		len(after.Stale) != 0 {
		t.Fatalf("check after the resync = %+v, %v; want current", after, afterErr)
	}
	if _, lockedErr := sync.Run(t.Context(), cfg, sync.Options{Locked: true}); lockedErr != nil {
		t.Fatalf("--locked after the resync: %v", lockedErr)
	}
}
