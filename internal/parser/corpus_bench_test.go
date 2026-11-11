package parser_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/g5becks/dox/internal/benchdata"
	"github.com/g5becks/dox/internal/parser"
)

//go:generate go run ../../testdata/bench/generate -output ../../testdata/bench/pathological.md

func BenchmarkOutlinePathological(b *testing.B) {
	content, err := os.ReadFile("../../testdata/bench/pathological.md")
	if err != nil {
		b.Fatal(err)
	}
	for _, size := range []int{256 * 1024, 1024 * 1024} {
		b.Run(fmt.Sprintf("%dKiB", size/1024), func(b *testing.B) {
			benchmarkMarkdown(b, content[:size])
		})
	}
}

func benchmarkMarkdown(b *testing.B, content []byte) {
	b.Helper()
	p := parser.NewMarkdownParser()
	b.SetBytes(int64(len(content)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := p.Parse("large.md", content); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOutlineLargeMarkdown(b *testing.B) {
	cfg, _, _ := benchdata.Load(b)
	content, err := os.ReadFile(filepath.Join(cfg.Output, "bun", "llms-full.txt"))
	if err != nil {
		b.Fatal(err)
	}
	for _, size := range []int{64 * 1024, 256 * 1024, 512 * 1024, len(content)} {
		b.Run(fmt.Sprintf("%dKiB", size/1024), func(b *testing.B) {
			benchmarkMarkdown(b, content[:size])
		})
	}
}
