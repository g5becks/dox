package search

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/sahilm/fuzzy"

	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/manifest"
)

// MetadataResult represents a single match from metadata search.
type MetadataResult struct {
	Collection  string `json:"collection"`
	Path        string `json:"path"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	MatchField  string `json:"match_field"`
	MatchValue  string `json:"match_value"`
	Score       int    `json:"score"`
}

// MetadataOptions configures metadata search behavior.
type MetadataOptions struct {
	Query      string
	Collection string
	Limit      int
}

type indexEntry struct {
	Collection  string
	Path        string
	Type        string
	Description string
	MatchField  string
	MatchValue  string
}

type searchIndex struct {
	entries []indexEntry
}

const defaultFileEntryCapacity = 2

func (s searchIndex) String(i int) string {
	return s.entries[i].MatchValue
}

func (s searchIndex) Len() int {
	return len(s.entries)
}

func sortedCollectionNames(m *manifest.Manifest) []string {
	names := make([]string, 0, len(m.Collections))
	for name := range m.Collections {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func fileEntries(collection string, file manifest.FileInfo) []indexEntry {
	entries := make([]indexEntry, 0, defaultFileEntryCapacity)
	entries = append(entries, indexEntry{
		Collection:  collection,
		Path:        file.Path,
		Type:        file.Type,
		Description: file.Description,
		MatchField:  "path",
		MatchValue:  file.Path,
	})

	if file.Description != "" {
		entries = append(entries, indexEntry{
			Collection:  collection,
			Path:        file.Path,
			Type:        file.Type,
			Description: file.Description,
			MatchField:  "description",
			MatchValue:  file.Description,
		})
	}

	if file.Outline == nil {
		return entries
	}

	for _, heading := range file.Outline.Headings {
		entries = append(entries, indexEntry{
			Collection:  collection,
			Path:        file.Path,
			Type:        file.Type,
			Description: file.Description,
			MatchField:  "heading",
			MatchValue:  heading.Text,
		})
	}

	for _, export := range file.Outline.Exports {
		entries = append(entries, indexEntry{
			Collection:  collection,
			Path:        file.Path,
			Type:        file.Type,
			Description: file.Description,
			MatchField:  "export",
			MatchValue:  export.Name,
		})
	}

	return entries
}

func buildIndex(m *manifest.Manifest, collection string) searchIndex {
	index, _ := buildIndexContext(context.Background(), m, collection)
	return index
}

func buildIndexContext(ctx context.Context, m *manifest.Manifest, collection string) (searchIndex, error) {
	names := sortedCollectionNames(m)
	var entries []indexEntry
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return searchIndex{}, err
		}
		if collection != "" && name != collection {
			continue
		}

		coll := m.Collections[name]
		for _, file := range coll.Files {
			if err := ctx.Err(); err != nil {
				return searchIndex{}, err
			}
			entries = append(entries, fileEntries(name, file)...)
		}
	}

	return searchIndex{entries: entries}, nil
}

// Metadata performs fuzzy search across manifest metadata fields.
func Metadata(m *manifest.Manifest, opts MetadataOptions) ([]MetadataResult, error) {
	return MetadataContext(context.Background(), m, opts)
}

// MetadataContext performs fuzzy search across manifest metadata fields, honoring ctx cancellation.
func MetadataContext(ctx context.Context, m *manifest.Manifest, opts MetadataOptions) ([]MetadataResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		return nil, errs.New(
			errs.Usage,
			"search query cannot be empty",
			errs.WithHint("Provide a non-empty search query"),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyInvalidArgs}),
		)
	}

	if opts.Collection != "" {
		if _, exists := m.Collections[opts.Collection]; !exists {
			return nil, errs.New(
				errs.NotFound,
				fmt.Sprintf("collection %q not found", opts.Collection),
				errs.WithHint("Run 'dox collections' to see available collections"),
				errs.WithFields(
					map[string]any{"collection": opts.Collection, errs.LegacyCodeField: errs.LegacyCollectionNotFound},
				),
			)
		}
	}

	index, err := buildIndexContext(ctx, m, opts.Collection)
	if err != nil {
		return nil, err
	}
	matches := fuzzy.FindFrom(query, index)

	deduped, err := dedupeMetadataMatches(ctx, index, matches)
	if err != nil {
		return nil, err
	}

	results := make([]MetadataResult, 0, len(deduped))
	for _, result := range deduped {
		results = append(results, result)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].Collection != results[j].Collection {
			return results[i].Collection < results[j].Collection
		}
		return results[i].Path < results[j].Path
	})

	if opts.Limit > 0 && len(results) > opts.Limit {
		results = results[:opts.Limit]
	}

	return results, nil
}

func dedupeMetadataMatches(
	ctx context.Context,
	index searchIndex,
	matches fuzzy.Matches,
) (map[string]MetadataResult, error) {
	deduped := make(map[string]MetadataResult)
	for _, match := range matches {
		if cancelErr := ctx.Err(); cancelErr != nil {
			return nil, cancelErr
		}
		if match.Score < 0 {
			continue
		}
		entry := index.entries[match.Index]
		key := entry.Collection + "\x00" + entry.Path

		if existing, exists := deduped[key]; !exists || match.Score > existing.Score {
			deduped[key] = MetadataResult{
				Collection:  entry.Collection,
				Path:        entry.Path,
				Type:        entry.Type,
				Description: entry.Description,
				MatchField:  entry.MatchField,
				MatchValue:  entry.MatchValue,
				Score:       match.Score,
			}
		}
	}
	return deduped, nil
}
