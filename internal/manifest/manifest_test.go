package manifest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/g5becks/dox/internal/manifest"
)

func TestNew(t *testing.T) {
	m := manifest.New()

	if m.Version != manifest.CurrentVersion {
		t.Errorf("Version = %q, want %q", m.Version, manifest.CurrentVersion)
	}

	if m.Collections == nil {
		t.Error("Collections should be initialized")
	}

	if m.Generated.IsZero() {
		t.Error("Generated time should be set")
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()

	original := manifest.New()
	original.Collections["test"] = &manifest.Collection{
		Name:      "test",
		Type:      "github",
		Source:    "owner/repo",
		Path:      "docs",
		LastSync:  time.Now().Truncate(time.Second),
		FileCount: 5,
		TotalSize: 1024,
		Files: []manifest.FileInfo{
			{
				Path:        "README.md",
				Type:        "md",
				Size:        512,
				Lines:       20,
				Modified:    time.Now().Truncate(time.Second),
				Description: "Test file",
			},
		},
	}

	if err := original.Save(dir); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := manifest.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.Version != original.Version {
		t.Errorf("Version = %q, want %q", loaded.Version, original.Version)
	}

	if len(loaded.Collections) != len(original.Collections) {
		t.Errorf(
			"Collections count = %d, want %d",
			len(loaded.Collections),
			len(original.Collections),
		)
	}

	coll := loaded.Collections["test"]
	if coll == nil {
		t.Fatal("Collection 'test' not found")
	}

	if coll.Name != "test" {
		t.Errorf("Collection name = %q, want 'test'", coll.Name)
	}

	if len(coll.Files) != 1 {
		t.Errorf("Files count = %d, want 1", len(coll.Files))
	}
}

func TestLoadNonExistent(t *testing.T) {
	dir := t.TempDir()

	_, err := manifest.Load(dir)
	if err == nil {
		t.Fatal("Load() should return error for non-existent manifest")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "manifest not found") {
		t.Errorf("Error should mention manifest not found, got: %v", err)
	}
}

func TestLoadCorrupted(t *testing.T) {
	dir := t.TempDir()
	manifestPath := manifest.ManifestPath(dir)

	if err := os.WriteFile(manifestPath, []byte("invalid json{"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := manifest.Load(dir)
	if err == nil {
		t.Fatal("Load() should return error for corrupted manifest")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "parsing manifest") {
		t.Errorf("Error should mention parsing manifest, got: %v", err)
	}
}

func TestSaveCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "nested", "path")

	m := manifest.New()
	if err := m.Save(subdir); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	manifestPath := manifest.ManifestPath(subdir)
	if _, err := os.Stat(manifestPath); err != nil {
		t.Errorf("Manifest file should exist at %q", manifestPath)
	}
}

func TestSaveNilManifest(t *testing.T) {
	dir := t.TempDir()

	var m *manifest.Manifest
	err := m.Save(dir)
	if err == nil {
		t.Fatal("Save() should return error for nil manifest")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "cannot save nil manifest") {
		t.Errorf("Error should mention nil manifest, got: %v", err)
	}
}

func TestAtomicWrite(t *testing.T) {
	dir := t.TempDir()

	m := manifest.New()
	if err := m.Save(dir); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Check no temp files left behind
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp") {
			t.Errorf("Temp file should be cleaned up: %s", entry.Name())
		}
	}

	// Verify manifest.json exists
	manifestPath := manifest.ManifestPath(dir)
	if _, statErr := os.Stat(manifestPath); statErr != nil {
		t.Errorf("Manifest file should exist at %q", manifestPath)
	}
}
