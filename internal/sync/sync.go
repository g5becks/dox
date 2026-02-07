package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	stdsync "sync"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/lockfile"
	"github.com/g5becks/dox/internal/source"
	"github.com/samber/oops"
	"golang.org/x/sync/errgroup"
)

const defaultMaxParallel = 3

type SyncOptions struct {
	SourceNames []string
	Force       bool
	DryRun      bool
	MaxParallel int
	Clean       bool
}

type runState struct {
	result *source.SyncResult
	err    error
}

func Run(ctx context.Context, cfg *config.Config, opts SyncOptions) error {
	if cfg == nil {
		return oops.
			Code("CONFIG_INVALID").
			Errorf("config is required")
	}

	outputDir := resolveOutputRoot(cfg)
	if opts.Clean && !opts.DryRun {
		if err := os.RemoveAll(outputDir); err != nil {
			return oops.
				Code("WRITE_FAILED").
				With("path", outputDir).
				Wrapf(err, "cleaning output directory")
		}
	}

	lock, err := lockfile.Load(outputDir)
	if err != nil {
		return err
	}

	sourceNames, err := resolveSourceNames(cfg.Sources, opts.SourceNames)
	if err != nil {
		return err
	}

	maxParallel := opts.MaxParallel
	if maxParallel <= 0 {
		maxParallel = defaultMaxParallel
	}

	results := make(map[string]runState, len(sourceNames))
	var resultsMu stdsync.Mutex
	token := resolveGitHubToken(cfg)
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(maxParallel)

	for _, sourceName := range sourceNames {
		sourceName := sourceName
		sourceCfg := cfg.Sources[sourceName]
		destinationDir := resolveSourceOutputDir(outputDir, sourceName, sourceCfg)
		previousLock := lock.GetEntry(sourceName)

		group.Go(func() error {
			state := runState{}

			src, err := source.New(sourceName, sourceCfg, token)
			if err != nil {
				state.err = err
			} else {
				state.result, state.err = src.Sync(
					groupCtx,
					destinationDir,
					previousLock,
					source.SyncOptions{
						Force:  opts.Force,
						DryRun: opts.DryRun,
					},
					nil,
				)
			}

			resultsMu.Lock()
			results[sourceName] = state
			resultsMu.Unlock()
			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return oops.Wrapf(err, "waiting for source sync workers")
	}

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

		if !opts.DryRun && state.result.LockEntry != nil {
			lock.SetEntry(sourceName, state.result.LockEntry)
		}
	}

	if !opts.DryRun {
		if err := lock.Save(outputDir); err != nil {
			return err
		}
	}

	printSummary(sourceNames, results, downloadedCount, deletedCount, skippedCount, opts.DryRun)

	if errorCount > 0 {
		return oops.
			Code("DOWNLOAD_FAILED").
			With("failed_sources", errorCount).
			Errorf("%d source(s) failed during sync", errorCount)
	}

	return nil
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

func printSummary(
	sourceNames []string,
	results map[string]runState,
	downloadedCount int,
	deletedCount int,
	skippedCount int,
	dryRun bool,
) {
	summaryLabel := "sync summary"
	if dryRun {
		summaryLabel = "dry-run summary"
	}

	fmt.Printf(
		"%s: sources=%d downloaded=%d deleted=%d skipped=%d\n",
		summaryLabel,
		len(sourceNames),
		downloadedCount,
		deletedCount,
		skippedCount,
	)
	if dryRun {
		fmt.Println("dry-run: no files were written or removed")
	}

	for _, sourceName := range sourceNames {
		state := results[sourceName]
		if state.err != nil {
			fmt.Printf("%s: failed (%v)\n", sourceName, state.err)
			continue
		}

		if state.result == nil {
			fmt.Printf("%s: no result\n", sourceName)
			continue
		}

		switch {
		case state.result.Skipped:
			fmt.Printf("%s: skipped\n", sourceName)
		default:
			fmt.Printf(
				"%s: synced (downloaded=%d deleted=%d)\n",
				sourceName,
				state.result.Downloaded,
				state.result.Deleted,
			)
		}
	}
}
