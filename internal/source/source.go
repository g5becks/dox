package source

import (
	"context"

	"github.com/samber/oops"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/lockfile"
)

// SyncResult reports what happened during a sync.
type SyncResult struct {
	Downloaded int
	Deleted    int
	Skipped    bool
	LockEntry  *lockfile.LockEntry
}

// SyncOptions controls behavior for source sync operations.
type SyncOptions struct {
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
}

// New creates a Source from config.
func New(name string, cfg config.Source, token string) (Source, error) {
	switch cfg.Type {
	case "github":
		return NewGitHub(name, cfg, token)
	case "url":
		return NewURL(name, cfg)
	default:
		return nil, oops.
			Code("UNKNOWN_SOURCE_TYPE").
			With("type", cfg.Type).
			Hint("Supported types: github, url").
			Errorf("unknown source type %q for source %q", cfg.Type, name)
	}
}

func NewGitHub(name string, cfg config.Source, token string) (Source, error) {
	return newGitHubSource(name, cfg, token)
}
