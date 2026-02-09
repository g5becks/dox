package sync

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	stdsync "sync"

	"github.com/samber/oops"
	"golang.org/x/sync/errgroup"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/lockfile"
	"github.com/g5becks/dox/internal/manifest"
	"github.com/g5becks/dox/internal/source"
)

const (
	cpuMultiplier         = 4  // Multiply CPU count for I/O-bound parallelism
	minDefaultParallelism = 10 // Minimum parallelism even on low-core machines
)

// getDefaultMaxParallel returns a smart default for I/O-bound operations.
// Since syncing is network I/O (not CPU-bound), we can be aggressive.
func getDefaultMaxParallel() int {
	cpus := runtime.NumCPU()
	return max(cpus*cpuMultiplier, minDefaultParallelism)
}

// EventKind identifies the type of sync progress event.
type EventKind int

const (
	EventSourceStart EventKind = iota
	EventSourceDone
	EventManifestError
)

// Event is emitted during sync to report per-source progress.
type Event struct {
	Kind   EventKind
	Source string
	Result *source.SyncResult // nil for start events
	Err    error              // non-nil if source failed
}

// RunResult contains aggregate counts from a completed sync run.
type RunResult struct {
	Sources    int
	Downloaded int
	Deleted    int
	Skipped    int
	Errors     int
}

type Options struct {
	SourceNames []string
	Force       bool
	DryRun      bool
	MaxParallel int
	Clean       bool
	OnEvent     func(Event) // optional; nil = silent
}

type runState struct {
	result *source.SyncResult
	err    error
}

func Run(ctx context.Context, cfg *config.Config, opts Options) (*RunResult, error) {
	if cfg == nil {
		return nil, oops.
			Code("CONFIG_INVALID").
			Errorf("config is required")
	}

	outputDir := resolveOutputRoot(cfg)
	if opts.Clean && !opts.DryRun {
		if err := os.RemoveAll(outputDir); err != nil {
			return nil, oops.
				Code("WRITE_FAILED").
				With("path", outputDir).
				Wrapf(err, "cleaning output directory")
		}
	}

	lock, err := lockfile.Load(outputDir)
	if err != nil {
		return nil, err
	}

	sourceNames, err := resolveSourceNames(cfg.Sources, opts.SourceNames)
	if err != nil {
		return nil, err
	}

	maxParallel := opts.MaxParallel
	if maxParallel <= 0 {
		// Check if config specifies a default, otherwise use smart default
		if cfg.MaxParallel > 0 {
			maxParallel = cfg.MaxParallel
		} else {
			maxParallel = getDefaultMaxParallel()
		}
	}

	emit := opts.OnEvent
	if emit == nil {
		emit = func(Event) {}
	}

	results := make(map[string]runState, len(sourceNames))
	var resultsMu stdsync.Mutex
	token := resolveGitHubToken(cfg)
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(maxParallel)

	for _, sourceName := range sourceNames {
		sourceCfg := cfg.Sources[sourceName]
		destinationDir := resolveSourceOutputDir(outputDir, sourceName, sourceCfg)
		previousLock := lock.GetEntry(sourceName)

		group.Go(func() error {
			state := syncSource(groupCtx, sourceName, sourceCfg, destinationDir, previousLock, token, opts, emit)
			resultsMu.Lock()
			results[sourceName] = state
			resultsMu.Unlock()
			return nil
		})
	}

	if waitErr := group.Wait(); waitErr != nil {
		return nil, oops.Wrapf(waitErr, "waiting for source sync workers")
	}

	errorCount, downloadedCount, deletedCount, skippedCount := processResults(
		lock,
		sourceNames,
		results,
		opts.DryRun,
	)

	if !opts.DryRun {
		if saveErr := lock.Save(outputDir); saveErr != nil {
			return nil, saveErr
		}

		// Generate manifest (non-fatal)
		if genErr := manifest.Generate(ctx, cfg, lock); genErr != nil {
			if opts.OnEvent != nil {
				opts.OnEvent(Event{
					Kind: EventManifestError,
					Err:  genErr,
				})
			}
		}
	}

	runResult := &RunResult{
		Sources:    len(sourceNames),
		Downloaded: downloadedCount,
		Deleted:    deletedCount,
		Skipped:    skippedCount,
		Errors:     errorCount,
	}

	if errorCount > 0 {
		return runResult, oops.
			Code("DOWNLOAD_FAILED").
			With("failed_sources", errorCount).
			Errorf("%d source(s) failed during sync", errorCount)
	}

	return runResult, nil
}

