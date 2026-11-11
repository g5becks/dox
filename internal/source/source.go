package source

import (
	"context"
	"fmt"
	"os"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/fsutil"
	"github.com/g5becks/dox/internal/lockfile"
)

const sourceTypeURL = "url"

// SyncResult reports what happened during a sync.
type SyncResult struct {
	Downloaded int
	Deleted    int
	Skipped    bool
	LockEntry  *lockfile.LockEntry
}

// SyncOptions controls behavior for source sync operations.
type SyncOptions struct {
	// Root is the caller-owned, already confined source directory.
	Root   *os.Root
	Force  bool
	DryRun bool
}

// Source defines a documentation source that can be synced.
type Source interface {
	Sync(
		ctx context.Context,
		destDir string,
		prevLock *lockfile.LockEntry,
		opts SyncOptions,
	) (*SyncResult, error)
	Close() error
}

// New creates a Source from config.
func New(name string, cfg config.Source, token string) (Source, error) {
	switch cfg.Type {
	case "github":
		return NewGitHub(name, cfg, token)
	case sourceTypeURL:
		return NewURL(name, cfg)
	default:
		return nil, errs.New(
			errs.Config,
			fmt.Sprintf("unknown source type %q for source %q", cfg.Type, name),
			errs.WithHint("Supported types: github, url"),
			errs.WithFields(map[string]any{"type": cfg.Type, errs.LegacyCodeField: errs.LegacyUnknownSourceType}),
		)
	}
}

func NewGitHub(name string, cfg config.Source, token string) (Source, error) {
	return newGitHubSource(name, cfg, token)
}

// destinationRoot duplicates the caller root or creates a trusted standalone destination.
func destinationRoot(path string, opts SyncOptions) (*os.Root, error) {
	if opts.Root != nil {
		return opts.Root.OpenRoot(".")
	}
	return fsutil.OpenDir(path)
}
