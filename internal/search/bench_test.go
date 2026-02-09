//nolint:testpackage // Benchmarks need unexported buildIndex access for isolated index-cost measurement.
package search

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/g5becks/dox/internal/manifest"
	"github.com/g5becks/dox/internal/parser"
)

func BenchmarkBuildIndex700Files(b *testing.B) {
	m := buildBenchmarkManifest(700)
	var idx searchIndex

	b.ResetTimer()
	for b.Loop() {
		idx = buildIndex(m, "")
	}

	if idx.Len() == 0 {
		b.Fatal("expected non-empty index")
	}
}

func BenchmarkMetadataSearch700Files(b *testing.B) {
	m := buildBenchmarkManifest(700)

	b.ResetTimer()
	for b.Loop() {
		_, err := Metadata(m, MetadataOptions{
			Query: "configuration",
			Limit: 50,
		})
		if err != nil {
			b.Fatalf("search failed: %v", err)
		}
	}
}

func BenchmarkContentSearch700Files(b *testing.B) {
	tmpDir := b.TempDir()
	setupBenchmarkContentFiles(b, tmpDir, 700)
	m := buildBenchmarkContentManifest(tmpDir, 700)

	b.ResetTimer()
	for b.Loop() {
		_, err := Content(m, ContentOptions{
			OutputDir: tmpDir,
			Query:     "configuration",
			Limit:     50,
		})
		if err != nil {
			b.Fatalf("search failed: %v", err)
		}
	}
}

func BenchmarkContentSearchRegex700Files(b *testing.B) {
	tmpDir := b.TempDir()
	setupBenchmarkContentFiles(b, tmpDir, 700)
	m := buildBenchmarkContentManifest(tmpDir, 700)

	b.ResetTimer()
	for b.Loop() {
		_, err := Content(m, ContentOptions{
			OutputDir: tmpDir,
			Query:     "func.*Config",
			UseRegex:  true,
			Limit:     50,
		})
		if err != nil {
			b.Fatalf("search failed: %v", err)
		}
	}
}

func buildBenchmarkManifest(fileCount int) *manifest.Manifest {
	m := &manifest.Manifest{
		Version:     "1.0.0",
		Generated:   time.Now(),
		Collections: make(map[string]*manifest.Collection),
	}

	filesPerCollection := fileCount / 3
	collections := []string{"docs", "api", "guides"}

	for _, collName := range collections {
		coll := &manifest.Collection{
			Name:  collName,
			Dir:   collName,
			Type:  "github",
			Files: make([]manifest.FileInfo, 0, filesPerCollection),
		}

		for i := range filesPerCollection {
			file := manifest.FileInfo{
				Path:        fmt.Sprintf("file-%d.md", i),
				Type:        "md",
				Description: fmt.Sprintf("Documentation file %d with configuration details", i),
				Outline: &parser.Outline{
					Type: parser.OutlineTypeHeadings,
					Headings: []parser.Heading{
						{Level: 1, Text: fmt.Sprintf("Document %d", i), Line: 1},
						{Level: 2, Text: "Configuration", Line: 5},
						{Level: 2, Text: "Usage", Line: 10},
					},
				},
			}

			if i%5 == 0 {
				file.Type = "ts"
				file.Outline = &parser.Outline{
					Type: parser.OutlineTypeExports,
					Exports: []parser.Export{
						{Type: "function", Name: fmt.Sprintf("getConfig%d", i), Line: 10},
						{Type: "class", Name: fmt.Sprintf("Config%d", i), Line: 20},
					},
				}
			}

			coll.Files = append(coll.Files, file)
		}

		m.Collections[collName] = coll
	}

	return m
}

func setupBenchmarkContentFiles(b *testing.B, tmpDir string, fileCount int) {
	b.Helper()

	filesPerCollection := fileCount / 3
	collections := []string{"docs", "api", "guides"}

	for _, collName := range collections {
		collDir := filepath.Join(tmpDir, collName)
		if err := os.MkdirAll(collDir, 0o755); err != nil {
			b.Fatalf("failed to create dir: %v", err)
		}

		for i := range filesPerCollection {
			content := fmt.Sprintf(`# Document %d

This is a sample document for benchmarking.

## Configuration

Details about configuration options.

## Usage

How to use this feature.

function getConfig%d() {
  return { setting: "value" };
}
`, i, i)

			filename := filepath.Join(collDir, fmt.Sprintf("file-%d.md", i))
			if err := os.WriteFile(filename, []byte(content), 0o644); err != nil {
				b.Fatalf("failed to write file: %v", err)
			}
		}
	}
}

func buildBenchmarkContentManifest(_ string, fileCount int) *manifest.Manifest {
	m := &manifest.Manifest{
		Version:     "1.0.0",
		Generated:   time.Now(),
		Collections: make(map[string]*manifest.Collection),
	}

	filesPerCollection := fileCount / 3
	collections := []string{"docs", "api", "guides"}

	for _, collName := range collections {
		coll := &manifest.Collection{
			Name:  collName,
			Dir:   collName,
			Type:  "github",
			Files: make([]manifest.FileInfo, 0, filesPerCollection),
		}

		for i := range filesPerCollection {
			coll.Files = append(coll.Files, manifest.FileInfo{
				Path: fmt.Sprintf("file-%d.md", i),
				Type: "md",
			})
		}

		m.Collections[collName] = coll
	}

	return m
}
