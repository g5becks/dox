package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/g5becks/dox/internal/config"
)

func TestValidatePath(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		path  string
		valid bool
	}{{".dox", true}, {"docs/nested", true}, {".", false}, {"..", false}, {"../../victim", false}, {"/", false}, {"", false}} {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			err := config.ValidatePath(tc.path)
			if (err == nil) != tc.valid {
				t.Fatalf("ValidatePath(%q)=%v, valid=%t", tc.path, err, tc.valid)
			}
		})
	}
}

func TestOutputSymlinkCannotEscape(t *testing.T) {
	t.Parallel()
	project, victim := t.TempDir(), t.TempDir()
	if err := os.Symlink(victim, filepath.Join(project, "docs")); err != nil {
		t.Fatal(err)
	}
	paths, err := config.NewPaths(project, "docs", false)
	if err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenOutput(true)
	if err == nil {
		_ = root.Close()
		t.Fatal("output symlink escaped config root")
	}
	if entries, readErr := os.ReadDir(victim); readErr != nil || len(entries) != 0 {
		t.Fatalf("victim changed: %v, %v", entries, readErr)
	}
}

func FuzzValidatePath(f *testing.F) {
	for _, path := range []string{".dox", "docs/nested", ".", "..", "/", "../../victim", "", "a/../b", "../x", "a\\b", "NUL", "a\x00b"} {
		f.Add(path)
	}
	f.Fuzz(func(t *testing.T, path string) {
		err := config.ValidatePath(path)
		if err == nil && (!filepath.IsLocal(path) || filepath.Clean(path) == ".") {
			t.Fatalf("unsafe path accepted: %q", path)
		}
	})
}

func TestCleanRequiresOwnership(t *testing.T) {
	t.Parallel()
	output := t.TempDir()
	root, err := os.OpenRoot(output)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if writeFileErr := root.WriteFile("sentinel", []byte("keep"), 0o600); writeFileErr != nil {
		t.Fatal(writeFileErr)
	}
	if cleanOutputErr := config.CleanOutput(root); cleanOutputErr == nil {
		t.Fatal("unowned directory cleaned")
	}
	before, err := root.ReadFile("sentinel")
	if err != nil || string(before) != "keep" {
		t.Fatalf("sentinel changed: %q, %v", before, err)
	}
	if markOutputErr := config.MarkOutput(root); markOutputErr != nil {
		t.Fatal(markOutputErr)
	}
	if cleanOutputErr := config.CleanOutput(root); cleanOutputErr != nil {
		t.Fatal(cleanOutputErr)
	}
	if _, statErr := root.Stat("sentinel"); !os.IsNotExist(statErr) {
		t.Fatalf("owned cleanup did not remove contents: %v", statErr)
	}
	if requireOwnedErr := config.RequireOwned(root); requireOwnedErr != nil {
		t.Fatal(requireOwnedErr)
	}
}

func TestExternalOutputOptIn(t *testing.T) {
	t.Parallel()
	project := t.TempDir()
	outside := filepath.Join(filepath.Dir(project), "external")
	if _, err := config.NewPaths(project, outside, false); err == nil {
		t.Fatal("absolute output accepted without opt-in")
	}
	paths, err := config.NewPaths(project, outside, true)
	if err != nil || paths.Output() != outside {
		t.Fatalf("explicit external path rejected: %v", err)
	}
	for _, unsafe := range []string{project, filepath.VolumeName(project) + string(filepath.Separator)} {
		if _, newPathsErr := config.NewPaths(project, unsafe, true); newPathsErr == nil {
			t.Fatalf("protected path accepted: %q", unsafe)
		}
	}
}

func TestOutputAliasCannotSelectConfig(t *testing.T) {
	t.Parallel()
	project := t.TempDir()
	if err := os.Symlink(".", filepath.Join(project, "alias")); err != nil {
		t.Fatal(err)
	}
	paths, err := config.NewPaths(project, "alias", false)
	if err != nil {
		t.Fatal(err)
	}
	root, err := paths.OpenOutput(false)
	if err == nil {
		_ = root.Close()
		t.Fatal("config directory alias accepted as output")
	}
}
