package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/manifest"
	"github.com/g5becks/dox/internal/parser"
)

func newOutlineCommand() *cli.Command {
	return &cli.Command{
		Name:      "outline",
		Usage:     "Show file structure (headings, exports)",
		ArgsUsage: "<collection> <file>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    flagConfig,
				Aliases: []string{"c"},
				Usage:   helpConfig,
			},
			&cli.BoolFlag{
				Name:  formatJSON,
				Usage: helpJSON,
			},
		},
		Action: outlineAction,
	}
}

func outlineAction(_ context.Context, cmd *cli.Command) error {
	const requiredArgs = 2
	if cmd.Args().Len() != requiredArgs {
		return errs.New(
			errs.Usage,
			fmt.Sprintf("expected %d arguments, got %d", requiredArgs, cmd.Args().Len()),
			errs.WithHint("Usage: dox outline <collection> <file>"),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyInvalidArgs}),
		)
	}

	collectionName := cmd.Args().Get(0)
	filePath := cmd.Args().Get(1)

	configPath, err := resolveConfigPath(cmd.String(flagConfig))
	if err != nil {
		return err
	}

	cfg, err := loadCommandConfig(cmd, configPath)
	if err != nil {
		return err
	}

	m, err := manifest.Load(cfg.Output)
	if err != nil {
		return err
	}

	collection, ok := m.Collections[collectionName]
	if !ok {
		return errs.New(
			errs.NotFound,
			fmt.Sprintf("collection %q not found", collectionName),
			errs.WithHint("Run 'dox collections' to see available collections"),
			errs.WithFields(
				map[string]any{fieldCollection: collectionName, errs.LegacyCodeField: errs.LegacyCollectionNotFound},
			),
		)
	}

	var fileInfo *manifest.FileInfo
	for i := range collection.Files {
		if collection.Files[i].Path == filePath {
			fileInfo = &collection.Files[i]
			break
		}
	}

	if fileInfo == nil {
		return errs.New(
			errs.NotFound,
			fmt.Sprintf("file %q not found in collection %q", filePath, collectionName),
			errs.WithHint("Run 'dox files' to see available files"),
			errs.WithFields(
				map[string]any{
					"file":               filePath,
					fieldCollection:      collectionName,
					errs.LegacyCodeField: errs.LegacyFileNotFound,
				},
			),
		)
	}

	if cmd.Bool(formatJSON) {
		return outputOutlineJSON(fileInfo)
	}

	return outputOutlineText(fileInfo)
}

func outputOutlineJSON(fileInfo *manifest.FileInfo) error {
	data, err := json.MarshalIndent(fileInfo.Outline, "", "  ")
	if err != nil {
		return errs.Wrap(
			errs.Internal,
			fmt.Errorf("encoding outline: %w", err),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyJSONError}),
		)
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
