package parser_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/g5becks/dox/internal/parser"
)

type legacyOutlineFixture struct {
	Path   string              `json:"Path"`
	Result *parser.ParseResult `json:"Result"`
}
type outlineCorrection struct {
	Path   string              `json:"Path"`
	Before *parser.ParseResult `json:"Before"`
	After  *parser.ParseResult `json:"After"`
	Reason string              `json:"Reason"`
}

func readOutlineJSON[T any](t *testing.T, path string) T {
	t.Helper()
	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	var value T
	if decodeErr := json.Unmarshal(raw, &value); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	return value
}

func correctedOutline(t *testing.T, record legacyOutlineFixture, corrections []outlineCorrection) *parser.ParseResult {
	t.Helper()
	for _, correction := range corrections {
		if correction.Path == record.Path {
			if correction.Reason == "" || !reflect.DeepEqual(correction.Before, record.Result) {
				t.Fatal("unexplained or stale fixture correction")
			}
			t.Log(correction.Reason)
			return correction.After
		}
	}
	return record.Result
}

func TestOutlineLegacyFixtures(t *testing.T) {
	t.Parallel()
	records := readOutlineJSON[[]legacyOutlineFixture](t, "testdata/outline/legacy.json")
	corrections := readOutlineJSON[[]outlineCorrection](t, "testdata/outline/corrections.json")
	for _, record := range records {
		t.Run(record.Path, func(t *testing.T) {
			t.Parallel()
			body, readErr := os.ReadFile(record.Path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			actual, parseErr := outlineParser(record.Path).Parse(record.Path, body)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			want := correctedOutline(t, record, corrections)
			if !reflect.DeepEqual(actual, want) {
				t.Fatalf(
					"fixture differs from documented output: got %+v / %+v, want %+v / %+v",
					actual,
					actual.Outline,
					want,
					want.Outline,
				)
			}
		})
	}
}

func TestOutlineAuditCorrections(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		path        string
		lines       []int
		description string
	}{
		{"testdata/outline/fences.md", []int{1, 13}, "Real"},
		{"testdata/outline/positions.mdx", []int{8, 12}, "Component"},
		{"testdata/outline/toml.md", []int{6}, "TOML title - TOML description"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			body, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			result, err := outlineParser(tc.path).Parse(tc.path, body)
			if err != nil {
				t.Fatal(err)
			}
			var lines []int
			for _, heading := range result.Outline.Headings {
				lines = append(lines, heading.Line)
			}
			if !reflect.DeepEqual(lines, tc.lines) || result.Description != tc.description {
				t.Fatalf("lines=%v, description=%q; want %v / %q", lines, result.Description, tc.lines, tc.description)
			}
		})
	}
}

func outlineParser(path string) parser.Parser {
	if strings.HasSuffix(path, ".mdx") {
		return parser.NewMDXParser()
	}
	return parser.NewMarkdownParser()
}

func FuzzOutlineScanner(f *testing.F) {
	for _, text := range []string{"# Title\n\nBody\n", "---\ntitle: Test\n---\n# Title\n", "````go\n# Fake\n```\n## Fake\n````\n# Real", "Title\n=====\n", "+++\ntitle = 'Test'\n+++\n## Heading"} {
		f.Add([]byte(text), false)
		f.Add([]byte(text), true)
	}
	f.Fuzz(func(t *testing.T, body []byte, mdx bool) {
		var p parser.Parser = parser.NewMarkdownParser()
		if mdx {
			p = parser.NewMDXParser()
		}
		result, err := p.Parse("fuzz.md", body)
		if err != nil {
			t.Fatal(err)
		}
		total := bytes.Count(body, []byte{'\n'}) + 1
		for _, heading := range result.Outline.Headings {
			if heading.Line < 1 || heading.Line > total {
				t.Fatalf("heading line %d outside [1,%d]", heading.Line, total)
			}
		}
	})
}

func TestOutlineBlockBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, content, description string }{
		{"list paragraph", "# Title\n\n- First `code`\n- Second\n", "Title - First"},
		{"numbered heading", "# 1. Create `file`\n", "1. Create"},
		{"autolink", "# Title\n\nPR: <https://example.invalid>.\n", "Title - PR: https://example.invalid."},
		{"script block", "<script setup>\nimport Thing from 'thing';\n</script>\n\n# Title\n\nBody.\n", "Title - Body."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, err := parser.NewMarkdownParser().Parse("test.md", []byte(tc.content))
			if err != nil {
				t.Fatal(err)
			}
			if result.Description != tc.description {
				t.Fatalf("description=%q, want %q", result.Description, tc.description)
			}
		})
	}
}

func TestOutlineMetadataAndInlineBudgets(t *testing.T) {
	t.Parallel()
	content := []byte("---\ntitle: 'Quoted: title'\ndescription: >-\n  first line\n  second line\n---\n# Heading\n")
	result, err := parser.NewMarkdownParser().Parse("metadata.md", content)
	if err != nil {
		t.Fatal(err)
	}
	if result.Description != "Quoted: title - first line second line" {
		t.Fatalf("metadata decode: %q", result.Description)
	}
	largeMetadata := []byte("---\ntitle: " + strings.Repeat("x", 65*1024) + "\n---\n# Fallback\n")
	fallback, parseErr := parser.NewMarkdownParser().Parse("large-metadata.md", largeMetadata)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	if fallback.Description != "Fallback" || fallback.Outline.Headings[0].Line != 4 {
		t.Fatalf("metadata budget fallback: %+v", fallback)
	}
	largeInline := "**" + strings.Repeat("x", 257*1024) + "**"
	inline, inlineErr := parser.NewMarkdownParser().Parse("large-inline.md", []byte("# "+largeInline+"\n"))
	if inlineErr != nil {
		t.Fatal(inlineErr)
	}
	if inline.Outline.Headings[0].Text != largeInline {
		t.Fatal("large inline input should retain normalized source without AST parsing")
	}
}
