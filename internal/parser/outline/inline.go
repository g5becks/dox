package outline

import (
	"html"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const maxInlineBytes = 256 * 1024
const inlinePrefix = "x "

type inlineText struct {
	markdown  goldmark.Markdown
	remaining int
}

func (p *inlineText) extract(value string) string {
	plain := strings.Join(strings.Fields(value), " ")
	if !strings.ContainsAny(value, "*_~[]`<&\\!") {
		return plain
	}
	if len(value)+len(inlinePrefix) > p.remaining {
		return plain
	}
	p.remaining -= len(value) + len(inlinePrefix)
	if p.markdown == nil {
		p.markdown = goldmark.New(goldmark.WithExtensions(extension.Strikethrough))
	}
	source := []byte(inlinePrefix + value)
	document := p.markdown.Parser().Parse(text.NewReader(source))
	var output strings.Builder
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := node.(type) {
		case *ast.CodeSpan, *ast.CodeBlock, *ast.FencedCodeBlock, *ast.HTMLBlock:
			// The previous renderer omitted code literals from heading/paragraph text.
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			output.Write(n.Segment.Value(source))
			if n.SoftLineBreak() || n.HardLineBreak() {
				output.WriteByte(' ')
			}
		case *ast.AutoLink:
			output.Write(n.Label(source))
		case *ast.String:
			if !n.IsCode() {
				output.Write(n.Value)
			}
		}
		return ast.WalkContinue, nil
	})
	rendered := html.UnescapeString(
		string(util.UnescapePunctuations([]byte(strings.TrimPrefix(output.String(), inlinePrefix)))),
	)
	return strings.Join(strings.Fields(rendered), " ")
}
