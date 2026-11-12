// Package benchdata loads the optional, commit-pinned documentation corpus.
package benchdata

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/lockfile"
	"github.com/g5becks/dox/internal/manifest"
)

// Load skips corpus benchmarks when their optional input has not been synced.
func Load(tb testing.TB) (*config.Config, *lockfile.LockFile, *manifest.Manifest) {
	tb.Helper()
	root := os.Getenv("DOX_BENCH_CORPUS")
	if root == "" {
		tb.Skip("set DOX_BENCH_CORPUS and run scripts/bench-corpus.sh for corpus benchmarks")
	}
	directory, err := os.OpenRoot(root)
	if os.IsNotExist(err) {
		tb.Skip("corpus is missing; run scripts/bench-corpus.sh")
	}
	if err != nil {
		tb.Fatal(err)
	}
	defer directory.Close()
	if _, statErr := directory.Stat("docs/" + manifest.ManifestFile); os.IsNotExist(statErr) {
		tb.Skip("corpus is missing; run scripts/bench-corpus.sh")
	}
	cfg, err := config.Load(filepath.Join(root, "dox.toml"))
	if err != nil {
		tb.Fatal(err)
	}
	if !filepath.IsAbs(cfg.Output) {
		cfg.Output = filepath.Join(cfg.ConfigDir, cfg.Output)
	}
	lock, err := lockfile.Load(cfg.Output)
	if err != nil {
		tb.Fatal(err)
	}
	m, err := manifest.Load(cfg.Output)
	if err != nil {
		tb.Fatal(err)
	}
	return cfg, lock, m
}
