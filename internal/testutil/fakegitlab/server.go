// Package fakegitlab serves a deterministic GitLab API v4 project for tests:
// commits, paginated tree listings, file metadata and raw content, and
// path-filtered tar.gz archives, mirroring gitlab.com's observed behavior.
package fakegitlab

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha1" //nolint:gosec // Git object ids are SHA-1 by definition; not used for security.
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const (
	// Subtree is the docs folder's tree id; it stays fixed across commits.
	Subtree     = "cccccccccccccccccccccccccccccccccccccccc"
	initialSHA  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fieldID     = "id"
	fieldPath   = "path"
	fieldType   = "type"
	defaultPage = 20
	fileMode    = 0o644
)

// Server is one fake project. Project is its full path (subgroups allowed).
type Server struct {
	URL     string
	Project string
	// APICalls counts every /api/v4 request (GitLab rate-limits all of them).
	APICalls     atomic.Int64
	ArchiveCalls atomic.Int64
	TreeCalls    atomic.Int64
	RawCalls     atomic.Int64
	// RateLimitAfter, when > 0, answers 429 to API calls beyond that count.
	RateLimitAfter atomic.Int64

	mu      sync.Mutex
	commit  string
	files   map[string][]byte // repo path -> content
	tokens  []string          // PRIVATE-TOKEN values seen
	escaped []string          // escaped request paths seen
}

// New serves count files under docs/ (one nested in docs/guide/) plus README.md.
func New(tb testing.TB, project string, count int) *Server {
	tb.Helper()
	f := &Server{Project: project, commit: initialSHA, files: map[string][]byte{"README.md": []byte("# Project\n")}}
	for index := range count {
		name := fmt.Sprintf("docs/page-%03d.md", index)
		if index == 0 {
			name = "docs/guide/page-000.md"
		}
		f.files[name] = fmt.Appendf(nil, "# Page %d\n\nGitLab fixture.\n", index)
	}
	server := httptest.NewServer(f)
	f.URL = server.URL
	tb.Cleanup(server.Close)
	return f
}

// Commit returns the commit HEAD and branch refs resolve to.
func (f *Server) Commit() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.commit
}

// SetCommit moves HEAD to sha without changing docs/ (an unrelated commit).
func (f *Server) SetCommit(sha string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commit = sha
}

// Tokens returns the PRIVATE-TOKEN headers received.
func (f *Server) Tokens() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.tokens)
}

// EscapedPaths returns request paths as sent (percent-encoding preserved).
func (f *Server) EscapedPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.escaped)
}

// BlobID is the git blob id of content: sha1("blob <len>\x00" + content).
func BlobID(content []byte) string {
	//nolint:gosec // Git object ids are SHA-1 by definition; not used for security.
	sum := sha1.Sum(append(fmt.Appendf(nil, "blob %d\x00", len(content)), content...))
	return hex.EncodeToString(sum[:])
}

