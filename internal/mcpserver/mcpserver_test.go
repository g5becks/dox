package mcpserver_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/g5becks/dox/dox"
	"github.com/g5becks/dox/internal/mcpserver"
)

const guideMarkdown = `# Guide

Dox keeps documentation next to your code.

## Install

Run go install to get the binary.

## Usage

Run dox sync before you search.
`

// project serves each collection's body from a local server, optionally syncs
// it and returns the loaded project.
func project(t *testing.T, bodies map[string]string, synced bool) *dox.Project {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := bodies[strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".md")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	var config strings.Builder
	for name := range bodies {
		fmt.Fprintf(&config, "[sources.%[1]s]\ntype = \"url\"\nurl = \"%[2]s/%[1]s.md\"\nfilename = \"%[1]s.md\"\n\n",
			name, server.URL)
	}
	configPath := filepath.Join(t.TempDir(), "dox.toml")
	if err := os.WriteFile(configPath, []byte(config.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := dox.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if synced {
		if _, err = loaded.Sync(t.Context(), dox.SyncOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	return loaded
}

// connect starts the server over an in-memory transport and returns a client
// session, the way an MCP host would hold one.
func connect(t *testing.T, docs mcpserver.Docs) *mcp.ClientSession {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := mcpserver.New(docs, "v-test").Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test-host", Version: "v0"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func call(t *testing.T, session *mcp.ClientSession, tool string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: arguments})
	if err != nil {
		t.Fatalf("CallTool(%s, %v) protocol error = %v", tool, arguments, err)
	}
	return result
}

func text(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	var out strings.Builder
	for _, content := range result.Content {
		textContent, ok := content.(*mcp.TextContent)
		if !ok {
			t.Fatalf("content %T, want *mcp.TextContent", content)
		}
		out.WriteString(textContent.Text)
	}
	return out.String()
}

func decode[T any](t *testing.T, result *mcp.CallToolResult) T {
	t.Helper()
	if result.IsError {
		t.Fatalf("tool returned an error: %s", text(t, result))
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err = json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decoding %s: %v", raw, err)
	}
	return out
}

func numberedLines(count int, format string) string {
	var out strings.Builder
	for index := 1; index <= count; index++ {
		fmt.Fprintf(&out, format+"\n", index)
	}
	return out.String()
}

func TestInitializeAndListTools(t *testing.T) {
	t.Parallel()
	session := connect(t, project(t, map[string]string{"guide": guideMarkdown}, true))

	initialized := session.InitializeResult()
	if initialized.ServerInfo.Name != "dox" || initialized.ServerInfo.Version != "v-test" {
		t.Errorf("server info = %+v, want dox v-test", initialized.ServerInfo)
	}
	if initialized.Capabilities.Tools == nil {
		t.Error("tools capability not advertised")
	}
	if !strings.Contains(initialized.Instructions, "collections") {
		t.Errorf("instructions = %q, want workflow guidance", initialized.Instructions)
	}

	var names []string
	for tool, err := range session.Tools(t.Context(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, tool.Name)
		if tool.Description == "" {
			t.Errorf("tool %s has no description", tool.Name)
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("tool %s is not annotated read-only: %+v", tool.Name, tool.Annotations)
		}
		input, ok := tool.InputSchema.(map[string]any)
		if !ok || input["type"] != "object" {
			t.Errorf("tool %s input schema = %v, want an object schema", tool.Name, tool.InputSchema)
		}
		if tool.OutputSchema == nil {
			t.Errorf("tool %s has no output schema", tool.Name)
		}
	}
	slices.Sort(names)
	if want := []string{"cat", "collections", "files", "outline", "search"}; !slices.Equal(names, want) {
		t.Errorf("tools = %v, want %v", names, want)
	}
}

func TestCollectionsTool(t *testing.T) {
	t.Parallel()
	session := connect(t, project(t, map[string]string{"guide": guideMarkdown, "api": "# API\n"}, true))

	// A host may omit the arguments object for a tool without parameters.
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "collections"})
	if err != nil {
		t.Fatal(err)
	}
	got := decode[struct {
		Collections []struct {
			Name  string `json:"name"`
			Type  string `json:"type"`
			Files int    `json:"files"`
			Size  int64  `json:"size"`
		} `json:"collections"`
	}](t, result)
	if len(got.Collections) != 2 || got.Collections[0].Name != "api" || got.Collections[1].Name != "guide" {
		t.Fatalf("collections = %+v, want api and guide in order", got.Collections)
	}
	if guide := got.Collections[1]; guide.Type != "url" || guide.Files != 1 || guide.Size != int64(len(guideMarkdown)) {
		t.Errorf("guide = %+v, want one url file of %d bytes", guide, len(guideMarkdown))
	}
}

func TestFilesOutlineAndCatTools(t *testing.T) {
	t.Parallel()
	session := connect(t, project(t, map[string]string{"guide": guideMarkdown}, true))

	files := decode[struct {
		Collection string `json:"collection"`
		Total      int    `json:"total"`
		Files      []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"files"`
		NextOffset int `json:"next_offset"`
	}](t, call(t, session, "files", map[string]any{"collection": "guide"}))
	if files.Collection != "guide" || files.Total != 1 || len(files.Files) != 1 ||
		files.Files[0].Path != "guide.md" || files.NextOffset != 0 {
		t.Errorf("files = %+v, want the single guide.md and no next page", files)
	}

	outline := decode[struct {
		Kind     string `json:"kind"`
		Headings []struct {
			Level int    `json:"level"`
			Text  string `json:"text"`
			Line  int    `json:"line"`
		} `json:"headings"`
	}](t, call(t, session, "outline", map[string]any{"collection": "guide", "path": "guide.md"}))
	if outline.Kind != "headings" || len(outline.Headings) != 3 ||
		outline.Headings[1].Text != "Install" || outline.Headings[1].Line != 5 || outline.Headings[1].Level != 2 {
		t.Errorf("outline = %+v, want three headings with Install at line 5", outline)
	}

	type catOutput struct {
		Path       string `json:"path"`
		TotalLines int    `json:"total_lines"`
		FirstLine  int    `json:"first_line"`
		LineCount  int    `json:"line_count"`
		Truncated  bool   `json:"truncated"`
		NextLine   int    `json:"next_line"`
		Content    string `json:"content"`
	}
	slice := call(t, session, "cat", map[string]any{"collection": "guide", "path": "guide.md", "line": 5, "limit": 3})
	got := decode[catOutput](t, slice)
	if got.Content != "## Install\n\nRun go install to get the binary." || got.FirstLine != 5 || got.LineCount != 3 ||
		got.TotalLines != 11 || got.Truncated {
		t.Errorf("cat = %+v, want lines 5-7 of 11 and no truncation", got)
	}
	if text(t, slice) != got.Content {
		t.Errorf("cat text = %q, want the plain lines %q", text(t, slice), got.Content)
	}

	whole := decode[catOutput](t, call(t, session, "cat", map[string]any{"collection": "guide", "path": "guide.md"}))
	if whole.Content != strings.TrimSuffix(guideMarkdown, "\n") || whole.LineCount != 11 {
		t.Errorf("whole-file cat = %+v, want all 11 lines", whole)
	}
}

func TestCatPagesLargeFiles(t *testing.T) {
	t.Parallel()
	const total = 2300
	session := connect(t, project(t, map[string]string{"big": numberedLines(total, "line %d")}, true))
	type catOutput struct {
		FirstLine  int    `json:"first_line"`
		LineCount  int    `json:"line_count"`
		TotalLines int    `json:"total_lines"`
		Truncated  bool   `json:"truncated"`
		NextLine   int    `json:"next_line"`
		Content    string `json:"content"`
	}
	catAt := func(arguments map[string]any) (*mcp.CallToolResult, catOutput) {
		arguments["collection"], arguments["path"] = "big", "big.md"
		result := call(t, session, "cat", arguments)
		return result, decode[catOutput](t, result)
	}

	first, got := catAt(map[string]any{})
	if got.FirstLine != 1 || got.LineCount != mcpserver.DefaultCatLines || !got.Truncated ||
		got.NextLine != mcpserver.DefaultCatLines+1 || got.TotalLines != total {
		t.Errorf("first page = %+v, want %d lines, truncated, next line %d of %d",
			got, mcpserver.DefaultCatLines, mcpserver.DefaultCatLines+1, total)
	}
	footer := text(t, first)
	if !strings.Contains(footer, "line=501") || !strings.Contains(footer, "2300") ||
		!strings.HasPrefix(footer, "line 1\nline 2\n") {
		t.Errorf(
			"first page text = %q ... %q, want the lines then a paging footer",
			footer[:20],
			footer[len(footer)-120:],
		)
	}
	if !strings.HasPrefix(footer, got.Content) {
		t.Error("the text result does not start with the structured content lines")
	}

	_, last := catAt(map[string]any{"line": 2201})
	if last.LineCount != 100 || last.Truncated || last.NextLine != 0 {
		t.Errorf("last page = %+v, want 100 lines and no next page", last)
	}
	_, capped := catAt(map[string]any{"limit": 100000})
	if capped.LineCount != mcpserver.MaxCatLines || !capped.Truncated || capped.NextLine != mcpserver.MaxCatLines+1 {
		t.Errorf("limit over the cap = %+v, want %d lines, truncated, next line %d",
			capped, mcpserver.MaxCatLines, mcpserver.MaxCatLines+1)
	}
	_, exact := catAt(map[string]any{"line": 2201, "limit": 100})
	if exact.LineCount != 100 || exact.Truncated {
		t.Errorf("a page that ends at the last line = %+v, want no truncation", exact)
	}
}

func TestSearchTool(t *testing.T) {
	t.Parallel()
	session := connect(t, project(t, map[string]string{
		"guide": guideMarkdown,
		"many":  numberedLines(30, "needle on row %d"),
	}, true))
	type searchOutput struct {
		Results []struct {
			Collection string `json:"collection"`
			Path       string `json:"path"`
			Line       int    `json:"line"`
			Text       string `json:"text"`
			MatchField string `json:"match_field"`
		} `json:"results"`
		Truncated bool `json:"truncated"`
	}

	metadata := decode[searchOutput](t, call(t, session, "search", map[string]any{"query": "install"}))
	if len(metadata.Results) == 0 || metadata.Results[0].Collection != "guide" || metadata.Results[0].MatchField == "" {
		t.Errorf("metadata search = %+v, want a guide match with its field", metadata)
	}

	content := decode[searchOutput](t, call(t, session, "search",
		map[string]any{"query": "dox sync", "content": true, "collection": "guide"}))
	if len(content.Results) != 1 || content.Results[0].Line != 11 ||
		content.Results[0].Text != "Run dox sync before you search." || content.Truncated {
		t.Errorf("content search = %+v, want one match on line 11", content)
	}

	regex := decode[searchOutput](t, call(t, session, "search",
		map[string]any{"query": `go (install|build)`, "content": true, "regex": true}))
	if len(regex.Results) != 1 || regex.Results[0].Line != 7 {
		t.Errorf("regex search = %+v, want one match on line 7", regex)
	}

	defaulted := decode[searchOutput](t, call(t, session, "search", map[string]any{"query": "needle", "content": true}))
	if len(defaulted.Results) != mcpserver.DefaultSearchResults || !defaulted.Truncated {
		t.Errorf("default limit = %d results, truncated %v, want %d and truncated",
			len(defaulted.Results), defaulted.Truncated, mcpserver.DefaultSearchResults)
	}
	small := decode[searchOutput](t, call(t, session, "search",
		map[string]any{"query": "needle", "content": true, "limit": 5}))
	if len(small.Results) != 5 || !small.Truncated {
		t.Errorf("limit 5 = %d results, truncated %v, want 5 and truncated", len(small.Results), small.Truncated)
	}
	all := decode[searchOutput](t, call(t, session, "search",
		map[string]any{"query": "needle", "content": true, "limit": mcpserver.MaxSearchResults}))
	if len(all.Results) != 30 || all.Truncated {
		t.Errorf("limit %d = %d results, truncated %v, want all 30", mcpserver.MaxSearchResults,
			len(all.Results), all.Truncated)
	}
}

func TestFailuresAreToolErrorsTheModelCanRead(t *testing.T) {
	t.Parallel()
	session := connect(t, project(t, map[string]string{"guide": guideMarkdown}, true))
	tests := []struct {
		name      string
		tool      string
		arguments map[string]any
		want      []string
	}{
		{"unknown collection", "files", map[string]any{"collection": "gide"}, []string{"gide", "guide"}},
		{
			"unknown file",
			"cat",
			map[string]any{"collection": "guide", "path": "gide.md"},
			[]string{"gide.md", "guide.md"},
		},
		{
			"outline of unknown file",
			"outline",
			map[string]any{"collection": "guide", "path": "nope.md"},
			[]string{"nope.md"},
		},
		{
			"search in unknown collection",
			"search",
			map[string]any{"query": "x", "collection": "gide"},
			[]string{"guide"},
		},
		{"invalid regex", "search", map[string]any{"query": "(", "content": true, "regex": true}, []string{"regex"}},
		{"regex without content", "search", map[string]any{"query": "x", "regex": true}, []string{"content"}},
		{"blank query", "search", map[string]any{"query": "  "}, []string{"empty"}},
		{"negative limit", "search", map[string]any{"query": "x", "limit": -1}, []string{"limit"}},
		{
			"negative line",
			"cat",
			map[string]any{"collection": "guide", "path": "guide.md", "line": -1},
			[]string{"line"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := call(t, session, tt.tool, tt.arguments)
			if !result.IsError {
				t.Fatalf("%s %v succeeded: %s", tt.tool, tt.arguments, text(t, result))
			}
			message := text(t, result)
			for _, want := range tt.want {
				if !strings.Contains(message, want) {
					t.Errorf("error %q does not mention %q", message, want)
				}
			}
		})
	}
}

func TestMissingRequiredArgumentsAreRejected(t *testing.T) {
	t.Parallel()
	session := connect(t, project(t, map[string]string{"guide": guideMarkdown}, true))
	for tool, arguments := range map[string]map[string]any{
		"files":   {},
		"outline": {"collection": "guide"},
		"cat":     {"path": "guide.md"},
		"search":  {},
	} {
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: arguments})
		if err == nil && !result.IsError {
			t.Errorf("%s %v succeeded, want a rejection", tool, arguments)
		}
	}
}

func TestUnsyncedProjectTellsTheModelToSync(t *testing.T) {
	t.Parallel()
	session := connect(t, project(t, map[string]string{"guide": guideMarkdown}, false))
	result := call(t, session, "collections", nil)
	if !result.IsError || !strings.Contains(text(t, result), "dox sync") {
		t.Errorf("collections before sync = %q (error %v), want guidance to run dox sync",
			text(t, result), result.IsError)
	}
}

// fakeDocs lists many files so paging can be exercised without a large sync.
type fakeDocs struct {
	mcpserver.Docs

	files []dox.File
}

func (f fakeDocs) Files(context.Context, string) ([]dox.File, error) { return f.files, nil }

func (fakeDocs) Search(context.Context, dox.Query) iter.Seq2[dox.Result, error] { return nil }

func TestFilesPagesLongListings(t *testing.T) {
	t.Parallel()
	docs := fakeDocs{files: make([]dox.File, 250)}
	for index := range docs.files {
		docs.files[index].Path = fmt.Sprintf("doc-%03d.md", index)
	}
	session := connect(t, docs)
	type filesOutput struct {
		Total      int `json:"total"`
		Offset     int `json:"offset"`
		NextOffset int `json:"next_offset"`
		Files      []struct {
			Path string `json:"path"`
		} `json:"files"`
	}

	first := decode[filesOutput](t, call(t, session, "files", map[string]any{"collection": "x"}))
	if first.Total != 250 || len(first.Files) != mcpserver.DefaultFiles || first.NextOffset != mcpserver.DefaultFiles ||
		first.Files[0].Path != "doc-000.md" {
		t.Errorf("first page = total %d, %d files, next %d; want 250, %d, %d",
			first.Total, len(first.Files), first.NextOffset, mcpserver.DefaultFiles, mcpserver.DefaultFiles)
	}
	last := decode[filesOutput](t, call(t, session, "files", map[string]any{"collection": "x", "offset": 200}))
	if len(last.Files) != 50 || last.NextOffset != 0 || last.Files[0].Path != "doc-200.md" || last.Offset != 200 {
		t.Errorf("last page = %d files from %d, next %d; want 50 from 200 and no next page",
			len(last.Files), last.Offset, last.NextOffset)
	}
	past := decode[filesOutput](t, call(t, session, "files", map[string]any{"collection": "x", "offset": 999}))
	if len(past.Files) != 0 || past.NextOffset != 0 {
		t.Errorf("offset past the end = %+v, want an empty page", past)
	}
	if result := call(t, session, "files", map[string]any{"collection": "x", "offset": -1}); !result.IsError {
		t.Error("negative offset succeeded")
	}
}

// serveAsync runs Serve on one end of an in-memory transport and returns a
// connected client plus a channel that receives Serve's result.
func serveAsync(ctx context.Context, t *testing.T) (*mcp.ClientSession, <-chan error) {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	docs := project(t, map[string]string{"guide": guideMarkdown}, true)
	done := make(chan error, 1)
	go func() { done <- mcpserver.Serve(ctx, docs, "v-test", serverTransport) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-host", Version: "v0"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	return session, done
}

func waitForServe(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve() error = %v, want nil on a normal shutdown", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve() did not return")
	}
}

func TestServeReturnsWhenTheClientDisconnects(t *testing.T) {
	t.Parallel()
	session, done := serveAsync(t.Context(), t)
	if err := session.Close(); err != nil {
		t.Logf("client close: %v", err)
	}
	waitForServe(t, done)
}

func TestServeStopsWhenTheContextIsCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	session, done := serveAsync(ctx, t)
	defer func() { _ = session.Close() }()
	cancel()
	waitForServe(t, done)
}

// lockedBuffer is an io.WriteCloser that is safe to write from the server's
// handler goroutines and read after Serve returns.
type lockedBuffer struct {
	mu   sync.Mutex
	data strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.Write(p)
}

func (*lockedBuffer) Close() error { return nil }

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.String()
}

// TestServeAnswersBatchedRequestsBeforeInputEnds covers `printf ... | dox mcp`
// and one-shot clients that close stdin right after their last request: the
// SDK would otherwise cancel every pending call when it reads the end of
// input.
func TestServeAnswersBatchedRequestsBeforeInputEnds(t *testing.T) {
	t.Parallel()
	requests := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18",` +
			`"capabilities":{},"clientInfo":{"name":"batch","version":"0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"collections","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"search","arguments":{"query":"install"}}}`,
	}, "\n") + "\n"
	var out lockedBuffer
	transport := &mcp.IOTransport{Reader: io.NopCloser(strings.NewReader(requests)), Writer: &out}
	docs := project(t, map[string]string{"guide": guideMarkdown}, true)

	done := make(chan error, 1)
	go func() { done <- mcpserver.Serve(t.Context(), docs, "v-test", transport) }()
	waitForServe(t, done)

	answered := map[float64]bool{}
	for line := range strings.SplitSeq(out.String(), "\n") {
		if line == "" {
			continue
		}
		var message struct {
			ID    float64 `json:"id"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			t.Fatalf("stdout line %q is not JSON: %v", line, err)
		}
		if message.Error != nil {
			t.Errorf("request %v failed: %s", message.ID, message.Error.Message)
		}
		answered[message.ID] = true
	}
	for _, id := range []float64{1, 2, 3, 4} {
		if !answered[id] {
			t.Errorf("request %v was not answered; output:\n%s", id, out.String())
		}
	}
}
