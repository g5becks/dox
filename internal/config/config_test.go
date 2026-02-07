package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/g5becks/dox/internal/config"
)

func TestLoadAppliesDefaultsAndResolvesOutput(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "dox.toml")
	writeFile(t, configPath, `
[sources.goreleaser]
type = "github"
repo = "goreleaser/goreleaser"
path = "www/docs"
`)

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ConfigDir != tempDir {
		t.Fatalf("ConfigDir = %q, want %q", cfg.ConfigDir, tempDir)
	}

	expectedOutput := filepath.Join(tempDir, ".dox")
	if cfg.Output != expectedOutput {
		t.Fatalf("Output = %q, want %q", cfg.Output, expectedOutput)
	}

	sourceCfg, ok := cfg.Sources["goreleaser"]
	if !ok {
		t.Fatalf("source goreleaser not found")
	}

	expectedPatterns := []string{"**/*.md", "**/*.mdx", "**/*.txt"}
	if len(sourceCfg.Patterns) != len(expectedPatterns) {
		t.Fatalf("Patterns len = %d, want %d", len(sourceCfg.Patterns), len(expectedPatterns))
	}

	for i, want := range expectedPatterns {
		if sourceCfg.Patterns[i] != want {
			t.Fatalf("Patterns[%d] = %q, want %q", i, sourceCfg.Patterns[i], want)
		}
	}
}

func TestLoadUsesProvidedConfigPath(t *testing.T) {
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "custom.toml")
	writeFile(t, configPath, `
output = "docs"

[sources.hono]
type = "url"
url = "https://hono.dev/llms-full.txt"
`)

	workDir := t.TempDir()
	t.Chdir(workDir)

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	expectedOutput := filepath.Join(configDir, "docs")
	if cfg.Output != expectedOutput {
		t.Fatalf("Output = %q, want %q", cfg.Output, expectedOutput)
	}
}

func TestLoadReturnsErrorForMissingExplicitPath(t *testing.T) {
	_, err := config.Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err == nil {
		t.Fatalf("Load() error = nil, want non-nil")
	}

	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("Load() error = %q, expected missing-file message", err.Error())
	}
}

func TestLoadReturnsErrorForInvalidTOML(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "dox.toml")
	writeFile(t, configPath, `
[sources.bad
type = "url"
url = "https://example.com/docs.txt"
`)

	_, err := config.Load(configPath)
	if err == nil {
		t.Fatalf("Load() error = nil, want non-nil")
	}

	if !strings.Contains(err.Error(), "toml:") {
		t.Fatalf("Load() error = %q, expected TOML parse message", err.Error())
	}
}

func TestFindConfigFileWalksParentDirectories(t *testing.T) {
	rootDir := t.TempDir()
	configPath := filepath.Join(rootDir, ".dox.toml")
	writeFile(t, configPath, `
[sources.effect]
type = "url"
url = "https://effect.website/llms-full.txt"
`)

	nestedDir := filepath.Join(rootDir, "a", "b", "c")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	t.Chdir(nestedDir)

	foundPath, err := config.FindConfigFile()
	if err != nil {
		t.Fatalf("FindConfigFile() error = %v", err)
	}

	foundPathEval, err := filepath.EvalSymlinks(foundPath)
	if err != nil {
		t.Fatalf("EvalSymlinks(foundPath) error = %v", err)
	}

	configPathEval, err := filepath.EvalSymlinks(configPath)
	if err != nil {
		t.Fatalf("EvalSymlinks(configPath) error = %v", err)
	}

	if foundPathEval != configPathEval {
		t.Fatalf("FindConfigFile() = %q, want %q", foundPathEval, configPathEval)
	}
}

func TestFindConfigFileReturnsErrorWhenMissing(t *testing.T) {
	emptyDir := t.TempDir()
	t.Chdir(emptyDir)

	_, err := config.FindConfigFile()
	if err == nil {
		t.Fatalf("FindConfigFile() error = nil, want non-nil")
	}

	if !strings.Contains(err.Error(), "no dox.toml or .dox.toml found") {
		t.Fatalf("FindConfigFile() error = %q, expected not-found message", err.Error())
	}
}

func TestConfigOutputDir(t *testing.T) {
	cfg := &config.Config{
		ConfigDir: "/tmp/project",
		Output:    ".dox",
	}

	gotDefaultOut := cfg.OutputDir("goreleaser", config.Source{})
	wantDefaultOut := filepath.Join("/tmp/project", ".dox", "goreleaser")
	if gotDefaultOut != wantDefaultOut {
		t.Fatalf("OutputDir() default = %q, want %q", gotDefaultOut, wantDefaultOut)
	}

	gotCustomOut := cfg.OutputDir("goreleaser", config.Source{Out: "go/goreleaser"})
	wantCustomOut := filepath.Join("/tmp/project", ".dox", "go/goreleaser")
	if gotCustomOut != wantCustomOut {
		t.Fatalf("OutputDir() custom = %q, want %q", gotCustomOut, wantCustomOut)
	}
}

func TestConfigValidate(t *testing.T) {
	testCases := []struct {
		name            string
		cfg             *config.Config
		wantErrContains string
	}{
		{
			name: "valid github source",
			cfg: &config.Config{
				Sources: map[string]config.Source{
					"goreleaser": {
						Type: "github",
						Repo: "goreleaser/goreleaser",
						Path: "www/docs",
					},
				},
			},
		},
		{
			name: "valid url source",
			cfg: &config.Config{
				Sources: map[string]config.Source{
					"hono": {
						Type: "url",
						URL:  "https://hono.dev/llms-full.txt",
					},
				},
			},
		},
		{
			name: "missing github repo",
			cfg: &config.Config{
				Sources: map[string]config.Source{
					"bad": {
						Type: "github",
						Path: "docs",
					},
				},
			},
			wantErrContains: "missing repo",
		},
		{
			name: "invalid github repo format",
			cfg: &config.Config{
				Sources: map[string]config.Source{
					"bad": {
						Type: "github",
						Repo: "goreleaser",
						Path: "docs",
					},
				},
			},
			wantErrContains: "invalid repo format",
		},
		{
			name: "missing github path",
			cfg: &config.Config{
				Sources: map[string]config.Source{
					"bad": {
						Type: "github",
						Repo: "goreleaser/goreleaser",
					},
				},
			},
			wantErrContains: "missing path",
		},
		{
			name: "missing url",
			cfg: &config.Config{
				Sources: map[string]config.Source{
					"bad": {
						Type: "url",
					},
				},
			},
			wantErrContains: "missing url",
		},
		{
			name: "unknown source type",
			cfg: &config.Config{
				Sources: map[string]config.Source{
					"bad": {
						Type: "gitlab",
					},
				},
			},
			wantErrContains: "unknown source type",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErrContains == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("Validate() error = nil, want message containing %q", tc.wantErrContains)
			}

			if !strings.Contains(err.Error(), tc.wantErrContains) {
				t.Fatalf("Validate() error = %q, expected %q", err.Error(), tc.wantErrContains)
			}
		})
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
