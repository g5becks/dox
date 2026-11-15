package source_test

import (
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/source"
)

// encodingServer behaves like raw.githubusercontent.com: the ETag depends on
// the negotiated encoding (weak for gzip), and 304 is only sent when
// If-None-Match matches the variant being requested.
func encodingServer(t *testing.T, allowHead bool) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var fullBodies atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead && !allowHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		gzipped := strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")
		etag := `"identity"`
		if gzipped {
			etag = `W/"gzip"`
		}
		w.Header().Set("Vary", "Accept-Encoding")
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if r.Method == http.MethodHead || r.Header.Get("Range") != "" {
			return
		}
		fullBodies.Add(1)
		if gzipped {
			w.Header().Set("Content-Encoding", "gzip")
			zw := gzip.NewWriter(w)
			_, _ = zw.Write([]byte("# Guide\n"))
			_ = zw.Close()
			return
		}
		_, _ = w.Write([]byte("# Guide\n"))
	}))
	t.Cleanup(server.Close)
	return server, &fullBodies
}

func TestURLCheckNegotiatesSameEncodingAsSync(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		allowHead bool
	}{
		{"HEAD", true},
		{"ranged GET fallback", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server, fullBodies := encodingServer(t, tc.allowHead)
			src, err := source.NewWithHTTPClient(
				"docs",
				config.Source{Type: "url", URL: server.URL + "/README.md", Filename: "README.md"},
				"",
				server.Client(),
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = src.Close() })

			first, err := src.Sync(t.Context(), t.TempDir(), nil, source.SyncOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if first.LockEntry.ETag != `W/"gzip"` {
				t.Fatalf("recorded ETag = %q, want the gzip variant's", first.LockEntry.ETag)
			}
			checkOpts := source.SyncOptions{DryRun: true, Check: true}
			check, err := src.Sync(t.Context(), t.TempDir(), first.LockEntry, checkOpts)
			if err != nil {
				t.Fatal(err)
			}
			if !check.Skipped || check.Downloaded != 0 {
				t.Fatalf("check right after a sync = %+v, want current", check)
			}
			if fullBodies.Load() != 1 {
				t.Fatalf("full downloads = %d, want only the sync's", fullBodies.Load())
			}
		})
	}
}
