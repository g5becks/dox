package search

import "github.com/g5becks/dox/internal/manifest"

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

// Metadata performs fuzzy search across manifest metadata fields.
func Metadata(_ *manifest.Manifest, _ MetadataOptions) ([]MetadataResult, error) {
	return nil, nil
}
