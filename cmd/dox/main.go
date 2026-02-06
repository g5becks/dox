package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/lockfile"
	doxsync "github.com/g5becks/dox/internal/sync"
	"github.com/g5becks/dox/internal/ui"
	"github.com/samber/oops"
	"github.com/urfave/cli/v3"
)

const defaultParallel = 3

const initTemplate = `# dox.toml - Documentation source configuration
# Docs: https://github.com/g5becks/dox

# Directory where docs will be downloaded (relative to this file)
# Default: ".dox"
# output = ".dox"

# GitHub token for higher API rate limits (5000/hr vs 60/hr unauthenticated)
# Can also be set via GITHUB_TOKEN or GH_TOKEN environment variable
# github_token = ""

# --- Example: Download docs from a GitHub repo directory ---
# [sources.my-library]
# type = "github"
# repo = "owner/repo"
# path = "docs"
# ref = "main"                                       # optional (default: repo default branch)
# patterns = ["**/*.md", "**/*.mdx", "**/*.txt"]     # optional (these are the defaults)
# exclude = ["**/changelog.md"]                       # optional
# out = "custom-dir-name"                             # optional (default: source key name)

# --- Example: Download a single file from a URL ---
# [sources.my-framework]
# type = "url"
# url = "https://example.com/llms-full.txt"
# filename = "my-framework.txt"                       # optional (default: basename from URL)
`

var (
	//nolint:gochecknoglobals // Build metadata is injected at build time with ldflags.
	version = "dev"
	//nolint:gochecknoglobals // Build metadata is injected at build time with ldflags.
	commit = "unknown"
	//nolint:gochecknoglobals // Build metadata is injected at build time with ldflags.
	buildTime = "unknown"
)

func main() {
	if err := run(os.Args); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	return newRootCommand().Run(context.Background(), args)
}

func newRootCommand() *cli.Command {
	return &cli.Command{
		Name:    "dox",
		Usage:   "Sync framework and library docs to local files",
		Version: versionString(),
		Commands: []*cli.Command{
			newSyncCommand(),
			newListCommand(),
			newAddCommand(),
			newCleanCommand(),
			newInitCommand(),
		},
	}
}

func newSyncCommand() *cli.Command {
	return &cli.Command{
		Name:      "sync",
		Usage:     "Sync configured documentation sources",
		ArgsUsage: "[source-name...]",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config", Aliases: []string{"c"}, Usage: "Path to config file"},
			&cli.BoolFlag{Name: "force", Aliases: []string{"f"}, Usage: "Force refresh and skip freshness checks"},
			&cli.BoolFlag{Name: "clean", Usage: "Delete output directory before syncing"},
			&cli.BoolFlag{Name: "dry-run", Usage: "Show planned changes without writing files"},
			&cli.IntFlag{Name: "parallel", Aliases: []string{"p"}, Usage: "Maximum parallel source syncs", Value: defaultParallel},
		},
		Action: syncAction,
	}
}

func newListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List configured sources and status",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "json", Usage: "Emit JSON output"},
			&cli.BoolFlag{Name: "verbose", Aliases: []string{"v"}, Usage: "Show expanded source fields"},
			&cli.BoolFlag{Name: "files", Usage: "Include file counts from local output directories"},
			&cli.StringFlag{Name: "config", Aliases: []string{"c"}, Usage: "Path to config file"},
		},
		Action: listAction,
	}
}

func newAddCommand() *cli.Command {
	return &cli.Command{
		Name:      "add",
		Usage:     "Add a source definition to the config file",
		ArgsUsage: "<name>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "type", Aliases: []string{"t"}, Usage: "Source type: github or url", Required: true},
			&cli.StringFlag{Name: "repo", Usage: "Repository in owner/repo format"},
			&cli.StringFlag{Name: "path", Usage: "Path within repository"},
			&cli.StringFlag{Name: "ref", Usage: "Branch, tag, or commit SHA"},
			&cli.StringSliceFlag{Name: "patterns", Usage: "Include glob pattern (repeatable)"},
			&cli.StringSliceFlag{Name: "exclude", Usage: "Exclude glob pattern (repeatable)"},
			&cli.StringFlag{Name: "url", Usage: "Source URL"},
			&cli.StringFlag{Name: "filename", Usage: "Output filename for URL source"},
			&cli.StringFlag{Name: "out", Usage: "Custom output subdirectory"},
			&cli.StringFlag{Name: "config", Aliases: []string{"c"}, Usage: "Path to config file"},
			&cli.BoolFlag{Name: "force", Usage: "Overwrite an existing source with the same name"},
		},
		Action: notImplementedAction("add"),
	}
}

func newCleanCommand() *cli.Command {
	return &cli.Command{
		Name:      "clean",
		Usage:     "Remove downloaded documentation output",
		ArgsUsage: "[source-name...]",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config", Aliases: []string{"c"}, Usage: "Path to config file"},
		},
		Action: cleanAction,
	}
}

func newInitCommand() *cli.Command {
	return &cli.Command{
		Name:   "init",
		Usage:  "Create a starter dox.toml in the current directory",
		Action: initAction,
	}
}

func syncAction(ctx context.Context, cmd *cli.Command) error {
	cfg, err := config.Load(cmd.String("config"))
	if err != nil {
		return err
	}

	return doxsync.Run(ctx, cfg, doxsync.SyncOptions{
		SourceNames: commandArgs(cmd),
		Force:       cmd.Bool("force"),
		DryRun:      cmd.Bool("dry-run"),
		MaxParallel: cmd.Int("parallel"),
		Clean:       cmd.Bool("clean"),
	})
}

