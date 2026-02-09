package manifest_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/manifest"
)

func TestGenerate_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	cfg := &config.Config{
		Output:  dir,
		Sources: map[string]config.Source{},
	}

	err := manifest.Generate(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	m, err := manifest.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(m.Collections) != 0 {
		t.Errorf("Collections count = %d, want 0", len(m.Collections))
	}
}

func TestGenerate_WithMarkdownFiles(t *testing.T) {
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "test-source")

	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	mdContent := []byte("# Test Document\n\nThis is a test.\n")
	if err := os.WriteFile(filepath.Join(sourceDir, "test.md"), mdContent, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Output: dir,
		Sources: map[string]config.Source{
			"test-source": {
				Type: "github",
				Repo: "owner/repo",
				Path: "docs",
			},
		},
	}

	err := manifest.Generate(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	m, err := manifest.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(m.Collections) != 1 {
		t.Fatalf("Collections count = %d, want 1", len(m.Collections))
	}

	coll := m.Collections["test-source"]
	if coll == nil {
		t.Fatal("Collection 'test-source' not found")
	}

	if coll.FileCount != 1 {
		t.Errorf("FileCount = %d, want 1", coll.FileCount)
	}

	if len(coll.Files) != 1 {
		t.Fatalf("Files count = %d, want 1", len(coll.Files))
	}

	file := coll.Files[0]
	if file.Path != "test.md" {
		t.Errorf("File path = %q, want 'test.md'", file.Path)
	}

	if file.Type != "md" {
		t.Errorf("File type = %q, want 'md'", file.Type)
	}

	if file.Description == "" {
		t.Error("File description should not be empty")
	}
}

func TestGenerate_MixedFileTypes(t *testing.T) {
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "mixed")

	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	files := map[string][]byte{
		"doc.md":   []byte("# Markdown\n"),
		"comp.mdx": []byte("---\ntitle: MDX\n---\n# Content\n"),
		"note.txt": []byte("Plain text file\n"),
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(sourceDir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := &config.Config{
		Output: dir,
		Sources: map[string]config.Source{
			"mixed": {
				Type: "github",
				Repo: "owner/repo",
				Path: "docs",
			},
		},
	}

	err := manifest.Generate(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	m, err := manifest.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	coll := m.Collections["mixed"]
	if coll == nil {
		t.Fatal("Collection 'mixed' not found")
	}

	if coll.FileCount != 3 {
		t.Errorf("FileCount = %d, want 3", coll.FileCount)
	}

	types := make(map[string]bool)
	for _, file := range coll.Files {
		types[file.Type] = true
	}

	expectedTypes := []string{"md", "mdx", "txt"}
	for _, expectedType := range expectedTypes {
		if !types[expectedType] {
			t.Errorf("Missing file type: %s", expectedType)
		}
	}
}
