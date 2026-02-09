package search

import "github.com/g5becks/dox/internal/manifest"

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

// Content performs literal or regex search across synced file contents.
func Content(_ *manifest.Manifest, _ ContentOptions) ([]ContentResult, error) {
	return nil, nil
}
