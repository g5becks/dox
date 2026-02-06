package source

import (
	"context"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/lockfile"
	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/samber/oops"
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
		tracker *progress.Tracker,
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

// NewGitHub is a temporary placeholder until GitHub source sync is implemented.
func NewGitHub(name string, cfg config.Source, token string) (Source, error) {
	_ = cfg
	_ = token

	return &notImplementedSource{
		name: name,
		kind: "github",
	}, nil
}

type notImplementedSource struct {
	name string
	kind string
}

func (s *notImplementedSource) Sync(
	_ context.Context,
	_ string,
	_ *lockfile.LockEntry,
	_ SyncOptions,
	_ *progress.Tracker,
) (*SyncResult, error) {
	return nil, oops.
		Code("NOT_IMPLEMENTED").
		With("source", s.name).
		With("type", s.kind).
		Hint("Continue implementation in the source package tasks").
		Errorf("%s source sync is not implemented for %q", s.kind, s.name)
}
