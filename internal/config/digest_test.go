package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/g5becks/dox/internal/config"
)

func writeDigestConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dox.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDigestStableAcrossLoads(t *testing.T) {
	path := writeDigestConfig(t, `excludes = ["**/*.png", "**/*.jpg", "**/*.gif", "**/*.svg", "**/*.pdf", "**/*.zip"]

[sources.docs]
repo = "owner/repo"
path = "docs"
exclude = ["blog/**", "changes/**"]
`)

	first := ""
	for i := range 200 {
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		digest := cfg.Sources["docs"].Digest()
		if i == 0 {
			first = digest
			continue
		}
		if digest != first {
			t.Fatalf("load %d: digest %s, want %s", i, digest, first)
		}
	}
}

func TestDigestIgnoresPatternAndExcludeOrder(t *testing.T) {
	a := writeDigestConfig(t, `excludes = ["**/*.png", "**/*.jpg", "**/*.gif"]

[sources.docs]
repo = "owner/repo"
path = "docs"
patterns = ["**/*.md", "**/*.mdx"]
exclude = ["blog/**", "changes/**"]
`)
	b := writeDigestConfig(t, `excludes = ["**/*.gif", "**/*.png", "**/*.jpg"]

[sources.docs]
repo = "owner/repo"
path = "docs"
patterns = ["**/*.mdx", "**/*.md"]
exclude = ["changes/**", "blog/**"]
`)
	cfgA, err := config.Load(a)
	if err != nil {
		t.Fatal(err)
	}
	cfgB, err := config.Load(b)
	if err != nil {
		t.Fatal(err)
	}
	if da, db := cfgA.Sources["docs"].Digest(), cfgB.Sources["docs"].Digest(); da != db {
		t.Fatalf("reordered config changed the digest: %s vs %s", da, db)
	}
}

func TestDigestChangesWithFilters(t *testing.T) {
	base := config.Source{Type: "github", Repo: "o/r", Path: "docs", Patterns: []string{"**/*.md"}}
	withExclude := base
	withExclude.Exclude = []string{"blog/**"}
	if base.Digest() == withExclude.Digest() {
		t.Fatal("adding an exclude must change the digest")
	}
	widened := base
	widened.Patterns = []string{"**/*.md", "**/*.txt"}
	if base.Digest() == widened.Digest() {
		t.Fatal("adding a pattern must change the digest")
	}
}
