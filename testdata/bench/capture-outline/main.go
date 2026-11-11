package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/g5becks/dox/internal/parser"
)

type row struct {
	Path   string
	Result *parser.ParseResult
	Error  string `json:",omitempty"`
}

func main() {
	root := os.Args[1]
	encoder := json.NewEncoder(os.Stdout)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > 256*1024 {
			return nil
		}
		var p parser.Parser
		switch parser.DetectFileType(path) {
		case "md":
			p = parser.NewMarkdownParser()
		case "mdx":
			p = parser.NewMDXParser()
		default:
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result, parseErr := p.Parse(path, body)
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		record := row{Path: filepath.ToSlash(relative), Result: result}
		if parseErr != nil {
			record.Error = parseErr.Error()
		}
		return encoder.Encode(record)
	})
	if err != nil {
		panic(err)
	}
}
