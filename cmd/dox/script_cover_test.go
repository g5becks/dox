package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

const (
	coverEnvPrefix     = "GOCOVERDIR="
	coverCountersPref  = "covcounters."
	coverMetaFilePref  = "covmeta."
	coverMergeTempPref = "tmp.dox-merge-"
)

// scriptSetup prepares each testscript environment: it records the base
// environment for the commands that spawn dox themselves and isolates coverage
// output.
func scriptSetup(env *testscript.Env) error {
	if err := isolateCoverDir(env); err != nil {
		return err
	}
	env.Values[scriptEnvKey{}] = append([]string(nil), env.Vars...)
	return nil
}

// isolateCoverDir gives every script a private GOCOVERDIR. dox children are
// instrumented re-executions of the test binary, and Go's coverage runtime names
// its meta-data temp file only by the clock, so children started at the same
// moment share a name and the loser prints "error: coverage meta-data emit
// failed" to stderr, which broke exact stderr assertions (dox-4e1.11). The
// private files are merged back into the shared directory so child coverage is
// still counted.
func isolateCoverDir(env *testscript.Env) error {
	shared := os.Getenv("GOCOVERDIR")
	if shared == "" {
		return nil
	}
	private, err := os.MkdirTemp("", "dox-gocover-")
	if err != nil {
		return err
	}
	for index, entry := range env.Vars {
		if strings.HasPrefix(entry, coverEnvPrefix) {
			env.Vars[index] = coverEnvPrefix + private
		}
	}
	env.Defer(func() {
		if mergeErr := mergeCoverDir(private, shared); mergeErr != nil {
			env.T().Log("merging child coverage:", mergeErr)
		}
		_ = os.RemoveAll(private)
	})
	return nil
}

// mergeCoverDir copies completed coverage files from a script's private
// directory into the shared one. Counter files carry the process ID and a
// timestamp, so they never collide. The meta-data file for a given binary is
// identical everywhere, so it is copied only when missing.
func mergeCoverDir(private, shared string) error {
	entries, err := os.ReadDir(private)
	if err != nil {
		return err
	}
	var failures []error
	for _, entry := range entries {
		name := entry.Name()
		switch {
		case strings.HasPrefix(name, coverCountersPref):
		case strings.HasPrefix(name, coverMetaFilePref):
			if _, statErr := os.Stat(filepath.Join(shared, name)); statErr == nil {
				continue
			}
		default:
			continue
		}
		failures = append(failures, copyCoverFile(filepath.Join(private, name), filepath.Join(shared, name)))
	}
	return errors.Join(failures...)
}

// copyCoverFile publishes a file atomically so readers never see a partial one.
func copyCoverFile(from, to string) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	staged, err := os.CreateTemp(filepath.Dir(to), coverMergeTempPref)
	if err != nil {
		return err
	}
	_, writeErr := staged.Write(data)
	if closeErr := staged.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		_ = os.Remove(staged.Name())
		return writeErr
	}
	if err = os.Rename(staged.Name(), to); err != nil {
		_ = os.Remove(staged.Name())
	}
	return err
}

func writeCoverFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func coverDirFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	slices.Sort(names)
	return names
}

func TestMergeCoverDir(t *testing.T) {
	t.Parallel()
	private, shared := t.TempDir(), t.TempDir()
	writeCoverFixture(t, private, "covcounters.abc.10.1", "counters-a")
	writeCoverFixture(t, private, "covmeta.abc", "meta-from-child")
	writeCoverFixture(t, private, "tmp.covmeta.abc99", "unfinished")
	writeCoverFixture(t, private, "notes.txt", "ignored")
	writeCoverFixture(t, shared, "covcounters.abc.9.1", "counters-b")

	if err := mergeCoverDir(private, shared); err != nil {
		t.Fatal(err)
	}

	want := []string{"covcounters.abc.10.1", "covcounters.abc.9.1", "covmeta.abc"}
	if got := coverDirFiles(t, shared); !slices.Equal(got, want) {
		t.Fatalf("merged files = %v, want %v", got, want)
	}
	for name, content := range map[string]string{
		"covcounters.abc.10.1": "counters-a",
		"covcounters.abc.9.1":  "counters-b",
		"covmeta.abc":          "meta-from-child",
	} {
		got, err := os.ReadFile(filepath.Join(shared, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != content {
			t.Errorf("%s = %q, want %q", name, got, content)
		}
	}
}

func TestMergeCoverDirKeepsExistingMeta(t *testing.T) {
	t.Parallel()
	private, shared := t.TempDir(), t.TempDir()
	writeCoverFixture(t, private, "covmeta.abc", "from-child")
	writeCoverFixture(t, shared, "covmeta.abc", "already-there")

	if err := mergeCoverDir(private, shared); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(shared, "covmeta.abc"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "already-there" {
		t.Errorf("covmeta.abc = %q, want it left untouched", got)
	}
}