func syncSource(
	ctx context.Context,
	sourceName string,
	sourceCfg config.Source,
	destinationDir string,
	previousLock *lockfile.LockEntry,
	token string,
	opts Options,
	emit func(Event),
) runState {
	state := runState{}

	emit(Event{Kind: EventSourceStart, Source: sourceName})

	src, newErr := source.New(sourceName, sourceCfg, token)
	if newErr != nil {
		state.err = newErr
	} else {
		defer src.Close()
		state.result, state.err = src.Sync(
			ctx,
			destinationDir,
			previousLock,
			source.SyncOptions{
				Force:  opts.Force,
				DryRun: opts.DryRun,
			},
		)
	}

	emit(Event{
		Kind:   EventSourceDone,
		Source: sourceName,
		Result: state.result,
		Err:    state.err,
	})

	return state
}

func resolveSourceNames(
	sourceConfigs map[string]config.Source,
	requestedNames []string,
) ([]string, error) {
	if len(requestedNames) == 0 {
		sourceNames := make([]string, 0, len(sourceConfigs))
		for sourceName := range sourceConfigs {
			sourceNames = append(sourceNames, sourceName)
		}

		slices.Sort(sourceNames)
		return sourceNames, nil
	}

	sourceNames := make([]string, 0, len(requestedNames))
	seen := make(map[string]struct{}, len(requestedNames))

	for _, sourceName := range requestedNames {
		if _, ok := sourceConfigs[sourceName]; !ok {
			return nil, oops.
				Code("SOURCE_NOT_FOUND").
				With("source", sourceName).
				Hint("Run 'dox list' to see configured sources").
				Errorf("source %q not found in config", sourceName)
		}

		if _, exists := seen[sourceName]; exists {
			continue
		}

		seen[sourceName] = struct{}{}
		sourceNames = append(sourceNames, sourceName)
	}

	return sourceNames, nil
}

func resolveGitHubToken(cfg *config.Config) string {
	if cfg.GitHubToken != "" {
		return cfg.GitHubToken
	}

	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}

	return os.Getenv("GH_TOKEN")
}

func resolveOutputRoot(cfg *config.Config) string {
	if filepath.IsAbs(cfg.Output) {
		return cfg.Output
	}

	return filepath.Join(cfg.ConfigDir, cfg.Output)
}

func resolveSourceOutputDir(outputRoot string, sourceName string, sourceCfg config.Source) string {
	if sourceCfg.Out != "" {
		return filepath.Join(outputRoot, sourceCfg.Out)
	}

	return filepath.Join(outputRoot, sourceName)
}

func processResults(
	lock *lockfile.LockFile,
	sourceNames []string,
	results map[string]runState,
	dryRun bool,
) (int, int, int, int) {
	errorCount := 0
	downloadedCount := 0
	deletedCount := 0
	skippedCount := 0

	for _, sourceName := range sourceNames {
		state := results[sourceName]
		if state.err != nil {
			errorCount++
			continue
		}

		if state.result == nil {
			continue
		}

		downloadedCount += state.result.Downloaded
		deletedCount += state.result.Deleted
		if state.result.Skipped {
			skippedCount++
		}

		if !dryRun && state.result.LockEntry != nil {
			lock.SetEntry(sourceName, state.result.LockEntry)
		}
	}

	return errorCount, downloadedCount, deletedCount, skippedCount
}
