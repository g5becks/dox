package sync

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"runtime"
	"slices"
	"strings"
	stdsync "sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/fsutil"
	"github.com/g5becks/dox/internal/httpx"
	"github.com/g5becks/dox/internal/lockfile"
	"github.com/g5becks/dox/internal/manifest"
	"github.com/g5becks/dox/internal/source"
)

const (
	cpuMultiplier         = 4  // Multiply CPU count for I/O-bound parallelism
	minDefaultParallelism = 10 // Minimum parallelism even on low-core machines
	maxDefaultParallelism = 16 // Bound the computed fallback; explicit choices are not capped
	fieldFailedSources    = "failed_sources"
)

// getDefaultMaxParallel returns a smart default for I/O-bound operations.
// The fallback balances network overlap with bounded resource use.
func getDefaultMaxParallel() int {
	cpus := runtime.NumCPU()
	return min(max(cpus*cpuMultiplier, minDefaultParallelism), maxDefaultParallelism)
}

// EventKind identifies the type of sync progress event.
type EventKind int

const (
	EventSourceStart EventKind = iota
	EventSourceDone
	EventManifestError
	EventWarning
)

// Event is emitted during sync to report per-source progress.
type Event struct {
	Kind    EventKind
	Source  string
	Warning string
	Result  *source.SyncResult // nil for start events
	Err     error              // non-nil if source failed
}

// RunResult contains aggregate counts from a completed sync run.
type RunResult struct {
	Sources    int
	Downloaded int
	Deleted    int
	Skipped    int
	Errors     int
	// Stale lists, for Check runs, the sources that would change.
	Stale []string
}

type Options struct {
	outputRoot *os.Root
	httpClient *http.Client
	// downloadConcurrency comes from config download_concurrency (0 = source default).
	downloadConcurrency int
	sourceDir           string
	SourceNames         []string
	Force               bool
	DryRun              bool
	MaxParallel         int
	Timeout             time.Duration
	Clean               bool
	// Locked reproduces the lock: GitHub sources fetch the locked commits, URL
	// sources must match their recorded digests, and config drift fails.
	Locked bool
	// Check reports staleness without writing anything (implies DryRun).
	Check   bool
	OnEvent func(Event) // optional; nil = silent
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

	if opts.Check {
		opts.DryRun = true
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

	if opts.Locked {
		if driftErr := checkLockDrift(cfg, lock, sourceNames, len(opts.SourceNames) == 0); driftErr != nil {
			return nil, driftErr
		}
	}

	emit := opts.OnEvent
	if emit == nil {
		emit = func(Event) {}
	}
	results, err := executeSources(ctx, cfg, opts, sourceNames, lock, emit)
	if err != nil {
		return nil, err
	}

	return finishRun(ctx, cfg, opts, lock, sourceNames, results, emit)
}

// finishRun records results in the lock, persists them (except for dry runs)
// and turns failures, interruption and staleness into errors.
func finishRun(
	ctx context.Context,
	cfg *config.Config,
	opts Options,
	lock *lockfile.LockFile,
	sourceNames []string,
	results map[string]runState,
	emit func(Event),
) (*RunResult, error) {
	errorCount, downloadedCount, deletedCount, skippedCount, firstErr := processResults(
		lock,
		sourceNames,
		results,
		opts.DryRun,
	)
	for _, sourceName := range sourceNames {
		if entry := lock.GetEntry(sourceName); entry != nil && !opts.DryRun && results[sourceName].err == nil {
			entry.ConfigDigest = cfg.Sources[sourceName].Digest()
		}
	}

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
		return runResult, buildSyncFailure(ctx, errorCount, len(sourceNames), firstErr)
	}
	if opts.Check {
		runResult.Stale = staleSources(sourceNames, results)
		if len(runResult.Stale) > 0 {
			return runResult, errs.New(
				errs.Stale,
				fmt.Sprintf("%d source(s) are stale: %s", len(runResult.Stale), strings.Join(runResult.Stale, ", ")),
				errs.WithHint("Run 'dox sync' to update them"),
				errs.WithFields(map[string]any{"stale": runResult.Stale}),
			)
		}
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return runResult, errs.Wrap(
			errs.Interrupted,
			fmt.Errorf("sync interrupted: %w", context.Canceled),
			errs.WithFields(
				map[string]any{fieldFailedSources: errorCount, errs.LegacyCodeField: errs.LegacyDownloadFailed},
			),
		)
	}

	return runResult, nil
}

// staleSources lists sources whose dry run would download or delete files.
func staleSources(sourceNames []string, results map[string]runState) []string {
	stale := []string{}
	for _, sourceName := range sourceNames {
		result := results[sourceName].result
		if result != nil && (result.Downloaded > 0 || result.Deleted > 0) {
			stale = append(stale, sourceName)
		}
	}
	return stale
}

