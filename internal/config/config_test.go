package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
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
			name: "valid gitlab source",
			cfg: &config.Config{
				Sources: map[string]config.Source{
					"project": {
						Type: "gitlab",
						Repo: "owner/repo",
						Path: "docs",
					},
				},
			},
		},
		{
			name: "valid codeberg source",
			cfg: &config.Config{
				Sources: map[string]config.Source{
					"project": {
						Type: "codeberg",
						Repo: "owner/repo",
						Path: "docs",
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
			name: "neither repo nor url",
			cfg: &config.Config{
				Sources: map[string]config.Source{
					"bad": {
						Type: "github",
						Path: "docs",
					},
				},
			},
			wantErrContains: "has neither 'repo' nor 'url'",
		},
		{
			name: "both repo and url",
			cfg: &config.Config{
				Sources: map[string]config.Source{
					"bad": {
						Type: "github",
						Repo: "owner/repo",
						URL:  "https://example.com/file.txt",
						Path: "docs",
					},
				},
			},
			wantErrContains: "has both 'repo' and 'url'",
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
			wantErrContains: "missing 'path'",
		},
		{
			name: "unknown source type",
			cfg: &config.Config{
				Sources: map[string]config.Source{
					"bad": {
						Type: "bitbucket",
						Repo: "owner/repo",
						Path: "docs",
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

func TestLoadConfigWithGlobalExcludes(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "dox.toml")

	configContent := `
output = ".dox"

excludes = [
    "*.png",
    "node_modules/**",
]

[sources.test]
type = "github"
repo = "owner/repo"
path = "docs"
exclude = ["*.jpg"]
`

	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Check global excludes loaded
	expectedGlobal := []string{"*.png", "node_modules/**"}
	sort.Strings(cfg.Excludes)
	sort.Strings(expectedGlobal)

	if !reflect.DeepEqual(cfg.Excludes, expectedGlobal) {
		t.Errorf("Config.Excludes = %v, want %v", cfg.Excludes, expectedGlobal)
	}

	// Check merged excludes on GitHub source
	sourceExcludes := cfg.Sources["test"].Exclude
	sort.Strings(sourceExcludes)
	expectedMerged := []string{"*.jpg", "*.png", "node_modules/**"}
	sort.Strings(expectedMerged)

	if !reflect.DeepEqual(sourceExcludes, expectedMerged) {
		t.Errorf("Source excludes = %v, want %v", sourceExcludes, expectedMerged)
	}
}

func TestLoadConfigWithDisplaySection(t *testing.T) {
	tests := []struct {
		name        string
		configTOML  string
		wantDisplay config.Display
	}{
		{
			name: "config with full display section",
			configTOML: `
[display]
default_limit = 100
description_length = 300
line_numbers = true
format = "json"
list_fields = ["path", "type"]

[sources.test]
repo = "owner/repo"
path = "docs"
`,
			wantDisplay: config.Display{
				DefaultLimit:      100,
				DescriptionLength: 300,
				LineNumbers:       true,
				Format:            "json",
				ListFields:        []string{"path", "type"},
			},
		},
		{
			name: "config without display section gets defaults",
			configTOML: `
[sources.test]
repo = "owner/repo"
path = "docs"
`,
			wantDisplay: config.Display{
				DefaultLimit:      50,
				DescriptionLength: 200,
				LineNumbers:       false,
				Format:            "table",
				ListFields:        []string{"path", "type", "lines", "size", "description"},
			},
		},
		{
			name: "config with partial display section",
			configTOML: `
[display]
default_limit = 25
format = "csv"

[sources.test]
repo = "owner/repo"
path = "docs"
`,
			wantDisplay: config.Display{
				DefaultLimit:      25,
				DescriptionLength: 200,
				LineNumbers:       false,
				Format:            "csv",
				ListFields:        []string{"path", "type", "lines", "size", "description"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "dox.toml")
			writeFile(t, configPath, tt.configTOML)

			cfg, err := config.Load(configPath)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			if cfg.Display.DefaultLimit != tt.wantDisplay.DefaultLimit {
				t.Errorf("DefaultLimit = %d, want %d", cfg.Display.DefaultLimit, tt.wantDisplay.DefaultLimit)
			}
			if cfg.Display.DescriptionLength != tt.wantDisplay.DescriptionLength {
				t.Errorf("DescriptionLength = %d, want %d",
					cfg.Display.DescriptionLength, tt.wantDisplay.DescriptionLength)
			}
			if cfg.Display.LineNumbers != tt.wantDisplay.LineNumbers {
				t.Errorf("LineNumbers = %v, want %v", cfg.Display.LineNumbers, tt.wantDisplay.LineNumbers)
			}
			if cfg.Display.Format != tt.wantDisplay.Format {
				t.Errorf("Format = %q, want %q", cfg.Display.Format, tt.wantDisplay.Format)
			}
			if !reflect.DeepEqual(cfg.Display.ListFields, tt.wantDisplay.ListFields) {
				t.Errorf("ListFields = %v, want %v", cfg.Display.ListFields, tt.wantDisplay.ListFields)
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
