package ui_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/g5becks/dox/internal/ui"
)

func TestRenderSourceListJSON(t *testing.T) {
	sources := []ui.SourceStatus{
		{
			Name:      "test-source",
			Type:      "github",
			Repo:      "owner/repo",
			Path:      "docs",
			Ref:       "main",
			Patterns:  []string{"*.md", "*.txt"},
			OutputDir: "/tmp/output",
			Status:    "synced",
			FileCount: 5,
		},
		{
			Name:      "url-source",
			Type:      "url",
			URL:       "https://example.com/doc.pdf",
			OutputDir: "/tmp/output",
			Status:    "pending",
		},
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w //nolint:reassign // Test helper to capture stdout

	err := ui.RenderSourceList(sources, ui.ListOptions{JSON: true})
	w.Close()

	var buf bytes.Buffer
	io.Copy(&buf, r)
	os.Stdout = oldStdout //nolint:reassign // Restore stdout after test

	if err != nil {
		t.Fatalf("RenderSourceList(JSON=true) error = %v", err)
	}

	var decoded []ui.SourceStatus
	if unmarshalErr := json.Unmarshal(buf.Bytes(), &decoded); unmarshalErr != nil {
		t.Fatalf("JSON unmarshal error = %v, output:\n%s", unmarshalErr, buf.String())
	}

	if len(decoded) != 2 {
		t.Errorf("decoded JSON has %d sources, want 2", len(decoded))
	}

	if decoded[0].Name != "test-source" {
		t.Errorf("decoded[0].Name = %q, want %q", decoded[0].Name, "test-source")
	}

	if decoded[0].Type != "github" {
		t.Errorf("decoded[0].Type = %q, want %q", decoded[0].Type, "github")
	}

	if decoded[1].Type != "url" {
		t.Errorf("decoded[1].Type = %q, want %q", decoded[1].Type, "url")
	}
}

func TestRenderLocation(t *testing.T) {
	tests := []struct {
		name   string
		source ui.SourceStatus
		want   string
	}{
		{
			name: "url source shows URL",
			source: ui.SourceStatus{
				Type: "url",
				URL:  "https://example.com/doc.pdf",
			},
			want: "https://example.com/doc.pdf",
		},
		{
			name: "github source with path shows repo/path",
			source: ui.SourceStatus{
				Type: "github",
				Repo: "owner/repo",
				Path: "docs",
			},
			want: "owner/repo/docs",
		},
		{
			name: "github source without path shows repo",
			source: ui.SourceStatus{
				Type: "github",
				Repo: "owner/repo",
				Path: "",
			},
			want: "owner/repo",
		},
		{
			name: "strips leading slash",
			source: ui.SourceStatus{
				Type: "github",
				Repo: "/owner/repo",
				Path: "docs",
			},
			want: "owner/repo/docs",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ui.RenderLocation(tc.source)
			if got != tc.want {
				t.Errorf("RenderLocation() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRenderStatus(t *testing.T) {
	tests := []struct {
		name         string
		source       ui.SourceStatus
		includeFiles bool
		want         string
	}{
		{
			name: "status without file count",
			source: ui.SourceStatus{
				Status:    "synced",
				FileCount: 5,
			},
			includeFiles: false,
			want:         "synced",
		},
		{
			name: "status with file count",
			source: ui.SourceStatus{
				Status:    "synced",
				FileCount: 5,
			},
			includeFiles: true,
			want:         "synced (5 files)",
		},
		{
			name: "status with zero file count not included",
			source: ui.SourceStatus{
				Status:    "pending",
				FileCount: 0,
			},
			includeFiles: true,
			want:         "pending",
		},
		{
			name: "status without includeFiles flag ignores count",
			source: ui.SourceStatus{
				Status:    "error",
				FileCount: 10,
			},
			includeFiles: false,
			want:         "error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ui.RenderStatus(tc.source, tc.includeFiles)
			if got != tc.want {
				t.Errorf("RenderStatus() = %q, want %q", got, tc.want)
			}
		})
	}
}
