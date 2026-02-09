package parser

import (
	"bytes"
	"strings"
)

type TextParser struct{}

func NewTextParser() *TextParser {
	return &TextParser{}
}

func (p *TextParser) CanParse(path string) bool {
	return DetectFileType(path) == "txt"
}

func (p *TextParser) Parse(_ string, content []byte) (*ParseResult, error) {
	content = StripBOM(content)
	lines := bytes.Count(content, []byte("\n")) + 1

	description := extractFirstLine(content)

	return &ParseResult{
		Description: description,
		Outline: &Outline{
			Type: OutlineTypeNone,
		},
		Lines: lines,
	}, nil
}

func extractFirstLine(content []byte) string {
	for line := range bytes.SplitSeq(content, []byte("\n")) {
		trimmed := strings.TrimSpace(string(line))
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
