package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/urfave/cli/v3"

	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/manifest"
)

func newCollectionsCommand() *cli.Command {
	return &cli.Command{
		Name:  "collections",
		Usage: "List all documentation collections",
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
				Usage: "Limit number of results (0 = all)",
			},
		},
		Action: collectionsAction,
	}
}

type collectionOutput struct {
	Name     string    `json:"name"`
	Type     string    `json:"type"`
	Source   string    `json:"source"`
	Path     string    `json:"path,omitempty"`
	Files    int       `json:"files"`
	Size     int64     `json:"size"`
	LastSync time.Time `json:"last_sync"`
}

func collectionsAction(_ context.Context, cmd *cli.Command) error {
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

	names := make([]string, 0, len(m.Collections))
	for name := range m.Collections {
		names = append(names, name)
	}
	sort.Strings(names)

	limit := cmd.Int(flagLimit)
	if limit > 0 && len(names) > limit {
		names = names[:limit]
	}

	collections := make([]collectionOutput, 0, len(names))
	for _, name := range names {
		coll := m.Collections[name]
		collections = append(collections, collectionOutput{
			Name:     coll.Name,
			Type:     coll.Type,
			Source:   coll.Source,
			Path:     coll.Path,
			Files:    coll.FileCount,
			Size:     coll.TotalSize,
			LastSync: coll.LastSync,
		})
	}

	if cmd.Bool(formatJSON) {
		return outputCollectionsJSON(collections)
	}

	return outputCollectionsTable(collections)
}

func outputCollectionsJSON(collections []collectionOutput) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(collections); err != nil {
		return errs.Wrap(
			errs.Internal,
			fmt.Errorf("encoding collections: %w", err),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyJSONError}),
		)
	}

	return nil
}

func outputCollectionsTable(collections []collectionOutput) error {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetStyle(table.StyleRounded)

	t.AppendHeader(table.Row{"NAME", "TYPE", "FILES", "SIZE", "LAST SYNC"})

	for _, coll := range collections {
		t.AppendRow(table.Row{
			coll.Name,
			coll.Type,
			coll.Files,
			formatSize(coll.Size),
			formatTime(coll.LastSync),
		})
	}

	t.Render()
	return nil
}
