package source_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/lockfile"
	"github.com/g5becks/dox/internal/source"
	"github.com/g5becks/dox/internal/testutil/githubfixture"
)

// providerFlavors run one contract suite against github.com-style hosting
// (API + raw + codeload) and GitHub Enterprise-style hosting (/api/v3 only).
func providerFlavors() []string { return []string{"github.com", "enterprise"} }

func contractSource(t *testing.T, flavor string, handler http.Handler, cfg config.Source) source.Source {
	t.Helper()
	if flavor == "enterprise" {
		handler = enterpriseAPI(t, handler)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	if cfg.Repo == "" {
		cfg = config.Source{Repo: "acme/docs", Path: "docs"}
	}
	if flavor == "enterprise" {
		cfg.Type, cfg.Host = "github", "github.mycorp.com"
	}
	src, err := source.NewGitHub("docs", cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	source.ConfigureGitHubTestRetry(src, server.URL+"/api", 1, time.Millisecond, time.Second)
	if flavor == "enterprise" {
		source.SetGitHubEnterprise(src, server.URL)
	}
	return src
}

// enterpriseAPI requires API calls to use the GHE /api/v3 prefix and maps them
// onto the fixture layout, so wrapping handlers see the same paths in both
// flavors.
func enterpriseAPI(t *testing.T, next http.Handler) http.Handler {
	t.Helper()
	var v3Calls atomic.Int64
	t.Cleanup(func() {
		if v3Calls.Load() == 0 {
			t.Error("enterprise flavor made no /api/v3 calls")
		}
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/api/v3/") {
			http.Error(w, "enterprise API calls must use /api/v3", http.StatusNotFound)
			return
		}
		if rest, ok := strings.CutPrefix(r.URL.Path, "/api/v3/"); ok {
			v3Calls.Add(1)
			r = r.Clone(r.Context())
			r.URL.Path = "/api/" + rest
		}
		next.ServeHTTP(w, r)
	})
}

func eachFlavor(t *testing.T, run func(t *testing.T, flavor string)) {
	t.Helper()
	for _, flavor := range providerFlavors() {
		t.Run(flavor, func(t *testing.T) {
			t.Parallel()
			run(t, flavor)
		})
	}
}

func TestProviderContractDirectorySync(t *testing.T) {
	t.Parallel()
	eachFlavor(t, func(t *testing.T, flavor string) {
		for _, count := range []int{35, 108} { // per-file and tarball paths
			f := githubfixture.New(t, count)
			f.Latency = 0
			dest, result := syncInto(t, contractSource(t, flavor, f, config.Source{}))
			if result.Downloaded != count || result.LockEntry.Commit != githubfixture.Commit {
				t.Fatalf("%d files: result = %+v", count, result)
			}
			content, err := os.ReadFile(filepath.Join(dest, "page-007.md"))
			if err != nil || string(content) != "# Page 7\n\nDocumentation fixture.\n" {
				t.Fatalf("%d files: page-007.md = %q, %v", count, content, err)
			}
			if flavor == "enterprise" && f.RawCalls.Load() != 0 {
				t.Fatalf("enterprise used %d raw.githubusercontent-style calls", f.RawCalls.Load())
			}
		}
	})
}

func TestProviderContractUnchangedFolderSkipsWithTwoCalls(t *testing.T) {
	t.Parallel()
	eachFlavor(t, func(t *testing.T, flavor string) {
		f := githubfixture.New(t, 5)
		f.Latency = 0
		_, first := syncInto(t, contractSource(t, flavor, f, config.Source{}))
		before := f.RESTCalls.Load()
		result, err := contractSource(t, flavor, unrelatedCommitHandler(f), config.Source{}).
			Sync(t.Context(), t.TempDir(), first.LockEntry, source.SyncOptions{})
		if err != nil || !result.Skipped {
			t.Fatalf("Sync() = %+v, %v; want skipped", result, err)
		}
		if calls := f.RESTCalls.Load() - before; calls > 2 {
			t.Fatalf("API calls = %d, want <= 2", calls)
		}
	})
}

func TestProviderContractTruncatedTree(t *testing.T) {
	t.Parallel()
	eachFlavor(t, func(t *testing.T, flavor string) {
		f := githubfixture.New(t, 12)
		f.Latency = 0
		_, result := syncInto(t, contractSource(t, flavor, truncatingHandler(f), config.Source{}))
		if result.Downloaded != 12 {
			t.Fatalf("Downloaded = %d, want 12", result.Downloaded)
		}
	})
}

func TestProviderContractLockedReproduces(t *testing.T) {
	t.Parallel()
	eachFlavor(t, func(t *testing.T, flavor string) {
		f := githubfixture.New(t, 6)
		f.Latency = 0
		firstDir, first := syncInto(t, contractSource(t, flavor, f, config.Source{}))
		lockedDir := t.TempDir()
		_, err := contractSource(t, flavor, unrelatedCommitHandler(f), config.Source{}).
			Sync(t.Context(), lockedDir, first.LockEntry, source.SyncOptions{Locked: true, Force: true})
		if err != nil {
			t.Fatal(err)
		}
		assertSameTree(t, firstDir, lockedDir)
	})
}

func TestProviderContractSingleFile(t *testing.T) {
	t.Parallel()
	eachFlavor(t, func(t *testing.T, flavor string) {
		f := githubfixture.New(t, 3)
		f.Latency = 0
		cfg := config.Source{Repo: "acme/docs", Path: "docs/page-001.md"}
		dest, result := syncInto(t, contractSource(t, flavor, f, cfg))
		if result.Downloaded != 1 {
			t.Fatalf("Downloaded = %d", result.Downloaded)
		}
		content, err := os.ReadFile(filepath.Join(dest, "page-001.md"))
		if err != nil || !strings.HasPrefix(string(content), "# Page 1") {
			t.Fatalf("page-001.md = %q, %v", content, err)
		}
	})
}

func TestProviderContractMissingFolderIsNotFound(t *testing.T) {
	t.Parallel()
	eachFlavor(t, func(t *testing.T, flavor string) {
		f := githubfixture.New(t, 1)
		f.Latency = 0
		cfg := config.Source{Repo: "acme/docs", Path: "missing"}
		_, err := contractSource(t, flavor, f, cfg).
			Sync(t.Context(), t.TempDir(), &lockfile.LockEntry{}, source.SyncOptions{})
		if detail, ok := errors.AsType[*errs.Error](err); !ok || detail.Code != errs.NotFound {
			t.Fatalf("Sync() error = %v, want NotFound", err)
		}
	})
}
