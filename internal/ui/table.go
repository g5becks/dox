package ui

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
)

type SourceStatus struct {
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Repo      string    `json:"repo,omitempty"`
	Path      string    `json:"path,omitempty"`
	URL       string    `json:"url,omitempty"`
	Ref       string    `json:"ref,omitempty"`
	Patterns  []string  `json:"patterns,omitempty"`
	OutputDir string    `json:"output_dir"`
	Status    string    `json:"status"`
	FileCount int       `json:"file_count,omitempty"`
	SyncedAt  time.Time `json:"synced_at,omitzero"`
}

// Output formats of RenderSourceList; anything else renders the table.
const (
	FormatJSON  = "json"
	FormatJSONL = "jsonl"
	FormatCSV   = "csv"
)

type ListOptions struct {
	Format  string
	Verbose bool
	Files   bool
}

func RenderSourceList(sources []SourceStatus, opts ListOptions) error {
	switch opts.Format {
	case FormatJSON:
		return renderSourceListJSON(sources)
	case FormatJSONL:
		return renderSourceListJSONL(sources)
	case FormatCSV:
		return renderSourceListCSV(sources)
	}

	renderSourceListTable(sources, opts)
	return nil
}

func renderSourceListJSON(sources []SourceStatus) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(sources); err != nil {
		return fmt.Errorf("encode source list json: %w", err)
	}

	return nil
}

func renderSourceListJSONL(sources []SourceStatus) error {
	encoder := json.NewEncoder(os.Stdout)
	for _, source := range sources {
		if err := encoder.Encode(source); err != nil {
			return fmt.Errorf("encode source list jsonl: %w", err)
		}
	}

	return nil
}

func renderSourceListCSV(sources []SourceStatus) error {
	writer := csv.NewWriter(os.Stdout)
	header := []string{
		"name", "type", "repo", "path", "url", "ref", "patterns", "output_dir", "status", "file_count", "synced_at",
	}
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("write source list csv: %w", err)
	}
	for _, source := range sources {
		syncedAt := ""
		if !source.SyncedAt.IsZero() {
			syncedAt = source.SyncedAt.Format(time.RFC3339)
		}
		row := []string{
			source.Name, source.Type, source.Repo, source.Path, source.URL, source.Ref,
			strings.Join(source.Patterns, ";"), source.OutputDir, source.Status,
			strconv.Itoa(source.FileCount), syncedAt,
		}
		if err := writer.Write(row); err != nil {
			return fmt.Errorf("write source list csv: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("write source list csv: %w", err)
	}

	return nil
}

func renderSourceListTable(sources []SourceStatus, opts ListOptions) {
	writer := table.NewWriter()
	writer.SetOutputMirror(os.Stdout)
	writer.SetStyle(table.StyleRounded)

	if opts.Verbose {
		writer.AppendHeader(table.Row{"SOURCE", "TYPE", "LOCATION", "STATUS", "REF", "PATTERNS", "OUTPUT DIR"})
	} else {
		writer.AppendHeader(table.Row{"SOURCE", "TYPE", "LOCATION", "STATUS"})
	}

	for _, source := range sources {
		location := renderLocation(source)
		status := renderStatus(source, opts.Files)

		if opts.Verbose {
			writer.AppendRow(table.Row{
				source.Name,
				source.Type,
				location,
				status,
				source.Ref,
				strings.Join(source.Patterns, ", "),
				source.OutputDir,
			})
			continue
		}

		writer.AppendRow(table.Row{
			source.Name,
			source.Type,
			location,
			status,
		})
	}

	writer.Render()
}

func renderLocation(source SourceStatus) string {
	if source.Type == "url" {
		return source.URL
	}

	location := source.Repo
	if source.Path != "" {
		location += "/" + source.Path
	}

	return strings.TrimPrefix(location, "/")
}

func renderStatus(source SourceStatus, includeFiles bool) string {
	if includeFiles && source.FileCount > 0 {
		return fmt.Sprintf("%s (%d files)", source.Status, source.FileCount)
	}

	return source.Status
}
