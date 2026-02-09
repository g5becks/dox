package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/manifest"
	"github.com/g5becks/dox/internal/parser"
	"github.com/samber/oops"
	"github.com/urfave/cli/v3"
)

func newOutlineCommand() *cli.Command {
	return &cli.Command{
		Name:      "outline",
		Usage:     "Show file structure (headings, exports)",
		ArgsUsage: "<collection> <file>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Usage:   "Path to config file",
			},
			&cli.BoolFlag{
				Name:  "json",
				Usage: "Output as JSON",
			},
		},
		Action: outlineAction,
	}
}

func outlineAction(_ context.Context, cmd *cli.Command) error {
	const requiredArgs = 2
	if cmd.Args().Len() != requiredArgs {
		return oops.
			Code("INVALID_ARGS").
			Hint("Usage: dox outline <collection> <file>").
			Errorf("expected %d arguments, got %d", requiredArgs, cmd.Args().Len())
	}

	collectionName := cmd.Args().Get(0)
	filePath := cmd.Args().Get(1)

	configPath, err := resolveConfigPath(cmd.String("config"))
	if err != nil {
		return err
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	m, err := manifest.Load(cfg.Output)
	if err != nil {
		return err
	}

	collection, ok := m.Collections[collectionName]
	if !ok {
		return oops.
			Code("COLLECTION_NOT_FOUND").
			With("collection", collectionName).
			Hint("Run 'dox collections' to see available collections").
			Errorf("collection %q not found", collectionName)
	}

	var fileInfo *manifest.FileInfo
	for i := range collection.Files {
		if collection.Files[i].Path == filePath {
			fileInfo = &collection.Files[i]
			break
		}
	}

	if fileInfo == nil {
		return oops.
			Code("FILE_NOT_FOUND").
			With("file", filePath).
			With("collection", collectionName).
			Hint("Run 'dox files' to see available files").
			Errorf("file %q not found in collection %q", filePath, collectionName)
	}

	if cmd.Bool("json") {
		return outputOutlineJSON(fileInfo)
	}

	return outputOutlineText(fileInfo)
}

func outputOutlineJSON(fileInfo *manifest.FileInfo) error {
	data, err := json.MarshalIndent(fileInfo.Outline, "", "  ")
	if err != nil {
		return oops.
			Code("JSON_ERROR").
			Wrapf(err, "encoding outline")
	}

	fmt.Fprintln(os.Stdout, string(data))
	return nil
}

func outputOutlineText(fileInfo *manifest.FileInfo) error {
	fmt.Fprintf(
		os.Stdout,
		"%s (%d lines, %s)\n\n",
		fileInfo.Path,
		fileInfo.Lines,
		formatSize(fileInfo.Size),
	)

	if fileInfo.Outline == nil || fileInfo.Outline.Type == parser.OutlineTypeNone {
		fmt.Fprintln(os.Stdout, "No outline available for text files.")
		fmt.Fprintln(os.Stdout, "Use 'dox cat' to read the full content.")
		return nil
	}

	switch fileInfo.Outline.Type {
	case parser.OutlineTypeHeadings:
		fmt.Fprintln(os.Stdout, "STRUCTURE:")
		for _, h := range fileInfo.Outline.Headings {
			indent := strings.Repeat("  ", h.Level-1)
			fmt.Fprintf(os.Stdout, "%3d  %s%s\n", h.Line, indent, h.Text)
		}

	case parser.OutlineTypeExports:
		fmt.Fprintln(os.Stdout, "EXPORTS:")
		for _, e := range fileInfo.Outline.Exports {
			fmt.Fprintf(os.Stdout, "%3d   %s %s\n", e.Line, e.Type, e.Name)
		}

	case parser.OutlineTypeNone:
		// Already handled above
	}

	return nil
}

func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
