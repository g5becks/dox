package main

import (
	"context"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/urfave/cli/v3"

	"github.com/g5becks/dox/internal/manifest"
)

func newCollectionsCommand() *cli.Command {
	return &cli.Command{
		Name:  "collections",
		Usage: "List all documentation collections",
		Flags: append(formatFlags(),
			&cli.IntFlag{Name: flagLimit, Usage: helpLimitAll},
		),
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

	format, err := resolveOutputFormat(cmd, cfg)
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

	limit := resolveListLimit(cmd, 0)
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

	switch format {
	case formatJSON:
		return writeJSONValue(collections)
	case formatJSONL:
		return writeJSONLines(collections)
	case formatCSV:
		return outputCollectionsCSV(collections)
	default:
		return outputCollectionsTable(collections)
	}
}

func outputCollectionsCSV(collections []collectionOutput) error {
	rows := make([][]string, 0, len(collections))
	for _, coll := range collections {
		rows = append(rows, []string{
			coll.Name,
			coll.Type,
			coll.Source,
			coll.Path,
			strconv.Itoa(coll.Files),
			strconv.FormatInt(coll.Size, 10),
			coll.LastSync.Format(time.RFC3339),
		})
	}
	return writeCSVRows([]string{"name", "type", "source", "path", nameFiles, "size", "last_sync"}, rows)
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
