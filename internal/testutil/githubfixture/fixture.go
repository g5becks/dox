// Package githubfixture serves deterministic GitHub API, raw and tarball inputs.
package githubfixture

import (
	"archive/tar"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	Commit          = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	Tree            = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	Subtree         = "cccccccccccccccccccccccccccccccccccccccc"
	DefaultLatency  = 50 * time.Millisecond
	fieldSHA        = "sha"
	fieldPath       = "path"
	fieldType       = "type"
	archiveFileMode = 0o644
	apiPrefix       = "/api/repos/acme/docs"
)

// Server reports API quota cost separately from raw/CDN requests.
type Server struct {
	URL          string
	Latency      time.Duration
	RESTCalls    atomic.Int64
	RawCalls     atomic.Int64
	ArchiveCalls atomic.Int64
	MaxActive    atomic.Int64
	active       atomic.Int64
	files        map[string][]byte
	blobs        map[string]string
	entries      []map[string]string
}

// New creates a fixture with a default 50 ms delay per request.
// Set Latency before sending any requests to configure a different delay.
func New(tb testing.TB, count int) *Server {
	tb.Helper()
	f := &Server{
		Latency: DefaultLatency,
		files:   make(map[string][]byte, count),
		blobs:   make(map[string]string, count),
	}
	for index := range count {
		name := fmt.Sprintf("page-%03d.md", index)
		// IDs have the Git SHA shape; file data is deliberately synthetic.
		sha := fmt.Sprintf("%040x", index+1)
		f.files[name] = fmt.Appendf(nil, "# Page %d\n\nDocumentation fixture.\n", index)
		f.blobs[sha] = name
		f.entries = append(f.entries, map[string]string{fieldPath: "docs/" + name, fieldType: "blob", fieldSHA: sha})
	}
	server := httptest.NewServer(f)
	f.URL = server.URL
	tb.Cleanup(server.Close)
	return f
}

// ServeHTTP exposes the same fixture routes to configurable test wrappers.
func (f *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// GitHub Enterprise Server serves the same REST API below /api/v3.
	if rest, ok := strings.CutPrefix(r.URL.Path, "/api/v3/"); ok {
		r = r.Clone(r.Context())
		r.URL.Path = "/api/" + rest
	}
	active := f.active.Add(1)
	defer f.active.Add(-1)
	f.observeMax(active)
	f.countRequest(r.URL.Path)
	if f.Latency > 0 {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(f.Latency):
		}
	}
	switch {
	case strings.HasPrefix(r.URL.Path, apiPrefix):
		w.Header().Set("X-Ratelimit-Remaining", "100000")
		f.serveAPI(w, r)
	case strings.HasPrefix(r.URL.Path, "/raw/acme/docs/"+Commit+"/docs/"):
		name := strings.TrimPrefix(r.URL.Path, "/raw/acme/docs/"+Commit+"/docs/")
		f.serveRaw(w, r, name)
	case r.URL.Path == "/codeload/acme/docs/tar.gz/"+Commit:
		f.serveArchive(w)
	default:
		http.NotFound(w, r)
	}
}

func (f *Server) countRequest(path string) {
	switch {
	case strings.HasPrefix(path, apiPrefix):
		f.RESTCalls.Add(1)
	case strings.HasPrefix(path, "/raw/"):
		f.RawCalls.Add(1)
	case strings.HasPrefix(path, "/codeload/"):
		f.ArchiveCalls.Add(1)
	}
}

func (f *Server) observeMax(active int64) {
	for {
		previous := f.MaxActive.Load()
		if active <= previous || f.MaxActive.CompareAndSwap(previous, active) {
			return
		}
	}
}

