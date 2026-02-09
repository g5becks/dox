package parser

import (
	"bytes"
	"strings"

	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/parser"
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
			processHeading(heading, original, body, &headings, &firstH1Text, &foundH1)
		} else if para, isPara := node.(*ast.Paragraph); isPara {
			processParagraph(para, &firstParagraph, &paragraphAfterH1, foundH1)
		}

		return ast.GoToNext
	})

	return headings, firstH1Text, firstParagraph, paragraphAfterH1
}

func processHeading(n *ast.Heading, original, body []byte, headings *[]Heading, firstH1 *string, foundH1 *bool) {
	text := extractText(n)
	if text == "" {
		return
	}

	*headings = append(*headings, Heading{
		Level: n.Level,
		Text:  text,
		Line:  countLines(original, body, n),
	})

	if n.Level == 1 && *firstH1 == "" {
		*firstH1 = text
		*foundH1 = true
	}
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

func countLines(original, body []byte, node ast.Node) int {
	offset := len(original) - len(body)
	container := node.AsContainer()
	if container != nil && container.Literal != nil {
		pos := bytes.Index(body, container.Literal)
		if pos != -1 {
			return bytes.Count(original[:offset+pos], []byte("\n")) + 1
		}
	}
	return 1
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
