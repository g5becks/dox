package manifest_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/manifest"
)

func TestFullWorkflowIntegration(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	doxDir := filepath.Join(tmpDir, ".dox")
	collectionDir := filepath.Join(doxDir, "test-collection")

	if err := os.MkdirAll(collectionDir, 0o755); err != nil {
		t.Fatalf("failed to create collection dir: %v", err)
	}

	// Create sample files
	files := map[string]string{
		"readme.md": "# Test\n\nDescription here.",
		"guide.mdx": "# Guide\n\nGuide content.",
		"notes.txt": "Plain text notes.",
		"types.tsx": "export interface Props {}\nexport function Component() {}",
	}

	for name, content := range files {
		path := filepath.Join(collectionDir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}

	cfg := &config.Config{
		Output: doxDir,
		Sources: map[string]config.Source{
			"test-collection": {
				Type: "github",
				Repo: "test/test",
				Path: "docs",
			},
		},
	}

	// Generate manifest
	if err := manifest.Generate(context.Background(), cfg); err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	// Load manifest
	m, err := manifest.Load(doxDir)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	// Verify structure
	if len(m.Collections) != 1 {
		t.Fatalf("expected 1 collection, got %d", len(m.Collections))
	}

	coll := m.Collections["test-collection"]
	if coll == nil {
		t.Fatal("collection not found")
	}

	if len(coll.Files) != len(files) {
		t.Errorf("expected %d files, got %d", len(files), len(coll.Files))
	}

	// Verify each file
	for name := range files {
		found := false
		for _, f := range coll.Files {
			if filepath.Base(f.Path) == name {
				found = true
				if f.Size == 0 {
					t.Errorf("file %s has zero size", name)
				}
				break
			}
		}
		if !found {
			t.Errorf("file %s not found in manifest", name)
		}
	}
}

func TestManifestRoundTrip(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	doxDir := filepath.Join(tmpDir, ".dox")
	collectionDir := filepath.Join(doxDir, "docs")

	if err := os.MkdirAll(collectionDir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	// Create test file
	testFile := filepath.Join(collectionDir, "test.md")
	testContent := "# Test\n\nTest content."
	if err := os.WriteFile(testFile, []byte(testContent), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	cfg := &config.Config{
		Output: doxDir,
		Sources: map[string]config.Source{
			"docs": {
				Type: "github",
				Repo: "test/test",
				Path: "docs",
			},
		},
	}

	// Generate
	if err := manifest.Generate(context.Background(), cfg); err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	// Load
	m1, err := manifest.Load(doxDir)
	if err != nil {
		t.Fatalf("first load failed: %v", err)
	}

	// Save
	if saveErr := m1.Save(doxDir); saveErr != nil {
		t.Fatalf("save failed: %v", saveErr)
	}

	// Load again
	m2, err := manifest.Load(doxDir)
	if err != nil {
		t.Fatalf("second load failed: %v", err)
	}

	// Verify identical
	if len(m1.Collections) != len(m2.Collections) {
		t.Errorf("collection count mismatch: %d vs %d",
			len(m1.Collections), len(m2.Collections))
	}

	for name, c1 := range m1.Collections {
		c2 := m2.Collections[name]
		if c2 == nil {
			t.Errorf("collection %s missing after round-trip", name)
			continue
		}

		if len(c1.Files) != len(c2.Files) {
			t.Errorf("file count mismatch for %s: %d vs %d",
				name, len(c1.Files), len(c2.Files))
		}
	}
}

func TestMixedFileTypes(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	doxDir := filepath.Join(tmpDir, ".dox")
	collectionDir := filepath.Join(doxDir, "mixed")

	if err := os.MkdirAll(collectionDir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	files := map[string]struct {
		content  string
		wantType string
	}{
		"doc.md":  {"# Markdown\n\nContent.", "md"},
		"doc.mdx": {"# MDX\n\nContent.", "mdx"},
		"doc.txt": {"Plain text.", "txt"},
		"doc.tsx": {"export const x = 1;", "tsx"},
	}

	for name, data := range files {
		path := filepath.Join(collectionDir, name)
		if err := os.WriteFile(path, []byte(data.content), 0o644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}

	cfg := &config.Config{
		Output: doxDir,
		Sources: map[string]config.Source{
			"mixed": {
				Type: "github",
				Repo: "test/test",
				Path: "docs",
			},
		},
	}

	if err := manifest.Generate(context.Background(), cfg); err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	m, err := manifest.Load(doxDir)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	coll := m.Collections["mixed"]
	if coll == nil {
		t.Fatal("collection not found")
	}

	for name, data := range files {
		found := false
		for _, f := range coll.Files {
			if filepath.Base(f.Path) == name {
				found = true
				if f.Type != data.wantType {
					t.Errorf("file %s: got type %q, want %q",
						name, f.Type, data.wantType)
				}
				break
			}
		}
		if !found {
			t.Errorf("file %s not found", name)
		}
	}
}

func TestEmptyCollection(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	doxDir := filepath.Join(tmpDir, ".dox")
	collectionDir := filepath.Join(doxDir, "empty")

	if err := os.MkdirAll(collectionDir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	cfg := &config.Config{
		Output: doxDir,
		Sources: map[string]config.Source{
			"empty": {
				Type: "github",
				Repo: "test/test",
				Path: "docs",
			},
		},
	}

	if err := manifest.Generate(context.Background(), cfg); err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	m, err := manifest.Load(doxDir)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	coll := m.Collections["empty"]
	if coll == nil {
		t.Fatal("empty collection should exist")
	}

	if len(coll.Files) != 0 {
		t.Errorf("expected 0 files, got %d", len(coll.Files))
	}
}

func TestFileMetadata(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	doxDir := filepath.Join(tmpDir, ".dox")
	collectionDir := filepath.Join(doxDir, "meta")

	if err := os.MkdirAll(collectionDir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	content := `# Title

Description line.

## Section

Content here.
`

	testFile := filepath.Join(collectionDir, "test.md")
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	cfg := &config.Config{
		Output: doxDir,
		Sources: map[string]config.Source{
			"meta": {
				Type: "github",
				Repo: "test/test",
				Path: "docs",
			},
		},
	}

	if err := manifest.Generate(context.Background(), cfg); err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	m, err := manifest.Load(doxDir)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	coll := m.Collections["meta"]
	if coll == nil {
		t.Fatal("collection not found")
	}

	if len(coll.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(coll.Files))
	}

	f := coll.Files[0]

	if f.Size == 0 {
		t.Error("expected non-zero size")
	}

	if f.Description == "" {
		t.Error("expected description")
	}

	if f.Outline == nil || len(f.Outline.Headings) == 0 {
		t.Error("expected headings in outline")
	}

	if f.Type != "md" {
		t.Errorf("got type %q, want md", f.Type)
	}
}
