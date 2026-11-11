package search_test

import (
	"testing"

	"github.com/g5becks/dox/internal/benchdata"
	"github.com/g5becks/dox/internal/search"
)

func BenchmarkSearchContentLiteral(b *testing.B) {
	benchmarkCorpusContent(b, "query", false)
}

func BenchmarkSearchContentRegex(b *testing.B) {
	benchmarkCorpusContent(b, `use[A-Z]\w+`, true)
}

func benchmarkCorpusContent(b *testing.B, query string, regex bool) {
	b.Helper()
	cfg, _, m := benchdata.Load(b)
	b.ReportAllocs()
	for b.Loop() {
		results, err := search.Content(m, search.ContentOptions{OutputDir: cfg.Output, Query: query, UseRegex: regex})
		if err != nil {
			b.Fatal(err)
		}
		if len(results) == 0 {
			b.Fatal("expected matches in the pinned corpus")
		}
	}
}

func BenchmarkSearchMetadata(b *testing.B) {
	_, _, m := benchdata.Load(b)
	b.ReportAllocs()
	for b.Loop() {
		results, err := search.Metadata(m, search.MetadataOptions{Query: "routing"})
		if err != nil {
			b.Fatal(err)
		}
		if len(results) == 0 {
			b.Fatal("expected metadata matches in the pinned corpus")
		}
	}
}
