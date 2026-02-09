package search

import (
	"sort"
	"strings"

	"github.com/sahilm/fuzzy"
	"github.com/samber/oops"

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

func (s searchIndex) String(i int) string {
	return s.entries[i].MatchValue
}

func (s searchIndex) Len() int {
	return len(s.entries)
}

// Metadata performs fuzzy search across manifest metadata fields.
func Metadata(m *manifest.Manifest, opts MetadataOptions) ([]MetadataResult, error) {
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

	names := make([]string, 0, len(m.Collections))
	for name := range m.Collections {
		names = append(names, name)
	}
	sort.Strings(names)

	var entries []indexEntry
	for _, name := range names {
		if opts.Collection != "" && name != opts.Collection {
			continue
		}

		coll := m.Collections[name]
		for _, file := range coll.Files {
			entries = append(entries, indexEntry{
				Collection:  name,
				Path:        file.Path,
				Type:        file.Type,
				Description: file.Description,
				MatchField:  "path",
				MatchValue:  file.Path,
			})

			if file.Description != "" {
				entries = append(entries, indexEntry{
					Collection:  name,
					Path:        file.Path,
					Type:        file.Type,
					Description: file.Description,
					MatchField:  "description",
					MatchValue:  file.Description,
				})
			}

			if file.Outline != nil {
				for _, heading := range file.Outline.Headings {
					entries = append(entries, indexEntry{
						Collection:  name,
						Path:        file.Path,
						Type:        file.Type,
						Description: file.Description,
						MatchField:  "heading",
						MatchValue:  heading.Text,
					})
				}

				for _, export := range file.Outline.Exports {
					entries = append(entries, indexEntry{
						Collection:  name,
						Path:        file.Path,
						Type:        file.Type,
						Description: file.Description,
						MatchField:  "export",
						MatchValue:  export.Name,
					})
				}
			}
		}
	}

	index := searchIndex{entries: entries}
	matches := fuzzy.FindFrom(query, index)

	deduped := make(map[string]MetadataResult)
	for _, match := range matches {
		if match.Score < 0 {
			continue
		}
		entry := entries[match.Index]
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
