package source_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/lockfile"
	"github.com/g5becks/dox/internal/source"
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
		{
			name: "query only path", sourceURL: "https://example.com/docs/readme.md?lang=en",
			sourceKey: "docs", want: "readme.md",
		},
		{name: "trailing slash", sourceURL: "https://example.com/docs/", sourceKey: "docs", want: "docs"},
		{name: "invalid url", sourceURL: ":// bad", sourceKey: "docs", want: "docs.txt"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := source.FilenameFromURL(tc.sourceKey, tc.sourceURL)
			if got != tc.want {
				t.Fatalf("FilenameFromURL(%q, %q) = %q, want %q", tc.sourceKey, tc.sourceURL, got, tc.want)
			}
		})
	}
}

func TestURLSyncDownloadsFileAndUpdatesLock(t *testing.T) {
	t.Parallel()

	src, setClient := source.TestableURLSource(t, "test-source", config.Source{
		URL: "https://example.test/llms-full.txt",
	})

	setClient(source.NewMockRestyClient(func(req *http.Request) *http.Response {
		headers := http.Header{}
		headers.Set("ETag", `"abc123"`)
		headers.Set("Last-Modified", "Tue, 15 Jan 2024 10:30:00 GMT")

		return source.NewHTTPResponse(req, http.StatusOK, "doc-body", headers)
	}))

	destDir := t.TempDir()

	result, err := src.Sync(context.Background(), destDir, nil, source.SyncOptions{}, nil)
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

	src, setClient := source.TestableURLSource(t, "test-source", config.Source{
		URL: "https://example.test/llms-full.txt",
	})

	var ifNoneMatch string
	var ifModifiedSince string

	setClient(source.NewMockRestyClient(func(req *http.Request) *http.Response {
		ifNoneMatch = req.Header.Get("If-None-Match")
		ifModifiedSince = req.Header.Get("If-Modified-Since")

		return source.NewHTTPResponse(req, http.StatusNotModified, "", nil)
	}))

	prevLock := &lockfile.LockEntry{
		Type:     "url",
		ETag:     `"etag-prev"`,
		LastMod:  "Wed, 16 Jan 2024 10:30:00 GMT",
		SyncedAt: time.Now().UTC(),
	}

	result, err := src.Sync(context.Background(), t.TempDir(), prevLock, source.SyncOptions{}, nil)
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

	src, setClient := source.TestableURLSource(t, "test-source", config.Source{
		URL: "https://example.test/llms-full.txt",
	})

	setClient(source.NewMockRestyClient(func(req *http.Request) *http.Response {
		return source.NewHTTPResponse(req, http.StatusOK, "dry-run-content", nil)
	}))

	destDir := t.TempDir()

	result, err := src.Sync(context.Background(), destDir, nil, source.SyncOptions{DryRun: true}, nil)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if result.Downloaded != 1 {
		t.Fatalf("Downloaded = %d, want 1", result.Downloaded)
	}

	if _, statErr := os.Stat(filepath.Join(destDir, "llms-full.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no file to be written in dry-run mode")
	}
}

func TestURLSyncReturnsErrorOnFailureStatus(t *testing.T) {
	t.Parallel()

	src, setClient := source.TestableURLSource(t, "test-source", config.Source{
		URL: "https://example.test/llms-full.txt",
	})

	setClient(source.NewMockRestyClient(func(req *http.Request) *http.Response {
		return source.NewHTTPResponse(req, http.StatusBadGateway, "gateway error", nil)
	}))

	_, err := src.Sync(context.Background(), t.TempDir(), nil, source.SyncOptions{}, nil)
	if err == nil {
		t.Fatalf("Sync() error = nil, want non-nil")
	}

	if !strings.Contains(err.Error(), "non-success status") {
		t.Fatalf("Sync() error = %q, expected status error", err.Error())
	}
}
