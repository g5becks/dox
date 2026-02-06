package source

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/lockfile"
	"resty.dev/v3"
)

func TestFilenameFromURL(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sourceURL string
		sourceKey string
		want      string
	}{
		{name: "path basename", sourceURL: "https://hono.dev/llms-full.txt", sourceKey: "hono", want: "llms-full.txt"},
		{name: "query only path", sourceURL: "https://example.com/docs/readme.md?lang=en", sourceKey: "docs", want: "readme.md"},
		{name: "trailing slash", sourceURL: "https://example.com/docs/", sourceKey: "docs", want: "docs"},
		{name: "invalid url", sourceURL: ":// bad", sourceKey: "docs", want: "docs.txt"},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := filenameFromURL(tc.sourceKey, tc.sourceURL)
			if got != tc.want {
				t.Fatalf("filenameFromURL(%q, %q) = %q, want %q", tc.sourceKey, tc.sourceURL, got, tc.want)
			}
		})
	}
}

func TestURLSyncDownloadsFileAndUpdatesLock(t *testing.T) {
	t.Parallel()

	source := mustNewURLSource(t, config.Source{
		URL: "https://example.test/llms-full.txt",
	})

	source.client = newMockRestyClient(func(req *http.Request) *http.Response {
		headers := http.Header{}
		headers.Set("ETag", `"abc123"`)
		headers.Set("Last-Modified", "Tue, 15 Jan 2024 10:30:00 GMT")
		return newHTTPResponse(req, http.StatusOK, "doc-body", headers)
	})

	destDir := t.TempDir()
	result, err := source.Sync(context.Background(), destDir, nil, SyncOptions{}, nil)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if result.Downloaded != 1 {
		t.Fatalf("Downloaded = %d, want 1", result.Downloaded)
	}

	if result.LockEntry == nil {
		t.Fatalf("LockEntry = nil, want non-nil")
	}

	if result.LockEntry.ETag != `"abc123"` {
		t.Fatalf("LockEntry.ETag = %q, want %q", result.LockEntry.ETag, `"abc123"`)
	}

	content, err := os.ReadFile(filepath.Join(destDir, "llms-full.txt"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if string(content) != "doc-body" {
		t.Fatalf("file content = %q, want %q", string(content), "doc-body")
	}
}

func TestURLSyncSendsConditionalHeadersAndSkips304(t *testing.T) {
	t.Parallel()

	source := mustNewURLSource(t, config.Source{
		URL: "https://example.test/llms-full.txt",
	})

	var ifNoneMatch string
	var ifModifiedSince string

	source.client = newMockRestyClient(func(req *http.Request) *http.Response {
		ifNoneMatch = req.Header.Get("If-None-Match")
		ifModifiedSince = req.Header.Get("If-Modified-Since")
		return newHTTPResponse(req, http.StatusNotModified, "", nil)
	})

	prevLock := &lockfile.LockEntry{
		Type:     "url",
		ETag:     `"etag-prev"`,
		LastMod:  "Wed, 16 Jan 2024 10:30:00 GMT",
		SyncedAt: time.Now().UTC(),
	}

	result, err := source.Sync(context.Background(), t.TempDir(), prevLock, SyncOptions{}, nil)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if !result.Skipped {
		t.Fatalf("Skipped = %v, want true", result.Skipped)
	}

	if ifNoneMatch != `"etag-prev"` {
		t.Fatalf("If-None-Match = %q, want %q", ifNoneMatch, `"etag-prev"`)
	}

	if ifModifiedSince != "Wed, 16 Jan 2024 10:30:00 GMT" {
		t.Fatalf("If-Modified-Since = %q, want expected value", ifModifiedSince)
	}
}

func TestURLSyncDryRunDoesNotWriteFile(t *testing.T) {
	t.Parallel()

	source := mustNewURLSource(t, config.Source{
		URL: "https://example.test/llms-full.txt",
	})

	source.client = newMockRestyClient(func(req *http.Request) *http.Response {
		return newHTTPResponse(req, http.StatusOK, "dry-run-content", nil)
	})

	destDir := t.TempDir()
	result, err := source.Sync(context.Background(), destDir, nil, SyncOptions{DryRun: true}, nil)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if result.Downloaded != 1 {
		t.Fatalf("Downloaded = %d, want 1", result.Downloaded)
	}

	if _, err := os.Stat(filepath.Join(destDir, "llms-full.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected no file to be written in dry-run mode")
	}
}

func TestURLSyncReturnsErrorOnFailureStatus(t *testing.T) {
	t.Parallel()

	source := mustNewURLSource(t, config.Source{
		URL: "https://example.test/llms-full.txt",
	})

	source.client = newMockRestyClient(func(req *http.Request) *http.Response {
		return newHTTPResponse(req, http.StatusBadGateway, "gateway error", nil)
	})

	_, err := source.Sync(context.Background(), t.TempDir(), nil, SyncOptions{}, nil)
	if err == nil {
		t.Fatalf("Sync() error = nil, want non-nil")
	}

	if !strings.Contains(err.Error(), "non-success status") {
		t.Fatalf("Sync() error = %q, expected status error", err.Error())
	}
}

type roundTripFunc func(*http.Request) *http.Response

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req), nil
}

func newMockRestyClient(handler roundTripFunc) *resty.Client {
	client := resty.New()
	client.SetTransport(handler)
	return client
}

func newHTTPResponse(req *http.Request, status int, body string, header http.Header) *http.Response {
	if header == nil {
		header = make(http.Header)
	}

	return &http.Response{
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		StatusCode:    status,
		Header:        header,
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}
}

func mustNewURLSource(t *testing.T, cfg config.Source) *urlSource {
	t.Helper()

	source, err := NewURL("test-source", cfg)
	if err != nil {
		t.Fatalf("NewURL() error = %v", err)
	}

	urlSource, ok := source.(*urlSource)
	if !ok {
		t.Fatalf("NewURL() returned unexpected type %T", source)
	}

	return urlSource
}
