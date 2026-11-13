package dox

import (
	"context"
	"iter"
	"regexp"
	"strings"

	"github.com/g5becks/dox/internal/lookup"
	"github.com/g5becks/dox/internal/search"
)

// Query selects what Search looks for.
type Query struct {
	// Text is the non-blank text to find.
	Text string
	// Collection limits the search to one collection; empty searches all.
	Collection string
	// Content searches file contents line by line instead of the manifest
	// metadata (paths, descriptions and headings), which is fuzzy-matched.
	Content bool
	// Regex treats Text as a case-insensitive regular expression; it requires
	// Content.
	Regex bool
	// Limit is the maximum number of results; 0 means all.
	Limit int
}

// Result is one search match. Metadata searches fill Type, Description,
// MatchField, MatchValue and Score; content searches fill Line and Text.
type Result struct {
	Collection  string
	Path        string
	Type        string
	Description string
	MatchField  string // which metadata field matched
	MatchValue  string // the matching value of that field
	Score       int    // higher is a better match
	Line        int    // 1-based line of a content match
	Text        string // the matching line
}

// Search runs a query and yields its results in rank (metadata) or file order
// (content). An invalid query, an unknown collection or a read failure is
// yielded once as the final (zero Result, error) pair. The search runs to
// completion before the first result is yielded, so breaking out of the loop
// early only skips conversion work.
func (p *Project) Search(ctx context.Context, query Query) iter.Seq2[Result, error] {
	return func(yield func(Result, error) bool) {
		results, err := p.search(ctx, query)
		if err != nil {
			yield(Result{}, err)
			return
		}
		for _, result := range results {
			if ctxErr := ctx.Err(); ctxErr != nil {
				yield(Result{}, wrapError(ctxErr))
				return
			}
			if !yield(result, nil) {
				return
			}
		}
	}
}

func (p *Project) search(ctx context.Context, query Query) ([]Result, error) {
	if err := validateQuery(query); err != nil {
		return nil, err
	}
	m, err := p.manifest(ctx)
	if err != nil {
		return nil, err
	}
	if query.Collection != "" {
		if _, lookupErr := lookup.Collection(m, query.Collection); lookupErr != nil {
			return nil, wrapError(lookupErr)
		}
	}

	if query.Content {
		found, searchErr := search.ContentContext(ctx, m, search.ContentOptions{
			OutputDir:  p.cfg.Output,
			Query:      query.Text,
			Collection: query.Collection,
			UseRegex:   query.Regex,
			Limit:      query.Limit,
		})
		if searchErr != nil {
			return nil, wrapError(searchErr)
		}
		results := make([]Result, len(found))
		for index, match := range found {
			results[index] = Result{
				Collection: match.Collection,
				Path:       match.Path,
				Line:       match.Line,
				Text:       match.Text,
			}
		}
		return results, nil
	}

	found, searchErr := search.MetadataContext(ctx, m, search.MetadataOptions{
		Query:      query.Text,
		Collection: query.Collection,
		Limit:      query.Limit,
	})
	if searchErr != nil {
		return nil, wrapError(searchErr)
	}
	results := make([]Result, len(found))
	for index, match := range found {
		results[index] = Result{
			Collection:  match.Collection,
			Path:        match.Path,
			Type:        match.Type,
			Description: match.Description,
			MatchField:  match.MatchField,
			MatchValue:  match.MatchValue,
			Score:       match.Score,
		}
	}
	return results, nil
}

func validateQuery(query Query) error {
	switch {
	case strings.TrimSpace(query.Text) == "":
		return usageError("search text cannot be empty", "Provide a non-empty Query.Text")
	case query.Limit < 0:
		return usageError("search limit must not be negative", "Use Limit 0 for all results")
	case query.Regex && !query.Content:
		return usageError("regex search requires content search", "Set Query.Content when Query.Regex is set")
	case query.Regex:
		// The search package compiles the same case-insensitive pattern.
		if _, err := regexp.Compile("(?i)" + query.Text); err != nil {
			return usageError("invalid regex pattern: "+err.Error(), "Check regex syntax")
		}
	}
	return nil
}
