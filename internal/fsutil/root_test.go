package fsutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/g5becks/dox/internal/fsutil"
)

func TestAtomicWriteRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()
	output, victim := t.TempDir(), t.TempDir()
	if err := os.Symlink(victim, filepath.Join(output, "escape")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(output)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if atomicWriteErr := fsutil.AtomicWrite(root, "escape/payload", []byte("unsafe")); atomicWriteErr == nil {
		t.Fatal("symlink escape accepted")
	}
	entries, err := os.ReadDir(victim)
	if err != nil || len(entries) != 0 {
		t.Fatalf("victim changed: %v, %v", entries, err)
	}
}

func TestAtomicWriteReplacesHardLink(t *testing.T) {
	t.Parallel()
	output, victim := t.TempDir(), t.TempDir()
	outside := filepath.Join(victim, "sentinel")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, filepath.Join(output, "payload")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(output)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if atomicWriteErr := fsutil.AtomicWrite(root, "payload", []byte("new")); atomicWriteErr != nil {
		t.Fatal(atomicWriteErr)
	}
	unchanged, err := os.ReadFile(outside)
	if err != nil || string(unchanged) != "keep" {
		t.Fatalf("hard-link victim changed: %q, %v", unchanged, err)
	}
	actual, err := root.ReadFile("payload")
	if err != nil || string(actual) != "new" {
		t.Fatalf("replacement failed: %q, %v", actual, err)
	}
}

func TestAtomicWriteAnchorsRenamedDirectory(t *testing.T) {
	t.Parallel()
	parent, victim := t.TempDir(), t.TempDir()
	original := filepath.Join(parent, "source")
	if err := os.Mkdir(original, 0o750); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(original)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	moved := filepath.Join(parent, "moved")
	if renameErr := os.Rename(original, moved); renameErr != nil {
		t.Fatal(renameErr)
	}
	if symlinkErr := os.Symlink(victim, original); symlinkErr != nil {
		t.Fatal(symlinkErr)
	}
	if atomicWriteErr := fsutil.AtomicWrite(root, "payload", []byte("safe")); atomicWriteErr != nil {
		t.Fatal(atomicWriteErr)
	}
	actual, err := os.ReadFile(filepath.Join(moved, "payload"))
	if err != nil || string(actual) != "safe" {
		t.Fatalf("opened root lost: %q, %v", actual, err)
	}
	entries, err := os.ReadDir(victim)
	if err != nil || len(entries) != 0 {
		t.Fatalf("victim changed: %v, %v", entries, err)
	}
}
