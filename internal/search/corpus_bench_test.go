package search_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/g5becks/dox/internal/benchdata"
	"github.com/g5becks/dox/internal/manifest"
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
	verifyCorpusContentGolden(b, m, search.ContentOptions{OutputDir: cfg.Output, Query: query, UseRegex: regex})
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

func verifyCorpusContentGolden(b *testing.B, m *manifest.Manifest, opts search.ContentOptions) {
	b.Helper()
	data, err := os.ReadFile("testdata/corpus-v0.3.0.json")
	if err != nil {
		b.Fatal(err)
	}
	var golden []struct {
		Query  string `json:"query"`
		Regex  bool   `json:"regex"`
		Count  int    `json:"count"`
		SHA256 string `json:"sha256"`
	}
	if decodeErr := json.Unmarshal(data, &golden); decodeErr != nil {
		b.Fatal(decodeErr)
	}
	results, err := search.Content(m, opts)
	if err != nil {
		b.Fatal(err)
	}
	encoded, err := json.Marshal(results)
	if err != nil {
		b.Fatal(err)
	}
	for _, expected := range golden {
		if expected.Query == opts.Query && expected.Regex == opts.UseRegex {
			digest := fmt.Sprintf("%x", sha256.Sum256(encoded))
			if len(results) != expected.Count || digest != expected.SHA256 {
				b.Fatalf("corpus results differ from v0.3.0: count=%d hash=%s", len(results), digest)
			}
			return
		}
	}
	b.Fatal("no corpus golden for query")
}
