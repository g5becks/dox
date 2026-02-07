package sync_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/sync"
)

func TestRunWithNilConfigReturnsError(t *testing.T) {
	ctx := context.Background()
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

			result := sync.ResolveOutputRoot(cfg)

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

			result := sync.ResolveSourceOutputDir(tc.outputRoot, tc.sourceName, sourceCfg)
			if result != tc.want {
				t.Errorf("ResolveSourceOutputDir() = %q, want %q", result, tc.want)
			}
		})
	}
}
