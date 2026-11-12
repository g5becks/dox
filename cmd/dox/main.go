package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/httpx"
	"github.com/g5becks/dox/internal/lockfile"
	doxsync "github.com/g5becks/dox/internal/sync"
	"github.com/g5becks/dox/internal/ui"
)

const initTemplate = `# dox.toml - Documentation source configuration
# Docs: https://github.com/g5becks/dox

# ============================================================================
# GLOBAL SETTINGS
# ============================================================================

# Directory below this config where docs will be downloaded (relative path)
# Use a dedicated directory, never ".", "..", or a filesystem root.
# A separate external directory requires: dox --allow-external-output <command>
# Default: ".dox"
# output = ".dox"

# GitHub token for private repos and higher API rate limits (5000/hr vs 60/hr).
# Public syncs need few API calls (2 per GitHub directory source; files come
# from raw/archive URLs), so most setups work without one.
# Can also be set via GITHUB_TOKEN or GH_TOKEN environment variable
# github_token = ""

# Per-host tokens, e.g. for GitHub Enterprise Server (or GH_ENTERPRISE_TOKEN)
# tokens = { "github.mycorp.com" = "" }

# Max parallel source syncs, 1-100 (default: 4x CPU cores, min 10, max 16)
# Override per-command with: dox sync --parallel N
# max_parallel = 20
#
# Max concurrent HTTP requests to one host across all sources (default: 16)
# max_per_host = 16
#
# Max concurrent file downloads within one GitHub source (default: 8)
# download_concurrency = 8

# Per-source sync and HTTP request timeout (default: "30s")
# Override per-command with: dox sync --timeout 30s
# timeout = "30s"

# ============================================================================
# GLOBAL EXCLUDES (applied to all git hosting sources)
# ============================================================================
# These patterns are merged with per-source excludes (duplicates removed)
excludes = [
    # Hidden/config directories
    ".vitepress/**",
    ".github/**",
    ".git/**",
    ".idea/**",
    ".vscode/**",

    # Dependencies
    "node_modules/**",
    "vendor/**",

    # Images (common formats)
    "**/*.png",
    "**/*.jpg",
    "**/*.jpeg",
    "**/*.gif",
    "**/*.svg",
    "**/*.ico",
    "**/*.webp",

    # Build artifacts
    "dist/**",
    "build/**",
    ".next/**",
    ".nuxt/**",
    "out/**",

    # Compiled/binary files
    "**/*.wasm",
    "**/*.so",
    "**/*.dylib",
    "**/*.dll",
    "**/*.exe",

    # Archives
    "**/*.zip",
    "**/*.tar",
    "**/*.gz",
    "**/*.tgz",

    # Media
    "**/*.mp4",
    "**/*.mov",
    "**/*.avi",
    "**/*.mp3",
    "**/*.wav",

    # Fonts
    "**/*.woff",
    "**/*.woff2",
    "**/*.ttf",
    "**/*.eot",
    "**/*.otf",
]

# ============================================================================
# QUERY COMMAND DEFAULTS (used by: dox collections, dox files, dox cat, dox outline)
# ============================================================================
# [display]
# Number of files to show in 'dox files' (0 = all)
# default_limit = 50
#
# Maximum length for file descriptions
# description_length = 200
#
# Show line numbers in 'dox cat' by default
# line_numbers = true
#
# Default output format: table, json, csv
# format = "table"
#
# Default fields for 'dox files' table
# list_fields = ["path", "type", "lines", "size", "description"]

# ============================================================================
# SOURCES - Type is inferred from 'repo' or 'url' presence
# ============================================================================

# --- GitHub (default) - type inferred from 'repo', host defaults to github.com ---
# [sources.my-library]
# repo = "owner/repo"
# path = "docs"
# ref = "main"                                       # optional (default: repo default branch)
# patterns = ["**/*.md", "**/*.mdx", "**/*.txt"]     # optional (these are the defaults)
# exclude = ["custom-pattern/**"]                    # optional (adds to global excludes, no duplicates)
# out = "custom-dir-name"                             # optional (default: source key name)

# --- GitHub Enterprise Server - set host (type "github" if the name lacks "github") ---
# [sources.internal-docs]
# type = "github"
# host = "git.mycorp.com"
# repo = "platform/handbook"
# path = "docs"

# --- GitLab (gitlab.com or self-managed; token: tokens.<host> or GITLAB_TOKEN) ---
# [sources.gitlab-project]
# repo = "group/subgroup/project"
# host = "gitlab.com"                                # hosts containing "gitlab" infer type gitlab
# path = "docs"

# --- Gitea, Forgejo, Codeberg (token: tokens.<host> or GITEA_TOKEN) ---
# [sources.codeberg-docs]
# repo = "owner/repo"
# host = "codeberg.org"                              # codeberg/gitea/forgejo hosts infer type gitea
# path = "docs"

# --- Direct URL download - type inferred from 'url' ---
# [sources.my-framework]
# url = "https://example.com/llms-full.txt"
# filename = "my-framework.txt"                       # optional (default: basename from URL)
# out = "custom-dir"                                  # optional (default: source key name)
`