func (f *Server) serveAPI(w http.ResponseWriter, r *http.Request) {
	resource := strings.TrimPrefix(r.URL.Path, apiPrefix)
	switch {
	case resource == "":
		writeJSON(w, map[string]string{"default_branch": "main"})
	case resource == "/commits/main" || resource == "/commits/HEAD" || resource == "/commits/"+Commit:
		// Like GitHub, the sha media type returns the bare SHA as text.
		if r.Header.Get("Accept") == "application/vnd.github.sha" {
			_, _ = w.Write([]byte(Commit))
			return
		}
		writeJSON(w, map[string]string{fieldSHA: Commit})
	case strings.HasPrefix(resource, "/contents/docs/"):
		f.serveContents(w, r, strings.TrimPrefix(resource, "/contents/docs/"))
	case resource == "/tarball/"+Commit:
		// The authenticated API redirects to a codeload archive URL.
		http.Redirect(w, r, "/codeload/acme/docs/tar.gz/"+Commit, http.StatusFound)
	case strings.HasPrefix(resource, "/git/trees/"):
		f.serveTree(w, strings.TrimPrefix(resource, "/git/trees/"))
	case strings.HasPrefix(resource, "/git/blobs/"):
		name, ok := f.blobs[strings.TrimPrefix(resource, "/git/blobs/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		// Like GitHub, serve raw bytes for the raw media type and base64 JSON otherwise.
		if r.Header.Get("Accept") == "application/vnd.github.raw" {
			_, _ = w.Write(f.files[name]) // A client may cancel while a response is being sent.
			return
		}
		writeJSON(
			w,
			map[string]string{"content": base64.StdEncoding.EncodeToString(f.files[name]), "encoding": "base64"},
		)
	default:
		http.NotFound(w, r)
	}
}

// serveContents answers the contents API for one file: raw bytes for the raw
// media type, otherwise its JSON metadata.
func (f *Server) serveContents(w http.ResponseWriter, r *http.Request, name string) {
	data, ok := f.files[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("Accept") == "application/vnd.github.raw" {
		_, _ = w.Write(data) // A client may cancel while a response is being sent.
		return
	}
	for sha, blobName := range f.blobs {
		if blobName == name {
			writeJSON(w, map[string]string{fieldType: "file", fieldSHA: sha, fieldPath: "docs/" + name})
			return
		}
	}
	http.NotFound(w, r)
}

func (f *Server) serveTree(w http.ResponseWriter, ref string) {
	// "<commit>:docs" names the docs folder's tree, as on GitHub.
	if ref == Commit+":docs" {
		ref = Subtree
	}
	switch ref {
	case "main", "HEAD", Commit, Tree, Subtree:
	default:
		w.WriteHeader(http.StatusNotFound)
		return
	}
	entries := make([]map[string]string, 0, len(f.entries)+1)
	for _, entry := range f.entries {
		copyEntry := maps.Clone(entry)
		if ref == Subtree {
			copyEntry[fieldPath] = strings.TrimPrefix(copyEntry[fieldPath], "docs/")
		}
		entries = append(entries, copyEntry)
	}
	sha := Subtree
	if ref != Subtree {
		sha = Tree
		entries = append(entries, map[string]string{fieldPath: "docs", fieldType: "tree", fieldSHA: Subtree})
	}
	writeJSON(w, map[string]any{fieldSHA: sha, "truncated": false, "tree": entries})
}

func (f *Server) serveRaw(w http.ResponseWriter, r *http.Request, name string) {
	data, ok := f.files[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write(data) // A client may cancel while a response is being sent.
}

func (f *Server) serveArchive(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/gzip")
	compressed := gzip.NewWriter(w)
	defer compressed.Close()
	archive := tar.NewWriter(compressed)
	defer archive.Close()
	for _, name := range slices.Sorted(maps.Keys(f.files)) {
		data := f.files[name]
		header := &tar.Header{Name: "docs-" + Commit + "/docs/" + name, Mode: archiveFileMode, Size: int64(len(data))}
		if err := archive.WriteHeader(header); err != nil {
			return
		}
		if _, err := archive.Write(data); err != nil {
			return
		}
	}
}

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data) // A canceled client cannot receive the body.
}
