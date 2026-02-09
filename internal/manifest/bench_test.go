package manifest_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/manifest"
)

func BenchmarkManifestLoad100Files(b *testing.B) {
	tmpDir := b.TempDir()
	doxDir := filepath.Join(tmpDir, ".dox")
	setupBenchmarkFiles(b, doxDir, 100)

	cfg := &config.Config{Output: doxDir}
	if err := manifest.Generate(context.Background(), cfg); err != nil {
		b.Fatalf("generate failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := manifest.Load(doxDir)
		if err != nil {
			b.Fatalf("load failed: %v", err)
		}
	}
}

func BenchmarkManifestLoad1000Files(b *testing.B) {
	tmpDir := b.TempDir()
	doxDir := filepath.Join(tmpDir, ".dox")
	setupBenchmarkFiles(b, doxDir, 1000)

	cfg := &config.Config{Output: doxDir}
	if err := manifest.Generate(context.Background(), cfg); err != nil {
		b.Fatalf("generate failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := manifest.Load(doxDir)
		if err != nil {
			b.Fatalf("load failed: %v", err)
		}
	}
}

func BenchmarkManifestGenerate100Files(b *testing.B) {
	tmpDir := b.TempDir()
	doxDir := filepath.Join(tmpDir, ".dox")
	setupBenchmarkFiles(b, doxDir, 100)

	cfg := &config.Config{Output: doxDir}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := manifest.Generate(context.Background(), cfg); err != nil {
			b.Fatalf("generate failed: %v", err)
		}
	}
}

func setupBenchmarkFiles(b *testing.B, doxDir string, count int) {
	b.Helper()

	collectionDir := filepath.Join(doxDir, "bench")
	if err := os.MkdirAll(collectionDir, 0o755); err != nil {
		b.Fatalf("failed to create dir: %v", err)
	}

	for i := 0; i < count; i++ {
		content := fmt.Sprintf(`# Document %d

This is a sample document for benchmarking.

## Section 1

Content for section 1.

## Section 2

Content for section 2.

### Subsection

More content here.
`, i)

		filename := filepath.Join(collectionDir, fmt.Sprintf("doc-%d.md", i))
		if err := os.WriteFile(filename, []byte(content), 0o644); err != nil {
			b.Fatalf("failed to write file: %v", err)
		}
	}
}