func (f *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.escaped = append(f.escaped, r.URL.EscapedPath())
	if token := r.Header.Get("Private-Token"); token != "" {
		f.tokens = append(f.tokens, token)
	}
	commit := f.commit
	f.mu.Unlock()

	prefix := "/api/v4/projects/" + url.PathEscape(f.Project) + "/repository/"
	resource, ok := strings.CutPrefix(r.URL.EscapedPath(), prefix)
	if !ok {
		http.NotFound(w, r)
		return
	}
	calls := f.APICalls.Add(1)
	if limit := f.RateLimitAfter.Load(); limit > 0 && calls > limit {
		w.Header().Set("Ratelimit-Remaining", "0")
		w.Header().Set("Ratelimit-Reset", "4102444800")
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	w.Header().Set("Ratelimit-Remaining", "1000")
	switch {
	case strings.HasPrefix(resource, "commits/"):
		f.serveCommit(w, r, strings.TrimPrefix(resource, "commits/"), commit)
	case resource == "tree":
		f.TreeCalls.Add(1)
		f.serveTree(w, r)
	case strings.HasPrefix(resource, "files/"):
		f.serveFile(w, r, strings.TrimPrefix(resource, "files/"))
	case resource == "archive.tar.gz":
		f.ArchiveCalls.Add(1)
		f.serveArchive(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *Server) serveCommit(w http.ResponseWriter, r *http.Request, ref, commit string) {
	switch ref {
	case "HEAD", "main", commit, initialSHA:
		writeJSON(w, map[string]string{fieldID: commit})
	default:
		http.NotFound(w, r)
	}
}

func (f *Server) serveTree(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	folder := query.Get(fieldPath)
	recursive := query.Get("recursive") == "true"
	var entries []map[string]string
	dirs := map[string]bool{}
	for _, repoPath := range slices.Sorted(maps.Keys(f.files)) {
		rest, inside := strings.CutPrefix(repoPath, folder+"/")
		if folder == "" {
			rest, inside = repoPath, true
		}
		if !inside {
			continue
		}
		if dir, _, nested := strings.Cut(rest, "/"); nested {
			dirPath := strings.TrimPrefix(folder+"/"+dir, "/")
			if !dirs[dirPath] {
				dirs[dirPath] = true
				entries = append(entries, f.treeEntry(dirPath, dir, "tree"))
			}
			if !recursive {
				continue
			}
		}
		entries = append(entries, f.treeEntry(repoPath, rest[strings.LastIndex(rest, "/")+1:], "blob"))
	}
	if len(entries) == 0 {
		http.NotFound(w, r)
		return
	}
	// Like GitLab, the server caps per_page; a small cap exercises pagination.
	perPage, _ := strconv.Atoi(query.Get("per_page"))
	if perPage <= 0 || perPage > defaultPage {
		perPage = defaultPage
	}
	page, _ := strconv.Atoi(query.Get("page"))
	page = max(page, 1)
	start := min((page-1)*perPage, len(entries))
	end := min(start+perPage, len(entries))
	if end < len(entries) {
		w.Header().Set("X-Next-Page", strconv.Itoa(page+1))
	} else {
		w.Header().Set("X-Next-Page", "")
	}
	writeJSON(w, entries[start:end])
}

func (f *Server) treeEntry(repoPath, name, kind string) map[string]string {
	id := ""
	if kind == "blob" {
		id = BlobID(f.files[repoPath])
	}
	if kind == "tree" {
		id = Subtree
		if repoPath != "docs" {
			id = fmt.Sprintf("%040x", len(repoPath))
		}
	}
	return map[string]string{fieldID: id, "name": name, fieldType: kind, fieldPath: repoPath, "mode": "100644"}
}

func (f *Server) serveFile(w http.ResponseWriter, r *http.Request, escaped string) {
	encoded, raw := strings.CutSuffix(escaped, "/raw")
	repoPath, err := url.PathUnescape(encoded)
	if err != nil || strings.Contains(encoded, "/") {
		http.Error(w, "file path must be URL-encoded", http.StatusBadRequest)
		return
	}
	data, ok := f.files[repoPath]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if raw {
		f.RawCalls.Add(1)
		_, _ = w.Write(data)
		return
	}
	writeJSON(w, map[string]string{"blob_id": BlobID(data), "file_path": repoPath})
}

// serveArchive mirrors GitLab: entries sit below "<project>-<sha>-<sha>-<path>/"
// with full repository paths, limited to ?path when given.
func (f *Server) serveArchive(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	folder, sha := query.Get(fieldPath), query.Get("sha")
	top := fmt.Sprintf(
		"%s-%s-%s-%s/",
		f.Project[strings.LastIndex(f.Project, "/")+1:],
		sha,
		sha,
		strings.ReplaceAll(folder, "/", "-"),
	)
	w.Header().Set("Content-Type", "application/octet-stream")
	compressed := gzip.NewWriter(w)
	defer compressed.Close()
	archive := tar.NewWriter(compressed)
	defer archive.Close()
	for _, repoPath := range slices.Sorted(maps.Keys(f.files)) {
		if folder != "" && !strings.HasPrefix(repoPath, folder+"/") {
			continue
		}
		data := f.files[repoPath]
		header := &tar.Header{Name: top + repoPath, Mode: fileMode, Size: int64(len(data)), Typeflag: tar.TypeReg}
		if archive.WriteHeader(header) != nil {
			return
		}
		if _, err := archive.Write(data); err != nil {
			return
		}
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
