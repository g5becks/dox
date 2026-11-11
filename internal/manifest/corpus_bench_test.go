package manifest_test

import (
	"errors"
	"os"
	"testing"

	"github.com/g5becks/dox/internal/benchdata"
	"github.com/g5becks/dox/internal/manifest"
)

func BenchmarkManifestGenerateFull(b *testing.B) {
	cfg, lock, _ := benchdata.Load(b)
	output := b.TempDir()
	if err := os.CopyFS(output, os.DirFS(cfg.Output)); err != nil {
		b.Fatal(err)
	}
	cfg.Output = output
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		err := os.Remove(manifest.Path(output))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			b.Fatal(err)
		}
		b.StartTimer()
		if genErr := manifest.Generate(b.Context(), cfg, lock); genErr != nil {
			b.Fatal(genErr)
		}
	}
}

func BenchmarkManifestGenerateNoop(b *testing.B) {
	cfg, lock, _ := benchdata.Load(b)
	output := b.TempDir()
	if err := os.CopyFS(output, os.DirFS(cfg.Output)); err != nil {
		b.Fatal(err)
	}
	cfg.Output = output
	if err := manifest.Generate(b.Context(), cfg, lock); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := manifest.Generate(b.Context(), cfg, lock); err != nil {
			b.Fatal(err)
		}
	}
}