//nolint:gochecknoglobals // Build metadata is injected at build time with ldflags.
var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args)
	stop()
	if err != nil {
		os.Exit(renderError(os.Stderr, err, os.Args))
	}
}

func run(ctx context.Context, args []string) error {
	return newRootCommand().Run(ctx, args)
}

func newRootCommand() *cli.Command {
	root := &cli.Command{
		Name:        "dox",
		Usage:       "Sync framework and library docs to local files",
		Description: rootDescription,
		Version:     versionString(),
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  flagAllowExternalOutput,
				Usage: "Allow output outside the config directory (never the config directory or filesystem root)",
			},
			&cli.BoolFlag{
				Name:  formatJSON,
				Usage: helpJSON,
			},
			&cli.BoolFlag{
				Name:  flagVerbose,
				Usage: "Show error context fields and cause chain",
			},
		},
		Commands: []*cli.Command{
			newSyncCommand(),
			newListCommand(),
			newAddCommand(),
			newCleanCommand(),
			newInitCommand(),
			newCollectionsCommand(),
			newFilesCommand(),
			newCatCommand(),
			newOutlineCommand(),
			newSearchCommand(),
			newVersionCommand(),
		},
	}
	attachUsageHandlers(root)
	return root
}

func newVersionCommand() *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "Print the version",
		Action: func(_ context.Context, cmd *cli.Command) error {
			cli.ShowVersion(cmd.Root())
			return nil
		},
	}
}

func newSyncCommand() *cli.Command {
	return &cli.Command{
		Name:      "sync",
		Usage:     "Sync configured documentation sources",
		ArgsUsage: "[source-name...]",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagConfig, Aliases: []string{"c"}, Usage: helpConfig},
			&cli.BoolFlag{
				Name:    "force",
				Aliases: []string{"f"},
				Usage:   "Force refresh and skip freshness checks",
			},
			&cli.BoolFlag{Name: "clean", Usage: "Clear owned output contents before syncing"},
			&cli.BoolFlag{Name: "dry-run", Usage: "Show planned changes without writing files"},
			&cli.BoolFlag{
				Name:  "locked",
				Usage: "Reproduce the lock: fetch locked commits and verify URL digests; fail on config drift",
			},
			&cli.BoolFlag{
				Name:  "check",
				Usage: "Exit 3 if any source is stale versus upstream; writes nothing",
			},
			&cli.IntFlag{
				Name: "parallel", Aliases: []string{"p"},
				Usage: "Maximum parallel source syncs (0 uses config or computed default)",
			},
			&cli.StringFlag{
				Name:  "timeout",
				Usage: "Per-source sync and request timeout (for example, 30s; overrides config timeout)",
			},
		},
		Action: syncAction,
	}
}

func newListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List configured sources and status",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: formatJSON, Usage: "Emit JSON output"},
			&cli.BoolFlag{
				Name:    "verbose",
				Aliases: []string{"v"},
				Usage:   "Show expanded source fields",
			},
			&cli.BoolFlag{
				Name:  "files",
				Usage: "Include file counts from local output directories",
			},
			&cli.StringFlag{Name: flagConfig, Aliases: []string{"c"}, Usage: helpConfig},
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
			&cli.StringFlag{
				Name:    fieldType,
				Aliases: []string{"t"},
				Usage:   "Source type: github or url (default: inferred from --repo or --url)",
			},
			&cli.StringFlag{Name: "repo", Aliases: []string{"r"}, Usage: "Repository in owner/repo format"},
			&cli.StringFlag{Name: "host", Usage: "Git host (only github.com is supported until v0.5.0)"},
			&cli.StringFlag{Name: fieldPath, Aliases: []string{"p"}, Usage: "Path within repository"},
			&cli.StringFlag{Name: "ref", Aliases: []string{"R"}, Usage: "Branch, tag, or commit SHA"},
			&cli.StringSliceFlag{Name: "patterns", Usage: "Include glob pattern (repeatable)"},
			&cli.StringSliceFlag{Name: "exclude", Usage: "Exclude glob pattern (repeatable)"},
			&cli.StringFlag{Name: fieldURL, Aliases: []string{"u"}, Usage: "Source URL"},
			&cli.StringFlag{Name: "filename", Usage: "Output filename for URL source"},
			&cli.StringFlag{Name: "out", Usage: "Custom output subdirectory"},
			&cli.StringFlag{Name: flagConfig, Aliases: []string{"c"}, Usage: helpConfig},
			&cli.BoolFlag{Name: "force", Usage: "Overwrite an existing source with the same name"},
		},
		Action: addAction,
	}
}

func newCleanCommand() *cli.Command {
	return &cli.Command{
		Name:      "clean",
		Usage:     "Remove downloaded documentation output",
		ArgsUsage: "[source-name...]",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagConfig, Aliases: []string{"c"}, Usage: helpConfig},
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
	cfg, err := loadCommandConfig(cmd, cmd.String(flagConfig))
	if err != nil {
		return err
	}

	var timeout time.Duration
	if rawTimeout := cmd.String("timeout"); rawTimeout != "" {
		parsedTimeout, parseErr := httpx.ParseTimeout(errs.Usage, rawTimeout)
		if parseErr != nil {
			return parseErr
		}
		timeout = parsedTimeout
	}

	check, locked := cmd.Bool("check"), cmd.Bool("locked")
	if conflictErr := syncFlagConflicts(cmd, check, locked); conflictErr != nil {
		return conflictErr
	}

	var onEvent func(doxsync.Event)
	var printer *ui.SyncPrinter
	if !jsonEnabled(cmd) {
		printer = ui.NewSyncPrinter(cmd.Bool("dry-run") || check)
		onEvent = printer.HandleEvent
	}

	result, runErr := doxsync.Run(ctx, cfg, doxsync.Options{
		SourceNames: commandArgs(cmd),
		Force:       cmd.Bool("force"),
		DryRun:      cmd.Bool("dry-run"),
		MaxParallel: cmd.Int("parallel"),
		Timeout:     timeout,
		Clean:       cmd.Bool("clean"),
		Locked:      locked,
		Check:       check,
		OnEvent:     onEvent,
	})

	if printer != nil {
		printer.PrintSummary(result)
	}
	if check && jsonEnabled(cmd) && result != nil {
		if writeErr := writeCheckJSON(cfg, commandArgs(cmd), result); writeErr != nil {
			return writeErr
		}
	}

	return runErr
}

// syncFlagConflicts rejects combinations whose meaning would be ambiguous.
func syncFlagConflicts(cmd *cli.Command, check, locked bool) error {
	conflict := ""
	switch {
	case check && locked:
		conflict = "--check and --locked"
	case check && cmd.Bool("clean"):
		conflict = "--check and --clean"
	case check && cmd.Bool("force"):
		conflict = "--check and --force"
	case locked && cmd.Bool("clean"):
		conflict = "--locked and --clean"
	}
	if conflict == "" {
		return nil
	}
	return errs.New(
		errs.Usage,
		conflict+" cannot be combined",
		errs.WithHint("--check only reports staleness; --locked reproduces the lock"),
		errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyInvalidArgs}),
	)
}

// writeCheckJSON prints {"stale": [...], "current": [...]} for sync --check.
func writeCheckJSON(cfg *config.Config, selected []string, result *doxsync.RunResult) error {
	names := selected
	if len(names) == 0 {
		names = slices.Sorted(maps.Keys(cfg.Sources))
	}
	current := []string{}
	for _, name := range names {
		if !slices.Contains(result.Stale, name) {
			current = append(current, name)
		}
	}
	stale := result.Stale
	if stale == nil {
		stale = []string{}
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string][]string{"stale": stale, "current": current})
}

func commandArgs(cmd *cli.Command) []string {
	args := make([]string, 0, cmd.Args().Len())
	for i := range cmd.Args().Len() {
		args = append(args, cmd.Args().Get(i))
	}

	return args
}

