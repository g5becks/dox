package main

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/g5becks/dox/dox"
	"github.com/g5becks/dox/internal/mcpserver"
)

const mcpDescription = `Starts a Model Context Protocol server on standard input and output so an AI
agent can browse the synced documentation. It exposes five read-only tools:
collections, files, outline, cat and search. It never syncs or writes; run
'dox sync' yourself, and the server sees new content on its next call.

Configure your MCP client to run this command, for example:

  {"mcpServers": {"dox": {"command": "dox", "args": ["mcp"]}}}

Pass the root --config before the command (dox --config path/to/dox.toml mcp)
when the client does not start in the project directory.`

func newMCPCommand() *cli.Command {
	return &cli.Command{
		Name:        "mcp",
		Usage:       "Serve the synced documentation to AI agents over MCP (stdio)",
		Description: mcpDescription,
		Action:      mcpAction,
	}
}

func mcpAction(ctx context.Context, cmd *cli.Command) error {
	var options []dox.LoadOption
	if cmd.Bool(flagAllowExternalOutput) {
		options = append(options, dox.WithExternalOutput())
	}
	project, err := dox.Load(cmd.String(flagConfig), options...)
	if err != nil {
		return err
	}
	if stdinIsTerminal() {
		// stdout carries the protocol, so guidance for a human goes to stderr.
		fmt.Fprintln(os.Stderr, "dox mcp: serving MCP on stdin/stdout; run it from an MCP client (Ctrl+C to stop)")
	}
	return mcpserver.ServeStdio(ctx, project, currentBuildMetadata().version)
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