// checkLockDrift fails when the lock cannot reproduce the selected sources:
// a source is missing from the lock or its configuration changed since the
// lock was written, or (for a full sync) the lock names unconfigured sources.
func checkLockDrift(cfg *config.Config, lock *lockfile.LockFile, sourceNames []string, all bool) error {
	var drift []string
	for _, sourceName := range sourceNames {
		entry := lock.GetEntry(sourceName)
		switch {
		case entry == nil:
			drift = append(drift, sourceName+" (not in lock)")
		case entry.ConfigDigest == "":
			drift = append(drift, sourceName+" (lock predates config digests)")
		case entry.ConfigDigest != cfg.Sources[sourceName].Digest():
			drift = append(drift, sourceName+" (config changed)")
		}
	}
	if all {
		for _, lockedName := range slices.Sorted(maps.Keys(lock.Sources)) {
			if _, configured := cfg.Sources[lockedName]; !configured {
				drift = append(drift, lockedName+" (in lock, not in config)")
			}
		}
	}
	if len(drift) == 0 {
		return nil
	}
	return errs.New(
		errs.Config,
		"config does not match the lock: "+strings.Join(drift, ", "),
		errs.WithHint("Run 'dox sync' without --locked to update the lock, then commit it"),
		errs.WithFields(map[string]any{"drift": drift, errs.LegacyCodeField: errs.LegacyLockError}),
	)
}

func buildSyncFailure(ctx context.Context, errorCount, totalSources int, firstErr error) error {
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(firstErr, context.Canceled) {
		return errs.Wrap(
			errs.Interrupted,
			fmt.Errorf("sync interrupted: %w", context.Canceled),
			errs.WithFields(
				map[string]any{fieldFailedSources: errorCount, errs.LegacyCodeField: errs.LegacyDownloadFailed},
			),
		)
	}

	code := errs.Network
	var hint string
	if detail, ok := errors.AsType[*errs.Error](firstErr); ok {
		hint = detail.Hint
		if detail.Code != "" {
			code = detail.Code
		}
	}
	if errorCount < totalSources {
		code = errs.PartialFailure
	}

	return errs.New(
		code,
		fmt.Sprintf("%d source(s) failed during sync", errorCount),
		errs.WithHint(hint),
		errs.WithFields(
			map[string]any{fieldFailedSources: errorCount, errs.LegacyCodeField: errs.LegacyDownloadFailed},
		),
		errs.WithCause(firstErr),
	)
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

func resolveTimeout(cfg *config.Config, opts Options) time.Duration {
	if opts.Timeout > 0 {
		return opts.Timeout
	}
	if cfg != nil {
		return cfg.TimeoutDuration()
	}
	return httpx.DefaultTimeout
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
	timeout := resolveTimeout(cfg, opts)
	perHost := 0
	if cfg != nil {
		perHost = cfg.MaxPerHost
		opts.downloadConcurrency = cfg.DownloadConcurrency
	}
	// One client for the whole run: its pooled transport reuses connections per
	// host, and its limiter bounds concurrent requests to each host.
	httpClient := httpx.NewClientWithOptions(httpx.Options{Timeout: timeout, PerHostLimit: perHost})
	defer httpClient.CloseIdleConnections()
	opts.Timeout = timeout
	opts.httpClient = httpClient
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
			token := resolveToken(cfg, sourceCfg)
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
	if err := manifest.GenerateRoot(ctx, cfg, lock, root); err != nil && ctx.Err() == nil {
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

	sourceCtx, cancel := httpx.SourceContext(ctx, opts.Timeout)
	defer cancel()

	src, newErr := source.NewWithHTTPClient(sourceName, sourceCfg, token, opts.httpClient)
	if newErr != nil {
		state.err = newErr
	} else {
		defer src.Close()
		state.result, state.err = syncPreparedSource(
			sourceCtx,
			src,
			sourceName,
			destinationDir,
			previousLock,
			opts,
			emit,
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

// resolveToken returns the credential for a repo source's host: a
// `tokens.<host>` entry first, then for github.com github_token, GITHUB_TOKEN
// and GH_TOKEN, and for GitHub Enterprise hosts GH_ENTERPRISE_TOKEN.
func resolveToken(cfg *config.Config, sourceCfg config.Source) string {
	if sourceCfg.Repo == "" {
		return ""
	}
	host := strings.ToLower(strings.TrimSpace(sourceCfg.Host))
	if host == "" {
		host = "github.com"
	}
	if token := cfg.Tokens[host]; token != "" {
		return token
	}
	if host != "github.com" {
		if sourceCfg.Type == "github" {
			return os.Getenv("GH_ENTERPRISE_TOKEN")
		}
		return ""
	}
	return resolveGitHubToken(cfg)
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
	sourceName string,
	destinationDir string,
	previousLock *lockfile.LockEntry,
	opts Options,
	emit func(Event),
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
		source.SyncOptions{
			Force:               opts.Force,
			DryRun:              opts.DryRun,
			Locked:              opts.Locked,
			Check:               opts.Check,
			Root:                sourceRoot,
			DownloadConcurrency: opts.downloadConcurrency,
			OnWarning: func(msg string) {
				emit(Event{Kind: EventWarning, Source: sourceName, Warning: msg})
			},
		},
	)
}

func processResults(
	lock *lockfile.LockFile,
	sourceNames []string,
	results map[string]runState,
	dryRun bool,
) (int, int, int, int, error) {
	errorCount := 0
	downloadedCount := 0
	deletedCount := 0
	skippedCount := 0
	var firstErr error

	for _, sourceName := range sourceNames {
		state := results[sourceName]
		if state.err != nil {
			errorCount++
			if firstErr == nil {
				firstErr = state.err
			}
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

	return errorCount, downloadedCount, deletedCount, skippedCount, firstErr
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
