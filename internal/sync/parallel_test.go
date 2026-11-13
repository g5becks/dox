package sync_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	gosync "sync"
	"sync/atomic"
	"testing"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/lockfile"
	"github.com/g5becks/dox/internal/sync"
)

// TestParallelSyncKeepsEveryLockEntry runs more sources than workers so the
// scheduler, the shared lock and the event callback are all contended, then
// checks that the lock, the files on disk and the result counters agree. It is
// meaningful under -race.
func TestParallelSyncKeepsEveryLockEntry(t *testing.T) {
	t.Parallel()

	const (
		sources = 24
		workers = 8
	)
	failing := map[int]bool{5: true, 11: true, 17: true}

	body := func(i int) string { return fmt.Sprintf("# source %02d\n\nbody for source %02d\n", i, i) }

	var served atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served.Add(1)
		var i int
		if _, err := fmt.Sscanf(r.URL.Path, "/doc-%d.md", &i); err != nil || failing[i] {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body(i)))
	}))
	t.Cleanup(server.Close)

	project := t.TempDir()
	output := filepath.Join(project, ".dox")
	cfg := &config.Config{
		ConfigDir: project,
		Output:    output,
		Sources:   make(map[string]config.Source, sources),
	}
	for i := range sources {
		cfg.Sources[fmt.Sprintf("src-%02d", i)] = config.Source{
			Type:     "url",
			URL:      fmt.Sprintf("%s/doc-%d.md", server.URL, i),
			Filename: "doc.md",
		}
	}

	var (
		mu     gosync.Mutex
		starts = map[string]int{}
		dones  = map[string]int{}
	)
	result, err := sync.Run(t.Context(), cfg, sync.Options{
		MaxParallel: workers,
		OnEvent: func(e sync.Event) {
			mu.Lock()
			defer mu.Unlock()
			if e.Kind == sync.EventSourceStart {
				starts[e.Source]++
			}
			if e.Kind == sync.EventSourceDone {
				dones[e.Source]++
			}
		},
	})
	if err == nil {
		t.Fatal("Run() error = nil, want a partial failure for the 404 sources")
	}

	wantOK := sources - len(failing)
	if result == nil || result.Sources != sources || result.Downloaded != wantOK || result.Errors != len(failing) {
		t.Fatalf("RunResult = %+v, want %d sources, %d downloaded, %d errors", result, sources, wantOK, len(failing))
	}
	if got := served.Load(); got < sources {
		t.Fatalf("server saw %d requests, want at least %d", got, sources)
	}

	assertLockMatchesDisk(t, output, sources, failing, body)

	mu.Lock()
	defer mu.Unlock()
	for i := range sources {
		name := fmt.Sprintf("src-%02d", i)
		if starts[name] != 1 || dones[name] != 1 {
			t.Errorf("%s events: %d start, %d done, want 1 each", name, starts[name], dones[name])
		}
	}
}

func assertLockMatchesDisk(
	t *testing.T,
	output string,
	sources int,
	failing map[int]bool,
	body func(int) string,
) {
	t.Helper()

	lock, err := lockfile.Load(output)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(lock.Sources), sources-len(failing); got != want {
		t.Fatalf("lock has %d entries, want %d", got, want)
	}

	for i := range sources {
		name := fmt.Sprintf("src-%02d", i)
		entry := lock.GetEntry(name)
		if failing[i] {
			if entry != nil {
				t.Errorf("failed source %s has a lock entry: %+v", name, entry)
			}
			continue
		}
		if entry == nil {
			t.Errorf("source %s is missing from the lock", name)
			continue
		}

		sum := sha256.Sum256([]byte(body(i)))
		if got, want := entry.Files["doc.md"], "sha256:"+hex.EncodeToString(sum[:]); got != want {
			t.Errorf("%s lock digest = %q, want %q", name, got, want)
		}
		onDisk, readErr := os.ReadFile(filepath.Join(output, name, "doc.md"))
		if readErr != nil || string(onDisk) != body(i) {
			t.Errorf("%s file on disk = %q, %v; want %q", name, onDisk, readErr, body(i))
		}
	}
}
