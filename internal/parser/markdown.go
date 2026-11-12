package parser

import "github.com/g5becks/dox/internal/parser/outline"

type MarkdownParser struct{}

func NewMarkdownParser() *MarkdownParser            { return &MarkdownParser{} }
func (p *MarkdownParser) CanParse(path string) bool { return DetectFileType(path) == "md" }
func (p *MarkdownParser) Parse(_ string, content []byte) (*ParseResult, error) {
	return scanOutline(content, false), nil
}
func scanOutline(content []byte, mdx bool) *ParseResult {
	result := outline.Scan(content, outline.Options{MDX: mdx})
	return &ParseResult{
		Description: result.Description,
		Outline:     &Outline{Type: OutlineTypeHeadings, Headings: result.Headings},
		Lines:       result.Lines,
	}
}
