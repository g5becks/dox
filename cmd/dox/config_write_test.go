package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteConfigReplacesFileWithoutChangingOtherLinks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	original := filepath.Join(dir, "original.toml")
	configPath := filepath.Join(dir, "dox.toml")
	if err := os.WriteFile(original, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(original, configPath); err != nil {
		t.Fatal(err)
	}
	if err := writeConfig(configPath, "replacement\n"); err != nil {
		t.Fatal(err)
	}
	assertConfigContent(t, original, "original\n")
	assertConfigContent(t, configPath, "replacement\n")
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o644 {
		t.Errorf("config permissions = %o, want preserved 644", info.Mode().Perm())
	}
	assertNoConfigTemps(t, dir)
}

func TestWriteConfigPreservesPrivatePermissions(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not modeled on Windows")
	}

	dir := t.TempDir()
	configPath := filepath.Join(dir, "dox.toml")
	if err := os.WriteFile(configPath, []byte("github_token = \"secret\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeConfig(configPath, "replacement\n"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config permissions = %o, want preserved 600", info.Mode().Perm())
	}
	assertNoConfigTemps(t, dir)
}

func TestWriteConfigRenameFailureCleansTemporaryFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "dox.toml")
	if err := os.Mkdir(configPath, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(configPath, "keep")
	if err := os.WriteFile(marker, []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeConfig(configPath, "replacement\n"); err == nil {
		t.Fatal("expected replacement of a directory to fail")
	}
	assertConfigContent(t, marker, "original\n")
	assertNoConfigTemps(t, dir)
}

func assertConfigContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Errorf("%s content = %q, want %q", path, data, want)
	}
}

func assertNoConfigTemps(t *testing.T, dir string) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, ".dox-config-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 {
		t.Errorf("temporary files left behind: %v", paths)
	}
}
