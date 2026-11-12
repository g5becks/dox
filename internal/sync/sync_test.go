package sync_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/lockfile"
	"github.com/g5becks/dox/internal/sync"
)

func TestRunWithNilConfigReturnsError(t *testing.T) {
	ctx := t.Context()
	opts := sync.Options{}

	_, err := sync.Run(ctx, nil, opts)
	if err == nil {
		t.Fatal("Run() with nil config: got nil error, want non-nil")
	}
}

func TestResolveSourceNamesReturnsAllSorted(t *testing.T) {
	sources := map[string]config.Source{
		"zebra":  {Type: "github"},
		"alpha":  {Type: "url"},
		"middle": {Type: "github"},
	}

	names, err := sync.ResolveSourceNames(sources, nil)
	if err != nil {
		t.Fatalf("ResolveSourceNames() error = %v", err)
	}

	want := []string{"alpha", "middle", "zebra"}
	if len(names) != len(want) {
		t.Fatalf("ResolveSourceNames() returned %d names, want %d", len(names), len(want))
	}

	for i, name := range names {
		if name != want[i] {
			t.Errorf("ResolveSourceNames()[%d] = %q, want %q", i, name, want[i])
		}
	}
}

func TestResolveSourceNamesValidatesRequested(t *testing.T) {
	sources := map[string]config.Source{
		"exists": {Type: "github"},
	}

	_, err := sync.ResolveSourceNames(sources, []string{"missing"})
	if err == nil {
		t.Fatal("ResolveSourceNames() with invalid source: got nil error, want non-nil")
	}
	if !errors.Is(err, errs.ErrSourceNotFound) {
		t.Fatalf("source sentinel missing: %v", err)
	}
}

func TestResolveSourceNamesDeduplicates(t *testing.T) {
	sources := map[string]config.Source{
		"source1": {Type: "github"},
		"source2": {Type: "url"},
	}

	names, err := sync.ResolveSourceNames(sources, []string{"source1", "source2", "source1"})
	if err != nil {
		t.Fatalf("ResolveSourceNames() error = %v", err)
	}

	if len(names) != 2 {
		t.Fatalf("ResolveSourceNames() returned %d names, want 2 (deduplicated)", len(names))
	}

	if names[0] != "source1" || names[1] != "source2" {
		t.Errorf("ResolveSourceNames() = %v, want [source1 source2]", names)
	}
}

func TestResolveGitHubTokenPrecedence(t *testing.T) {
	tests := []struct {
		name          string
		configToken   string
		githubToken   string
		ghToken       string
		expectedToken string
	}{
		{
			name:          "config token takes precedence",
			configToken:   "config-token",
			githubToken:   "github-token",
			ghToken:       "gh-token",
			expectedToken: "config-token",
		},
		{
			name:          "GITHUB_TOKEN used when config empty",
			configToken:   "",
			githubToken:   "github-token",
			ghToken:       "gh-token",
			expectedToken: "github-token",
		},
		{
			name:          "GH_TOKEN used when config and GITHUB_TOKEN empty",
			configToken:   "",
			githubToken:   "",
			ghToken:       "gh-token",
			expectedToken: "gh-token",
		},
		{
			name:          "empty when all empty",
			configToken:   "",
			githubToken:   "",
			ghToken:       "",
			expectedToken: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Always set both env vars to ensure clean test environment
			if tc.githubToken != "" {
				t.Setenv("GITHUB_TOKEN", tc.githubToken)
			} else {
				t.Setenv("GITHUB_TOKEN", "")
			}

			if tc.ghToken != "" {
				t.Setenv("GH_TOKEN", tc.ghToken)
			} else {
				t.Setenv("GH_TOKEN", "")
			}

			cfg := &config.Config{
				GitHubToken: tc.configToken,
			}

			token := sync.ResolveGitHubToken(cfg)
			if token != tc.expectedToken {
				t.Errorf("ResolveGitHubToken() = %q, want %q", token, tc.expectedToken)
			}
		})
	}
}

func TestResolveOutputRoot(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		configDir string
		wantAbs   bool
	}{
		{
			name:      "absolute path unchanged",
			output:    "/absolute/path/docs",
			configDir: "/home/user/.config",
			wantAbs:   true,
		},
		{
			name:      "relative path joined with config dir",
			output:    "docs",
			configDir: "/home/user/.config",
			wantAbs:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{
				Output:    tc.output,
				ConfigDir: tc.configDir,
			}

			cfg.AllowExternalOutput = tc.wantAbs
			paths, err := cfg.Paths()
			if err != nil {
				t.Fatal(err)
			}
			result := paths.Output()

			if tc.wantAbs {
				if result != tc.output {
					t.Errorf("ResolveOutputRoot() = %q, want %q", result, tc.output)
				}
			} else {
				expected := filepath.Join(tc.configDir, tc.output)
				if result != expected {
					t.Errorf("ResolveOutputRoot() = %q, want %q", result, expected)
				}
			}
		})
	}
}

