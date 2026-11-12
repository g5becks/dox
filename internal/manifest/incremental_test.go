package manifest_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"strings"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/lockfile"
	"github.com/g5becks/dox/internal/manifest"
)

func setupIncremental(t *testing.T) (*config.Config, string) {
	t.Helper()
	project := t.TempDir()
	output := filepath.Join(project, "docs")
	if err := os.MkdirAll(filepath.Join(output, "sample"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(output, "sample", "guide.md"),
		[]byte("# Original\n\nBody.\n"),
		0600,
	); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		ConfigDir: project,
		Output:    output,
		Sources:   map[string]config.Source{"sample": {Type: "url", URL: "https://example.invalid/guide.md"}},
	}
	return cfg, output
}

func TestFullManifestDeterminism(t *testing.T) {
	t.Parallel()
	cfg, output := setupIncremental(t)
	addDeterminismSources(t, cfg, output)
	if generateErr := manifest.Generate(t.Context(), cfg, nil); generateErr != nil {
		t.Fatal(generateErr)
	}
	first, err := os.ReadFile(manifest.Path(output))
	if err != nil {
		t.Fatal(err)
	}
	if removeErr := os.Remove(manifest.Path(output)); removeErr != nil {
		t.Fatal(removeErr)
	}
	if generateErr := manifest.Generate(t.Context(), cfg, nil); generateErr != nil {
		t.Fatal(generateErr)
	}
	second, err := os.ReadFile(manifest.Path(output))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("full generations differ:\n%s\n%s", first, second)
	}
}

