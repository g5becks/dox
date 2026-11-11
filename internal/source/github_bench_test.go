package source_test

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/g5becks/dox/internal/source"
	"github.com/g5becks/dox/internal/testutil/githubfixture"
)

func BenchmarkGitHubDirectory(b *testing.B) {
	latency := githubfixture.DefaultLatency
	if value := os.Getenv("DOX_BENCH_LATENCY"); value != "" {
		var err error
		latency, err = time.ParseDuration(value)
		if err != nil {
			b.Fatal(err)
		}
	}
	for _, count := range []int{35, 108} {
		b.Run(fmt.Sprintf("%dfiles", count), func(b *testing.B) {
			f := githubfixture.New(b, count)
			f.Latency = latency
			b.ReportAllocs()
			for b.Loop() {
				benchmarkDirectoryOnce(b, f, count)
			}
			b.ReportMetric(float64(f.RESTCalls.Load())/float64(b.N), "REST/op")
			b.ReportMetric(b.Elapsed().Seconds()/float64(b.N), "s/sync")
			b.ReportMetric(float64(latency.Milliseconds()), "latency-ms")
		})
	}
}

func benchmarkDirectoryOnce(b *testing.B, fixture *githubfixture.Server, count int) {
	b.Helper()
	src := source.NewBenchmarkGitHubSource(b, fixture.URL)
	defer src.Close()
	result, err := src.Sync(b.Context(), b.TempDir(), nil, source.SyncOptions{})
	if err != nil {
		b.Fatal(err)
	}
	if result.Downloaded != count {
		b.Fatalf("downloaded %d, want %d", result.Downloaded, count)
	}
}

func TestGitHubBenchmarkHarness(t *testing.T) {
	t.Parallel()
	for _, count := range []int{35, 108} {
		t.Run(fmt.Sprintf("%dfiles", count), func(t *testing.T) {
			t.Parallel()
			f := githubfixture.New(t, count)
			f.Latency = 0
			src := source.NewBenchmarkGitHubSource(t, f.URL)
			defer src.Close()
			destination := t.TempDir()
			result, err := src.Sync(t.Context(), destination, nil, source.SyncOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if result.Downloaded != count {
				t.Errorf("downloaded %d, want %d", result.Downloaded, count)
			}
			files, err := filepath.Glob(filepath.Join(destination, "*.md"))
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != count {
				t.Errorf("written files = %d, want %d", len(files), count)
			}
			if f.RESTCalls.Load() != int64(count+2) {
				t.Errorf("baseline REST calls = %d, want %d", f.RESTCalls.Load(), count+2)
			}
		})
	}
}

func TestGitHubFixtureRawAndArchive(t *testing.T) {
	t.Parallel()
	f := githubfixture.New(t, 35)
	f.Latency = 0
	raw := getFixtureResponse(t, f.URL+"/raw/acme/docs/"+githubfixture.Commit+"/docs/page-000.md")
	if raw.StatusCode != http.StatusOK {
		t.Fatalf("raw status = %d", raw.StatusCode)
	}
	_ = raw.Body.Close()
	response := getFixtureResponse(t, f.URL+"/codeload/acme/docs/tar.gz/"+githubfixture.Commit)
	defer response.Body.Close()
	compressed, err := gzip.NewReader(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	defer compressed.Close()
	archive := tar.NewReader(compressed)
	for range 35 {
		header, readErr := archive.Next()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if header.Size == 0 {
			t.Error("empty fixture file")
		}
	}
	if f.RawCalls.Load() != 1 || f.ArchiveCalls.Load() != 1 || f.RESTCalls.Load() != 0 {
		t.Fatal("raw/archive requests must not consume REST quota")
	}
}

func getFixtureResponse(t *testing.T, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
