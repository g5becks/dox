package parser_test

import (
	"testing"

	"github.com/g5becks/dox/internal/parser"
)

func TestTypeScriptParser_CanParse(t *testing.T) {
	p := parser.NewTypeScriptParser()

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"tsx file", "Component.tsx", true},
		{"ts file", "utils.ts", true},
		{"markdown file", "README.md", false},
		{"uppercase", "COMPONENT.TSX", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.CanParse(tt.path); got != tt.want {
				t.Errorf("CanParse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTypeScriptParser_Parse(t *testing.T) {
	p := parser.NewTypeScriptParser()

	tests := []struct {
		name              string
		content           string
		wantComponentType parser.ComponentType
		wantOutlineType   parser.OutlineType
		wantHeadings      int
		wantExports       int
	}{
		{
			name: "TSX documentation component",
			content: `export default function Docs() {
  return (
    <div>
      <h1>Installation Guide</h1>
      <p>Welcome to the guide</p>
      <h2>Prerequisites</h2>
      <p>You need Node.js</p>
    </div>
  )
}`,
			wantComponentType: parser.ComponentTypeDocumentation,
			wantOutlineType:   parser.OutlineTypeHeadings,
			wantHeadings:      2,
			wantExports:       0,
		},
		{
			name: "TSX code component",
			content: `export interface ButtonProps {
  label: string
}

export const Button = ({ label }: ButtonProps) => {
  return <button>{label}</button>
}`,
			wantComponentType: parser.ComponentTypeCode,
			wantOutlineType:   parser.OutlineTypeExports,
			wantHeadings:      0,
			wantExports:       2,
		},
		{
			name: "TSX with JSDoc",
			content: `/**
 * A reusable button component
 * @param props Button properties
 */
export const Button = (props) => {
  return <button>{props.label}</button>
}`,
			wantComponentType: parser.ComponentTypeCode,
			wantOutlineType:   parser.OutlineTypeExports,
			wantHeadings:      0,
			wantExports:       1,
		},
		{
			name: "TSX with nested tags in headings",
			content: `export default function Docs() {
  return (
    <div>
      <h1><code>npm install</code> Command</h1>
      <h2>Usage <strong>Guide</strong></h2>
    </div>
  )
}`,
			wantComponentType: parser.ComponentTypeDocumentation,
			wantOutlineType:   parser.OutlineTypeHeadings,
			wantHeadings:      2,
			wantExports:       0,
		},
		{
			name: "TSX with only imports",
			content: `import React from 'react'
import { useState } from 'react'

const Component = () => <div>Hello</div>`,
			wantComponentType: parser.ComponentTypeCode,
			wantOutlineType:   parser.OutlineTypeExports,
			wantHeadings:      0,
			wantExports:       0,
		},
		{
			name: "TS file with exports",
			content: `export function add(a: number, b: number): number {
  return a + b
}

export type Result = number`,
			wantComponentType: parser.ComponentTypeCode,
			wantOutlineType:   parser.OutlineTypeExports,
			wantHeadings:      0,
			wantExports:       2,
		},
		{
			name: "TSX with exactly 1 heading",
			content: `export default function Page() {
  return <h1>Single Heading</h1>
}`,
			wantComponentType: parser.ComponentTypeCode,
			wantOutlineType:   parser.OutlineTypeExports,
			wantHeadings:      0,
			wantExports:       0,
		},
		{
			name: "TSX with exactly 2 headings",
			content: `export default function Docs() {
  return (
    <>
      <h1>Title</h1>
      <h2>Subtitle</h2>
    </>
  )
}`,
			wantComponentType: parser.ComponentTypeDocumentation,
			wantOutlineType:   parser.OutlineTypeHeadings,
			wantHeadings:      2,
			wantExports:       0,
		},
		{
			name:              "empty file",
			content:           "",
			wantComponentType: parser.ComponentTypeCode,
			wantOutlineType:   parser.OutlineTypeExports,
			wantHeadings:      0,
			wantExports:       0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := p.Parse("test.tsx", []byte(tt.content))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}

			if result.ComponentType != tt.wantComponentType {
				t.Errorf("ComponentType = %v, want %v", result.ComponentType, tt.wantComponentType)
			}

			if result.Outline.Type != tt.wantOutlineType {
				t.Errorf("Outline.Type = %v, want %v", result.Outline.Type, tt.wantOutlineType)
			}

			if len(result.Outline.Headings) != tt.wantHeadings {
				t.Errorf("Headings count = %d, want %d", len(result.Outline.Headings), tt.wantHeadings)
			}

			if len(result.Outline.Exports) != tt.wantExports {
				t.Errorf("Exports count = %d, want %d", len(result.Outline.Exports), tt.wantExports)
			}
		})
	}
}

func TestTypeScriptParser_HeadingExtraction(t *testing.T) {
	p := parser.NewTypeScriptParser()
	content := `export default function Docs() {
  return (
    <div>
      <h1>Main Title</h1>
      <h2>Section One</h2>
      <h3>Subsection</h3>
    </div>
  )
}`

	result, err := p.Parse("test.tsx", []byte(content))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	expectedLevels := []int{1, 2, 3}
	expectedTexts := []string{"Main Title", "Section One", "Subsection"}

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
	}
}


func TestTypeScriptParser_DuplicateHeadingLineNumbers(t *testing.T) {
	t.Parallel()

	p := parser.NewTypeScriptParser()
	content := "<h1>Title</h1>\n<p>text</p>\n<h2>Section</h2>\n<p>text</p>\n<h2>Section</h2>"

	result, err := p.Parse("test.tsx", []byte(content))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(result.Outline.Headings) != 3 {
		t.Fatalf("expected 3 headings, got %d", len(result.Outline.Headings))
	}

	// The two "Section" headings should have different line numbers
	h2 := result.Outline.Headings[1]
	h3 := result.Outline.Headings[2]
	if h2.Line == h3.Line {
		t.Errorf("duplicate headings should have different line numbers, both got line %d", h2.Line)
	}
	if h3.Line != 5 {
		t.Errorf("third heading line = %d, want 5", h3.Line)
	}
}

func TestTypeScriptParser_DuplicateExportLineNumbers(t *testing.T) {
	t.Parallel()

	p := parser.NewTypeScriptParser()
	content := "export const foo = 1\nexport const bar = 2\nexport const foo = 3"

	result, err := p.Parse("test.ts", []byte(content))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Should have 3 exports with different line numbers
	if len(result.Outline.Exports) != 3 {
		t.Fatalf("expected 3 exports, got %d", len(result.Outline.Exports))
	}

	if result.Outline.Exports[2].Line != 3 {
		t.Errorf("third export line = %d, want 3", result.Outline.Exports[2].Line)
	}
}
