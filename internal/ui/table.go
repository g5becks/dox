package ui

import (
	"encoding/json"
	"fmt"
	"os"
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
	SyncedAt  time.Time `json:"synced_at,omitempty"`
}

type ListOptions struct {
	JSON    bool
	Verbose bool
	Files   bool
}

func RenderSourceList(sources []SourceStatus, opts ListOptions) error {
	if opts.JSON {
		return renderSourceListJSON(sources)
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