func listAction(_ context.Context, cmd *cli.Command) error {
	cfg, err := loadCommandConfig(cmd, cmd.String(flagConfig))
	if err != nil {
		return err
	}

	paths, pathErr := cfg.Paths()
	if pathErr != nil {
		return pathErr
	}
	lock, err := lockfile.Load(paths.Output())
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
		sourceOutput, sourceErr := paths.Source(sourceName, sourceCfg)
		if sourceErr != nil {
			return sourceErr
		}
		status := ui.SourceStatus{
			Name:      sourceName,
			Type:      sourceCfg.Type,
			Repo:      sourceCfg.Repo,
			Path:      sourceCfg.Path,
			URL:       sourceCfg.URL,
			Ref:       sourceCfg.Ref,
			Patterns:  sourceCfg.Patterns,
			OutputDir: sourceOutput,
			Status:    "not synced",
		}

		lockEntry := lock.GetEntry(sourceName)
		if lockEntry != nil {
			status.Status = "synced"
			status.SyncedAt = lockEntry.SyncedAt
			status.FileCount = len(lockEntry.Files)
			if lockEntry.Type == fieldURL && status.FileCount == 0 {
				status.FileCount = 1
			}
		}

		if includeFiles {
			fileCount, countErr := countFiles(status.OutputDir)
			if countErr != nil {
				return countErr
			}
			status.FileCount = fileCount
		}

		statuses = append(statuses, status)
	}

	return ui.RenderSourceList(statuses, ui.ListOptions{
		JSON:    jsonEnabled(cmd),
		Verbose: verboseEnabled(cmd),
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

		return 0, errs.Wrap(
			errs.Internal,
			fmt.Errorf("counting files: %w", err),
			errs.WithFields(map[string]any{fieldPath: root, errs.LegacyCodeField: errs.LegacyWriteFailed}),
		)
	}

	return count, nil
}

func initAction(_ context.Context, _ *cli.Command) error {
	for _, configName := range []string{"dox.toml", ".dox.toml"} {
		if _, err := os.Stat(configName); err == nil {
			return errs.New(
				errs.Config,
				fmt.Sprintf("config file already exists at %q", configName),
				errs.WithHint("Remove the existing file or edit it directly"),
				errs.WithFields(
					map[string]any{fieldPath: configName, errs.LegacyCodeField: errs.LegacyConfigWriteError},
				),
			)
		} else if !os.IsNotExist(err) {
			return errs.Wrap(
				errs.Config,
				fmt.Errorf("checking config path: %w", err),
				errs.WithFields(
					map[string]any{fieldPath: configName, errs.LegacyCodeField: errs.LegacyConfigWriteError},
				),
			)
		}
	}

	if err := os.WriteFile("dox.toml", []byte(initTemplate), 0o600); err != nil {
		return errs.Wrap(
			errs.Config,
			fmt.Errorf("writing starter config: %w", err),
			errs.WithFields(map[string]any{fieldPath: "dox.toml", errs.LegacyCodeField: errs.LegacyConfigWriteError}),
		)
	}

	return nil
}

