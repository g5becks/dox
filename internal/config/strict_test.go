package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
)

func loadText(t *testing.T, text string) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dox.toml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(path)
	return err
}

func TestLoadReportsTypeErrorsPrecisely(t *testing.T) {
	t.Parallel()
	err := loadText(t, "output = \".dox\"\n\n[sources.docs]\nrepo = 5\npath = \"docs\"\n")
	detail, ok := errors.AsType[*errs.Error](err)
	if !ok || detail.Code != errs.Config {
		t.Fatalf("Load() error = %v, want a config error", err)
	}
	for _, want := range []string{"sources.docs.repo", "line 4", "string"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err.Error(), want)
		}
	}
	if strings.Contains(err.Error(), "missing 'path'") {
		t.Errorf("type error misreported as a missing field: %v", err)
	}
}

func TestLoadReportsUnknownKeys(t *testing.T) {
	t.Parallel()
	err := loadText(
		t,
		"output = \".dox\"\nmax_paralel = 4\n\n[sources.docs]\nrepo = \"a/b\"\npath = \"docs\"\npatern = [\"*.md\"]\n",
	)
	detail, ok := errors.AsType[*errs.Error](err)
	if !ok || detail.Code != errs.Config {
		t.Fatalf("Load() error = %v, want a config error", err)
	}
	for _, want := range []string{`"max_paralel" (line 2)`, `"sources.docs.patern" (line 7)`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not report %s", err.Error(), want)
		}
	}
}

func TestLoadReportsSyntaxErrorLine(t *testing.T) {
	t.Parallel()
	err := loadText(t, "output = \".dox\"\n[sources.docs\nrepo = \"a/b\"\n")
	if detail, ok := errors.AsType[*errs.Error](
		err,
	); !ok || detail.Code != errs.Config ||
		!strings.Contains(err.Error(), "line 2") {
		t.Fatalf("Load() error = %v, want a config error naming line 2", err)
	}
}

// FuzzLoadConfig: arbitrary files never panic, and failures are config errors.
func FuzzLoadConfig(f *testing.F) {
	for _, seed := range []string{
		"output = \".dox\"\n[sources.docs]\nrepo = \"a/b\"\npath = \"docs\"\n",
		"[sources.docs]\nrepo = 5\n",
		"unknown = 1\n",
		"[sources.x]\nurl = \"https://example.com/a.txt\"\n[display]\nformat = \"csv\"\n",
		"timeout = \"-1s\"\n[sources.x]\nurl = \"https://e.com/a\"\n",
		"[[sources]]\n",
		"\x00",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		path := filepath.Join(t.TempDir(), "dox.toml")
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := config.Load(path)
		if err == nil {
			return
		}
		detail, ok := errors.AsType[*errs.Error](err)
		if !ok || (detail.Code != errs.Config && detail.Code != errs.Usage) {
			t.Fatalf("Load() error %v (%T) is not a config/usage errs.Error", err, err)
		}
	})
}
