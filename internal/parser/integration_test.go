package parser_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/g5becks/dox/internal/parser"
)

func TestCrossParserIntegration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		file        string
		wantOutline parser.OutlineType
		wantDesc    bool
	}{
		{
			name:        "markdown file",
			file:        "../../testdata/sample.md",
			wantOutline: parser.OutlineTypeHeadings,
			wantDesc:    true,
		},
		{
			name:        "mdx file",
			file:        "../../testdata/sample.mdx",
			wantOutline: parser.OutlineTypeHeadings,
			wantDesc:    true,
		},
		{
			name:        "text file",
			file:        "../../testdata/sample.txt",
			wantOutline: parser.OutlineTypeNone,
			wantDesc:    true,
		},
		{
			name:        "tsx doc component",
			file:        "../../testdata/doc-component.tsx",
			wantOutline: parser.OutlineTypeHeadings,
			wantDesc:    true,
		},
		{
			name:        "tsx code component",
			file:        "../../testdata/code-component.tsx",
			wantOutline: parser.OutlineTypeExports,
			wantDesc:    true,
		},
	}

	parsers := []parser.Parser{
		parser.NewMarkdownParser(),
		parser.NewMDXParser(),
		parser.NewTextParser(),
		parser.NewTypeScriptParser(),
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			content, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatalf("failed to read test file: %v", err)
			}

			foundParser := parserForFile(t, parsers, tt.file)

			result, err := foundParser.Parse(tt.file, content)
			if err != nil {
				t.Fatalf("parse failed: %v", err)
			}

			if result.Outline != nil && result.Outline.Type != tt.wantOutline {
				t.Errorf("got outline type %q, want %q", result.Outline.Type, tt.wantOutline)
			}

			if tt.wantDesc && result.Description == "" {
				t.Error("expected description, got empty string")
			}
		})
	}
}

func TestAllParsersWithEmptyFiles(t *testing.T) {
	t.Parallel()

	parsers := []struct {
		name   string
		parser parser.Parser
		path   string
	}{
		{"markdown", parser.NewMarkdownParser(), "empty.md"},
		{"mdx", parser.NewMDXParser(), "empty.mdx"},
		{"text", parser.NewTextParser(), "empty.txt"},
		{"typescript", parser.NewTypeScriptParser(), "empty.tsx"},
	}

	for _, tt := range parsers {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := tt.parser.Parse(tt.path, []byte{})
			if err != nil {
				t.Fatalf("parse failed: %v", err)
			}

			if result == nil {
				t.Fatal("expected result, got nil")
			}
		})
	}
}

func TestMarkdownVsMDXSimilarity(t *testing.T) {
	t.Parallel()

	content := `# Test Heading

This is test content.

## Subheading

More content here.
`

	mdParser := parser.NewMarkdownParser()
	mdxParser := parser.NewMDXParser()

	mdResult, err := mdParser.Parse("test.md", []byte(content))
	if err != nil {
		t.Fatalf("markdown parse failed: %v", err)
	}

	mdxResult, err := mdxParser.Parse("test.mdx", []byte(content))
	if err != nil {
		t.Fatalf("mdx parse failed: %v", err)
	}

	if mdResult.Outline == nil || mdxResult.Outline == nil {
		t.Fatal("expected outlines, got nil")
	}

	if len(mdResult.Outline.Headings) != len(mdxResult.Outline.Headings) {
		t.Errorf("heading count mismatch: md=%d, mdx=%d",
			len(mdResult.Outline.Headings), len(mdxResult.Outline.Headings))
	}
}

func TestTSXDocVsCodeComponent(t *testing.T) {
	t.Parallel()

	docContent, err := os.ReadFile("../../testdata/doc-component.tsx")
	if err != nil {
		t.Fatalf("failed to read doc component: %v", err)
	}

	codeContent, err := os.ReadFile("../../testdata/code-component.tsx")
	if err != nil {
		t.Fatalf("failed to read code component: %v", err)
	}

	tsParser := parser.NewTypeScriptParser()

	docResult, err := tsParser.Parse("doc.tsx", docContent)
	if err != nil {
		t.Fatalf("doc parse failed: %v", err)
	}

	codeResult, err := tsParser.Parse("code.tsx", codeContent)
	if err != nil {
		t.Fatalf("code parse failed: %v", err)
	}

	// Doc component should have headings
	if docResult.Outline == nil || len(docResult.Outline.Headings) == 0 {
		t.Error("expected headings in doc component")
	}

	// Code component should have exports
	if codeResult.Outline == nil || len(codeResult.Outline.Exports) == 0 {
		t.Error("expected exports in code component")
	}
}

func TestParserRoundTrip(t *testing.T) {
	t.Parallel()

	testFiles := []string{
		"../../testdata/sample.md",
		"../../testdata/sample.mdx",
		"../../testdata/sample.txt",
		"../../testdata/doc-component.tsx",
		"../../testdata/code-component.tsx",
	}

	parsers := []parser.Parser{
		parser.NewMarkdownParser(),
		parser.NewMDXParser(),
		parser.NewTextParser(),
		parser.NewTypeScriptParser(),
	}

	for _, file := range testFiles {
		t.Run(filepath.Base(file), func(t *testing.T) {
			t.Parallel()

			content, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("failed to read file: %v", err)
			}

			p := parserForFile(t, parsers, file)

			// Parse twice, results should be identical
			result1, err := p.Parse(file, content)
			if err != nil {
				t.Fatalf("first parse failed: %v", err)
			}

			result2, err := p.Parse(file, content)
			if err != nil {
				t.Fatalf("second parse failed: %v", err)
			}

			t.Run("identical results", func(t *testing.T) {
				if !reflect.DeepEqual(result1, result2) {
					t.Errorf("parse results differ: first=%+v, second=%+v", result1, result2)
				}
			})
		})
	}
}

func parserForFile(t *testing.T, parsers []parser.Parser, file string) parser.Parser {
	t.Helper()
	for _, candidate := range parsers {
		if candidate.CanParse(file) {
			return candidate
		}
	}
	t.Fatalf("no parser found for %s", file)
	return nil
}
