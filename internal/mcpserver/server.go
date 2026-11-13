// Package mcpserver serves synced documentation to AI agents over the Model
// Context Protocol. It exposes the read-only half of dox (collections, files,
// outline, cat and search) as MCP tools; syncing stays a deliberate action of
// the person running dox.
package mcpserver

import (
	"context"
	"iter"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/g5becks/dox/dox"
)

// Output bounds. They keep one tool call from flooding an agent's context;
// every bounded tool reports how to fetch the next page.
const (
	// DefaultCatLines is how many lines cat returns when no limit is given.
	DefaultCatLines = 500
	// MaxCatLines is the most lines one cat call returns, whatever the limit.
	MaxCatLines = 2000
	// DefaultSearchResults is how many results search returns by default.
	DefaultSearchResults = 20
	// MaxSearchResults is the most results one search call returns.
	MaxSearchResults = 100
	// DefaultFiles is how many files one files call lists by default.
	DefaultFiles = 200
	// MaxFiles is the most files one files call lists.
	MaxFiles = 1000
)

const instructions = `dox serves documentation that was synced to local files.
Work from broad to narrow: "collections" lists what is available; "search"
finds where something is (set content=true to grep file contents); "outline"
shows a file's headings or exports with line numbers; "cat" reads a file, so
pass line and limit to read only the part you need. Long results are paged:
follow next_line (cat) and next_offset (files), and narrow search queries
instead of raising limits. If a tool reports that nothing has been synced,
ask the user to run "dox sync".`

// Docs is the part of a dox.Project the server reads from. A *dox.Project
// satisfies it.
type Docs interface {
	Collections(ctx context.Context) ([]dox.Collection, error)
	Files(ctx context.Context, collection string) ([]dox.File, error)
	Outline(ctx context.Context, collection, path string) (*dox.Outline, error)
	Cat(ctx context.Context, collection, path string, selection dox.Range) (*dox.Content, error)
	Search(ctx context.Context, query dox.Query) iter.Seq2[dox.Result, error]
}

// New returns an MCP server exposing docs as read-only tools. version is
// reported to clients in the initialize result.
func New(docs Docs, version string) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "dox", Version: version},
		&mcp.ServerOptions{Instructions: instructions},
	)
	handlers := handlers{docs: docs}
	handlers.register(server)
	return server
}

// Serve runs the server on transport until the client disconnects or ctx is
// cancelled. Both are normal shutdowns and return nil. Calls already read when
// the input ends are still answered.
func Serve(ctx context.Context, docs Docs, version string, transport mcp.Transport) error {
	err := New(docs, version).Run(ctx, drainTransport{inner: transport})
	select {
	case <-ctx.Done():
		return nil
	default:
		return err
	}
}

// ServeStdio serves docs on standard input and output. Nothing else may write
// to stdout while it runs.
func ServeStdio(ctx context.Context, docs Docs, version string) error {
	return Serve(ctx, docs, version, &mcp.StdioTransport{})
}
