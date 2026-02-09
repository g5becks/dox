package search

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/samber/oops"

	"github.com/g5becks/dox/internal/manifest"
	"github.com/g5becks/dox/internal/parser"
)

const maxFileSize = 50 * 1024 * 1024 // 50MB

// ContentResult represents a single match from content search.
type ContentResult struct {
	Collection string `json:"collection"`
	Path       string `json:"path"`
	Line       int    `json:"line"`
	Text       string `json:"text"`
}

// ContentOptions configures content search behavior.
type ContentOptions struct {
	OutputDir  string
	Query      string
	Collection string
	UseRegex   bool
	Limit      int
}

type matcher func(string) bool

// Content performs literal or regex search across synced file contents.
func Content(m *manifest.Manifest, opts ContentOptions) ([]ContentResult, error) {
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		return nil, oops.
			Code("INVALID_ARGS").
			Hint("Provide a non-empty search query").
			Errorf("search query cannot be empty")
	}

	if opts.Collection != "" {
		if _, exists := m.Collections[opts.Collection]; !exists {
			return nil, oops.
				Code("COLLECTION_NOT_FOUND").
				With("collection", opts.Collection).
				Hint("Run 'dox collections' to see available collections").
				Errorf("collection %q not found", opts.Collection)
		}
	}

	match, err := buildMatcher(query, opts.UseRegex)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(m.Collections))
	for name := range m.Collections {
		names = append(names, name)
	}
	sort.Strings(names)

	var results []ContentResult
	for _, name := range names {
		if opts.Collection != "" && name != opts.Collection {
			continue
		}

		coll := m.Collections[name]
		for _, file := range coll.Files {
			filePath := filepath.Join(opts.OutputDir, coll.Dir, file.Path)

			matches, scanErr := scanFile(filePath, name, file.Path, match)
			if scanErr != nil {
				continue
			}

			results = append(results, matches...)

			if opts.Limit > 0 && len(results) >= opts.Limit {
				return results[:opts.Limit], nil
			}
		}
	}

	return results, nil
}

func buildMatcher(query string, useRegex bool) (matcher, error) {
	if useRegex {
		pattern := "(?i)" + query
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, oops.
				Code("SEARCH_ERROR").
				With("pattern", query).
				Hint("Check regex syntax").
				Wrapf(err, "invalid regex pattern")
		}
		return re.MatchString, nil
	}

	lowerQuery := strings.ToLower(query)
	return func(line string) bool {
		return strings.Contains(strings.ToLower(line), lowerQuery)
	}, nil
}

func scanFile(filePath, collection, relPath string, match matcher) ([]ContentResult, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}

	if info.Size() > maxFileSize {
		return nil, nil
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	if parser.IsBinary(content) {
		return nil, nil
	}

	lines := strings.Split(string(content), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var results []ContentResult
	for i, line := range lines {
		if match(line) {
			results = append(results, ContentResult{
				Collection: collection,
				Path:       relPath,
				Line:       i + 1,
				Text:       line,
			})
		}
	}

	return results, nil
}
