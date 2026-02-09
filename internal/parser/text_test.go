package parser_test

import (
	"strings"
	"testing"

	"github.com/g5becks/dox/internal/parser"
)

func TestTextParser_CanParse(t *testing.T) {
	p := parser.NewTextParser()

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"text file", "notes.txt", true},
		{"markdown file", "README.md", false},
		{"uppercase", "NOTES.TXT", true},
		{"unknown", "file.go", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.CanParse(tt.path); got != tt.want {
				t.Errorf("CanParse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTextParser_Parse(t *testing.T) {
	p := parser.NewTextParser()

	tests := []struct {
		name      string
		content   string
		wantDesc  string
		wantLines int
	}{
		{
			name:      "plain text",
			content:   "This is the first line.\nSecond line here.",
			wantDesc:  "This is the first line.",
			wantLines: 2,
		},
		{
			name:      "empty file",
			content:   "",
			wantDesc:  "",
			wantLines: 1,
		},
		{
			name:      "leading blank lines",
			content:   "\n\n\nFirst non-empty line.\nMore content.",
			wantDesc:  "First non-empty line.",
			wantLines: 5,
		},
		{
			name:      "only whitespace",
			content:   "   \n\t\n  \n",
			wantDesc:  "",
			wantLines: 4,
		},
		{
			name:      "very long first line",
			content:   strings.Repeat("a", 500),
			wantDesc:  strings.Repeat("a", 500),
			wantLines: 1,
		},
		{
			name:      "single line no newline",
			content:   "Single line content",
			wantDesc:  "Single line content",
			wantLines: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.Parse("test.txt", []byte(tt.content))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}

			if result.Description != tt.wantDesc {
				t.Errorf("Description = %q, want %q", result.Description, tt.wantDesc)
			}

			if result.Lines != tt.wantLines {
				t.Errorf("Lines = %d, want %d", result.Lines, tt.wantLines)
			}

			if result.Outline.Type != parser.OutlineTypeNone {
				t.Errorf("Outline type = %q, want %q", result.Outline.Type, parser.OutlineTypeNone)
			}

			if len(result.Outline.Headings) != 0 {
				t.Errorf("Headings count = %d, want 0", len(result.Outline.Headings))
			}

			if len(result.Outline.Exports) != 0 {
				t.Errorf("Exports count = %d, want 0", len(result.Outline.Exports))
			}
		})
	}
}
