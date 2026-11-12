package lockfile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/g5becks/dox/internal/lockfile"
)

func TestLoadReturnsEmptyLockWhenFileMissing(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()

	lock, err := lockfile.Load(outputDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if lock.Version != 2 {
		t.Fatalf("Version = %d, want 2", lock.Version)
	}

	if len(lock.Sources) != 0 {
		t.Fatalf("Sources len = %d, want 0", len(lock.Sources))
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)

	lock := lockfile.New()
	lock.SetEntry("goreleaser", &lockfile.LockEntry{
		Type:        "github",
		TreeSHA:     "abc123",
		RefResolved: "main",
		SyncedAt:    now,
		Files: map[string]string{
			"getting-started.md": "sha1",
			"custom/build.md":    "sha2",
		},
	})
	lock.SetEntry("hono", &lockfile.LockEntry{
		Type:     "url",
		ETag:     `"etag"`,
		LastMod:  "Tue, 15 Jan 2024 10:30:00 GMT",
		SyncedAt: now,
	})

	if err := lock.Save(outputDir); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := lockfile.Load(outputDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.Version != 2 {
		t.Fatalf("Version = %d, want 2", loaded.Version)
	}

	githubEntry := loaded.GetEntry("goreleaser")
	if githubEntry == nil {
		t.Fatalf("GetEntry(goreleaser) = nil, want non-nil")
	}

	if githubEntry.TreeSHA != "abc123" {
		t.Fatalf("TreeSHA = %q, want %q", githubEntry.TreeSHA, "abc123")
	}

	if githubEntry.Files["custom/build.md"] != "sha2" {
		t.Fatalf("Files[custom/build.md] = %q, want %q", githubEntry.Files["custom/build.md"], "sha2")
	}

	urlEntry := loaded.GetEntry("hono")
	if urlEntry == nil {
		t.Fatalf("GetEntry(hono) = nil, want non-nil")
	}

	if urlEntry.ETag != `"etag"` {
		t.Fatalf("ETag = %q, want %q", urlEntry.ETag, `"etag"`)
	}
}

func TestSaveWritesAtomicallyWithoutTempFilesLeft(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	lock := lockfile.New()
	lock.SetEntry("docs", &lockfile.LockEntry{
		Type:     "url",
		SyncedAt: time.Now().UTC(),
	})

	if err := lock.Save(outputDir); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	tempMatches, err := filepath.Glob(filepath.Join(outputDir, ".dox.lock.*.tmp"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}

	if len(tempMatches) != 0 {
		t.Fatalf("temporary files left behind: %v", tempMatches)
	}

	lockPath := filepath.Join(outputDir, ".dox.lock")
	if _, statErr := os.Stat(lockPath); statErr != nil {
		t.Fatalf("expected lock file at %q: %v", lockPath, statErr)
	}
}

func TestLoadInvalidJSONReturnsError(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	lockPath := filepath.Join(outputDir, ".dox.lock")
	if err := os.WriteFile(lockPath, []byte("{invalid"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := lockfile.Load(outputDir)
	if err == nil {
		t.Fatalf("Load() error = nil, want non-nil")
	}

	if !strings.Contains(err.Error(), "parsing lock file") {
		t.Fatalf("Load() error = %q, expected parsing message", err.Error())
	}
}

func TestEntryCRUD(t *testing.T) {
	t.Parallel()

	lock := lockfile.New()
	if entry := lock.GetEntry("missing"); entry != nil {
		t.Fatalf("GetEntry(missing) = %v, want nil", entry)
	}

	entry := &lockfile.LockEntry{
		Type:     "url",
		SyncedAt: time.Now().UTC(),
	}
	lock.SetEntry("hono", entry)

	got := lock.GetEntry("hono")
	if got == nil {
		t.Fatalf("GetEntry(hono) = nil, want non-nil")
	}

	if got.Type != "url" {
		t.Fatalf("Type = %q, want %q", got.Type, "url")
	}

	lock.RemoveEntry("hono")
	if lock.GetEntry("hono") != nil {
		t.Fatalf("GetEntry(hono) after RemoveEntry() = non-nil, want nil")
	}
}

func TestSaveOnNilLockReturnsError(t *testing.T) {
	t.Parallel()

	var lock *lockfile.LockFile

	err := lock.Save(t.TempDir())
	if err == nil {
		t.Fatalf("Save() error = nil, want non-nil")
	}

	if !strings.Contains(err.Error(), "cannot save nil lock file") {
		t.Fatalf("Save() error = %q, expected nil-lock message", err.Error())
	}
}

func TestLoadVersion1LockUpgradesWithoutLosingEntries(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	v1 := `{"version":1,"sources":{"docs":{"type":"github","tree_sha":"tree-1",` +
		`"ref_resolved":"main","synced_at":"2026-01-02T03:04:05Z","files":{"a.md":"sha-a"}}}}`
	if err := os.WriteFile(filepath.Join(dir, ".dox.lock"), []byte(v1), 0o600); err != nil {
		t.Fatal(err)
	}
	lock, err := lockfile.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if lock.Version != 2 {
		t.Fatalf("Version = %d, want 2", lock.Version)
	}
	entry := lock.GetEntry("docs")
	if entry == nil || entry.TreeSHA != "tree-1" || entry.RefResolved != "main" ||
		entry.Commit != "" || entry.Files["a.md"] != "sha-a" {
		t.Fatalf("v1 entry not preserved: %+v", entry)
	}
	entry.Commit = "c0ffee"
	if saveErr := lock.Save(dir); saveErr != nil {
		t.Fatal(saveErr)
	}
	reloaded, err := lockfile.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.GetEntry("docs").Commit; got != "c0ffee" {
		t.Fatalf("Commit after round trip = %q", got)
	}
}
