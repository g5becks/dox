package main

import (
	"slices"
	"testing"

	"github.com/g5becks/dox/internal/manifest"
)

func TestSuggest(t *testing.T) {
	t.Parallel()
	candidates := []string{"react", "react-query", "tanstack-db", "hono", "goreleaser"}
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"typo", "reakt", []string{"react"}},
		{"transposition", "hnoo", []string{"hono"}},
		{"case", "HONO", []string{"hono"}},
		{"prefix", "tanstack", []string{"tanstack-db"}},
		{"nothing close", "zzzzzzzz", nil},
		{"two matches best first", "react", []string{"react", "react-query"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := suggest(tt.input, candidates)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("suggest(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSuggestionHint(t *testing.T) {
	t.Parallel()
	const fallback = "Run 'dox collections'"
	if got, want := suggestionHint("hnoo", []string{"hono"}, fallback), `Did you mean "hono"? `+fallback; got != want {
		t.Errorf("suggestionHint() = %q, want %q", got, want)
	}
	if got := suggestionHint("zzzzzzzz", []string{"hono"}, fallback); got != fallback {
		t.Errorf("suggestionHint() without a match = %q, want the fallback", got)
	}
}

func TestFileHintSuggestsPaths(t *testing.T) {
	t.Parallel()
	collection := &manifest.Collection{Files: []manifest.FileInfo{
		{Path: "docs/install.md"},
		{Path: "docs/usage.md"},
	}}
	got := fileHint("docs/instal.md", collection)
	if want := `Did you mean "docs/install.md"? Run 'dox files' to see available files`; got != want {
		t.Errorf("fileHint() = %q, want %q", got, want)
	}
}