func TestManifestCancelledBeforePublish(t *testing.T) {
	t.Parallel()
	cfg, output := setupIncremental(t)
	before := []byte("previous manifest must survive\n")
	if err := os.WriteFile(manifest.Path(output), before, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := manifest.Generate(ctx, cfg, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
	after, err := os.ReadFile(manifest.Path(output))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("cancelled run replaced the previous manifest")
	}
}

func TestNoopManifestDoesNotRewrite(t *testing.T) {
	t.Parallel()
	cfg, output := setupIncremental(t)
	if generateErr := manifest.Generate(t.Context(), cfg, nil); generateErr != nil {
		t.Fatal(generateErr)
	}
	old := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(manifest.Path(output), old, old); err != nil {
		t.Fatal(err)
	}
	if generateErr := manifest.Generate(t.Context(), cfg, nil); generateErr != nil {
		t.Fatal(generateErr)
	}
	info, err := os.Stat(manifest.Path(output))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(old) {
		t.Fatal("no-op run rewrote the manifest")
	}
}

func markCachedDescription(t *testing.T, output string) {
	t.Helper()
	previous, err := manifest.Load(output)
	if err != nil {
		t.Fatal(err)
	}
	previous.Collections["sample"].Files[0].Description = "cached marker"
	if saveErr := previous.Save(output); saveErr != nil {
		t.Fatal(saveErr)
	}
}
func manifestDescription(t *testing.T, output string) string {
	t.Helper()
	current, err := manifest.Load(output)
	if err != nil {
		t.Fatal(err)
	}
	return current.Collections["sample"].Files[0].Description
}

func TestManifestReusesAndInvalidatesSHA(t *testing.T) {
	t.Parallel()
	cfg, output := setupIncremental(t)
	cfg.Sources["sample"] = config.Source{Type: "github", Repo: "acme/docs", Path: "docs"}
	lock := lockfile.New()
	lock.SetEntry("sample", &lockfile.LockEntry{Files: map[string]string{"guide.md": "one"}})
	if err := manifest.Generate(t.Context(), cfg, lock); err != nil {
		t.Fatal(err)
	}
	markCachedDescription(t, output)
	if err := manifest.Generate(t.Context(), cfg, lock); err != nil {
		t.Fatal(err)
	}
	if got := manifestDescription(t, output); got != "cached marker" {
		t.Fatalf("unchanged SHA was reparsed: %q", got)
	}
	file := filepath.Join(output, "sample", "guide.md")
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	// Keep size and mtime fixed: only the changed provider SHA invalidates this entry.
	if writeErr := os.WriteFile(file, []byte("# Changed!\n\nBody.\n"), 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if timeErr := os.Chtimes(file, info.ModTime(), info.ModTime()); timeErr != nil {
		t.Fatal(timeErr)
	}
	lock.Sources["sample"].Files["guide.md"] = "two"
	if generateErr := manifest.Generate(t.Context(), cfg, lock); generateErr != nil {
		t.Fatal(generateErr)
	}
	if got := manifestDescription(t, output); !strings.HasPrefix(got, "Changed!") {
		t.Fatalf("changed SHA reused stale metadata: %q", got)
	}
}

func TestManifestMetadataInvalidationAndDeletion(t *testing.T) {
	t.Parallel()
	cfg, output := setupIncremental(t)
	if err := manifest.Generate(t.Context(), cfg, nil); err != nil {
		t.Fatal(err)
	}
	markCachedDescription(t, output)
	if err := manifest.Generate(t.Context(), cfg, nil); err != nil {
		t.Fatal(err)
	}
	if got := manifestDescription(t, output); got != "cached marker" {
		t.Fatalf("unchanged URL file reparsed: %q", got)
	}
	file := filepath.Join(output, "sample", "guide.md")
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if writeErr := os.WriteFile(file, []byte("# Modified\n\nBody.\n"), 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	changedTime := info.ModTime().Add(time.Second)
	if timeErr := os.Chtimes(file, changedTime, changedTime); timeErr != nil {
		t.Fatal(timeErr)
	}
	if generateErr := manifest.Generate(t.Context(), cfg, nil); generateErr != nil {
		t.Fatal(generateErr)
	}
	if got := manifestDescription(t, output); !strings.HasPrefix(got, "Modified") {
		t.Fatalf("mtime change did not reparse: %q", got)
	}
	if writeErr := os.WriteFile(file, []byte("# Longer modified heading\n\nBody.\n"), 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if timeErr := os.Chtimes(file, changedTime, changedTime); timeErr != nil {
		t.Fatal(timeErr)
	}
	if generateErr := manifest.Generate(t.Context(), cfg, nil); generateErr != nil {
		t.Fatal(generateErr)
	}
	if got := manifestDescription(t, output); !strings.HasPrefix(got, "Longer modified") {
		t.Fatalf("size change did not reparse: %q", got)
	}
	if removeErr := os.Remove(file); removeErr != nil {
		t.Fatal(removeErr)
	}
	if generateErr := manifest.Generate(t.Context(), cfg, nil); generateErr != nil {
		t.Fatal(generateErr)
	}
	current, loadErr := manifest.Load(output)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(current.Collections["sample"].Files) != 0 {
		t.Fatal("deleted file retained in manifest")
	}
}

func TestManifestParserRevisionInvalidatesCache(t *testing.T) {
	t.Parallel()
	cfg, output := setupIncremental(t)
	if err := manifest.Generate(t.Context(), cfg, nil); err != nil {
		t.Fatal(err)
	}
	previous, err := manifest.Load(output)
	if err != nil {
		t.Fatal(err)
	}
	previous.ParserVersion--
	previous.Collections["sample"].Files[0].Description = "cached marker"
	if saveErr := previous.Save(output); saveErr != nil {
		t.Fatal(saveErr)
	}
	if generateErr := manifest.Generate(t.Context(), cfg, nil); generateErr != nil {
		t.Fatal(generateErr)
	}
	if got := manifestDescription(t, output); !strings.HasPrefix(got, "Original") {
		t.Fatalf("parser revision reused stale metadata: %q", got)
	}
}

func TestManifestDeadlinePreservesPrevious(t *testing.T) {
	t.Parallel()
	cfg, output := setupIncremental(t)
	before := []byte("previous manifest must survive\n")
	if err := os.WriteFile(manifest.Path(output), before, 0600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(output, "sample", "guide.md")
	if err := os.WriteFile(
		file,
		[]byte("# Large\n\n"+strings.Repeat("large paragraph ", 1_000_000)),
		0600,
	); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	if err := manifest.Generate(ctx, cfg, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline ignored: %v", err)
	}
	after, err := os.ReadFile(manifest.Path(output))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("deadline replaced the previous manifest")
	}
}

func addDeterminismSources(t *testing.T, cfg *config.Config, output string) {
	t.Helper()
	for _, name := range []string{"zeta", "alpha", "middle"} {
		cfg.Sources[name] = config.Source{Type: "url", URL: "https://example.invalid/" + name}
		directory := filepath.Join(output, name)
		if err := os.Mkdir(directory, 0750); err != nil {
			t.Fatal(err)
		}
		for index := 15; index >= 0; index-- {
			path := filepath.Join(directory, fmt.Sprintf("%02d.md", index))
			if err := os.WriteFile(path, fmt.Appendf(nil, "# %s %d\n", name, index), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}
