package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/g5becks/dox/dox"
)

type handlers struct {
	docs Docs
}

type noInput struct{}

type collectionInfo struct {
	Name     string    `json:"name"`
	Type     string    `json:"type"`
	Source   string    `json:"source"`
	Path     string    `json:"path,omitempty"`
	Ref      string    `json:"ref,omitempty"`
	Files    int       `json:"files"`
	Size     int64     `json:"size"`
	LastSync time.Time `json:"last_sync"`
}

type collectionsOutput struct {
	Collections []collectionInfo `json:"collections"`
}

type filesInput struct {
	Collection string `json:"collection"       jsonschema:"collection name from the collections tool"`
	Limit      int    `json:"limit,omitempty"  jsonschema:"maximum files to list; default 200, at most 1000"`
	Offset     int    `json:"offset,omitempty" jsonschema:"number of files to skip, from a previous next_offset"`
}

type fileInfo struct {
	Path        string `json:"path"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Size        int64  `json:"size"`
}

type filesOutput struct {
	Collection string     `json:"collection"`
	Total      int        `json:"total"`
	Offset     int        `json:"offset"`
	NextOffset int        `json:"next_offset,omitempty"`
	Files      []fileInfo `json:"files"`
}

type outlineInput struct {
	Collection string `json:"collection" jsonschema:"collection name from the collections tool"`
	Path       string `json:"path"       jsonschema:"file path inside the collection, as listed by files or search"`
}

type headingInfo struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	Line  int    `json:"line"`
}

type exportInfo struct {
	Type string `json:"type"`
	Name string `json:"name"`
	Line int    `json:"line"`
}

type outlineOutput struct {
	Collection string        `json:"collection"`
	Path       string        `json:"path"`
	Kind       string        `json:"kind"`
	Headings   []headingInfo `json:"headings,omitempty"`
	Exports    []exportInfo  `json:"exports,omitempty"`
}

type catInput struct {
	Collection string `json:"collection"      jsonschema:"collection name from the collections tool"`
	Path       string `json:"path"            jsonschema:"file path inside the collection, as listed by files or search"`
	Line       int    `json:"line,omitempty"  jsonschema:"1-based first line to read; default 1"`
	Limit      int    `json:"limit,omitempty" jsonschema:"maximum lines to read; default 500, at most 2000"`
}

type catOutput struct {
	Collection string `json:"collection"`
	Path       string `json:"path"`
	Type       string `json:"type"`
	TotalLines int    `json:"total_lines"`
	FirstLine  int    `json:"first_line"`
	LineCount  int    `json:"line_count"`
	Truncated  bool   `json:"truncated"`
	NextLine   int    `json:"next_line,omitempty"`
	Content    string `json:"content"`
}

type searchInput struct {
	Query      string `json:"query"                jsonschema:"text to find; for regex, a case-insensitive pattern"`
	Collection string `json:"collection,omitempty" jsonschema:"limit the search to one collection"`
	Content    bool   `json:"content,omitempty"    jsonschema:"search file contents line by line instead of titles, paths and headings"`
	Regex      bool   `json:"regex,omitempty"      jsonschema:"treat query as a regular expression; requires content"`
	Limit      int    `json:"limit,omitempty"      jsonschema:"maximum results; default 20, at most 100"`
}

type searchHit struct {
	Collection  string `json:"collection"`
	Path        string `json:"path"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
	MatchField  string `json:"match_field,omitempty"`
	MatchValue  string `json:"match_value,omitempty"`
	Score       int    `json:"score,omitempty"`
	Line        int    `json:"line,omitempty"`
	Text        string `json:"text,omitempty"`
}

type searchOutput struct {
	Results   []searchHit `json:"results"`
	Truncated bool        `json:"truncated"`
}

func (h handlers) register(server *mcp.Server) {
	mcp.AddTool(server, readOnly("collections",
		"List the documentation collections that were synced, with their source, file count and last sync time. "+
			"Start here to learn what documentation is available."), h.collections)
	mcp.AddTool(server, readOnly("files",
		"List the files of one collection with their type, description and size. "+
			"Long listings are paged with limit and offset."), h.files)
	mcp.AddTool(server, readOnly("outline",
		"Show a file's structure: headings for documents or exported symbols for code, each with its line number. "+
			"Use it to pick the lines to read with cat."), h.outline)
	mcp.AddTool(server, readOnly("cat",
		"Read a file, or a line range of it. Output is capped at 500 lines by default; "+
			"when truncated, call again with line set to next_line."), h.cat)
	mcp.AddTool(server, readOnly("search",
		"Find documentation. By default it fuzzy-matches titles, paths and headings; "+
			"with content=true it greps file contents and returns matching lines with line numbers."), h.search)
}

func readOnly(name, description string) *mcp.Tool {
	closedWorld := false
	return &mcp.Tool{
		Name:        name,
		Description: description,
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			IdempotentHint:  true,
			OpenWorldHint:   &closedWorld,
			DestructiveHint: nil,
			Title:           "",
		},
	}
}

func (h handlers) collections(
	ctx context.Context, _ *mcp.CallToolRequest, _ noInput,
) (*mcp.CallToolResult, collectionsOutput, error) {
	collections, err := h.docs.Collections(ctx)
	if err != nil {
		return nil, collectionsOutput{}, toolError(err)
	}
	out := collectionsOutput{Collections: make([]collectionInfo, len(collections))}
	for index := range collections {
		collection := &collections[index]
		out.Collections[index] = collectionInfo{
			Name:     collection.Name,
			Type:     collection.Type,
			Source:   collection.Source,
			Path:     collection.Path,
			Ref:      collection.Ref,
			Files:    collection.FileCount,
			Size:     collection.Size,
			LastSync: collection.LastSync,
		}
	}
	return nil, out, nil
}

