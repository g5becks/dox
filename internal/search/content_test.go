package search_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/g5becks/dox/internal/manifest"
	"github.com/g5becks/dox/internal/search"
)

func TestContent_LiteralCaseInsensitive(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	setupContentTestFiles(t, tmpDir)
	m := buildContentTestManifest(tmpDir)

	results, err := search.Content(m, search.ContentOptions{
		OutputDir: tmpDir,
		Query:     "HELLO",
		Limit:     0,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}

	found := false
	for _, r := range results {
		if strings.Contains(strings.ToLower(r.Text), "hello") {
			found = true
			break
		}
	}

	if !found {
		t.Error("expected case-insensitive match for 'HELLO'")
	}
}

func TestContent_RegexCaseInsensitive(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	setupContentTestFiles(t, tmpDir)
	m := buildContentTestManifest(tmpDir)

	results, err := search.Content(m, search.ContentOptions{
		OutputDir: tmpDir,
		Query:     "func.*test",
		UseRegex:  true,
		Limit:     0,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least one result for regex pattern")
	}
}

func TestContent_InvalidRegex(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	setupContentTestFiles(t, tmpDir)
	m := buildContentTestManifest(tmpDir)

	_, err := search.Content(m, search.ContentOptions{
		OutputDir: tmpDir,
		Query:     "[invalid",
		UseRegex:  true,
		Limit:     0,
	})

	if err == nil {
		t.Fatal("expected error for invalid regex")
	}
}

func TestContent_LimitStopsEarly(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	setupContentTestFiles(t, tmpDir)
	m := buildContentTestManifest(tmpDir)

	results, err := search.Content(m, search.ContentOptions{
		OutputDir: tmpDir,
		Query:     "line",
		Limit:     2,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) > 2 {
		t.Errorf("expected at most 2 results, got %d", len(results))
	}
}

func TestContent_CollectionFilter(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	setupContentTestFiles(t, tmpDir)
	m := buildContentTestManifest(tmpDir)

	results, err := search.Content(m, search.ContentOptions{
		OutputDir:  tmpDir,
		Query:      "line",
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

func TestContent_UnknownCollection(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	setupContentTestFiles(t, tmpDir)
	m := buildContentTestManifest(tmpDir)

	_, err := search.Content(m, search.ContentOptions{
		OutputDir:  tmpDir,
		Query:      "test",
		Collection: "unknown",
		Limit:      0,
	})

	if err == nil {
		t.Fatal("expected error for unknown collection")
	}
}

func TestContent_SkipsBinary(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	docsDir := filepath.Join(tmpDir, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	binaryFile := filepath.Join(docsDir, "binary.dat")
	if err := os.WriteFile(binaryFile, []byte("hello\x00world"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &manifest.Manifest{
		Collections: map[string]*manifest.Collection{
			"docs": {
				Name: "docs",
				Dir:  "docs",
				Files: []manifest.FileInfo{
					{Path: "binary.dat", Type: "unknown"},
				},
			},
		},
	}

	results, err := search.Content(m, search.ContentOptions{
		OutputDir: tmpDir,
		Query:     "hello",
		Limit:     0,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 0 {
		t.Error("expected binary file to be skipped")
	}
}

func TestContent_SkipsMissingFiles(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	m := &manifest.Manifest{
		Collections: map[string]*manifest.Collection{
			"docs": {
				Name: "docs",
				Dir:  "docs",
				Files: []manifest.FileInfo{
					{Path: "missing.txt", Type: "txt"},
				},
			},
		},
	}

	results, err := search.Content(m, search.ContentOptions{
		OutputDir: tmpDir,
		Query:     "test",
		Limit:     0,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 0 {
		t.Error("expected missing file to be skipped")
	}
}

func TestContent_SkipsLargeFiles(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	docsDir := filepath.Join(tmpDir, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	largeFile := filepath.Join(docsDir, "large.txt")
	f, err := os.Create(largeFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if truncErr := f.Truncate(51 * 1024 * 1024); truncErr != nil {
		t.Fatal(truncErr)
	}

	m := &manifest.Manifest{
		Collections: map[string]*manifest.Collection{
			"docs": {
				Name: "docs",
				Dir:  "docs",
				Files: []manifest.FileInfo{
					{Path: "large.txt", Type: "txt"},
				},
			},
		},
	}

	results, err := search.Content(m, search.ContentOptions{
		OutputDir: tmpDir,
		Query:     "test",
		Limit:     0,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 0 {
		t.Error("expected large file to be skipped")
	}
}

func TestContent_LineNumbersOneBased(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	docsDir := filepath.Join(tmpDir, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	testFile := filepath.Join(docsDir, "test.txt")
	content := "first line\nsecond line\nthird line"
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &manifest.Manifest{
		Collections: map[string]*manifest.Collection{
			"docs": {
				Name: "docs",
				Dir:  "docs",
				Files: []manifest.FileInfo{
					{Path: "test.txt", Type: "txt"},
				},
			},
		},
	}

	results, err := search.Content(m, search.ContentOptions{
		OutputDir: tmpDir,
		Query:     "first",
		Limit:     0,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Line != 1 {
		t.Errorf("expected line number 1, got %d", results[0].Line)
	}
}

func TestContent_EmptyQuery(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	setupContentTestFiles(t, tmpDir)
	m := buildContentTestManifest(tmpDir)

	_, err := search.Content(m, search.ContentOptions{
		OutputDir: tmpDir,
		Query:     "",
		Limit:     0,
	})

	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func setupContentTestFiles(t *testing.T, tmpDir string) {
	t.Helper()

	docsDir := filepath.Join(tmpDir, "docs")
	apiDir := filepath.Join(tmpDir, "api")

	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(apiDir, 0o755); err != nil {
		t.Fatal(err)
	}

	files := map[string]string{
		filepath.Join(docsDir, "readme.md"): "Hello world\nThis is a test\nAnother line here",
		filepath.Join(apiDir, "code.ts"):    "function testFunc() {\n  return true;\n}",
	}

	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func buildContentTestManifest(_ string) *manifest.Manifest {
	return &manifest.Manifest{
		Version:   "1.0.0",
		Generated: time.Now(),
		Collections: map[string]*manifest.Collection{
			"docs": {
				Name: "docs",
				Dir:  "docs",
				Type: "github",
				Files: []manifest.FileInfo{
					{Path: "readme.md", Type: "md"},
				},
			},
			"api": {
				Name: "api",
				Dir:  "api",
				Type: "github",
				Files: []manifest.FileInfo{
					{Path: "code.ts", Type: "ts"},
				},
			},
		},
	}
}