func addAction(_ context.Context, cmd *cli.Command) error {
	sourceName := strings.TrimSpace(cmd.Args().First())
	if sourceName == "" {
		return errs.New(
			errs.Config,
			"source name is required",
			errs.WithHint("Usage: dox add <name> --repo owner/repo --path docs | --url https://..."),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyConfigInvalid}),
		)
	}
	if nameErr := validateAddSourceName(sourceName); nameErr != nil {
		return nameErr
	}

	configPath, err := resolveConfigPath(cmd.String(flagConfig))
	if err != nil {
		return err
	}

	cfg, err := loadCommandConfig(cmd, configPath)
	if err != nil {
		return err
	}

	_, exists := cfg.Sources[sourceName]
	if exists && !cmd.Bool("force") {
		return errs.New(
			errs.Usage,
			fmt.Sprintf("source %q already exists", sourceName),
			errs.WithHint("Use --force to overwrite the existing source definition"),
			errs.WithFields(map[string]any{fieldSource: sourceName, errs.LegacyCodeField: errs.LegacySourceExists}),
		)
	}

	newSource := config.Source{
		Type:     cmd.String(fieldType),
		Repo:     cmd.String("repo"),
		Host:     cmd.String("host"),
		Path:     cmd.String(fieldPath),
		Ref:      cmd.String("ref"),
		Patterns: cmd.StringSlice("patterns"),
		Exclude:  cmd.StringSlice("exclude"),
		URL:      cmd.String(fieldURL),
		Filename: cmd.String("filename"),
		Out:      cmd.String("out"),
	}

	validationConfig := &config.Config{
		Sources: map[string]config.Source{
			sourceName: newSource,
		},
	}
	validationConfig.ApplyDefaults()
	if valErr := validationConfig.Validate(); valErr != nil {
		return valErr
	}
	// Persist only the inferred type; other defaults (patterns, host) stay
	// implicit so later default changes still apply to this source.
	newSource.Type = validationConfig.Sources[sourceName].Type

	section := renderSourceSection(sourceName, newSource)
	fileData, err := os.ReadFile(configPath)
	if err != nil {
		return errs.Wrap(
			errs.Config,
			fmt.Errorf("reading config file: %w", err),
			errs.WithFields(map[string]any{fieldPath: configPath, errs.LegacyCodeField: errs.LegacyConfigWriteError}),
		)
	}

	fileContent := string(fileData)
	var updatedContent string
	if exists {
		updatedContent, err = replaceSourceSection(fileContent, sourceName, section)
		if err != nil {
			return err
		}
	} else {
		updatedContent = appendSourceSection(fileContent, section)
	}

	if writeErr := writeConfig(configPath, updatedContent); writeErr != nil {
		return errs.Wrap(
			errs.Config,
			fmt.Errorf("writing config file: %w", writeErr),
			errs.WithFields(map[string]any{fieldPath: configPath, errs.LegacyCodeField: errs.LegacyConfigWriteError}),
		)
	}

	return nil
}

func resolveConfigPath(configPath string) (string, error) {
	if configPath != "" {
		if _, err := os.Stat(configPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return "", errs.New(
					errs.NotFound,
					fmt.Sprintf("config file %q does not exist", configPath),
					errs.WithHint("Create the file first or pass a valid --config path"),
					errs.WithFields(
						map[string]any{fieldPath: configPath, errs.LegacyCodeField: errs.LegacyConfigNotFound},
					),
				)
			}

			return "", fmt.Errorf("checking config file %q: %w", configPath, err)
		}

		return configPath, nil
	}

	return config.FindConfigFile()
}

func cleanAction(_ context.Context, cmd *cli.Command) error {
	cfg, err := loadCommandConfig(cmd, cmd.String(flagConfig))
	if err != nil {
		return err
	}
	paths, err := cfg.Paths()
	if err != nil {
		return err
	}
	root, err := paths.OpenOutput(false)
	if err != nil {
		return err
	}
	defer root.Close()
	if requireOwnedErr := config.RequireOwned(root); requireOwnedErr != nil {
		return requireOwnedErr
	}
	selected := commandArgs(cmd)
	if len(selected) == 0 {
		return config.CleanOutput(root)
	}
	lock, err := lockfile.Load(paths.Output())
	if err != nil {
		return err
	}
	// Validate the complete selection before deleting any collection.
	directories := make([]string, len(selected))
	wholeOutput := false
	for index, name := range selected {
		sourceCfg, ok := cfg.Sources[name]
		if !ok {
			return errs.New(
				errs.NotFound,
				fmt.Sprintf("source %q not found in config", name),
				errs.WithHint("Run 'dox list' to view configured sources"),
				errs.WithCause(errs.ErrSourceNotFound),
			)
		}
		directory, pathErr := paths.SourceDir(name, sourceCfg)
		if pathErr != nil {
			return pathErr
		}
		if directory == "." {
			wholeOutput = true
		}
		directories[index] = directory
	}
	if wholeOutput {
		return config.CleanOutput(root)
	}
	for index, directory := range directories {
		if removeAllErr := root.RemoveAll(directory); removeAllErr != nil {
			return fmt.Errorf("removing source output directory: %w", removeAllErr)
		}
		lock.RemoveEntry(selected[index])
	}
	return lock.SaveRoot(root)
}

func versionString() string {
	return fmt.Sprintf("%s (commit %s, built %s)", version, commit, buildTime)
}

func loadCommandConfig(cmd *cli.Command, path string) (*config.Config, error) {
	return config.LoadWithOptions(path, config.LoadOptions{AllowExternalOutput: cmd.Bool(flagAllowExternalOutput)})
}
