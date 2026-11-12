package source_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/source"
	"github.com/g5becks/dox/internal/testutil/githubfixture"
)

// fixtureSource returns a GitHub source wired to handler, which serves the
// githubfixture layout (API under /api, /raw, /codeload).
func fixtureSource(t *testing.T, handler http.Handler) source.Source {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	src, err := source.NewGitHub("docs", config.Source{Repo: "acme/docs", Path: "docs"}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	source.ConfigureGitHubTestRetry(src, server.URL+"/api", 1, time.Millisecond, time.Second)
	return src
}

func syncInto(t *testing.T, src source.Source) (string, *source.SyncResult) {
	t.Helper()
	dest := t.TempDir()
	result, err := src.Sync(t.Context(), dest, nil, source.SyncOptions{})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	return dest, result
}

func TestDirectorySyncPinsCommitInLock(t *testing.T) {
	t.Parallel()
	f := githubfixture.New(t, 3)
	f.Latency = 0
	_, result := syncInto(t, fixtureSource(t, f))
	entry := result.LockEntry
	if entry.Commit != githubfixture.Commit || entry.RefResolved != "HEAD" || entry.TreeSHA != githubfixture.Tree {
		t.Fatalf("lock entry = %+v, want commit %s, ref HEAD, tree %s", entry, githubfixture.Commit, githubfixture.Tree)
	}
}

// quotaHandler emulates an unauthenticated quota: API calls beyond limit get
// 403 with X-RateLimit-Remaining: 0. Raw and archive routes are not counted.
func quotaHandler(next http.Handler, limit int64) http.Handler {
	var used atomic.Int64
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && used.Add(1) > limit {
			w.Header().Set("X-Ratelimit-Remaining", "0")
			w.Header().Set("X-Ratelimit-Reset", "4102444800")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func TestDirectorySync108FilesFitsUnauthenticatedQuota(t *testing.T) {
	t.Parallel()
	for name, minFiles := range map[string]int{"tarball": 0, "raw": 1 << 30} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := githubfixture.New(t, 108)
			f.Latency = 0
			src := fixtureSource(t, quotaHandler(f, 60))
			source.SetGitHubTarballMinFiles(src, minFiles)
			_, result := syncInto(t, src)
			if result.Downloaded != 108 {
				t.Fatalf("Downloaded = %d, want 108", result.Downloaded)
			}
			if got := f.RESTCalls.Load(); got > 3 {
				t.Fatalf("REST calls = %d, want <= 3", got)
			}
		})
	}
}

func TestTarballAboveThresholdIsByteIdenticalToRaw(t *testing.T) {
	t.Parallel()
	tarFixture := githubfixture.New(t, 108)
	tarFixture.Latency = 0
	tarDir, tarResult := syncInto(t, fixtureSource(t, tarFixture))
	if tarFixture.ArchiveCalls.Load() != 1 || tarFixture.RawCalls.Load() != 0 {
		t.Fatalf("archive/raw calls = %d/%d, want the tarball path above the threshold",
			tarFixture.ArchiveCalls.Load(), tarFixture.RawCalls.Load())
	}

	rawFixture := githubfixture.New(t, 108)
	rawFixture.Latency = 0
	rawSrc := fixtureSource(t, rawFixture)
	source.SetGitHubTarballMinFiles(rawSrc, 1<<30)
	rawDir, rawResult := syncInto(t, rawSrc)
	if rawFixture.ArchiveCalls.Load() != 0 || rawFixture.RawCalls.Load() != 108 {
		t.Fatalf("archive/raw calls = %d/%d, want the per-file path",
			rawFixture.ArchiveCalls.Load(), rawFixture.RawCalls.Load())
	}

	assertSameTree(t, tarDir, rawDir)
	if tarResult.LockEntry.Commit != rawResult.LockEntry.Commit || len(tarResult.LockEntry.Files) != 108 {
		t.Fatalf("lock entries differ: %+v vs %+v", tarResult.LockEntry, rawResult.LockEntry)
	}
}

func TestBelowThresholdUsesRawWithBoundedConcurrency(t *testing.T) {
	t.Parallel()
	f := githubfixture.New(t, 35)
	f.Latency = 20 * time.Millisecond
	src := fixtureSource(t, f)
	dest := t.TempDir()
	result, err := src.Sync(t.Context(), dest, nil, source.SyncOptions{DownloadConcurrency: 4})
	if err != nil || result.Downloaded != 35 {
		t.Fatalf("Sync() = %+v, %v", result, err)
	}
	if f.ArchiveCalls.Load() != 0 || f.RawCalls.Load() != 35 {
		t.Fatalf("archive/raw calls = %d/%d, want 0/35", f.ArchiveCalls.Load(), f.RawCalls.Load())
	}
	if peak := f.MaxActive.Load(); peak < 2 || peak > 4 {
		t.Fatalf("peak concurrent requests = %d, want 2..4 (download_concurrency 4)", peak)
	}
}

func TestTarballFailureFallsBackToRaw(t *testing.T) {
	t.Parallel()
	f := githubfixture.New(t, 70)
	f.Latency = 0
	failingArchive := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/codeload/") {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		f.ServeHTTP(w, r)
	})
	dest, result := syncInto(t, fixtureSource(t, failingArchive))
	if result.Downloaded != 70 || f.RawCalls.Load() != 70 {
		t.Fatalf("Downloaded = %d, raw calls = %d; want 70 raw fallbacks", result.Downloaded, f.RawCalls.Load())
	}
	content, err := os.ReadFile(filepath.Join(dest, "page-069.md"))
	if err != nil || !bytes.HasPrefix(content, []byte("# Page 69")) {
		t.Fatalf("page-069.md = %q, %v", content, err)
	}
}

func TestAuthenticatedTarballUsesAPIRedirectWithinThreeRESTCalls(t *testing.T) {
	t.Parallel()
	f := githubfixture.New(t, 108)
	f.Latency = 0
	src := fixtureSource(t, f)
	source.SetGitHubToken(src, "test-token")
	_, result := syncInto(t, src)
	if result.Downloaded != 108 || f.ArchiveCalls.Load() != 1 {
		t.Fatalf("Downloaded = %d, archive calls = %d", result.Downloaded, f.ArchiveCalls.Load())
	}
	if got := f.RESTCalls.Load(); got != 3 {
		t.Fatalf("REST calls = %d, want 3 (commit, tree, tarball redirect)", got)
	}
}

func assertSameTree(t *testing.T, left, right string) {
	t.Helper()
	read := func(root string) map[string][]byte {
		files := map[string][]byte{}
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() {
				return walkErr
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			relative, relErr := filepath.Rel(root, path)
			files[relative] = data
			return relErr
		})
		if err != nil {
			t.Fatal(err)
		}
		return files
	}
	leftFiles, rightFiles := read(left), read(right)
	if len(leftFiles) != len(rightFiles) {
		t.Fatalf("file counts differ: %d vs %d", len(leftFiles), len(rightFiles))
	}
	for name, data := range leftFiles {
		if !bytes.Equal(data, rightFiles[name]) {
			t.Fatalf("%s differs between tarball and raw output", name)
		}
	}
}
