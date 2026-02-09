package parser

import (
	"bytes"
	"strings"

	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/parser"
)

const (
	setextH1Level = 1
	setextH2Level = 2
)

type MarkdownParser struct{}

func NewMarkdownParser() *MarkdownParser {
	return &MarkdownParser{}
}

func (p *MarkdownParser) CanParse(path string) bool {
	return DetectFileType(path) == "md"
}

func (p *MarkdownParser) Parse(_ string, content []byte) (*ParseResult, error) {
	content = StripBOM(content)
	body, fmTitle, fmDesc := StripFrontmatter(content)

	mdParser := parser.NewWithExtensions(parser.CommonExtensions)
	doc := mdParser.Parse(body)

	headings, firstH1, firstPara, paraAfterH1 := extractMarkdownContent(doc, content, body)
	description := buildDescription(fmTitle, fmDesc, firstH1, paraAfterH1, firstPara)
	lines := bytes.Count(content, []byte("\n")) + 1

	return &ParseResult{
		Description: description,
		Outline: &Outline{
			Type:     OutlineTypeHeadings,
			Headings: headings,
		},
		Lines: lines,
	}, nil
}

func extractMarkdownContent(doc ast.Node, original, body []byte) ([]Heading, string, string, string) {
	var headings []Heading
	var firstH1Text string
	var firstParagraph string
	var paragraphAfterH1 string
	foundH1 := false

	ast.WalkFunc(doc, func(node ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.GoToNext
		}

		if heading, isHeading := node.(*ast.Heading); isHeading {
			text := extractText(heading)
			if text != "" {
				headings = append(headings, Heading{
					Level: heading.Level,
					Text:  text,
				})
				if heading.Level == 1 && firstH1Text == "" {
					firstH1Text = text
					foundH1 = true
				}
			}
		} else if para, isPara := node.(*ast.Paragraph); isPara {
			processParagraph(para, &firstParagraph, &paragraphAfterH1, foundH1)
		}

		return ast.GoToNext
	})

	// Compute frontmatter line count so heading line numbers reflect the full file
	fmLineOffset := bytes.Count(original[:len(original)-len(body)], []byte("\n"))
	assignHeadingLineNumbers(headings, body, fmLineOffset)
	return headings, firstH1Text, firstParagraph, paragraphAfterH1
}

func processParagraph(n *ast.Paragraph, firstPara, paraAfterH1 *string, foundH1 bool) {
	if *firstPara != "" {
		return
	}

	text := extractText(n)
	if text != "" {
		*firstPara = text
		if foundH1 && *paraAfterH1 == "" {
			*paraAfterH1 = text
		}
	}
}

func extractText(node ast.Node) string {
	var buf strings.Builder
	ast.WalkFunc(node, func(n ast.Node, entering bool) ast.WalkStatus {
		if entering {
			if text, ok := n.(*ast.Text); ok {
				buf.Write(text.Literal)
			}
		}
		return ast.GoToNext
	})
	text := strings.TrimSpace(buf.String())
	// Normalize whitespace - replace multiple spaces/newlines with single space
	text = strings.Join(strings.Fields(text), " ")
	return text
}

// assignHeadingLineNumbers scans content for heading markers and assigns
// the correct line number to each heading in document order.
// This is necessary because gomarkdown's AST does not store source positions.
func assignHeadingLineNumbers(headings []Heading, content []byte, lineOffset int) {
	if len(headings) == 0 {
		return
	}

	lines := bytes.Split(content, []byte("\n"))
	hi := 0
	inFenced := false

	for lineIdx := 0; lineIdx < len(lines) && hi < len(headings); lineIdx++ {
		line := lines[lineIdx]
		trimmed := bytes.TrimSpace(line)

		if isFenceMarker(trimmed) {
			inFenced = !inFenced
			continue
		}
		if inFenced {
			continue
		}

		if level := atxHeadingLevel(line); level == headings[hi].Level {
			headings[hi].Line = lineOffset + lineIdx + 1
			hi++
			continue
		}

		if level := setextHeadingLevel(lines, lineIdx, trimmed); level == headings[hi].Level {
			headings[hi].Line = lineOffset + lineIdx + 1
			hi++
		}
	}
}

func isFenceMarker(trimmed []byte) bool {
	return bytes.HasPrefix(trimmed, []byte("```")) || bytes.HasPrefix(trimmed, []byte("~~~"))
}

// atxHeadingLevel returns the heading level (1-6) for an ATX heading line,
// or 0 if the line is not an ATX heading.
func atxHeadingLevel(line []byte) int {
	spaces := 0
	for spaces < len(line) && spaces < 4 && line[spaces] == ' ' {
		spaces++
	}
	if spaces >= 4 || spaces >= len(line) || line[spaces] != '#' {
		return 0
	}

	level := 0
	for spaces+level < len(line) && level < 7 && line[spaces+level] == '#' {
		level++
	}
	if level >= 1 && level <= 6 && spaces+level < len(line) && line[spaces+level] == ' ' {
		return level
	}
	return 0
}

// setextHeadingLevel returns the heading level for setext-style headings
// (1 for === underline, 2 for --- underline), or 0 if not a setext heading.
func setextHeadingLevel(lines [][]byte, lineIdx int, trimmed []byte) int {
	if lineIdx+1 >= len(lines) || len(trimmed) == 0 {
		return 0
	}
	nextTrimmed := bytes.TrimSpace(lines[lineIdx+1])
	if allSameChar(nextTrimmed, '=') {
		return setextH1Level
	}
	if allSameChar(nextTrimmed, '-') {
		return setextH2Level
	}
	return 0
}

func allSameChar(b []byte, ch byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		if c != ch {
			return false
		}
	}
	return true
}

func buildDescription(fmTitle, fmDesc, firstH1, paragraphAfterH1, firstParagraph string) string {
	if fmTitle != "" && fmDesc != "" {
		return fmTitle + " - " + fmDesc
	}
	if fmTitle != "" {
		return fmTitle
	}
	if fmDesc != "" {
		return fmDesc
	}
	if firstH1 != "" {
		if paragraphAfterH1 != "" {
			return firstH1 + " - " + paragraphAfterH1
		}
		return firstH1
	}
	return firstParagraph
}