func commandArgs(cmd *cli.Command) []string {
	args := make([]string, 0, cmd.Args().Len())
	for i := 0; i < cmd.Args().Len(); i++ {
		args = append(args, cmd.Args().Get(i))
	}

	return args
}

func listAction(_ context.Context, cmd *cli.Command) error {
	cfg, err := config.Load(cmd.String("config"))
	if err != nil {
		return err
	}

	lock, err := lockfile.Load(resolveOutputRoot(cfg))
	if err != nil {
		return err
	}

	sourceNames := make([]string, 0, len(cfg.Sources))
	for sourceName := range cfg.Sources {
		sourceNames = append(sourceNames, sourceName)
	}
	slices.Sort(sourceNames)

	includeFiles := cmd.Bool("files")
	statuses := make([]ui.SourceStatus, 0, len(sourceNames))
	for _, sourceName := range sourceNames {
		sourceCfg := cfg.Sources[sourceName]
		status := ui.SourceStatus{
			Name:      sourceName,
			Type:      sourceCfg.Type,
			Repo:      sourceCfg.Repo,
			Path:      sourceCfg.Path,
			URL:       sourceCfg.URL,
			Ref:       sourceCfg.Ref,
			Patterns:  sourceCfg.Patterns,
			OutputDir: cfg.OutputDir(sourceName, sourceCfg),
			Status:    "not synced",
		}

		lockEntry := lock.GetEntry(sourceName)
		if lockEntry != nil {
			status.Status = "synced"
			status.SyncedAt = lockEntry.SyncedAt
			status.FileCount = len(lockEntry.Files)
			if lockEntry.Type == "url" && status.FileCount == 0 {
				status.FileCount = 1
			}
		}

		if includeFiles {
			fileCount, err := countFiles(status.OutputDir)
			if err != nil {
				return err
			}
			status.FileCount = fileCount
		}

		statuses = append(statuses, status)
	}

	return ui.RenderSourceList(statuses, ui.ListOptions{
		JSON:    cmd.Bool("json"),
		Verbose: cmd.Bool("verbose"),
		Files:   includeFiles,
	})
}

func countFiles(root string) (int, error) {
	count := 0

	err := filepath.WalkDir(root, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			return nil
		}

		count++
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}

		return 0, oops.
			Code("WRITE_FAILED").
			With("path", root).
			Wrapf(err, "counting files")
	}

	return count, nil
}

func resolveOutputRoot(cfg *config.Config) string {
	if filepath.IsAbs(cfg.Output) {
		return cfg.Output
	}

	return filepath.Join(cfg.ConfigDir, cfg.Output)
}

func initAction(_ context.Context, _ *cli.Command) error {
	for _, configName := range []string{"dox.toml", ".dox.toml"} {
		if _, err := os.Stat(configName); err == nil {
			return oops.
				Code("CONFIG_WRITE_ERROR").
				With("path", configName).
				Hint("Remove the existing file or edit it directly").
				Errorf("config file already exists at %q", configName)
		} else if !os.IsNotExist(err) {
			return oops.
				Code("CONFIG_WRITE_ERROR").
				With("path", configName).
				Wrapf(err, "checking config path")
		}
	}

	if err := os.WriteFile("dox.toml", []byte(initTemplate), 0o644); err != nil {
		return oops.
			Code("CONFIG_WRITE_ERROR").
			With("path", "dox.toml").
			Wrapf(err, "writing starter config")
	}

	return nil
}

func cleanAction(_ context.Context, cmd *cli.Command) error {
	cfg, err := config.Load(cmd.String("config"))
	if err != nil {
		return err
	}

	selectedSources := commandArgs(cmd)
	outputDir := resolveOutputRoot(cfg)
	if len(selectedSources) == 0 {
		if err := os.RemoveAll(outputDir); err != nil {
			return oops.
				Code("WRITE_FAILED").
				With("path", outputDir).
				Wrapf(err, "removing output directory")
		}

		return nil
	}

	lock, err := lockfile.Load(outputDir)
	if err != nil {
		return err
	}

	for _, sourceName := range selectedSources {
		sourceCfg, ok := cfg.Sources[sourceName]
		if !ok {
			return oops.
				Code("SOURCE_NOT_FOUND").
				With("source", sourceName).
				Hint("Run 'dox list' to view configured sources").
				Errorf("source %q not found in config", sourceName)
		}

		if err := os.RemoveAll(cfg.OutputDir(sourceName, sourceCfg)); err != nil {
			return oops.
				Code("WRITE_FAILED").
				With("path", cfg.OutputDir(sourceName, sourceCfg)).
				With("source", sourceName).
				Wrapf(err, "removing source output directory")
		}

		lock.RemoveEntry(sourceName)
	}

	return lock.Save(outputDir)
}

func notImplementedAction(commandName string) cli.ActionFunc {
	return func(_ context.Context, _ *cli.Command) error {
		return oops.
			Code("NOT_IMPLEMENTED").
			With("command", commandName).
			Hint("Follow PLAN.md implementation order to wire this command").
			Errorf("%s command is not implemented yet", commandName)
	}
}

func versionString() string {
	return fmt.Sprintf("%s (commit %s, built %s)", version, commit, buildTime)
}
