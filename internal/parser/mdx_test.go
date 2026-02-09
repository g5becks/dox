package parser_test

import (
	"testing"

	"github.com/g5becks/dox/internal/parser"
)

func TestMDXParser_CanParse(t *testing.T) {
	p := parser.NewMDXParser()

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"mdx file", "component.mdx", true},
		{"markdown file", "README.md", false},
		{"uppercase", "COMPONENT.MDX", true},
		{"text file", "notes.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.CanParse(tt.path); got != tt.want {
				t.Errorf("CanParse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMDXParser_Parse(t *testing.T) {
	p := parser.NewMDXParser()

	tests := []struct {
		name         string
		content      string
		wantDesc     string
		wantHeadings int
	}{
		{
			name: "with frontmatter title and description",
			content: `---
title: My Component
description: A React component
---
# Component Details`,
			wantDesc:     "My Component - A React component",
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
			name: "without frontmatter",
			content: `# Main Heading
## Subheading`,
			wantDesc:     "Main Heading",
			wantHeadings: 2,
		},
		{
			name: "with import statements",
			content: `import React from 'react'
import { Button } from './components'

# Documentation
Content here.`,
			wantDesc:     "Documentation - Content here.",
			wantHeadings: 1,
		},
		{
			name: "with export metadata",
			content: `export const metadata = { title: 'Test' }

# Real Heading
Content.`,
			wantDesc:     "Real Heading - Content.",
			wantHeadings: 1,
		},
		{
			name: "with JSX components",
			content: `# Introduction

Some text here.

<Button>Click me</Button>

## Features`,
			wantDesc:     "Introduction - Some text here.",
			wantHeadings: 2,
		},
		{
			name: "with multi-line import",
			content: `import {
  Component,
  Other
} from './components'

# Title
Content.`,
			wantDesc:     "Title - Content.",
			wantHeadings: 1,
		},
		{
			name: "with multi-line export",
			content: `export const meta = {
  title: 'Test'
}

# Title
Content.`,
			wantDesc:     "Title - Content.",
			wantHeadings: 1,
		},
		{
			name:         "empty file",
			content:      "",
			wantDesc:     "",
			wantHeadings: 0,
		},
		{
			name: "complex MDX",
			content: `---
title: Complex Example
description: Full MDX document
---
import { Card } from '@/components'
export const config = { runtime: 'edge' }

# Main Title
Some content here.

## Section 1
<Card>Content</Card>

### Subsection`,
			wantDesc:     "Complex Example - Full MDX document",
			wantHeadings: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.Parse("test.mdx", []byte(tt.content))
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
