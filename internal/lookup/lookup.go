// Package lookup finds collections and files in a manifest and explains misses
// with "did you mean" suggestions, for the CLI and the public API alike.
package lookup

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/manifest"
)

const (
	fieldCollection = "collection"
	fieldFile       = "file"
)

// Collection returns the named collection or a not-found error that suggests
// close names.
func Collection(m *manifest.Manifest, name string) (*manifest.Collection, error) {
	if collection, ok := m.Collections[name]; ok {
		return collection, nil
	}
	return nil, errs.New(
		errs.NotFound,
		fmt.Sprintf("collection %q not found", name),
		errs.WithHint(CollectionHint(name, m)),
		errs.WithCause(errs.ErrCollectionNotFound),
		errs.WithFields(
			map[string]any{fieldCollection: name, errs.LegacyCodeField: errs.LegacyCollectionNotFound},
		),
	)
}

// File returns the file with the exact manifest path or a not-found error that
// suggests close paths.
func File(collection *manifest.Collection, collectionName, filePath string) (*manifest.FileInfo, error) {
	for index := range collection.Files {
		if collection.Files[index].Path == filePath {
			return &collection.Files[index], nil
		}
	}
	return nil, errs.New(
		errs.NotFound,
		fmt.Sprintf("file %q not found in collection %q", filePath, collectionName),
		errs.WithHint(FileHint(filePath, collection)),
		errs.WithCause(errs.ErrFileNotFound),
		errs.WithFields(
			map[string]any{
				fieldFile:            filePath,
				fieldCollection:      collectionName,
				errs.LegacyCodeField: errs.LegacyFileNotFound,
			},
		),
	)
}

// CollectionHint suggests close collection names for a lookup miss.
func CollectionHint(name string, m *manifest.Manifest) string {
	return Hint(
		name,
		slices.Collect(maps.Keys(m.Collections)),
		"Run 'dox collections' to see available collections",
	)
}

// FileHint suggests close file paths within a collection for a lookup miss.
func FileHint(filePath string, collection *manifest.Collection) string {
	paths := make([]string, 0, len(collection.Files))
	for index := range collection.Files {
		paths = append(paths, collection.Files[index].Path)
	}
	return Hint(filePath, paths, "Run 'dox files' to see available files")
}

// Suggest returns the closest candidates to name ("did you mean"), by edit
// distance or prefix, best first.
func Suggest(name string, candidates []string) []string {
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

// Hint renders "Did you mean ...?" or the fallback hint.
func Hint(name string, candidates []string, fallback string) string {
	options := Suggest(name, candidates)
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
