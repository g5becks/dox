package main

import (
	"context"
	"fmt"
	"os"

	"github.com/samber/oops"
	"github.com/urfave/cli/v3"
)

const defaultParallel = 3

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
		Action: notImplementedAction("sync"),
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
		Action: notImplementedAction("list"),
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
		Action: notImplementedAction("clean"),
	}
}

func newInitCommand() *cli.Command {
	return &cli.Command{
		Name:   "init",
		Usage:  "Create a starter dox.toml in the current directory",
		Action: notImplementedAction("init"),
	}
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
