package parser

import (
	"bytes"
	"regexp"
)

var (
	importLineRegex = regexp.MustCompile(`(?m)^\s*import\s+`)
	exportMetaRegex = regexp.MustCompile(`(?m)^\s*export\s+(const|let|var)\s+\w+\s*=`)
)

type MDXParser struct {
	md *MarkdownParser
}

func NewMDXParser() *MDXParser {
	return &MDXParser{md: NewMarkdownParser()}
}

func (p *MDXParser) CanParse(path string) bool {
	return DetectFileType(path) == "mdx"
}

func (p *MDXParser) Parse(_ string, content []byte) (*ParseResult, error) {
	content = StripBOM(content)
	body, fmTitle, fmDesc := StripFrontmatter(content)

	cleaned := stripMDXSyntax(body)
	result, err := p.md.Parse("", cleaned)
	if err != nil {
		return nil, err
	}

	if fmTitle != "" || fmDesc != "" {
		switch {
		case fmTitle != "" && fmDesc != "":
			result.Description = fmTitle + " - " + fmDesc
		case fmTitle != "":
			result.Description = fmTitle
		default:
			result.Description = fmDesc
		}
	}

	result.Lines = bytes.Count(content, []byte("\n")) + 1
	return result, nil
}

func stripMDXSyntax(content []byte) []byte {
	lines := bytes.Split(content, []byte("\n"))
	var cleaned [][]byte

	for _, line := range lines {
		if importLineRegex.Match(line) {
			continue
		}
		if exportMetaRegex.Match(line) {
			continue
		}
		cleaned = append(cleaned, line)
	}

	return bytes.Join(cleaned, []byte("\n"))
}
