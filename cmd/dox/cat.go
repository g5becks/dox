package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/manifest"
)

func newCatCommand() *cli.Command {
	return &cli.Command{
		Name:      "cat",
		Usage:     "Read file contents from a collection",
		ArgsUsage: "<collection> <file>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    flagConfig,
				Aliases: []string{"c"},
				Usage:   helpConfig,
			},
			&cli.BoolFlag{
				Name:  formatJSON,
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
				Name:  flagLimit,
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
		return errs.New(
			errs.Usage,
			fmt.Sprintf("expected %d arguments, got %d", requiredArgs, cmd.Args().Len()),
			errs.WithHint("Usage: dox cat <collection> <file>"),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyInvalidArgs}),
		)
	}

	collectionName := cmd.Args().Get(0)
	filePath := cmd.Args().Get(1)

	configPath, err := resolveConfigPath(cmd.String(flagConfig))
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
		return errs.New(
			errs.NotFound,
			fmt.Sprintf("collection %q not found", collectionName),
			errs.WithHint("Run 'dox collections' to see available collections"),
			errs.WithFields(
				map[string]any{fieldCollection: collectionName, errs.LegacyCodeField: errs.LegacyCollectionNotFound},
			),
		)
	}

	fileInfo, err := findCatFile(collection, collectionName, filePath)
	if err != nil {
		return err
	}

	fullPath := filepath.Join(cfg.Output, collection.Dir, filePath)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return errs.Wrap(
			errs.Internal,
			fmt.Errorf("reading file: %w", err),
			errs.WithHint("Run 'dox sync' to re-download the file"),
			errs.WithFields(map[string]any{fieldPath: fullPath, errs.LegacyCodeField: errs.LegacyFileReadError}),
		)
	}

	lines := strings.Split(string(content), "\n")
	// Remove phantom trailing empty line from files ending with newline
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	offset := cmd.Int("offset")
	limit := cmd.Int(flagLimit)

	if offset >= len(lines) {
		lines = []string{}
	} else {
		lines = lines[offset:]
		if limit > 0 && len(lines) > limit {
			lines = lines[:limit]
		}
	}

	if cmd.Bool(formatJSON) {
		return outputCatJSON(collectionName, fileInfo, strings.Join(lines, "\n"), offset, limit)
	}

	showLineNumbers := cfg.Display.LineNumbers
	if cmd.IsSet("no-line-numbers") {
		showLineNumbers = false
	}
	outputCatText(lines, offset, showLineNumbers)
	return nil
}

func outputCatJSON(
	collection string,
	fileInfo *manifest.FileInfo,
	content string,
	offset, limit int,
) error {
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
		return errs.Wrap(
			errs.Internal,
			fmt.Errorf("encoding output: %w", err),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyJSONError}),
		)
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

func findCatFile(collection *manifest.Collection, collectionName, filePath string) (*manifest.FileInfo, error) {
	var fileInfo *manifest.FileInfo
	for i := range collection.Files {
		if collection.Files[i].Path == filePath {
			fileInfo = &collection.Files[i]
			break
		}
	}

	if fileInfo == nil {
		return nil, errs.New(
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

	return fileInfo, nil
}
