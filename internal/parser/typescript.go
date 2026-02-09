package parser

import (
	"bytes"
	"regexp"
	"strings"
)

var (
	jsxHeadingRegex = regexp.MustCompile(`<h([1-6])[^>]*>(.*?)</h[1-6]>`)
	exportRegex     = regexp.MustCompile(`(?m)^\s*export\s+(const|function|interface|type|class)\s+(\w+)`)
	jsdocRegex      = regexp.MustCompile(`(?s)/\*\*\s*\n(.*?)\*/`)
	jsxTagStripper  = regexp.MustCompile(`<[^>]+>`)
)

type TypeScriptParser struct{}

func NewTypeScriptParser() *TypeScriptParser {
	return &TypeScriptParser{}
}

func (p *TypeScriptParser) CanParse(path string) bool {
	ft := DetectFileType(path)
	return ft == "tsx" || ft == "ts"
}

func (p *TypeScriptParser) Parse(_ string, content []byte) (*ParseResult, error) {
	content = StripBOM(content)
	lines := bytes.Count(content, []byte("\n")) + 1

	headings := extractJSXHeadings(content)
	exports := extractExports(content)

	const minHeadingsForDoc = 2
	isDocComponent := len(headings) >= minHeadingsForDoc

	var description string
	var outline *Outline
	var componentType ComponentType

	if isDocComponent {
		componentType = ComponentTypeDocumentation
		description = buildTSXDescription(headings)
		outline = &Outline{
			Type:     OutlineTypeHeadings,
			Headings: headings,
		}
	} else {
		componentType = ComponentTypeCode
		description = buildCodeDescription(content, exports)
		outline = &Outline{
			Type:    OutlineTypeExports,
			Exports: exports,
		}
	}

	return &ParseResult{
		Description:   description,
		ComponentType: componentType,
		Outline:       outline,
		Lines:         lines,
	}, nil
}

func extractJSXHeadings(content []byte) []Heading {
	matches := jsxHeadingRegex.FindAllSubmatch(content, -1)
	headings := make([]Heading, 0, len(matches))

	for _, match := range matches {
		level := int(match[1][0] - '0')
		text := string(match[2])
		text = jsxTagStripper.ReplaceAllString(text, "")
		text = strings.TrimSpace(text)

		if text == "" {
			continue
		}

		lineNum := lineNumberAt(content, bytes.Index(content, match[0]))
		headings = append(headings, Heading{
			Level: level,
			Text:  text,
			Line:  lineNum,
		})
	}

	return headings
}

func extractExports(content []byte) []Export {
	matches := exportRegex.FindAllSubmatch(content, -1)
	exports := make([]Export, 0, len(matches))

	for _, match := range matches {
		exportType := string(match[1])
		name := string(match[2])
		lineNum := lineNumberAt(content, bytes.Index(content, match[0]))

		exports = append(exports, Export{
			Type: exportType,
			Name: name,
			Line: lineNum,
		})
	}

	return exports
}

func lineNumberAt(content []byte, offset int) int {
	if offset < 0 || offset > len(content) {
		return 1
	}
	return bytes.Count(content[:offset], []byte("\n")) + 1
}

func buildTSXDescription(headings []Heading) string {
	if len(headings) == 0 {
		return ""
	}

	for _, h := range headings {
		if h.Level == 1 {
			return h.Text
		}
	}

	return headings[0].Text
}

func buildCodeDescription(content []byte, exports []Export) string {
	jsdocMatches := jsdocRegex.FindSubmatch(content)
	if len(jsdocMatches) > 1 {
		doc := string(jsdocMatches[1])
		for line := range strings.SplitSeq(doc, "\n") {
			line = strings.TrimSpace(line)
			line = strings.TrimPrefix(line, "*")
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "@") {
				return line
			}
		}
	}

	if len(exports) > 0 {
		return exports[0].Type + " " + exports[0].Name
	}

	return ""
}
