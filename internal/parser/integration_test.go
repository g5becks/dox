package parser_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/g5becks/dox/internal/parser"
)

func TestCrossParserIntegration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		file         string
		wantOutline  parser.OutlineType
		wantDesc     bool
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
		&parser.MarkdownParser{},
		&parser.MDXParser{},
		&parser.TextParser{},
		&parser.TypeScriptParser{},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			content, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatalf("failed to read test file: %v", err)
			}

			var foundParser parser.Parser
			for _, p := range parsers {
				if p.CanParse(tt.file) {
					foundParser = p
					break
				}
			}

			if foundParser == nil {
				t.Fatal("no parser found for file")
			}

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
		{"markdown", &parser.MarkdownParser{}, "empty.md"},
		{"mdx", &parser.MDXParser{}, "empty.mdx"},
		{"text", &parser.TextParser{}, "empty.txt"},
		{"typescript", &parser.TypeScriptParser{}, "empty.tsx"},
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

	mdParser := &parser.MarkdownParser{}
	mdxParser := &parser.MDXParser{}

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

	tsParser := &parser.TypeScriptParser{}

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
		&parser.MarkdownParser{},
		&parser.MDXParser{},
		&parser.TextParser{},
		&parser.TypeScriptParser{},
	}

	for _, file := range testFiles {
		t.Run(filepath.Base(file), func(t *testing.T) {
			t.Parallel()

			content, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("failed to read file: %v", err)
			}

			var p parser.Parser
			for _, candidate := range parsers {
				if candidate.CanParse(file) {
					p = candidate
					break
				}
			}

			if p == nil {
				t.Fatal("no parser found")
			}

			// Parse twice, results should be identical
			result1, err := p.Parse(file, content)
			if err != nil {
				t.Fatalf("first parse failed: %v", err)
			}

			result2, err := p.Parse(file, content)
			if err != nil {
				t.Fatalf("second parse failed: %v", err)
			}

			if result1.Description != result2.Description {
				t.Errorf("description mismatch")
			}

			if result1.Lines != result2.Lines {
				t.Errorf("lines mismatch: %d vs %d", result1.Lines, result2.Lines)
			}

			// Compare outlines
			if (result1.Outline == nil) != (result2.Outline == nil) {
				t.Error("outline presence mismatch")
			}

			if result1.Outline != nil && result2.Outline != nil {
				if len(result1.Outline.Headings) != len(result2.Outline.Headings) {
					t.Errorf("headings count mismatch: %d vs %d",
						len(result1.Outline.Headings), len(result2.Outline.Headings))
				}

				if len(result1.Outline.Exports) != len(result2.Outline.Exports) {
					t.Errorf("exports count mismatch: %d vs %d",
						len(result1.Outline.Exports), len(result2.Outline.Exports))
				}
			}
		})
	}
}
