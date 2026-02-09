package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"os"
	"strconv"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/samber/oops"
	"github.com/urfave/cli/v3"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/manifest"
	"github.com/g5becks/dox/internal/search"
)

const (
	formatJSON = "json"
	formatCSV  = "csv"
)

func newSearchCommand() *cli.Command {
	return &cli.Command{
		Name:      "search",
		Usage:     "Search documentation metadata or file content",
		ArgsUsage: "<query>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Usage:   "Path to config file",
			},
			&cli.StringFlag{
				Name:  "collection",
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
				Name:  "json",
				Usage: "Output as JSON",
			},
			&cli.StringFlag{
				Name:  "format",
				Usage: "Output format: table, json, csv",
			},
			&cli.IntFlag{
				Name:  "limit",
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
		return oops.
			Code("INVALID_ARGS").
			Hint("Usage: dox search <query>").
			Errorf("expected 1 argument, got %d", cmd.Args().Len())
	}

	query := strings.TrimSpace(cmd.Args().First())
	if query == "" {
		return oops.
			Code("INVALID_ARGS").
			Hint("Provide a non-empty search query").
			Errorf("search query cannot be empty")
	}

	if cmd.Bool("regex") && !cmd.Bool("content") {
		return oops.
			Code("INVALID_ARGS").
			Hint("--regex requires --content flag").
			Errorf("--regex can only be used with --content")
	}

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
		Collection: cmd.String("collection"),
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
		Collection: cmd.String("collection"),
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
		return oops.Code("JSON_ERROR").Wrapf(err, "encoding results")
	}
	return nil
}

func outputMetadataCSV(results []search.MetadataResult) error {
	w := csv.NewWriter(os.Stdout)
	defer w.Flush()

	header := []string{"collection", "path", "type", "match_field", "match_value", "score", "description"}
	if err := w.Write(header); err != nil {
		return oops.Code("CSV_ERROR").Wrapf(err, "writing CSV header")
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
			return oops.Code("CSV_ERROR").Wrapf(err, "writing CSV row")
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
		return oops.Code("JSON_ERROR").Wrapf(err, "encoding results")
	}
	return nil
}

func outputContentCSV(results []search.ContentResult) error {
	w := csv.NewWriter(os.Stdout)
	defer w.Flush()

	if err := w.Write([]string{"collection", "path", "line", "text"}); err != nil {
		return oops.Code("CSV_ERROR").Wrapf(err, "writing CSV header")
	}

	for _, r := range results {
		if err := w.Write([]string{
			r.Collection,
			r.Path,
			strconv.Itoa(r.Line),
			r.Text,
		}); err != nil {
			return oops.Code("CSV_ERROR").Wrapf(err, "writing CSV row")
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
