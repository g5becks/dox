//nolint:testpackage // Testing private functions like mergeExcludes, applySourceDefaults
package config

import (
	"reflect"
	"slices"
	"sort"
	"testing"
)

func TestMergeExcludes(t *testing.T) {
	tests := []struct {
		name   string
		global []string
		source []string
		want   []string
	}{
		{
			name:   "both empty",
			global: []string{},
			source: []string{},
			want:   nil,
		},
		{
			name:   "only global",
			global: []string{"*.png", "*.jpg"},
			source: []string{},
			want:   []string{"*.jpg", "*.png"},
		},
		{
			name:   "only source",
			global: []string{},
			source: []string{"custom/**"},
			want:   []string{"custom/**"},
		},
		{
			name:   "no duplicates",
			global: []string{"*.png", "node_modules/**"},
			source: []string{"*.jpg", "dist/**"},
			want:   []string{"*.jpg", "*.png", "dist/**", "node_modules/**"},
		},
		{
			name:   "with duplicates",
			global: []string{"*.png", "node_modules/**"},
			source: []string{"*.png", "dist/**"},
			want:   []string{"*.png", "dist/**", "node_modules/**"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeExcludes(tt.global, tt.source)

			// Sort both slices for comparison since map iteration is random
			sort.Strings(got)
			sort.Strings(tt.want)

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("mergeExcludes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplyDefaultsMergesGlobalExcludes(t *testing.T) {
	cfg := &Config{
		Output:   ".dox",
		Excludes: []string{"*.png", "node_modules/**"},
		Sources: map[string]Source{
			"github-source": {
				Type:    "github",
				Repo:    "owner/repo",
				Path:    "docs",
				Exclude: []string{"*.jpg", "*.png"}, // *.png is duplicate
			},
			"url-source": {
				Type:    "url",
				URL:     "https://example.com/doc.md",
				Exclude: []string{"*.pdf"},
			},
		},
		ConfigDir: "/tmp",
	}

	cfg.ApplyDefaults()

	// GitHub source should have merged excludes (no duplicates)
	githubExcludes := cfg.Sources["github-source"].Exclude
	sort.Strings(githubExcludes)
	expectedGitHub := []string{"*.jpg", "*.png", "node_modules/**"}
	sort.Strings(expectedGitHub)

	if !reflect.DeepEqual(githubExcludes, expectedGitHub) {
		t.Errorf("GitHub source excludes = %v, want %v", githubExcludes, expectedGitHub)
	}

	// URL source should be unchanged (global excludes don't apply)
	urlExcludes := cfg.Sources["url-source"].Exclude
	expectedURL := []string{"*.pdf"}

	if !reflect.DeepEqual(urlExcludes, expectedURL) {
		t.Errorf("URL source excludes = %v, want %v", urlExcludes, expectedURL)
	}
}

func TestDefaultExcludes(t *testing.T) {
	defaults := DefaultExcludes()

	// Verify it's not empty
	if len(defaults) == 0 {
		t.Error("DefaultExcludes() should return non-empty slice")
	}

	// Verify it includes key patterns
	requiredPatterns := []string{
		".vitepress/**",
		"node_modules/**",
		"**/*.png",
		"dist/**",
	}

	for _, required := range requiredPatterns {
		if !slices.Contains(defaults, required) {
			t.Errorf("DefaultExcludes() missing required pattern: %s", required)
		}
	}
}

func TestApplyDefaultsTypeInference(t *testing.T) {
	tests := []struct {
		name     string
		source   Source
		wantType string
		wantHost string
	}{
		{
			name: "infer github from repo",
			source: Source{
				Repo: "owner/repo",
				Path: "docs",
			},
			wantType: "github",
			wantHost: "github.com",
		},
		{
			name: "infer url from url field",
			source: Source{
				URL: "https://example.com/doc.txt",
			},
			wantType: "url",
			wantHost: "",
		},
		{
			name: "explicit type kept",
			source: Source{
				Type: "gitlab",
				Repo: "owner/repo",
				Path: "docs",
			},
			wantType: "gitlab",
			wantHost: "github.com",
		},
		{
			name: "explicit host kept",
			source: Source{
				Repo: "owner/repo",
				Path: "docs",
				Host: "gitlab.com",
			},
			wantType: "github",
			wantHost: "gitlab.com",
		},
		{
			name: "normalize git type with github host",
			source: Source{
				Type: "git",
				Repo: "owner/repo",
				Path: "docs",
				Host: "github.com",
			},
			wantType: "github",
			wantHost: "github.com",
		},
		{
			name: "normalize git type with gitlab host",
			source: Source{
				Type: "git",
				Repo: "owner/repo",
				Path: "docs",
				Host: "gitlab.com",
			},
			wantType: "gitlab",
			wantHost: "gitlab.com",
		},
		{
			name: "normalize git type with codeberg host",
			source: Source{
				Type: "git",
				Repo: "owner/repo",
				Path: "docs",
				Host: "codeberg.org",
			},
			wantType: "codeberg",
			wantHost: "codeberg.org",
		},
		{
			name: "keep git type for unknown host",
			source: Source{
				Type: "git",
				Repo: "owner/repo",
				Path: "docs",
				Host: "git.company.com",
			},
			wantType: "git",
			wantHost: "git.company.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Sources: map[string]Source{
					"test": tt.source,
				},
			}

			cfg.ApplyDefaults()

			got := cfg.Sources["test"]
			if got.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", got.Type, tt.wantType)
			}
			if got.Host != tt.wantHost {
				t.Errorf("Host = %q, want %q", got.Host, tt.wantHost)
			}
		})
	}
}

func TestApplyDefaultsPatterns(t *testing.T) {
	cfg := &Config{
		Sources: map[string]Source{
			"with-patterns": {
				Type:     "github",
				Repo:     "owner/repo",
				Path:     "docs",
				Patterns: []string{"**/*.rst"},
			},
			"without-patterns": {
				Type: "github",
				Repo: "owner/repo",
				Path: "docs",
			},
			"url-source": {
				Type: "url",
				URL:  "https://example.com/doc.txt",
			},
		},
	}

	cfg.ApplyDefaults()

	// Source with explicit patterns should keep them
	withPatterns := cfg.Sources["with-patterns"].Patterns
	if len(withPatterns) != 1 || withPatterns[0] != "**/*.rst" {
		t.Errorf("with-patterns Patterns = %v, want [**/*.rst]", withPatterns)
	}

	// Source without patterns should get defaults
	withoutPatterns := cfg.Sources["without-patterns"].Patterns
	expectedDefaults := DefaultPatterns()
	if !reflect.DeepEqual(withoutPatterns, expectedDefaults) {
		t.Errorf("without-patterns Patterns = %v, want %v", withoutPatterns, expectedDefaults)
	}

	// URL source should not get patterns
	urlPatterns := cfg.Sources["url-source"].Patterns
	if urlPatterns != nil {
		t.Errorf("url-source Patterns = %v, want nil", urlPatterns)
	}
}
