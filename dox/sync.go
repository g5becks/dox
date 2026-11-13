package dox

import (
	"context"
	stdsync "sync"
	"time"

	doxsync "github.com/g5becks/dox/internal/sync"
)

// SyncOptions selects what Project.Sync does. The zero value syncs every
// source, skipping what is already current.
type SyncOptions struct {
	// Sources limits the sync to these configured source names.
	Sources []string
	// Force re-downloads files even when they look current.
	Force bool
	// DryRun reports what would change without writing anything.
	DryRun bool
	// Clean removes the existing output before syncing.
	Clean bool
	// Locked reproduces dox.lock: GitHub sources fetch the locked commits, URL
	// sources must match their recorded digests, and config drift is an error.
	Locked bool
	// Check reports sources that are behind upstream without writing anything;
	// the stale names are in SyncResult.Stale.
	Check bool
	// MaxParallel bounds concurrent sources (0 = default).
	MaxParallel int
	// Timeout bounds each source (0 = the configured default).
	Timeout time.Duration
	// LockTimeout bounds the wait for another sync or clean of the same output
	// directory (0 = default).
	LockTimeout time.Duration
	// OnEvent receives progress. Calls are serialized, so the function needs
	// no locking of its own, but it should return quickly.
	OnEvent func(SyncEvent)
}

// SyncEventKind identifies a SyncEvent.
type SyncEventKind int

// Sync progress events.
const (
	EventSourceStarted  SyncEventKind = iota // a source began syncing
	EventSourceFinished                      // a source finished; see Err, Downloaded, Deleted and Skipped
	EventManifestFailed                      // the manifest could not be written; see Err
	EventWarning                             // a non-fatal problem; see Warning
)

// SyncEvent reports progress for one source.
type SyncEvent struct {
	Kind       SyncEventKind
	Source     string // empty for EventManifestFailed
	Warning    string // set for EventWarning
	Downloaded int    // files downloaded, for EventSourceFinished
	Deleted    int    // files removed, for EventSourceFinished
	Skipped    bool   // the source was already current, for EventSourceFinished
	Err        error  // why the source (or the manifest) failed
}

// SyncResult summarizes a sync.
type SyncResult struct {
	Sources    int
	Downloaded int
	Deleted    int
	Skipped    int
	Errors     int
	// Stale names the sources that would change, for Check runs.
	Stale []string
}

// Sync downloads the configured sources into the output directory. When some
// sources fail, it returns both the partial result and an error with
// CodePartialFailure; with Check it returns CodeStale when sources are behind.
func (p *Project) Sync(ctx context.Context, options SyncOptions) (*SyncResult, error) {
	runOptions := doxsync.Options{
		SourceNames: options.Sources,
		Force:       options.Force,
		DryRun:      options.DryRun,
		Clean:       options.Clean,
		Locked:      options.Locked,
		Check:       options.Check,
		MaxParallel: options.MaxParallel,
		Timeout:     options.Timeout,
		LockTimeout: options.LockTimeout,
	}
	if options.OnEvent != nil {
		var mu stdsync.Mutex
		runOptions.OnEvent = func(event doxsync.Event) {
			mu.Lock()
			defer mu.Unlock()
			options.OnEvent(convertEvent(event))
		}
	}
	result, err := doxsync.Run(ctx, p.cfg, runOptions)
	if result == nil {
		return nil, wrapError(err)
	}
	return &SyncResult{
		Sources:    result.Sources,
		Downloaded: result.Downloaded,
		Deleted:    result.Deleted,
		Skipped:    result.Skipped,
		Errors:     result.Errors,
		Stale:      result.Stale,
	}, wrapError(err)
}

func convertEvent(event doxsync.Event) SyncEvent {
	converted := SyncEvent{Source: event.Source, Warning: event.Warning, Err: wrapError(event.Err)}
	switch event.Kind {
	case doxsync.EventSourceStart:
		converted.Kind = EventSourceStarted
	case doxsync.EventSourceDone:
		converted.Kind = EventSourceFinished
	case doxsync.EventManifestError:
		converted.Kind = EventManifestFailed
	case doxsync.EventWarning:
		converted.Kind = EventWarning
	}
	if event.Result != nil {
		converted.Downloaded = event.Result.Downloaded
		converted.Deleted = event.Result.Deleted
		converted.Skipped = event.Result.Skipped
	}
	return converted
}
