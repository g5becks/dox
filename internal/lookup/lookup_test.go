package lookup_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/lookup"
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
			got := lookup.Suggest(tt.input, candidates)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Suggest(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestHint(t *testing.T) {
	t.Parallel()
	const fallback = "Run 'dox collections'"
	if got, want := lookup.Hint("hnoo", []string{"hono"}, fallback), `Did you mean "hono"? `+fallback; got != want {
		t.Errorf("Hint() = %q, want %q", got, want)
	}
	if got := lookup.Hint("zzzzzzzz", []string{"hono"}, fallback); got != fallback {
		t.Errorf("Hint() without a match = %q, want the fallback", got)
	}
}

func testManifest() *manifest.Manifest {
	m := manifest.New()
	m.Collections["hono"] = &manifest.Collection{
		Name: "hono",
		Files: []manifest.FileInfo{
			{Path: "docs/install.md"},
			{Path: "docs/usage.md"},
		},
	}
	return m
}

func TestFileHintSuggestsPaths(t *testing.T) {
	t.Parallel()
	got := lookup.FileHint("docs/instal.md", testManifest().Collections["hono"])
	if want := `Did you mean "docs/install.md"? Run 'dox files' to see available files`; got != want {
		t.Errorf("FileHint() = %q, want %q", got, want)
	}
}

func TestCollectionFindsAndMisses(t *testing.T) {
	t.Parallel()
	m := testManifest()
	found, err := lookup.Collection(m, "hono")
	if err != nil || found != m.Collections["hono"] {
		t.Fatalf("Collection(hono) = %v, %v; want the collection", found, err)
	}

	_, err = lookup.Collection(m, "hnoo")
	detail, ok := errors.AsType[*errs.Error](err)
	if !ok || detail.Code != errs.NotFound {
		t.Fatalf("Collection(hnoo) error = %v, want a not_found *errs.Error", err)
	}
	if !errors.Is(err, errs.ErrCollectionNotFound) {
		t.Errorf("Collection(hnoo) error does not wrap ErrCollectionNotFound: %v", err)
	}
	if want := `collection "hnoo" not found`; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
	if !strings.HasPrefix(detail.Hint, `Did you mean "hono"?`) {
		t.Errorf("hint = %q, want a did-you-mean suggestion", detail.Hint)
	}
	if detail.Fields[errs.LegacyCodeField] != errs.LegacyCollectionNotFound || detail.Fields["collection"] != "hnoo" {
		t.Errorf("fields = %v, want the legacy code and collection", detail.Fields)
	}
}

func TestFileFindsAndMisses(t *testing.T) {
	t.Parallel()
	collection := testManifest().Collections["hono"]
	found, err := lookup.File(collection, "hono", "docs/usage.md")
	if err != nil || found != &collection.Files[1] {
		t.Fatalf("File(docs/usage.md) = %v, %v; want the file entry", found, err)
	}

	_, err = lookup.File(collection, "hono", "docs/instal.md")
	detail, ok := errors.AsType[*errs.Error](err)
	if !ok || detail.Code != errs.NotFound {
		t.Fatalf("File(docs/instal.md) error = %v, want a not_found *errs.Error", err)
	}
	if !errors.Is(err, errs.ErrFileNotFound) {
		t.Errorf("File() error does not wrap ErrFileNotFound: %v", err)
	}
	if want := `file "docs/instal.md" not found in collection "hono"`; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
	if !strings.HasPrefix(detail.Hint, `Did you mean "docs/install.md"?`) {
		t.Errorf("hint = %q, want a did-you-mean suggestion", detail.Hint)
	}
	if detail.Fields[errs.LegacyCodeField] != errs.LegacyFileNotFound || detail.Fields["file"] != "docs/instal.md" {
		t.Errorf("fields = %v, want the legacy code and file", detail.Fields)
	}
}
