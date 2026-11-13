package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/lookup"
	"github.com/g5becks/dox/internal/manifest"
	"github.com/g5becks/dox/internal/parser"
)

func newOutlineCommand() *cli.Command {
	return &cli.Command{
		Name:      "outline",
		Usage:     "Show file structure (headings, exports)",
		ArgsUsage: "<collection> <file>",
		Flags:     formatFlags(),
		Action:    outlineAction,
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
	format, err := resolveOutputFormat(cmd, cfg)
	if err != nil {
		return err
	}

	m, err := manifest.Load(cfg.Output)
	if err != nil {
		return err
	}

	collection, err := lookup.Collection(m, collectionName)
	if err != nil {
		return err
	}

	fileInfo, err := lookup.File(collection, collectionName, filePath)
	if err != nil {
		return err
	}

	switch format {
	case formatJSON:
		return writeJSONValue(fileInfo.Outline)
	case formatJSONL:
		return writeJSONLines(outlineEntries(fileInfo.Outline))
	case formatCSV:
		return outputOutlineCSV(outlineEntries(fileInfo.Outline))
	default:
		return outputOutlineText(fileInfo)
	}
}

// outlineEntry is one heading or export of an outline, flattened for the jsonl
// and csv formats.
type outlineEntry struct {
	Kind  string `json:"kind"`
	Line  int    `json:"line"`
	Level int    `json:"level,omitempty"`
	Type  string `json:"type,omitempty"`
	Text  string `json:"text"`
}

func outlineEntries(outline *parser.Outline) []outlineEntry {
	if outline == nil {
		return nil
	}
	entries := make([]outlineEntry, 0, len(outline.Headings)+len(outline.Exports))
	for _, heading := range outline.Headings {
		entries = append(entries, outlineEntry{
			Kind: "heading", Line: heading.Line, Level: heading.Level, Text: heading.Text,
		})
	}
	for _, export := range outline.Exports {
		entries = append(entries, outlineEntry{
			Kind: "export", Line: export.Line, Type: export.Type, Text: export.Name,
		})
	}
	return entries
}

func outputOutlineCSV(entries []outlineEntry) error {
	rows := make([][]string, len(entries))
	for index, entry := range entries {
		level := ""
		if entry.Level > 0 {
			level = strconv.Itoa(entry.Level)
		}
		rows[index] = []string{entry.Kind, strconv.Itoa(entry.Line), level, entry.Type, entry.Text}
	}
	return writeCSVRows([]string{"kind", fieldLine, "level", "type", fieldText}, rows)
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
