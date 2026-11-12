package source_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/httpx"
	"github.com/g5becks/dox/internal/lockfile"
	"github.com/g5becks/dox/internal/source"
)

func newURLTestSource(t *testing.T, url string) source.Source {
	t.Helper()
	client := httpx.NewClient(0)
	t.Cleanup(client.CloseIdleConnections)
	src, err := source.NewWithHTTPClient(
		"guide",
		config.Source{Type: "url", URL: url, Filename: "guide.txt"},
		"",
		client,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	return src
}

func TestURLDryRunUsesConditionalHEAD(t *testing.T) {
	t.Parallel()
	var methods []string
	var conditional atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		conditional.Store(r.Header.Get("If-None-Match"))
		w.Header().Set("ETag", `"v2"`)
		_, _ = io.WriteString(w, "body that must not be read")
	}))
	t.Cleanup(server.Close)

	dest := t.TempDir()
	prev := &lockfile.LockEntry{Type: "url", ETag: `"v1"`}
	result, err := newURLTestSource(
		t,
		server.URL+"/guide.txt",
	).Sync(t.Context(), dest, prev, source.SyncOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(methods) != 1 || methods[0] != http.MethodHead {
		t.Fatalf("methods = %v, want one HEAD", methods)
	}
	if conditional.Load() != `"v1"` {
		t.Fatalf("If-None-Match = %v, want the previous ETag", conditional.Load())
	}
	if result.Downloaded != 1 || result.Skipped {
		t.Fatalf("dry-run result = %+v, want would-download", result)
	}
	if _, statErr := os.Stat(filepath.Join(dest, "guide.txt")); !os.IsNotExist(statErr) {
		t.Fatal("dry run wrote the file")
	}
}

func TestURLDryRunFallsBackToRangeGETWithoutReadingBody(t *testing.T) {
	t.Parallel()
	var ranges []string
	var written atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		ranges = append(ranges, r.Header.Get("Range"))
		n, _ := w.Write([]byte("x"))
		written.Add(int64(n))
	}))
	t.Cleanup(server.Close)

	result, err := newURLTestSource(t, server.URL+"/guide.txt").
		Sync(t.Context(), t.TempDir(), nil, source.SyncOptions{DryRun: true})
	if err != nil || result.Downloaded != 1 {
		t.Fatalf("Sync() = %+v, %v", result, err)
	}
	if len(ranges) != 1 || ranges[0] != "bytes=0-0" {
		t.Fatalf("fallback Range headers = %v, want [bytes=0-0]", ranges)
	}
}

func TestURLDryRunNotModifiedIsSkipped(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = io.WriteString(w, "fresh")
	}))
	t.Cleanup(server.Close)
	prev := &lockfile.LockEntry{Type: "url", ETag: `"v1"`}
	result, err := newURLTestSource(t, server.URL+"/guide.txt").
		Sync(t.Context(), t.TempDir(), prev, source.SyncOptions{DryRun: true})
	if err != nil || !result.Skipped {
		t.Fatalf("Sync() = %+v, %v; want skipped", result, err)
	}
}

// TestURLSyncStreamsLargeBody runs without t.Parallel so process-wide heap
// statistics are not mixed with other tests' allocations.
//
//nolint:paralleltest // Measures process-wide TotalAlloc; parallel tests would skew it.
func TestURLSyncStreamsLargeBody(t *testing.T) {
	const size = 10 << 20
	chunk := bytes.Repeat([]byte("0123456789abcdef"), 4096) // 64 KiB, reused
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for written := 0; written < size; written += len(chunk) {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	src := newURLTestSource(t, server.URL+"/guide.txt")
	dest := t.TempDir()

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	result, err := src.Sync(t.Context(), dest, nil, source.SyncOptions{})
	runtime.ReadMemStats(&after)
	if err != nil || result.Downloaded != 1 {
		t.Fatalf("Sync() = %+v, %v", result, err)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > size/4 {
		t.Fatalf("sync of a %d-byte body allocated %d bytes; want streaming (< %d)", size, allocated, size/4)
	}
	info, err := os.Stat(filepath.Join(dest, "guide.txt"))
	if err != nil || info.Size() != size {
		t.Fatalf("written file = %v, %v; want %d bytes", info, err, size)
	}
}