func TestResolveSourceOutputDir(t *testing.T) {
	tests := []struct {
		name       string
		outputRoot string
		sourceName string
		sourceOut  string
		want       string
	}{
		{
			name:       "uses source.Out when specified",
			outputRoot: "/output",
			sourceName: "mySource",
			sourceOut:  "custom/path",
			want:       "/output/custom/path",
		},
		{
			name:       "uses sourceName when source.Out empty",
			outputRoot: "/output",
			sourceName: "mySource",
			sourceOut:  "",
			want:       "/output/mySource",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sourceCfg := config.Source{
				Out: tc.sourceOut,
			}

			paths, err := config.NewPaths(filepath.Dir(tc.outputRoot), filepath.Base(tc.outputRoot), false)
			if err != nil {
				t.Fatal(err)
			}
			result, err := paths.Source(tc.sourceName, sourceCfg)
			if err != nil {
				t.Fatal(err)
			}
			if result != tc.want {
				t.Errorf("ResolveSourceOutputDir() = %q, want %q", result, tc.want)
			}
		})
	}
}

func TestParallelPrecedence(t *testing.T) {
	t.Parallel()
	computed := min(max(4*runtime.NumCPU(), 10), 16)
	for _, tc := range []struct {
		name                   string
		flag, configured, want int
	}{
		{"flag wins", 7, 20, 7},
		{"config wins when flag unset", 0, 20, 20},
		{"computed fallback", 0, 0, computed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := sync.ResolveMaxParallel(
				&config.Config{MaxParallel: tc.configured},
				sync.Options{MaxParallel: tc.flag},
			)
			if got != tc.want {
				t.Fatalf("parallel=%d, want %d", got, tc.want)
			}
		})
	}
}

func TestTimeoutAndStalledSourceIsolation(t *testing.T) {
	t.Parallel()
	project := t.TempDir()
	output := filepath.Join(project, ".dox")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fast.txt" {
			_, _ = w.Write([]byte("fast content\n"))
			return
		}
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	cfg := &config.Config{
		ConfigDir: project,
		Output:    output,
		Timeout:   "200ms",
		Sources: map[string]config.Source{
			"fast": {Type: "url", URL: server.URL + "/fast.txt", Filename: "fast.txt"},
			"slow": {Type: "url", URL: server.URL + "/slow.txt", Filename: "slow.txt"},
		},
	}

	start := time.Now()
	result, err := sync.Run(t.Context(), cfg, sync.Options{MaxParallel: 2})
	elapsed := time.Since(start)
	if elapsed > 1200*time.Millisecond {
		t.Fatalf("sync.Run took %v, want <= 1.2s (200ms timeout + 1s)", elapsed)
	}
	if err == nil {
		t.Fatal("expected partial failure error from stalled source")
	}
	if result == nil || result.Downloaded != 1 || result.Errors != 1 {
		t.Fatalf("unexpected RunResult: %+v", result)
	}
	lock, loadErr := lockfile.Load(output)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if lock.GetEntry("fast") == nil {
		t.Fatal("expected completed 'fast' source lock entry to be saved")
	}
	if lock.GetEntry("slow") != nil {
		t.Fatal("did not expect stalled 'slow' source lock entry to be saved")
	}
}

func TestCancellationSavesCompletedSources(t *testing.T) {
	t.Parallel()
	project := t.TempDir()
	output := filepath.Join(project, ".dox")

	fastDone := make(chan struct{})
	slowStarted := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fast.txt" {
			_, _ = w.Write([]byte("fast content\n"))
			return
		}
		<-fastDone
		select {
		case slowStarted <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		<-slowStarted
		cancel()
	}()

	cfg := &config.Config{
		ConfigDir: project,
		Output:    output,
		Sources: map[string]config.Source{
			"fast": {Type: "url", URL: server.URL + "/fast.txt", Filename: "fast.txt"},
			"slow": {Type: "url", URL: server.URL + "/slow.txt", Filename: "slow.txt"},
		},
	}

	_, err := sync.Run(ctx, cfg, sync.Options{
		MaxParallel: 2,
		OnEvent: func(e sync.Event) {
			if e.Kind == sync.EventSourceDone && e.Source == "fast" {
				close(fastDone)
			}
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("sync.Run() error = %v, want context.Canceled", err)
	}
	detail, ok := errors.AsType[*errs.Error](err)
	if !ok || detail.Code != errs.Interrupted {
		t.Fatalf("expected errs.Interrupted, got %+v", detail)
	}
	lock, loadErr := lockfile.Load(output)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if lock.GetEntry("fast") == nil {
		t.Fatal("expected completed 'fast' source lock entry to be saved on cancellation")
	}
}
