package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/manifest"
	"github.com/samber/oops"
	"github.com/urfave/cli/v3"
)

func newCatCommand() *cli.Command {
	return &cli.Command{
		Name:      "cat",
		Usage:     "Read file contents from a collection",
		ArgsUsage: "<collection> <file>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Usage:   "Path to config file",
			},
			&cli.BoolFlag{
				Name:  "json",
				Usage: "Output as JSON with metadata",
			},
			&cli.BoolFlag{
				Name:  "no-line-numbers",
				Usage: "Don't show line numbers",
			},
			&cli.IntFlag{
				Name:  "offset",
				Usage: "Start at line N (0-based)",
			},
			&cli.IntFlag{
				Name:  "limit",
				Usage: "Show N lines (0 = all)",
			},
		},
		Action: catAction,
	}
}

type catOutput struct {
	Collection string `json:"collection"`
	Path       string `json:"path"`
	Type       string `json:"type"`
	Lines      int    `json:"lines"`
	Size       int64  `json:"size"`
	Content    string `json:"content"`
	Offset     int    `json:"offset"`
	Limit      int    `json:"limit"`
}

func catAction(_ context.Context, cmd *cli.Command) error {
	const requiredArgs = 2
	if cmd.Args().Len() != requiredArgs {
		return oops.
			Code("INVALID_ARGS").
			Hint("Usage: dox cat <collection> <file>").
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

	fullPath := filepath.Join(cfg.Output, collectionName, filePath)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return oops.
			Code("FILE_READ_ERROR").
			With("path", fullPath).
			Hint("Run 'dox sync' to re-download the file").
			Wrapf(err, "reading file")
	}

	lines := strings.Split(string(content), "\n")
	offset := cmd.Int("offset")
	limit := cmd.Int("limit")

	if offset >= len(lines) {
		lines = []string{}
	} else {
		lines = lines[offset:]
		if limit > 0 && len(lines) > limit {
			lines = lines[:limit]
		}
	}

	if cmd.Bool("json") {
		return outputCatJSON(collectionName, fileInfo, strings.Join(lines, "\n"), offset, limit)
	}

	showLineNumbers := !cmd.Bool("no-line-numbers")
	outputCatText(lines, offset, showLineNumbers)
	return nil
}

func outputCatJSON(collection string, fileInfo *manifest.FileInfo, content string, offset, limit int) error {
	output := catOutput{
		Collection: collection,
		Path:       fileInfo.Path,
		Type:       fileInfo.Type,
		Lines:      fileInfo.Lines,
		Size:       fileInfo.Size,
		Content:    content,
		Offset:     offset,
		Limit:      limit,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(output); err != nil {
		return oops.
			Code("JSON_ERROR").
			Wrapf(err, "encoding output")
	}

	return nil
}

func outputCatText(lines []string, offset int, showLineNumbers bool) {
	for i, line := range lines {
		lineNum := offset + i + 1
		if showLineNumbers {
			_, _ = os.Stdout.WriteString(formatLineNumber(lineNum, line))
		} else {
			_, _ = os.Stdout.WriteString(line + "\n")
		}
	}
}

func formatLineNumber(lineNum int, content string) string {
	return formatWithLineNumber(lineNum, content)
}

func formatWithLineNumber(lineNum int, content string) string {
	const lineNumWidth = 6
	const spacing = "  "
	return padLeft(lineNum, lineNumWidth) + spacing + content + "\n"
}

func padLeft(num, width int) string {
	s := strconv.Itoa(num)
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}
