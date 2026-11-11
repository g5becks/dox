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
	"github.com/g5becks/dox/internal/search"
)

func newSearchCommand() *cli.Command {
	return &cli.Command{
		Name:      "search",
		Usage:     "Search documentation metadata or file content",
		ArgsUsage: "<query>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    flagConfig,
				Aliases: []string{"c"},
				Usage:   helpConfig,
			},
			&cli.StringFlag{
				Name:  fieldCollection,
				Usage: "Search only within one collection",
			},
			&cli.BoolFlag{
				Name:  "content",
				Usage: "Search file contents instead of metadata",
			},
			&cli.BoolFlag{
				Name:  "regex",
				Usage: "Treat query as regex (requires --content)",
			},
			&cli.BoolFlag{
				Name:  formatJSON,
				Usage: helpJSON,
			},
			&cli.StringFlag{
				Name:  "format",
				Usage: "Output format: table, json, csv",
			},
			&cli.IntFlag{
				Name:  flagLimit,
				Usage: "Max results (0 = unlimited)",
			},
			&cli.IntFlag{
				Name:  "desc-length",
				Usage: "Max table text length (0 = use config default)",
			},
		},
		Action: searchAction,
	}
}

func searchAction(_ context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != 1 {
		return errs.New(
			errs.Usage,
			fmt.Sprintf("expected 1 argument, got %d", cmd.Args().Len()),
			errs.WithHint("Usage: dox search <query>"),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyInvalidArgs}),
		)
	}

	query := strings.TrimSpace(cmd.Args().First())
	if query == "" {
		return errs.New(
			errs.Usage,
			"search query cannot be empty",
			errs.WithHint("Provide a non-empty search query"),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyInvalidArgs}),
		)
	}

	if cmd.Bool("regex") && !cmd.Bool("content") {
		return errs.New(
			errs.Usage,
			"--regex can only be used with --content",
			errs.WithHint("--regex requires --content flag"),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyInvalidArgs}),
		)
	}

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

	format := resolveFormat(cmd, cfg)
	limit := resolveLimit(cmd, cfg)
	descLength := resolveDescLength(cmd, cfg)

	if cmd.Bool("content") {
		return runContentSearch(m, cfg, cmd, query, format, limit, descLength)
	}

	return runMetadataSearch(m, cmd, query, format, limit, descLength)
}

func runMetadataSearch(
	m *manifest.Manifest,
	cmd *cli.Command,
	query, format string,
	limit, descLength int,
) error {
	results, err := search.Metadata(m, search.MetadataOptions{
		Query:      query,
		Collection: cmd.String(fieldCollection),
		Limit:      limit,
	})
	if err != nil {
		return err
	}

	switch format {
	case formatJSON:
		return outputMetadataJSON(results)
	case formatCSV:
		return outputMetadataCSV(results)
	default:
		return outputMetadataTable(results, descLength)
	}
}

func runContentSearch(
	m *manifest.Manifest,
	cfg *config.Config,
	cmd *cli.Command,
	query, format string,
	limit, descLength int,
) error {
	results, err := search.Content(m, search.ContentOptions{
		OutputDir:  cfg.Output,
		Query:      query,
		Collection: cmd.String(fieldCollection),
		UseRegex:   cmd.Bool("regex"),
		Limit:      limit,
	})
	if err != nil {
		return err
	}

	switch format {
	case formatJSON:
		return outputContentJSON(results)
	case formatCSV:
		return outputContentCSV(results)
	default:
		return outputContentTable(results, descLength)
	}
}

func outputMetadataJSON(results []search.MetadataResult) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(results); err != nil {
		return errs.Wrap(
			errs.Internal,
			fmt.Errorf("encoding results: %w", err),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyJSONError}),
		)
	}
	return nil
}

func outputMetadataCSV(results []search.MetadataResult) error {
	w := csv.NewWriter(os.Stdout)
	defer w.Flush()

	header := []string{fieldCollection, fieldPath, fieldType, "match_field", "match_value", "score", "description"}
	if err := w.Write(header); err != nil {
		return errs.Wrap(
			errs.Internal,
			fmt.Errorf("writing CSV header: %w", err),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyCsvError}),
		)
	}

	for _, r := range results {
		if err := w.Write([]string{
			r.Collection,
			r.Path,
			r.Type,
			r.MatchField,
			r.MatchValue,
			strconv.Itoa(r.Score),
			r.Description,
		}); err != nil {
			return errs.Wrap(
				errs.Internal,
				fmt.Errorf("writing CSV row: %w", err),
				errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyCsvError}),
			)
		}
	}

	return nil
}

func outputMetadataTable(results []search.MetadataResult, descLength int) error {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetStyle(table.StyleRounded)

	t.AppendHeader(table.Row{"COLLECTION", "PATH", "TYPE", "MATCH FIELD", "SCORE", "DESCRIPTION"})

	for _, r := range results {
		t.AppendRow(table.Row{
			r.Collection,
			r.Path,
			r.Type,
			r.MatchField,
			r.Score,
			truncateDescription(r.Description, descLength),
		})
	}

	t.Render()
	return nil
}

func outputContentJSON(results []search.ContentResult) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(results); err != nil {
		return errs.Wrap(
			errs.Internal,
			fmt.Errorf("encoding results: %w", err),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyJSONError}),
		)
	}
	return nil
}

func outputContentCSV(results []search.ContentResult) error {
	w := csv.NewWriter(os.Stdout)
	defer w.Flush()

	if err := w.Write([]string{fieldCollection, fieldPath, "line", "text"}); err != nil {
		return errs.Wrap(
			errs.Internal,
			fmt.Errorf("writing CSV header: %w", err),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyCsvError}),
		)
	}

	for _, r := range results {
		if err := w.Write([]string{
			r.Collection,
			r.Path,
			strconv.Itoa(r.Line),
			r.Text,
		}); err != nil {
			return errs.Wrap(
				errs.Internal,
				fmt.Errorf("writing CSV row: %w", err),
				errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyCsvError}),
			)
		}
	}

	return nil
}

func outputContentTable(results []search.ContentResult, descLength int) error {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetStyle(table.StyleRounded)

	t.AppendHeader(table.Row{"COLLECTION", "PATH", "LINE", "TEXT"})

	for _, r := range results {
		t.AppendRow(table.Row{
			r.Collection,
			r.Path,
			r.Line,
			truncateDescription(r.Text, descLength),
		})
	}

	t.Render()
	return nil
}
