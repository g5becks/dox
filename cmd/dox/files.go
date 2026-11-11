package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/urfave/cli/v3"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/manifest"
)

func newFilesCommand() *cli.Command {
	return &cli.Command{
		Name:      "files",
		Usage:     "List files in a collection",
		ArgsUsage: "<collection>",
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
			&cli.IntFlag{
				Name:  flagLimit,
				Usage: "Show first N files (0 = use config default)",
			},
			&cli.BoolFlag{
				Name:  "all",
				Usage: "Show all files (no limit)",
			},
			&cli.StringFlag{
				Name:  "format",
				Usage: "Output format: table, json, csv",
			},
			&cli.StringFlag{
				Name:  "fields",
				Usage: "Comma-separated fields: path,type,lines,size,description,modified",
			},
			&cli.IntFlag{
				Name:  "desc-length",
				Usage: "Max description length (0 = use config default)",
			},
		},
		Action: filesAction,
	}
}

func filesAction(_ context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != 1 {
		return errs.New(
			errs.Usage,
			fmt.Sprintf("expected 1 argument, got %d", cmd.Args().Len()),
			errs.WithHint("Usage: dox files <collection>"),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyInvalidArgs}),
		)
	}

	collectionName := cmd.Args().First()

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

	limit := resolveLimit(cmd, cfg)
	format := resolveFormat(cmd, cfg)
	fields := resolveFields(cmd, cfg)
	descLength := resolveDescLength(cmd, cfg)

	files := collection.Files
	totalFiles := len(files)
	limited := false

	if limit > 0 && len(files) > limit {
		files = files[:limit]
		limited = true
	}

	switch format {
	case formatJSON:
		return outputFilesJSON(files)
	case "csv":
		return outputFilesCSV(files, fields, descLength)
	default:
		return outputFilesTable(files, fields, descLength, limited, totalFiles)
	}
}

func resolveLimit(cmd *cli.Command, cfg *config.Config) int {
	if cmd.Bool("all") {
		return 0
	}
	if cmd.IsSet(flagLimit) {
		return cmd.Int(flagLimit)
	}
	return cfg.Display.DefaultLimit
}

func resolveFormat(cmd *cli.Command, cfg *config.Config) string {
	if cmd.Bool(formatJSON) {
		return formatJSON
	}
	if cmd.IsSet("format") {
		return cmd.String("format")
	}
	return cfg.Display.Format
}

func resolveFields(cmd *cli.Command, cfg *config.Config) []string {
	if cmd.IsSet("fields") {
		return strings.Split(cmd.String("fields"), ",")
	}
	return cfg.Display.ListFields
}

func resolveDescLength(cmd *cli.Command, cfg *config.Config) int {
	if cmd.IsSet("desc-length") {
		return cmd.Int("desc-length")
	}
	return cfg.Display.DescriptionLength
}

func outputFilesJSON(files []manifest.FileInfo) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(files); err != nil {
		return errs.Wrap(
			errs.Internal,
			fmt.Errorf("encoding files: %w", err),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyJSONError}),
		)
	}

	return nil
}

func outputFilesCSV(files []manifest.FileInfo, fields []string, descLength int) error {
	w := csv.NewWriter(os.Stdout)
	defer w.Flush()

	if err := w.Write(fields); err != nil {
		return errs.Wrap(
			errs.Internal,
			fmt.Errorf("writing CSV header: %w", err),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyCsvError}),
		)
	}

	for _, file := range files {
		row := make([]string, len(fields))
		for i, field := range fields {
			row[i] = getFieldValue(file, field, descLength)
		}
		if err := w.Write(row); err != nil {
			return errs.Wrap(
				errs.Internal,
				fmt.Errorf("writing CSV row: %w", err),
				errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyCsvError}),
			)
		}
	}

	return nil
}

func outputFilesTable(
	files []manifest.FileInfo,
	fields []string,
	descLength int,
	limited bool,
	total int,
) error {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetStyle(table.StyleRounded)

	header := make(table.Row, len(fields))
	for i, field := range fields {
		header[i] = strings.ToUpper(field)
	}
	t.AppendHeader(header)

	for _, file := range files {
		row := make(table.Row, len(fields))
		for i, field := range fields {
			row[i] = getFieldValue(file, field, descLength)
		}
		t.AppendRow(row)
	}

	t.Render()

	if limited {
		msg := "\n(showing " + strconv.Itoa(len(files)) + " of " + strconv.Itoa(total) +
			" files, use --all to show all)\n"
		_, _ = os.Stdout.WriteString(msg)
	}

	return nil
}

func getFieldValue(file manifest.FileInfo, field string, descLength int) string {
	switch field {
	case fieldPath:
		return file.Path
	case fieldType:
		return file.Type
	case "lines":
		return strconv.Itoa(file.Lines)
	case "size":
		return formatSize(file.Size)
	case "description":
		return truncateDescription(file.Description, descLength)
	case "modified":
		return formatTime(file.Modified)
	default:
		return ""
	}
}

func truncateDescription(desc string, maxLen int) string {
	if maxLen <= 0 || len(desc) <= maxLen {
		return desc
	}
	const ellipsis = "..."
	if maxLen <= len(ellipsis) {
		return ellipsis
	}
	return desc[:maxLen-len(ellipsis)] + ellipsis
}
