package search_test

import (
	"testing"
	"time"

	"github.com/g5becks/dox/internal/manifest"
	"github.com/g5becks/dox/internal/parser"
	"github.com/g5becks/dox/internal/search"
)

func buildTestManifest() *manifest.Manifest {
	return &manifest.Manifest{
		Version:   "1.0.0",
		Generated: time.Now(),
		Collections: map[string]*manifest.Collection{
			"docs": {
				Name: "docs",
				Dir:  "docs",
				Type: "github",
				Files: []manifest.FileInfo{
					{
						Path:        "install.md",
						Type:        "md",
						Description: "Installation guide for getting started",
						Outline: &parser.Outline{
							Type: parser.OutlineTypeHeadings,
							Headings: []parser.Heading{
								{Level: 1, Text: "Installation", Line: 1},
								{Level: 2, Text: "Quick Start", Line: 5},
							},
						},
					},
					{
						Path:        "config.md",
						Type:        "md",
						Description: "Configuration options",
						Outline: &parser.Outline{
							Type: parser.OutlineTypeHeadings,
							Headings: []parser.Heading{
								{Level: 1, Text: "Configuration", Line: 1},
							},
						},
					},
				},
			},
			"api": {
				Name: "api",
				Dir:  "api",
				Type: "github",
				Files: []manifest.FileInfo{
					{
						Path:        "logger.ts",
						Type:        "ts",
						Description: "Logger utility functions",
						Outline: &parser.Outline{
							Type: parser.OutlineTypeExports,
							Exports: []parser.Export{
								{Type: "function", Name: "createLogger", Line: 10},
								{Type: "class", Name: "Logger", Line: 20},
							},
						},
					},
				},
			},
		},
	}
}

func TestMetadata_PathMatch(t *testing.T) {
	t.Parallel()
	m := buildTestManifest()

	results, err := search.Metadata(m, search.MetadataOptions{
		Query: "install",
		Limit: 0,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}

	found := false
	for _, r := range results {
		if r.Path == "install.md" && r.MatchField == "path" {
			found = true
			break
		}
	}

	if !found {
		t.Error("expected to find install.md in path matches")
	}
}

func TestMetadata_DescriptionMatch(t *testing.T) {
	t.Parallel()
	m := buildTestManifest()

	results, err := search.Metadata(m, search.MetadataOptions{
		Query: "utility",
		Limit: 0,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, r := range results {
		if r.Path == "logger.ts" && r.MatchField == "description" {
			found = true
			break
		}
	}

	if !found {
		t.Error("expected to find logger.ts via description match")
	}
}

func TestMetadata_HeadingMatch(t *testing.T) {
	t.Parallel()
	m := buildTestManifest()

	results, err := search.Metadata(m, search.MetadataOptions{
		Query: "Quick Start",
		Limit: 0,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, r := range results {
		if r.Path == "install.md" && r.MatchField == "heading" && r.MatchValue == "Quick Start" {
			found = true
			break
		}
	}

	if !found {
		t.Error("expected to find heading match for 'Quick Start'")
	}
}

func TestMetadata_ExportMatch(t *testing.T) {
	t.Parallel()
	m := buildTestManifest()

	results, err := search.Metadata(m, search.MetadataOptions{
		Query: "Logger",
		Limit: 0,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, r := range results {
		if r.Path == "logger.ts" && r.MatchField == "export" {
			found = true
			break
		}
	}

	if !found {
		t.Error("expected to find export match for Logger")
	}
}

func TestMetadata_CollectionFilter(t *testing.T) {
	t.Parallel()
	m := buildTestManifest()

	results, err := search.Metadata(m, search.MetadataOptions{
		Query:      "config",
		Collection: "docs",
		Limit:      0,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, r := range results {
		if r.Collection != "docs" {
			t.Errorf("expected only 'docs' collection, got %q", r.Collection)
		}
	}
}

func TestMetadata_UnknownCollection(t *testing.T) {
	t.Parallel()
	m := buildTestManifest()

	_, err := search.Metadata(m, search.MetadataOptions{
		Query:      "test",
		Collection: "unknown",
		Limit:      0,
	})

	if err == nil {
		t.Fatal("expected error for unknown collection")
	}
}

func TestMetadata_EmptyQuery(t *testing.T) {
	t.Parallel()
	m := buildTestManifest()

	_, err := search.Metadata(m, search.MetadataOptions{
		Query: "",
		Limit: 0,
	})

	if err == nil {
		t.Fatal("expected error for empty query")
	}

	_, err = search.Metadata(m, search.MetadataOptions{
		Query: "   ",
		Limit: 0,
	})

	if err == nil {
		t.Fatal("expected error for whitespace-only query")
	}
}

func TestMetadata_Limit(t *testing.T) {
	t.Parallel()
	m := buildTestManifest()

	results, err := search.Metadata(m, search.MetadataOptions{
		Query: "config",
		Limit: 2,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) > 2 {
		t.Errorf("expected at most 2 results, got %d", len(results))
	}
}

func TestMetadata_DedupesBestScorePerFile(t *testing.T) {
	t.Parallel()
	m := buildTestManifest()

	results, err := search.Metadata(m, search.MetadataOptions{
		Query: "install",
		Limit: 0,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	seen := make(map[string]int)
	for _, r := range results {
		key := r.Collection + "/" + r.Path
		seen[key]++
	}

	for key, count := range seen {
		if count > 1 {
			t.Errorf("file %q appears %d times, expected deduplication", key, count)
		}
	}
}

func TestMetadata_EmptyManifest(t *testing.T) {
	t.Parallel()
	m := &manifest.Manifest{
		Collections: make(map[string]*manifest.Collection),
	}

	results, err := search.Metadata(m, search.MetadataOptions{
		Query: "test",
		Limit: 0,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 results for empty manifest, got %d", len(results))
	}
}

func TestMetadata_NoResults(t *testing.T) {
	t.Parallel()
	m := buildTestManifest()

	results, err := search.Metadata(m, search.MetadataOptions{
		Query: "xyzzynonexistent",
		Limit: 0,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 results for nonsense query, got %d", len(results))
		for _, r := range results {
			t.Logf("  result: collection=%s path=%s score=%d", r.Collection, r.Path, r.Score)
		}
	}
}

func TestMetadata_ScoreOrdering(t *testing.T) {
	t.Parallel()
	m := buildTestManifest()

	results, err := search.Metadata(m, search.MetadataOptions{
		Query: "md",
		Limit: 0,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) < 2 {
		t.Fatalf("expected at least 2 results to validate ordering, got %d", len(results))
	}

	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted by score: result[%d].Score=%d > result[%d].Score=%d",
				i, results[i].Score, i-1, results[i-1].Score)
		}
	}
}
