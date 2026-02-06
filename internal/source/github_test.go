package source

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/lockfile"
	"resty.dev/v3"
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
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()

			got := isSingleFilePath(tc.path)
			if got != tc.want {
				t.Fatalf("isSingleFilePath(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestBuildFileMapFiltersByBaseAndPatterns(t *testing.T) {
	t.Parallel()

	src := &githubSource{
		source: config.Source{
			Path:     "docs",
			Patterns: []string{"**/*.md", "**/*.txt"},
			Exclude:  []string{"**/skip.md"},
		},
	}

	tree := []githubTreeEntry{
		{Path: "docs/getting-started.md", Type: "blob", SHA: "sha-1"},
		{Path: "docs/skip.md", Type: "blob", SHA: "sha-2"},
		{Path: "docs/sub/notes.txt", Type: "blob", SHA: "sha-3"},
		{Path: "docs/ignored.go", Type: "blob", SHA: "sha-4"},
		{Path: "other/outside.md", Type: "blob", SHA: "sha-5"},
		{Path: "docs/subdir", Type: "tree", SHA: "sha-tree"},
	}

	files, err := src.buildFileMap(tree)
	if err != nil {
		t.Fatalf("buildFileMap() error = %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("buildFileMap() len = %d, want 2", len(files))
	}

	if files["getting-started.md"] != "sha-1" {
		t.Fatalf("buildFileMap() getting-started.md = %q, want sha-1", files["getting-started.md"])
	}

	if files["sub/notes.txt"] != "sha-3" {
		t.Fatalf("buildFileMap() sub/notes.txt = %q, want sha-3", files["sub/notes.txt"])
	}
}

func TestSyncDirectoryDryRunComputesDiff(t *testing.T) {
	t.Parallel()

	src := &githubSource{
		name:   "widgets",
		source: config.Source{Path: "docs", Patterns: []string{"**/*.md", "**/*.txt"}},
		owner:  "acme",
		repo:   "widgets",
		client: newMockGitHubClient(t, map[string]mockHTTPResponse{
			"/repos/acme/widgets/git/trees/main?recursive=1": {
				body: `{
  "sha": "tree-new",
  "truncated": false,
  "tree": [
    {"path":"docs/a.md","type":"blob","sha":"sha-a-new"},
    {"path":"docs/b.txt","type":"blob","sha":"sha-b"},
    {"path":"docs/ignored.go","type":"blob","sha":"sha-go"}
  ]
}`,
			},
		}),
		resolvedRef: "main",
	}

	prevLock := &lockfile.LockEntry{
		Type:    "github",
		TreeSHA: "tree-old",
		Files: map[string]string{
			"a.md":   "sha-a-old",
			"old.md": "sha-old",
		},
	}

	result, err := src.syncDirectory(context.Background(), t.TempDir(), prevLock, SyncOptions{DryRun: true})
	if err != nil {
		t.Fatalf("syncDirectory() error = %v", err)
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

	src := &githubSource{
		name:   "widgets",
		source: config.Source{Path: "docs", Patterns: []string{"**/*.md"}},
		owner:  "acme",
		repo:   "widgets",
		client: newMockGitHubClient(t, map[string]mockHTTPResponse{
			"/repos/acme/widgets/git/trees/main?recursive=1": {
				body: `{"sha":"tree-same","truncated":false,"tree":[]}`,
			},
		}),
		resolvedRef: "main",
	}

	prevLock := &lockfile.LockEntry{
		Type:    "github",
		TreeSHA: "tree-same",
		Files: map[string]string{
			"a.md": "sha-a",
		},
	}

	result, err := src.syncDirectory(context.Background(), t.TempDir(), prevLock, SyncOptions{})
	if err != nil {
		t.Fatalf("syncDirectory() error = %v", err)
	}

	if !result.Skipped {
		t.Fatalf("Skipped = %v, want true", result.Skipped)
	}
}

func TestSyncSingleFileDownloadsChangedBlob(t *testing.T) {
	t.Parallel()

	destDir := t.TempDir()
	encoded := base64.StdEncoding.EncodeToString([]byte("hello docs"))
	src := &githubSource{
		name:   "widgets",
		source: config.Source{Path: "docs/overview.md"},
		owner:  "acme",
		repo:   "widgets",
		client: newMockGitHubClient(t, map[string]mockHTTPResponse{
			"/repos/acme/widgets/contents/docs/overview.md?ref=main": {
				body: `{"type":"file","sha":"blob-1"}`,
			},
			"/repos/acme/widgets/git/blobs/blob-1": {
				body: `{"encoding":"base64","content":"` + encoded + `"}`,
			},
		}),
		resolvedRef: "main",
	}

	prevLock := &lockfile.LockEntry{
		Type: "github",
		Files: map[string]string{
			"overview.md": "old-sha",
		},
	}

	result, err := src.syncSingleFile(context.Background(), destDir, prevLock, SyncOptions{})
	if err != nil {
		t.Fatalf("syncSingleFile() error = %v", err)
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
	src := &githubSource{
		name:   "widgets",
		source: config.Source{Path: "docs/overview.md"},
		owner:  "acme",
		repo:   "widgets",
		client: newMockGitHubClient(t, map[string]mockHTTPResponse{
			"/repos/acme/widgets/contents/docs/overview.md?ref=main": {
				body: `{"type":"file","sha":"same-sha"}`,
			},
		}),
		resolvedRef: "main",
	}

	prevLock := &lockfile.LockEntry{
		Type: "github",
		Files: map[string]string{
			"overview.md": "same-sha",
		},
	}

	result, err := src.syncSingleFile(context.Background(), destDir, prevLock, SyncOptions{})
	if err != nil {
		t.Fatalf("syncSingleFile() error = %v", err)
	}

	if !result.Skipped {
		t.Fatalf("Skipped = %v, want true", result.Skipped)
	}

	if _, err := os.Stat(filepath.Join(destDir, "overview.md")); !os.IsNotExist(err) {
		t.Fatalf("overview.md exists unexpectedly")
	}
}

type mockHTTPResponse struct {
	status int
	body   string
	header http.Header
}

type mockHTTPTransport struct {
	t         *testing.T
	responses map[string]mockHTTPResponse
}

func newMockGitHubClient(t *testing.T, responses map[string]mockHTTPResponse) *resty.Client {
	t.Helper()

	client := resty.New().SetBaseURL("https://api.github.test")
	client.SetTransport(&mockHTTPTransport{
		t:         t,
		responses: responses,
	})

	return client
}

func (m *mockHTTPTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m.t.Helper()

	key := req.URL.Path
	if req.URL.RawQuery != "" {
		key += "?" + req.URL.RawQuery
	}

	response, ok := m.responses[key]
	if !ok {
		m.t.Fatalf("unexpected request %s %s", req.Method, key)
	}

	status := response.status
	if status == 0 {
		status = http.StatusOK
	}

	header := response.header
	if header == nil {
		header = make(http.Header)
	}
	if header.Get("Content-Type") == "" {
		header.Set("Content-Type", "application/json")
	}

	return &http.Response{
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		StatusCode:    status,
		Header:        header,
		Body:          io.NopCloser(strings.NewReader(response.body)),
		ContentLength: int64(len(response.body)),
		Request:       req,
	}, nil
}
