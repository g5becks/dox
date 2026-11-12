package source_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/lockfile"
	"github.com/g5becks/dox/internal/source"
	"github.com/g5becks/dox/internal/testutil/githubfixture"
)

const (
	unrelatedCommit   = "dddddddddddddddddddddddddddddddddddddddd"
	unrelatedRootTree = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
)

// unrelatedCommitHandler serves a newer commit that changed files outside
// docs/: the root tree SHA differs while the docs subtree is unchanged.
func unrelatedCommitHandler(f *githubfixture.Server) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const api = "/api/repos/acme/docs"
		switch {
		case strings.HasPrefix(r.URL.Path, api+"/commits/"):
			f.RESTCalls.Add(1)
			_, _ = w.Write([]byte(unrelatedCommit))
		case r.URL.Path == api+"/git/trees/"+unrelatedCommit+":docs":
			// The docs folder is unchanged by the unrelated commit.
			proxied := r.Clone(r.Context())
			proxied.URL.Path = api + "/git/trees/" + githubfixture.Commit + ":docs"
			f.ServeHTTP(w, proxied)
		case r.URL.Path == api+"/git/trees/"+unrelatedCommit:
			recorder := httptest.NewRecorder()
			proxied := r.Clone(r.Context())
			proxied.URL.Path = api + "/git/trees/" + githubfixture.Commit
			f.ServeHTTP(recorder, proxied)
			var tree map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &tree); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			tree["sha"] = unrelatedRootTree
			_ = json.NewEncoder(w).Encode(tree)
		default:
			f.ServeHTTP(w, r)
		}
	})
}

func TestUnchangedSubtreeSkipsAfterUnrelatedCommitWithTwoAPICalls(t *testing.T) {
	t.Parallel()
	f := githubfixture.New(t, 5)
	f.Latency = 0
	_, first := syncInto(t, fixtureSource(t, f))

	before := f.RESTCalls.Load()
	src := fixtureSource(t, unrelatedCommitHandler(f))
	result, err := src.Sync(t.Context(), t.TempDir(), first.LockEntry, source.SyncOptions{})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if !result.Skipped {
		t.Fatalf("Skipped = false after a commit outside docs/; lock = %+v", result.LockEntry)
	}
	if calls := f.RESTCalls.Load() - before; calls > 2 {
		t.Fatalf("API calls for an unchanged folder = %d, want <= 2", calls)
	}
	if result.LockEntry.Commit != unrelatedCommit {
		t.Fatalf("lock commit = %q, want the new commit %q", result.LockEntry.Commit, unrelatedCommit)
	}
}

// truncatingHandler reports the recursive docs subtree as truncated, as
// GitHub does for very large trees; non-recursive listings still work.
func truncatingHandler(f *githubfixture.Server) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/git/trees/") && r.URL.Query().Get("recursive") != "" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"sha":"` + githubfixture.Subtree + `","truncated":true,"tree":[]}`))
			return
		}
		f.ServeHTTP(w, r)
	})
}

func TestTruncatedSubtreeSyncsThroughFallback(t *testing.T) {
	t.Parallel()
	f := githubfixture.New(t, 12)
	f.Latency = 0
	_, result := syncInto(t, fixtureSource(t, truncatingHandler(f)))
	if result.Downloaded != 12 || len(result.LockEntry.Files) != 12 {
		t.Fatalf("Downloaded = %d, files = %d; want 12 via the non-recursive walk",
			result.Downloaded, len(result.LockEntry.Files))
	}
}

func TestTruncatedTreeWalkFollowsNestedDirectories(t *testing.T) {
	t.Parallel()
	src := source.TestableGitHubSource(t, "widgets", config.Source{
		Repo: "acme/widgets",
		Path: "docs",
	}, source.NewMockGitHubClient(t, map[string]source.MockHTTPResponse{
		"/repos/acme/widgets/git/trees/main:docs?recursive=1": {
			Body: `{"sha":"sub-docs","truncated":true,"tree":[]}`,
		},
		"/repos/acme/widgets/git/trees/sub-docs": {
			Body: `{"sha":"sub-docs","truncated":false,"tree":[
  {"path":"a.md","type":"blob","sha":"sha-a"},
  {"path":"guide","type":"tree","sha":"sub-guide"}]}`,
		},
		"/repos/acme/widgets/git/trees/sub-guide": {
			Body: `{"sha":"sub-guide","truncated":false,"tree":[{"path":"b.md","type":"blob","sha":"sha-b"}]}`,
		},
	}), "main")

	result, err := src.Sync(t.Context(), t.TempDir(), nil, source.SyncOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	want := map[string]string{"a.md": "sha-a", "guide/b.md": "sha-b"}
	if len(result.LockEntry.Files) != len(want) {
		t.Fatalf("files = %v, want %v", result.LockEntry.Files, want)
	}
	for name, sha := range want {
		if result.LockEntry.Files[name] != sha {
			t.Fatalf("files = %v, want %v", result.LockEntry.Files, want)
		}
	}
	if result.LockEntry.TreeSHA != "sub-docs" {
		t.Fatalf("TreeSHA = %q, want the docs subtree SHA", result.LockEntry.TreeSHA)
	}
}

func TestMissingPathIsNotFound(t *testing.T) {
	t.Parallel()
	f := githubfixture.New(t, 1)
	f.Latency = 0
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	src, err := source.NewGitHub("docs", config.Source{Repo: "acme/docs", Path: "missing"}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	source.ConfigureGitHubTestRetry(src, server.URL+"/api", 0, 0, 0)
	_, syncErr := src.Sync(t.Context(), t.TempDir(), &lockfile.LockEntry{}, source.SyncOptions{})
	if syncErr == nil || !strings.Contains(syncErr.Error(), `"missing"`) {
		t.Fatalf("Sync() error = %v, want a not-found error naming the path", syncErr)
	}
}

func TestTarballOverBudgetFallsBackToRawByteIdentical(t *testing.T) {
	t.Parallel()
	budgetFixture := githubfixture.New(t, 108)
	budgetFixture.Latency = 0
	src := fixtureSource(t, budgetFixture)
	// A budget smaller than the archive aborts it partway through.
	source.SetGitHubTarballByteBudget(src, 600)
	budgetDir, budgetResult := syncInto(t, src)
	if budgetFixture.ArchiveCalls.Load() != 1 || budgetFixture.RawCalls.Load() == 0 {
		t.Fatalf("archive/raw calls = %d/%d, want an aborted archive then raw fallback",
			budgetFixture.ArchiveCalls.Load(), budgetFixture.RawCalls.Load())
	}

	rawFixture := githubfixture.New(t, 108)
	rawFixture.Latency = 0
	rawSrc := fixtureSource(t, rawFixture)
	source.SetGitHubTarballMinFiles(rawSrc, 1<<30)
	rawDir, _ := syncInto(t, rawSrc)

	if budgetResult.Downloaded != 108 {
		t.Fatalf("Downloaded = %d, want 108", budgetResult.Downloaded)
	}
	assertSameTree(t, budgetDir, rawDir)
}
