package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/manifest"
)

const (
	formatTable  = "table"
	formatJSONL  = "jsonl"
	flagFormat   = "format"
	helpFormat   = "Output format: table, json, jsonl, csv (--json is an alias for json)"
	helpLimit    = "Max results (0 = all; default: display.default_limit where configured)"
	helpLimitAll = "Max results (0 = all)"
)

// formatFlags are the uniform output flags of every read command.
func formatFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: flagFormat, Usage: helpFormat},
		&cli.BoolFlag{Name: formatJSON, Usage: helpJSON},
	}
}

// resolveOutputFormat picks --format, then --json, then the config's
// display.format, then table; unknown values are usage errors.
func resolveOutputFormat(cmd *cli.Command, cfg *config.Config) (string, error) {
	format := formatTable
	switch {
	case cmd.IsSet(flagFormat):
		format = strings.ToLower(strings.TrimSpace(cmd.String(flagFormat)))
	case jsonEnabled(cmd):
		format = formatJSON
	case cfg != nil && cfg.Display.Format != "":
		format = cfg.Display.Format
	}
	if !slices.Contains([]string{formatTable, formatJSON, formatJSONL, formatCSV}, format) {
		return "", errs.New(
			errs.Usage,
			fmt.Sprintf("unknown output format %q", format),
			errs.WithHint("Use --format table, json, jsonl or csv"),
			errs.WithFields(map[string]any{flagFormat: format, errs.LegacyCodeField: errs.LegacyInvalidArgs}),
		)
	}
	return format, nil
}

// resolveListLimit applies one --limit convention: an explicit value wins and
// 0 means all; otherwise defaultLimit applies (0 = all). The deprecated --all
// flag still means all, with a warning.
func resolveListLimit(cmd *cli.Command, defaultLimit int) int {
	if cmd.Bool("all") {
		warnDeprecated("--all is deprecated and will be removed in v0.6.0; use --limit 0")
		return 0
	}
	if cmd.IsSet(flagLimit) {
		return max(0, cmd.Int(flagLimit))
	}
	return max(0, defaultLimit)
}

func warnDeprecated(message string) {
	_, _ = fmt.Fprintln(os.Stderr, "warning: "+message)
}

// writeJSONValue prints v as indented JSON.
func writeJSONValue(v any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(v); err != nil {
		return jsonEncodeError(err)
	}
	return nil
}

// writeJSONLines prints one compact JSON object per item, for streaming.
func writeJSONLines[T any](items []T) error {
	encoder := json.NewEncoder(os.Stdout)
	for _, item := range items {
		if err := encoder.Encode(item); err != nil {
			return jsonEncodeError(err)
		}
	}
	return nil
}

func jsonEncodeError(err error) error {
	return errs.Wrap(
		errs.Internal,
		fmt.Errorf("encoding output: %w", err),
		errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyJSONError}),
	)
}

// writeCSVRows prints a header and rows as CSV.
func writeCSVRows(header []string, rows [][]string) error {
	writer := csv.NewWriter(os.Stdout)
	if err := writer.Write(header); err != nil {
		return csvWriteError(err)
	}
	if err := writer.WriteAll(rows); err != nil {
		return csvWriteError(err)
	}
	return nil
}

func csvWriteError(err error) error {
	return errs.Wrap(
		errs.Internal,
		fmt.Errorf("writing CSV: %w", err),
		errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyCsvError}),
	)
}

// suggest returns the closest candidates to name ("did you mean"), by edit
// distance or prefix, best first.
func suggest(name string, candidates []string) []string {
	const maxSuggestions = 3
	const minDistance, lengthDivisor = 2, 3
	type scored struct {
		value    string
		distance int
	}
	lowered := strings.ToLower(name)
	threshold := max(minDistance, len(name)/lengthDivisor)
	var matches []scored
	for _, candidate := range candidates {
		lowerCandidate := strings.ToLower(candidate)
		distance := editDistance(lowered, lowerCandidate)
		if distance <= threshold || strings.HasPrefix(lowerCandidate, lowered) ||
			strings.Contains(lowerCandidate, lowered) && len(lowered) >= 3 {
			matches = append(matches, scored{candidate, distance})
		}
	}
	slices.SortFunc(matches, func(a, b scored) int {
		if a.distance != b.distance {
			return a.distance - b.distance
		}
		return strings.Compare(a.value, b.value)
	})
	result := make([]string, 0, maxSuggestions)
	for _, match := range matches[:min(len(matches), maxSuggestions)] {
		result = append(result, match.value)
	}
	return result
}

// suggestionHint renders "Did you mean ...?" or the fallback hint.
func suggestionHint(name string, candidates []string, fallback string) string {
	options := suggest(name, candidates)
	if len(options) == 0 {
		return fallback
	}
	quoted := make([]string, len(options))
	for index, option := range options {
		quoted[index] = fmt.Sprintf("%q", option)
	}
	return "Did you mean " + strings.Join(quoted, " or ") + "? " + fallback
}

// editDistance is the Levenshtein distance between a and b.
func editDistance(a, b string) int {
	left, right := []rune(a), []rune(b)
	previous := make([]int, len(right)+1)
	current := make([]int, len(right)+1)
	for column := range previous {
		previous[column] = column
	}
	for row := 1; row <= len(left); row++ {
		current[0] = row
		for column := 1; column <= len(right); column++ {
			cost := 1
			if left[row-1] == right[column-1] {
				cost = 0
			}
			current[column] = min(previous[column]+1, current[column-1]+1, previous[column-1]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(right)]
}

// collectionHint suggests close collection names for a lookup miss.
func collectionHint(name string, m *manifest.Manifest) string {
	return suggestionHint(
		name,
		slices.Collect(maps.Keys(m.Collections)),
		"Run 'dox collections' to see available collections",
	)
}

// fileHint suggests close file paths within a collection for a lookup miss.
func fileHint(filePath string, collection *manifest.Collection) string {
	paths := make([]string, 0, len(collection.Files))
	for index := range collection.Files {
		paths = append(paths, collection.Files[index].Path)
	}
	return suggestionHint(filePath, paths, "Run 'dox files' to see available files")
}