func (h handlers) files(
	ctx context.Context, _ *mcp.CallToolRequest, in filesInput,
) (*mcp.CallToolResult, filesOutput, error) {
	if in.Limit < 0 || in.Offset < 0 {
		return nil, filesOutput{}, errors.New("limit and offset must not be negative")
	}
	files, err := h.docs.Files(ctx, in.Collection)
	if err != nil {
		return nil, filesOutput{}, toolError(err)
	}
	limit := bounded(in.Limit, DefaultFiles, MaxFiles)
	start := min(in.Offset, len(files))
	end := min(start+limit, len(files))
	out := filesOutput{
		Collection: in.Collection,
		Total:      len(files),
		Offset:     in.Offset,
		Files:      make([]fileInfo, 0, end-start),
	}
	for index := start; index < end; index++ {
		out.Files = append(out.Files, fileInfo{
			Path:        files[index].Path,
			Type:        files[index].Type,
			Description: files[index].Description,
			Size:        files[index].Size,
		})
	}
	if end < len(files) {
		out.NextOffset = end
	}
	return nil, out, nil
}

func (h handlers) outline(
	ctx context.Context, _ *mcp.CallToolRequest, in outlineInput,
) (*mcp.CallToolResult, outlineOutput, error) {
	outline, err := h.docs.Outline(ctx, in.Collection, in.Path)
	if err != nil {
		return nil, outlineOutput{}, toolError(err)
	}
	out := outlineOutput{Collection: in.Collection, Path: in.Path, Kind: string(outline.Kind)}
	for _, heading := range outline.Headings {
		out.Headings = append(out.Headings, headingInfo{Level: heading.Level, Text: heading.Text, Line: heading.Line})
	}
	for _, export := range outline.Exports {
		out.Exports = append(out.Exports, exportInfo{Type: export.Type, Name: export.Name, Line: export.Line})
	}
	return nil, out, nil
}

func (h handlers) cat(
	ctx context.Context, _ *mcp.CallToolRequest, in catInput,
) (*mcp.CallToolResult, catOutput, error) {
	if in.Line < 0 || in.Limit < 0 {
		return nil, catOutput{}, errors.New("line and limit must not be negative; use line 1 for the first line")
	}
	limit := bounded(in.Limit, DefaultCatLines, MaxCatLines)
	content, err := h.docs.Cat(ctx, in.Collection, in.Path, dox.Range{Line: in.Line, Limit: limit})
	if err != nil {
		return nil, catOutput{}, toolError(err)
	}
	out := catOutput{
		Collection: content.Collection,
		Path:       content.Path,
		Type:       content.Type,
		TotalLines: content.TotalLines,
		FirstLine:  content.Line,
		LineCount:  len(content.Lines),
		Content:    content.Text(),
	}
	next := content.Line + len(content.Lines)
	// Only the server's own cap counts as truncation; an explicit smaller
	// limit is what the caller asked for.
	if next <= content.TotalLines && (in.Limit == 0 || in.Limit > MaxCatLines) {
		out.Truncated, out.NextLine = true, next
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: catText(&out)}}}, out, nil
}

// catText is the plain text a host shows the model: the lines themselves,
// followed by how to continue when the server cut them short.
func catText(out *catOutput) string {
	switch {
	case out.Truncated:
		return fmt.Sprintf("%s\n\n[truncated: showing lines %d-%d of %d; call cat again with line=%d to continue]",
			out.Content, out.FirstLine, out.NextLine-1, out.TotalLines, out.NextLine)
	case out.LineCount == 0 && out.TotalLines == 0:
		return "[the file is empty]"
	case out.LineCount == 0:
		return fmt.Sprintf(
			"[no lines: the file has %d lines and line %d is past the end]",
			out.TotalLines,
			out.FirstLine,
		)
	default:
		return out.Content
	}
}

func (h handlers) search(
	ctx context.Context, _ *mcp.CallToolRequest, in searchInput,
) (*mcp.CallToolResult, searchOutput, error) {
	switch {
	case in.Limit < 0:
		return nil, searchOutput{}, errors.New("limit must not be negative")
	case in.Regex && !in.Content:
		return nil, searchOutput{}, errors.New("regex requires content to be true")
	}
	limit := bounded(in.Limit, DefaultSearchResults, MaxSearchResults)
	query := dox.Query{
		Text:       in.Query,
		Collection: in.Collection,
		Content:    in.Content,
		Regex:      in.Regex,
		// One extra result tells whether the list was cut short.
		Limit: limit + 1,
	}
	out := searchOutput{Results: []searchHit{}}
	for result, err := range h.docs.Search(ctx, query) {
		if err != nil {
			return nil, searchOutput{}, toolError(err)
		}
		if len(out.Results) == limit {
			out.Truncated = true
			break
		}
		out.Results = append(out.Results, searchHit{
			Collection:  result.Collection,
			Path:        result.Path,
			Type:        result.Type,
			Description: result.Description,
			MatchField:  result.MatchField,
			MatchValue:  result.MatchValue,
			Score:       result.Score,
			Line:        result.Line,
			Text:        result.Text,
		})
	}
	return nil, out, nil
}

// bounded applies the default to an unset value and the cap to a large one.
func bounded(value, fallback, maximum int) int {
	if value == 0 {
		return fallback
	}
	return min(value, maximum)
}

// toolError puts the hint in the message: MCP hands the error text to the
// model, which has no other way to see it.
func toolError(err error) error {
	if detail, ok := errors.AsType[*dox.Error](err); ok && detail.Hint != "" {
		return errors.New(strings.TrimRight(detail.Message, ".") + ".\nHint: " + detail.Hint)
	}
	return err
}
