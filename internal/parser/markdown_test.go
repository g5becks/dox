package parser_test

import (
	"testing"

	"github.com/g5becks/dox/internal/parser"
)

func TestMarkdownParser_CanParse(t *testing.T) {
	p := parser.NewMarkdownParser()

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"markdown file", "README.md", true},
		{"mdx file", "component.mdx", false},
		{"text file", "notes.txt", false},
		{"uppercase", "DOC.MD", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.CanParse(tt.path); got != tt.want {
				t.Errorf("CanParse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMarkdownParser_Parse(t *testing.T) {
	p := parser.NewMarkdownParser()

	tests := []struct {
		name         string
		content      string
		wantDesc     string
		wantHeadings int
	}{
		{
			name: "ATX headings",
			content: `# Main Title
## Section 1
### Subsection
## Section 2`,
			wantDesc:     "Main Title",
			wantHeadings: 4,
		},
		{
			name: "with frontmatter title and description",
			content: `---
title: My Document
description: A test document
---
# Content`,
			wantDesc:     "My Document - A test document",
			wantHeadings: 1,
		},
		{
			name: "with frontmatter title only",
			content: `---
title: Just Title
---
# Heading`,
			wantDesc:     "Just Title",
			wantHeadings: 1,
		},
		{
			name: "H1 with paragraph",
			content: `# Introduction
This is the first paragraph explaining the document.

More content here.`,
			wantDesc:     "Introduction - This is the first paragraph explaining the document.",
			wantHeadings: 1,
		},
		{
			name: "no headings only paragraph",
			content: `This is a document without any headings.
Just plain text.`,
			wantDesc:     "This is a document without any headings. Just plain text.",
			wantHeadings: 0,
		},
		{
			name:         "empty file",
			content:      "",
			wantDesc:     "",
			wantHeadings: 0,
		},
		{
			name:         "code blocks ignored",
			content:      "# Real Heading\n```\n# Fake Heading\n```",
			wantDesc:     "Real Heading",
			wantHeadings: 1,
		},
		{
			name: "multiple H1 use first",
			content: `# First Title
Content here.
# Second Title`,
			wantDesc:     "First Title - Content here.",
			wantHeadings: 2,
		},
		{
			name: "deep hierarchy",
			content: `# H1
## H2
### H3
#### H4
##### H5
###### H6`,
			wantDesc:     "H1",
			wantHeadings: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.Parse("test.md", []byte(tt.content))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}

			if result.Description != tt.wantDesc {
				t.Errorf("Description = %q, want %q", result.Description, tt.wantDesc)
			}

			if len(result.Outline.Headings) != tt.wantHeadings {
				t.Errorf("Headings count = %d, want %d", len(result.Outline.Headings), tt.wantHeadings)
			}

			if result.Outline.Type != parser.OutlineTypeHeadings {
				t.Errorf("Outline type = %q, want %q", result.Outline.Type, parser.OutlineTypeHeadings)
			}
		})
	}
}

func TestMarkdownParser_ParseHeadingLevels(t *testing.T) {
	p := parser.NewMarkdownParser()
	content := `# Level 1
## Level 2
### Level 3`

	result, err := p.Parse("test.md", []byte(content))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	expectedLevels := []int{1, 2, 3}
	expectedTexts := []string{"Level 1", "Level 2", "Level 3"}
	expectedLines := []int{1, 2, 3}

	if len(result.Outline.Headings) != len(expectedLevels) {
		t.Fatalf("Headings count = %d, want %d", len(result.Outline.Headings), len(expectedLevels))
	}

	for i, heading := range result.Outline.Headings {
		if heading.Level != expectedLevels[i] {
			t.Errorf("Heading[%d].Level = %d, want %d", i, heading.Level, expectedLevels[i])
		}
		if heading.Text != expectedTexts[i] {
			t.Errorf("Heading[%d].Text = %q, want %q", i, heading.Text, expectedTexts[i])
		}
		if heading.Line != expectedLines[i] {
			t.Errorf("Heading[%d].Line = %d, want %d", i, heading.Line, expectedLines[i])
		}
	}
}

func TestMarkdownParser_HeadingLineNumbers(t *testing.T) {
	t.Parallel()

	p := parser.NewMarkdownParser()

	tests := []struct {
		name      string
		content   string
		wantLines []int
	}{
		{
			name:      "ATX headings with blank lines",
			content:   "# Title\n\nSome text.\n\n## Section\n\n### Sub",
			wantLines: []int{1, 5, 7},
		},
		{
			name:      "frontmatter offsets line numbers",
			content:   "---\ntitle: Test\n---\n\n## Query Basics\n\n### Details",
			wantLines: []int{5, 7},
		},
		{
			name:      "setext headings",
			content:   "Title\n=====\n\nSection\n------\n\n### ATX",
			wantLines: []int{1, 4, 7},
		},
		{
			name:      "headings inside code blocks ignored",
			content:   "# Real\n\n```\n# Fake\n```\n\n## Also Real",
			wantLines: []int{1, 7},
		},
		{
			name:      "frontmatter dashes not matched as setext",
			content:   "---\ntitle: Queries\n---\n\n## Section",
			wantLines: []int{5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := p.Parse("test.md", []byte(tt.content))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}

			if len(result.Outline.Headings) != len(tt.wantLines) {
				t.Fatalf("Headings count = %d, want %d", len(result.Outline.Headings), len(tt.wantLines))
			}

			for i, heading := range result.Outline.Headings {
				if heading.Line != tt.wantLines[i] {
					t.Errorf("Heading[%d] %q: Line = %d, want %d", i, heading.Text, heading.Line, tt.wantLines[i])
				}
			}
		})
	}
}
