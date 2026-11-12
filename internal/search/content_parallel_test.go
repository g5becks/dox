package search_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/g5becks/dox/internal/manifest"
	"github.com/g5becks/dox/internal/search"
)

func TestContentOrderedLimitsAndBufferReuse(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	m := &manifest.Manifest{
		Collections: map[string]*manifest.Collection{"docs": {Dir: ".", Files: []manifest.FileInfo{}}},
	}
	var expected []search.ContentResult
	for index := range 25 {
		path := fmt.Sprintf("%02d.txt", index)
		text := fmt.Sprintf("needle %d", index)
		if err := os.WriteFile(filepath.Join(root, path), []byte(text+"\nignored\n"+text+"\r\n"), 0600); err != nil {
			t.Fatal(err)
		}
		m.Collections["docs"].Files = append(m.Collections["docs"].Files, manifest.FileInfo{Path: path})
		expected = append(
			expected,
			search.ContentResult{Collection: "docs", Path: path, Line: 1, Text: text},
			search.ContentResult{Collection: "docs", Path: path, Line: 3, Text: text + "\r"},
		)
	}
	for _, limit := range []int{0, 1, 3, 17, 100} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			t.Parallel()
			want := expected
			if limit > 0 && limit < len(want) {
				want = want[:limit]
			}
			for range 3 {
				got, err := search.Content(m, search.ContentOptions{OutputDir: root, Query: "needle", Limit: limit})
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("limit %d: got %+v; want %+v", limit, got, want)
				}
			}
		})
	}
}

func TestContentUnicodeLiteralCompatibility(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	const text = "İstanbul\nſ\ns\nΣ\nς\nσ\nK\nK\n"
	if err := os.WriteFile(filepath.Join(root, "unicode.txt"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	m := &manifest.Manifest{
		Collections: map[string]*manifest.Collection{
			"docs": {Dir: ".", Files: []manifest.FileInfo{{Path: "unicode.txt"}}},
		},
	}
	for _, query := range []string{"i", "İ", "s", "ſ", "σ", "ς", "k", "K"} {
		t.Run(query, func(t *testing.T) {
			t.Parallel()
			var expected []search.ContentResult
			for index, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
				if strings.Contains(strings.ToLower(line), strings.ToLower(query)) {
					expected = append(
						expected,
						search.ContentResult{Collection: "docs", Path: "unicode.txt", Line: index + 1, Text: line},
					)
				}
			}
			got, err := search.Content(m, search.ContentOptions{OutputDir: root, Query: query})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, expected) {
				t.Fatalf("query %q: got %+v; want %+v", query, got, expected)
			}
		})
	}
}
