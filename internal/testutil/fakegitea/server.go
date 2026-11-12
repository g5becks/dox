// Package fakegitea serves a deterministic Gitea/Forgejo API v1 repository
// for tests: commit resolution, contents listings, paginated recursive trees
// (truncated=true while pages remain), raw files and whole-repo archives,
// mirroring codeberg.org's observed behavior.
package fakegitea

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const (
	// Subtree is the docs folder's tree SHA; it stays fixed across commits.
	Subtree     = "cccccccccccccccccccccccccccccccccccccccc"
	RootTree    = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	initialSHA  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	maxPerPage  = 20
	fileMode    = 0o644
	fieldSHA    = "sha"
	fieldPath   = "path"
	fieldType   = "type"
	fieldName   = "name"
	fieldSize   = "size"
	typeDir     = "dir"
	typeTree    = "tree"
	typeBlob    = "blob"
	typeFile    = "file"
	contentsKey = "contents"
)

// Server is one fake repository owner/repo.
type Server struct {
	URL   string
	Owner string
	Repo  string
	// APICalls counts every /api/v1 request except archives.
	APICalls     atomic.Int64
	TreeCalls    atomic.Int64
	RawCalls     atomic.Int64
	ArchiveCalls atomic.Int64
	// RateLimitAfter, when > 0, answers 429 to API calls beyond that count.
	RateLimitAfter atomic.Int64

	mu     sync.Mutex
	commit string
	files  map[string][]byte // repo path -> content
	auth   []string          // Authorization headers seen
}

// New serves count files under docs/ (one nested in docs/guide/) plus README.md.
func New(tb testing.TB, owner, repo string, count int) *Server {
	tb.Helper()
	f := &Server{
		Owner:  owner,
		Repo:   repo,
		commit: initialSHA,
		files:  map[string][]byte{"README.md": []byte("# Repo\n")},
	}
	for index := range count {
		name := fmt.Sprintf("docs/page-%03d.md", index)
		if index == 0 {
			name = "docs/guide/page-000.md"
		}
		f.files[name] = fmt.Appendf(nil, "# Page %d\n\nGitea fixture.\n", index)
	}
	server := httptest.NewServer(f)
	f.URL = server.URL
	tb.Cleanup(server.Close)
	return f
}

// Commit returns the commit refs resolve to.
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

// Authorizations returns the Authorization headers received.
func (f *Server) Authorizations() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.auth)
}

// BlobSHA is the deterministic blob id the fake reports for a path.
func BlobSHA(repoPath string) string {
	sum := 0
	for index, r := range repoPath {
		sum = sum*31 + int(r) + index
	}
	return fmt.Sprintf("%040x", uint32(sum))
}

