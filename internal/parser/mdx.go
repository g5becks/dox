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
	inBlock := false

	for _, line := range lines {
		if inBlock {
			// Check if this line closes the block
			trimmed := bytes.TrimSpace(line)
			if bytes.HasPrefix(trimmed, []byte("}")) ||
				bytes.HasSuffix(trimmed, []byte(")")) ||
				(!bytes.ContainsAny(trimmed, "{}()") && bytes.Contains(trimmed, []byte("from "))) {
				inBlock = false
			}
			continue
		}

		if importLineRegex.Match(line) {
			// Check if this is a multi-line import (has { but no closing })
			if bytes.Contains(line, []byte("{")) && !bytes.Contains(line, []byte("}")) {
				inBlock = true
			}
			continue
		}
		if exportMetaRegex.Match(line) {
			// Check if multi-line (has { but no closing })
			if bytes.Contains(line, []byte("{")) && !bytes.Contains(line, []byte("}")) {
				inBlock = true
			}
			continue
		}

		cleaned = append(cleaned, line)
	}

	return bytes.Join(cleaned, []byte("\n"))
}
