package dox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/lookup"
	"github.com/g5becks/dox/internal/manifest"
	"github.com/g5becks/dox/internal/parser"
)

// Collection is one synced source.
type Collection struct {
	Name      string
	Type      string // source type: github, url, gitlab, ...
	Source    string // repository or URL
	Path      string // subdirectory of the source, when one was configured
	Ref       string // branch, tag or commit, when one was configured
	FileCount int
	Size      int64 // total bytes
	LastSync  time.Time
}

// File is one documentation file of a collection.
type File struct {
	Path        string // relative to the collection, with forward slashes
	Type        string // file type, such as "markdown" or "typescript"
	Description string
	Size        int64 // bytes
	// Lines is the count recorded at sync time. It matches Content.TotalLines:
	// a final newline ends the last line, so an empty file has none.
	Lines    int
	Modified time.Time
	Warning  string // set when the file was synced with a problem
}

// OutlineKind says what an Outline holds.
type OutlineKind string

// Outline kinds.
const (
	OutlineHeadings OutlineKind = "headings" // Markdown-style documents
	OutlineExports  OutlineKind = "exports"  // source code
	OutlineNone     OutlineKind = "none"     // plain text and anything else
)

// Heading is a heading of a document at a 1-based line.
type Heading struct {
	Level int
	Text  string
	Line  int
}

// Export is an exported symbol of a source file at a 1-based line.
type Export struct {
	Type string // function, class, const, ...
	Name string
	Line int
}

// Outline is the structure of a file.
type Outline struct {
	Kind     OutlineKind
	Headings []Heading
	Exports  []Export
}

// Range selects the lines Cat returns. The zero value selects the whole file.
type Range struct {
	// Line is the 1-based first line; 0 means the first line.
	Line int
	// Limit is the maximum number of lines; 0 means to the end of the file.
	Limit int
}

// Content is part of a file.
type Content struct {
	Collection string
	Path       string
	Type       string
	Size       int64    // bytes in the whole file
	TotalLines int      // lines in the whole file, excluding the empty line after a final newline
	Line       int      // 1-based number of Lines[0]; the requested start when Lines is empty
	Lines      []string // the selected lines without line terminators
}

// Text joins Lines with newlines.
func (c *Content) Text() string { return strings.Join(c.Lines, "\n") }

// Collections lists the synced collections sorted by name. It returns
// ErrNotSynced before the first sync.
func (p *Project) Collections(ctx context.Context) ([]Collection, error) {
	m, err := p.manifest(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(m.Collections))
	for name := range m.Collections {
		names = append(names, name)
	}
	slices.Sort(names)
	collections := make([]Collection, 0, len(names))
	for _, name := range names {
		collection := m.Collections[name]
		collections = append(collections, Collection{
			Name:      collection.Name,
			Type:      collection.Type,
			Source:    collection.Source,
			Path:      collection.Path,
			Ref:       collection.Ref,
			FileCount: collection.FileCount,
			Size:      collection.TotalSize,
			LastSync:  collection.LastSync,
		})
	}
	return collections, nil
}

// Files lists the files of a collection in manifest order. An unknown
// collection returns ErrCollectionNotFound with suggestions in the hint.
func (p *Project) Files(ctx context.Context, collection string) ([]File, error) {
	found, err := p.collection(ctx, collection)
	if err != nil {
		return nil, err
	}
	files := make([]File, len(found.Files))
	for index := range found.Files {
		info := &found.Files[index]
		files[index] = File{
			Path:        info.Path,
			Type:        info.Type,
			Description: info.Description,
			Size:        info.Size,
			Lines:       info.Lines,
			Modified:    info.Modified,
			Warning:     info.Warning,
		}
	}
	return files, nil
}

// Outline returns the headings or exports of a file. An unknown collection or
// path returns ErrCollectionNotFound or ErrFileNotFound with suggestions.
func (p *Project) Outline(ctx context.Context, collection, path string) (*Outline, error) {
	found, err := p.collection(ctx, collection)
	if err != nil {
		return nil, err
	}
	info, err := lookup.File(found, collection, path)
	if err != nil {
		return nil, wrapError(err)
	}
	return convertOutline(info.Outline), nil
}

func convertOutline(outline *parser.Outline) *Outline {
	if outline == nil {
		return &Outline{Kind: OutlineNone}
	}
	converted := &Outline{Kind: OutlineKind(outline.Type)}
	for _, heading := range outline.Headings {
		converted.Headings = append(converted.Headings, Heading{
			Level: heading.Level,
			Text:  heading.Text,
			Line:  heading.Line,
		})
	}
	for _, export := range outline.Exports {
		converted.Exports = append(converted.Exports, Export{
			Type: export.Type,
			Name: export.Name,
			Line: export.Line,
		})
	}
	return converted
}

// Cat reads the selected lines of a file. A start past the end of the file
// yields no lines. Negative values in the range are usage errors.
func (p *Project) Cat(ctx context.Context, collection, path string, selection Range) (*Content, error) {
	if selection.Line < 0 || selection.Limit < 0 {
		return nil, usageError("range values must not be negative", "Use Line >= 1 and Limit >= 0")
	}
	found, err := p.collection(ctx, collection)
	if err != nil {
		return nil, err
	}
	info, err := lookup.File(found, collection, path)
	if err != nil {
		return nil, wrapError(err)
	}

	data, err := p.readFile(filepath.Join(found.Dir, filepath.FromSlash(info.Path)))
	if err != nil {
		return nil, wrapError(err)
	}
	lines := strings.Split(string(data), "\n")
	// A file that ends with a newline has no phantom empty last line.
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	start := max(selection.Line, 1)
	content := &Content{
		Collection: collection,
		Path:       info.Path,
		Type:       info.Type,
		Size:       info.Size,
		TotalLines: len(lines),
		Line:       start,
	}
	if start <= len(lines) {
		selected := lines[start-1:]
		if selection.Limit > 0 && len(selected) > selection.Limit {
			selected = selected[:selection.Limit]
		}
		content.Lines = slices.Clone(selected)
	}
	return content, nil
}

// manifest loads the manifest, honoring cancellation.
func (p *Project) manifest(ctx context.Context) (*manifest.Manifest, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapError(err)
	}
	m, err := manifest.Load(p.cfg.Output)
	if err != nil {
		return nil, wrapError(err)
	}
	return m, nil
}

// collection loads the manifest and finds one collection in it.
func (p *Project) collection(ctx context.Context, name string) (*manifest.Collection, error) {
	m, err := p.manifest(ctx)
	if err != nil {
		return nil, err
	}
	found, err := lookup.Collection(m, name)
	if err != nil {
		return nil, wrapError(err)
	}
	return found, nil
}

// readFile reads a manifest path through os.Root, so a path that escapes the
// output directory is refused instead of followed.
func (p *Project) readFile(relative string) ([]byte, error) {
	root, err := os.OpenRoot(p.cfg.Output)
	if err != nil {
		return nil, readError(err)
	}
	defer root.Close()
	data, err := root.ReadFile(relative)
	if err != nil {
		return nil, readError(err)
	}
	return data, nil
}

func readError(err error) error {
	return errs.Wrap(
		errs.Internal,
		fmt.Errorf("reading file: %w", err),
		errs.WithHint("Run 'dox sync' to re-download the file"),
		errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyFileReadError}),
	)
}