func (f *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	if header := r.Header.Get("Authorization"); header != "" {
		f.auth = append(f.auth, header)
	}
	commit := f.commit
	f.mu.Unlock()

	resource, ok := strings.CutPrefix(r.URL.Path, "/api/v1/repos/"+f.Owner+"/"+f.Repo+"/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(resource, "archive/") {
		f.ArchiveCalls.Add(1)
		f.serveArchive(w, r, strings.TrimSuffix(strings.TrimPrefix(resource, "archive/"), ".tar.gz"))
		return
	}
	calls := f.APICalls.Add(1)
	if limit := f.RateLimitAfter.Load(); limit > 0 && calls > limit {
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	switch {
	case resource == "commits":
		f.serveCommits(w, r, commit)
	case resource == contentsKey || strings.HasPrefix(resource, contentsKey+"/"):
		f.serveContents(w, r, strings.TrimPrefix(strings.TrimPrefix(resource, contentsKey), "/"))
	case strings.HasPrefix(resource, "git/trees/"):
		f.TreeCalls.Add(1)
		f.serveTree(w, r, strings.TrimPrefix(resource, "git/trees/"))
	case strings.HasPrefix(resource, "raw/"):
		f.RawCalls.Add(1)
		f.serveRaw(w, r, strings.TrimPrefix(resource, "raw/"))
	default:
		http.NotFound(w, r)
	}
}

func (f *Server) serveCommits(w http.ResponseWriter, r *http.Request, commit string) {
	switch r.URL.Query().Get(fieldSHA) {
	case "HEAD", "main", commit, initialSHA:
		// Like Gitea, the reported tree id is the commit SHA itself.
		writeJSON(
			w,
			[]map[string]any{
				{fieldSHA: commit, "commit": map[string]any{typeTree: map[string]string{fieldSHA: commit}}},
			},
		)
	default:
		http.NotFound(w, r)
	}
}

// children lists the direct entries of a directory ("" = root).
func (f *Server) children(dir string) []map[string]any {
	var entries []map[string]any
	seen := map[string]bool{}
	for _, repoPath := range slices.Sorted(maps.Keys(f.files)) {
		rest, inside := strings.CutPrefix(repoPath, dir+"/")
		if dir == "" {
			rest, inside = repoPath, true
		}
		if !inside {
			continue
		}
		if child, _, nested := strings.Cut(rest, "/"); nested {
			childPath := strings.TrimPrefix(dir+"/"+child, "/")
			if !seen[childPath] {
				seen[childPath] = true
				entries = append(
					entries,
					map[string]any{
						fieldName: child,
						fieldPath: childPath,
						fieldType: typeDir,
						fieldSHA:  f.dirSHA(childPath),
					},
				)
			}
			continue
		}
		entries = append(entries, map[string]any{
			fieldName: rest,
			fieldPath: repoPath,
			fieldType: typeFile,
			fieldSHA:  BlobSHA(repoPath),
			fieldSize: len(f.files[repoPath]),
		})
	}
	return entries
}

func (f *Server) dirSHA(dir string) string {
	if dir == "docs" {
		return Subtree
	}
	return fmt.Sprintf("%040x", len(dir)+1)
}

func (f *Server) serveContents(w http.ResponseWriter, r *http.Request, repoPath string) {
	if data, ok := f.files[repoPath]; ok {
		writeJSON(w, map[string]any{fieldName: repoPath[strings.LastIndex(repoPath, "/")+1:], fieldPath: repoPath,
			fieldType: typeFile, fieldSHA: BlobSHA(repoPath), fieldSize: len(data)})
		return
	}
	entries := f.children(repoPath)
	if len(entries) == 0 {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, entries)
}

// serveTree lists a tree by SHA with paths relative to it; recursive listings
// paginate and report truncated=true while more pages remain.
func (f *Server) serveTree(w http.ResponseWriter, r *http.Request, sha string) {
	var dir string
	switch sha {
	case RootTree, f.Commit(), initialSHA: // Gitea accepts a commit as the root tree-ish
		dir = ""
	case Subtree:
		dir = "docs"
	default:
		http.NotFound(w, r)
		return
	}
	recursive := r.URL.Query().Get("recursive") == "true"
	var entries []map[string]any
	for _, repoPath := range slices.Sorted(maps.Keys(f.files)) {
		relative, inside := strings.CutPrefix(repoPath, dir+"/")
		if dir == "" {
			relative, inside = repoPath, true
		}
		if !inside {
			continue
		}
		parent, _, nested := strings.Cut(relative, "/")
		if nested && !slices.ContainsFunc(entries, func(e map[string]any) bool { return e[fieldPath] == parent }) {
			entries = append(entries, map[string]any{
				fieldPath: parent,
				fieldType: typeTree,
				fieldSHA:  f.dirSHA(strings.TrimPrefix(dir+"/"+parent, "/")),
				fieldSize: 0,
			})
		}
		if nested && !recursive {
			continue
		}
		entries = append(
			entries,
			map[string]any{
				fieldPath: relative,
				fieldType: typeBlob,
				fieldSHA:  BlobSHA(repoPath),
				fieldSize: len(f.files[repoPath]),
			},
		)
	}
	query := r.URL.Query()
	perPage, _ := strconv.Atoi(query.Get("per_page"))
	if perPage <= 0 || perPage > maxPerPage {
		perPage = maxPerPage
	}
	page := max(1, atoi(query.Get("page")))
	start := min((page-1)*perPage, len(entries))
	end := min(start+perPage, len(entries))
	writeJSON(w, map[string]any{fieldSHA: sha, "truncated": end < len(entries), "page": page,
		"total_count": len(entries), typeTree: entries[start:end]})
}

func atoi(value string) int {
	parsed, _ := strconv.Atoi(value)
	return parsed
}

func (f *Server) serveRaw(w http.ResponseWriter, r *http.Request, repoPath string) {
	data, ok := f.files[repoPath]
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write(data)
}

// serveArchive returns the whole repository below "<repo>/" like Codeberg.
func (f *Server) serveArchive(w http.ResponseWriter, r *http.Request, sha string) {
	if sha != f.Commit() && sha != initialSHA {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	compressed := gzip.NewWriter(w)
	defer compressed.Close()
	archive := tar.NewWriter(compressed)
	defer archive.Close()
	for _, repoPath := range slices.Sorted(maps.Keys(f.files)) {
		data := f.files[repoPath]
		header := &tar.Header{
			Name:     f.Repo + "/" + repoPath,
			Mode:     fileMode,
			Size:     int64(len(data)),
			Typeflag: tar.TypeReg,
		}
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
