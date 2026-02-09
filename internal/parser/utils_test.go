package parser_test

import (
	"reflect"
	"testing"

	"github.com/g5becks/dox/internal/parser"
)

func TestIsBinary(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		want    bool
	}{
		{
			name:    "null byte in content",
			content: []byte("hello\x00world"),
			want:    true,
		},
		{
			name:    "valid text",
			content: []byte("hello world"),
			want:    false,
		},
		{
			name:    "empty content",
			content: []byte{},
			want:    false,
		},
		{
			name:    "null byte at start",
			content: []byte("\x00hello"),
			want:    true,
		},
		{
			name: "null byte beyond 512 bytes",
			content: func() []byte {
				b := make([]byte, 513)
				for i := range b {
					b[i] = 'a' // Fill with non-null bytes
				}
				b[512] = 0 // Null byte at position 512 (beyond first 512 bytes checked)
				return b
			}(),
			want: false,
		},
		{
			name:    "null byte within 512 bytes",
			content: append(make([]byte, 256), 0),
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parser.IsBinary(tt.content); got != tt.want {
				t.Errorf("IsBinary() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsValidUTF8(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		want    bool
	}{
		{
			name:    "valid utf8",
			content: []byte("hello world"),
			want:    true,
		},
		{
			name:    "valid utf8 with unicode",
			content: []byte("hello 世界"),
			want:    true,
		},
		{
			name:    "invalid utf8",
			content: []byte{0xff, 0xfe, 0xfd},
			want:    false,
		},
		{
			name:    "empty content",
			content: []byte{},
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parser.IsValidUTF8(tt.content); got != tt.want {
				t.Errorf("IsValidUTF8() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStripBOM(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		want    []byte
	}{
		{
			name:    "with BOM",
			content: []byte{0xEF, 0xBB, 0xBF, 'h', 'e', 'l', 'l', 'o'},
			want:    []byte("hello"),
		},
		{
			name:    "without BOM",
			content: []byte("hello"),
			want:    []byte("hello"),
		},
		{
			name:    "empty content",
			content: []byte{},
			want:    []byte{},
		},
		{
			name:    "partial BOM",
			content: []byte{0xEF, 0xBB},
			want:    []byte{0xEF, 0xBB},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.StripBOM(tt.content)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("StripBOM() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetectFileType(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "markdown file",
			path: "README.md",
			want: "md",
		},
		{
			name: "mdx file",
			path: "component.mdx",
			want: "mdx",
		},
		{
			name: "text file",
			path: "notes.txt",
			want: "txt",
		},
		{
			name: "tsx file",
			path: "Component.tsx",
			want: "tsx",
		},
		{
			name: "ts file",
			path: "utils.ts",
			want: "ts",
		},
		{
			name: "unknown file",
			path: "file.go",
			want: "unknown",
		},
		{
			name: "uppercase extension",
			path: "README.MD",
			want: "md",
		},
		{
			name: "path with directory",
			path: "docs/guide.md",
			want: "md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parser.DetectFileType(tt.path); got != tt.want {
				t.Errorf("DetectFileType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStripFrontmatter(t *testing.T) {
	tests := []struct {
		name      string
		content   []byte
		wantBody  []byte
		wantTitle string
		wantDesc  string
	}{
		{
			name: "with title and description",
			content: []byte(`---
title: My Document
description: This is a test
---
# Content here`),
			wantBody:  []byte("# Content here"),
			wantTitle: "My Document",
			wantDesc:  "This is a test",
		},
		{
			name: "title only",
			content: []byte(`---
title: "Quoted Title"
---
Body content`),
			wantBody:  []byte("Body content"),
			wantTitle: "Quoted Title",
			wantDesc:  "",
		},
		{
			name:      "no frontmatter",
			content:   []byte("# Just content"),
			wantBody:  []byte("# Just content"),
			wantTitle: "",
			wantDesc:  "",
		},
		{
			name:      "empty content",
			content:   []byte{},
			wantBody:  []byte{},
			wantTitle: "",
			wantDesc:  "",
		},
		{
			name: "frontmatter with extra fields",
			content: []byte(`---
title: Test
author: John
description: A description
date: 2024-01-01
---
Content`),
			wantBody:  []byte("Content"),
			wantTitle: "Test",
			wantDesc:  "A description",
		},
		{
			name:      "windows line endings",
			content:   []byte("---\r\ntitle: Test\r\ndescription: Desc\r\n---\r\nBody"),
			wantBody:  []byte("Body"),
			wantTitle: "Test",
			wantDesc:  "Desc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBody, gotTitle, gotDesc := parser.StripFrontmatter(tt.content)
			if !reflect.DeepEqual(gotBody, tt.wantBody) {
				t.Errorf("StripFrontmatter() body = %q, want %q", gotBody, tt.wantBody)
			}
			if gotTitle != tt.wantTitle {
				t.Errorf("StripFrontmatter() title = %q, want %q", gotTitle, tt.wantTitle)
			}
			if gotDesc != tt.wantDesc {
				t.Errorf("StripFrontmatter() description = %q, want %q", gotDesc, tt.wantDesc)
			}
		})
	}
}
