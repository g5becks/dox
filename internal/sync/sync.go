package sync

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"slices"
	stdsync "sync"

	"golang.org/x/sync/errgroup"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/fsutil"
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
	outputRoot  *os.Root
	sourceDir   string
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
		return nil, errs.New(
			errs.Config,
			"config is required",
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyConfigInvalid}),
		)
	}

	paths, err := cfg.Paths()
	if err != nil {
		return nil, err
	}
	for name, sourceCfg := range cfg.Sources {
		if _, pathErr := paths.SourceDir(name, sourceCfg); pathErr != nil {
			return nil, pathErr
		}
	}
	outputDir := paths.Output()
	if !opts.DryRun {
		root, rootErr := prepareOutput(paths, opts)
		if rootErr != nil {
			return nil, rootErr
		}
		defer root.Close()
		opts.outputRoot = root
	}

	lock, err := lockfile.Load(outputDir)
	if err != nil {
		return nil, err
	}

	sourceNames, err := resolveSourceNames(cfg.Sources, opts.SourceNames)
	if err != nil {
		return nil, err
	}

	emit := opts.OnEvent
	if emit == nil {
		emit = func(Event) {}
	}
	results, err := executeSources(ctx, cfg, opts, sourceNames, lock, emit)
	if err != nil {
		return nil, err
	}

	errorCount, downloadedCount, deletedCount, skippedCount := processResults(
		lock,
		sourceNames,
		results,
		opts.DryRun,
	)

	if !opts.DryRun {
		if commitErr := commitSync(ctx, cfg, lock, opts.outputRoot, emit); commitErr != nil {
			return nil, commitErr
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
		return runResult, errs.New(
			errs.PartialFailure,
			fmt.Sprintf("%d source(s) failed during sync", errorCount),
			errs.WithFields(
				map[string]any{"failed_sources": errorCount, errs.LegacyCodeField: errs.LegacyDownloadFailed},
			),
		)
	}

	return runResult, nil
}

func resolveMaxParallel(cfg *config.Config, opts Options) int {
	maxParallel := opts.MaxParallel
	if maxParallel <= 0 {
		// Check if config specifies a default, otherwise use smart default
		if cfg.MaxParallel > 0 {
			maxParallel = cfg.MaxParallel
		} else {
			maxParallel = getDefaultMaxParallel()
		}
	}

	return maxParallel
}

func executeSources(
	ctx context.Context,
	cfg *config.Config,
	opts Options,
	sourceNames []string,
	lock *lockfile.LockFile,
	emit func(Event),
) (map[string]runState, error) {
	results := make(map[string]runState, len(sourceNames))
	var resultsMu stdsync.Mutex
	token := resolveGitHubToken(cfg)
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(resolveMaxParallel(cfg, opts))

	paths, pathErr := cfg.Paths()
	if pathErr != nil {
		return nil, pathErr
	}
	for _, sourceName := range sourceNames {
		sourceCfg := cfg.Sources[sourceName]
		destinationDir, sourceErr := paths.Source(sourceName, sourceCfg)
		if sourceErr != nil {
			return nil, sourceErr
		}
		sourceDir, sourceErr := paths.SourceDir(sourceName, sourceCfg)
		if sourceErr != nil {
			return nil, sourceErr
		}
		sourceOpts := opts
		sourceOpts.sourceDir = sourceDir
		previousLock := lock.GetEntry(sourceName)

		group.Go(func() error {
			state := syncSource(groupCtx, sourceName, sourceCfg, destinationDir, previousLock, token, sourceOpts, emit)
			resultsMu.Lock()
			results[sourceName] = state
			resultsMu.Unlock()
			return nil
		})
	}

	if waitErr := group.Wait(); waitErr != nil {
		return nil, fmt.Errorf("waiting for source sync workers: %w", waitErr)
	}

	return results, nil
}

func commitSync(
	ctx context.Context,
	cfg *config.Config,
	lock *lockfile.LockFile,
	root *os.Root,
	emit func(Event),
) error {
	if err := lock.SaveRoot(root); err != nil {
		return err
	}
	// Manifest failures are reported but do not fail the sync.
	if err := manifest.GenerateRoot(ctx, cfg, lock, root); err != nil {
		emit(Event{Kind: EventManifestError, Err: err})
	}
	return nil
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
		state.result, state.err = syncPreparedSource(ctx, src, destinationDir, previousLock, opts)
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
			return nil, errs.New(
				errs.NotFound,
				fmt.Sprintf("source %q not found in config", sourceName),
				errs.WithHint("Run 'dox list' to see configured sources"),
				errs.WithFields(map[string]any{"source": sourceName, errs.LegacyCodeField: errs.LegacySourceNotFound}),
				errs.WithCause(errs.ErrSourceNotFound),
			)
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

func syncPreparedSource(
	ctx context.Context,
	src source.Source,
	destinationDir string,
	previousLock *lockfile.LockEntry,
	opts Options,
) (*source.SyncResult, error) {
	var sourceRoot *os.Root
	if !opts.DryRun {
		if err := opts.outputRoot.MkdirAll(opts.sourceDir, fsutil.DirMode); err != nil {
			return nil, err
		}
		root, err := opts.outputRoot.OpenRoot(opts.sourceDir)
		if err != nil {
			return nil, err
		}
		defer root.Close()
		sourceRoot = root
	}
	return src.Sync(
		ctx,
		destinationDir,
		previousLock,
		source.SyncOptions{Force: opts.Force, DryRun: opts.DryRun, Root: sourceRoot},
	)
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

func prepareOutput(paths *config.Paths, opts Options) (*os.Root, error) {
	root, err := paths.OpenOutput(!opts.Clean)
	if err != nil {
		return nil, err
	}
	if opts.Clean {
		if cleanErr := config.CleanOutput(root); cleanErr != nil {
			_ = root.Close()
			return nil, cleanErr
		}
	}
	if markErr := config.MarkOutput(root); markErr != nil {
		_ = root.Close()
		return nil, markErr
	}
	return root, nil
}
