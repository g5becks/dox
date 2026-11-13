package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/lookup"
	"github.com/g5becks/dox/internal/manifest"
)

func newCatCommand() *cli.Command {
	return &cli.Command{
		Name:      "cat",
		Usage:     "Read file contents from a collection",
		ArgsUsage: "<collection> <file[:line]>",
		Flags: append(formatFlags(),
			&cli.BoolFlag{
				Name:  "no-line-numbers",
				Usage: "Don't show line numbers",
			},
			&cli.IntFlag{Name: flagLine, Usage: "Start at line N (1-based)", Value: 1},
			&cli.IntFlag{Name: "offset", Usage: "Deprecated: 0-based start (removed in v0.6.0); use --line"},
			&cli.IntFlag{Name: flagLimit, Usage: "Show N lines (0 = all)"},
		),
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
	Line       int    `json:"line"`
	Limit      int    `json:"limit"`
}

// catLine is one numbered line for the jsonl and csv formats.
type catLine struct {
	Line int    `json:"line"`
	Text string `json:"text"`
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
	position, positionErr := resolveCatPosition(cmd)
	if positionErr != nil {
		return positionErr
	}
	filePath := position.path

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

	offset := position.offset
	limit := resolveListLimit(cmd, 0)

	if offset >= len(lines) {
		lines = []string{}
	} else {
		lines = lines[offset:]
		if limit > 0 && len(lines) > limit {
			lines = lines[:limit]
		}
	}

	switch format {
	case formatJSON:
		return outputCatJSON(collectionName, fileInfo, strings.Join(lines, "\n"), offset, limit)
	case formatJSONL:
		return writeJSONLines(numberedLines(lines, offset))
	case formatCSV:
		return outputCatCSV(numberedLines(lines, offset))
	}

	showLineNumbers := cfg.Display.LineNumbers
	if cmd.IsSet("no-line-numbers") {
		showLineNumbers = false
	}
	outputCatText(lines, offset, showLineNumbers)
	return nil
}

func numberedLines(lines []string, offset int) []catLine {
	numbered := make([]catLine, len(lines))
	for index, text := range lines {
		numbered[index] = catLine{Line: offset + index + 1, Text: text}
	}
	return numbered
}

func outputCatCSV(lines []catLine) error {
	rows := make([][]string, len(lines))
	for index, line := range lines {
		rows[index] = []string{strconv.Itoa(line.Line), line.Text}
	}
	return writeCSVRows([]string{fieldLine, fieldText}, rows)
}

func outputCatJSON(
	collection string,
	fileInfo *manifest.FileInfo,
	content string,
	offset, limit int,
) error {
	return writeJSONValue(catOutput{
		Collection: collection,
		Path:       fileInfo.Path,
		Type:       fileInfo.Type,
		Lines:      fileInfo.Lines,
		Size:       fileInfo.Size,
		Content:    content,
		Offset:     offset,
		Line:       offset + 1,
		Limit:      limit,
	})
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
