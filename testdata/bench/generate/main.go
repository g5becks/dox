// Command generate creates the deterministic whitespace-heavy Markdown fixture.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
)

func main() {
	output := flag.String("output", "testdata/bench/pathological.md", "output file")
	flag.Parse()
	const size = 1024 * 1024
	var content bytes.Buffer
	// Geometrically increasing lines make the old table-header probe repeatedly
	// rescan long runs of spaces, even though the document contains no tables.
	for width := 64; content.Len()+width <= size; width *= 2 {
		content.WriteByte('x')
		content.Write(bytes.Repeat([]byte(" "), width/16))
		content.Write(bytes.Repeat([]byte("a"), width-width/16-2))
		content.WriteByte('\n')
	}
	content.Write(bytes.Repeat([]byte("x"), size-content.Len()))
	if err := os.WriteFile(*output, content.Bytes(), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
