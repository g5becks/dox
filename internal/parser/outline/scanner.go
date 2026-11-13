// Package outline scans Markdown structure without building a document AST.
package outline

import (
	"bytes"
	"strings"
)

// Heading identifies a heading at its 1-based source line.
type Heading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	Line  int    `json:"line"`
}

// Options enables MDX statement filtering while retaining source positions.
type Options struct{ MDX bool }

// Result contains the structure and description needed by a documentation index.
type Result struct {
	Headings    []Heading
	Description string
}

type scanner struct {
	rawHTML                                   string
	paragraphList                             bool
	options                                   Options
	result                                    Result
	inline                                    inlineText
	title, description                        string
	firstH1, firstParagraph, paragraphAfterH1 string
	paragraph                                 strings.Builder
	paragraphLine                             int
	fence                                     byte
	fenceLength                               int
	mdxDepth                                  int
	comment                                   bool
}

// Scan visits each body line once; inline and metadata decoding have fixed budgets.
func Scan(content []byte, options Options) Result {
	content = bytes.TrimPrefix(content, []byte{0xef, 0xbb, 0xbf})
	body, title, description, offset := Frontmatter(content)
	s := scanner{
		options:     options,
		title:       title,
		description: description,
		inline:      inlineText{remaining: maxInlineBytes},
	}
	for raw := range bytes.Lines(body) {
		offset++
		s.consume(bytes.TrimSuffix(bytes.TrimSuffix(raw, []byte{'\n'}), []byte{'\r'}), offset)
	}
	s.flushParagraph()
	s.result.Description = s.composeDescription()
	return s.result
}

func (s *scanner) consume(raw []byte, lineNumber int) {
	line, indented := prepareLine(raw)
	if s.fence != 0 {
		if !indented && closesFence(line, s.fence, s.fenceLength) {
			s.fence = 0
		}
		return
	}
	if indented {
		s.flushParagraph()
		return
	}
	if s.skipRawHTML(line) {
		s.flushParagraph()
		return
	}
	if s.comment || bytes.HasPrefix(line, []byte("<!--")) {
		s.comment = !bytes.Contains(line, []byte("-->"))
		s.flushParagraph()
		return
	}
	if marker, length := fenceMarker(line); length >= minFenceLength {
		s.flushParagraph()
		s.fence = marker
		s.fenceLength = length
		return
	}
	if s.options.MDX && s.skipMDX(line) {
		s.flushParagraph()
		return
	}
	s.consumeText(line, lineNumber)
}

func (s *scanner) consumeText(line []byte, lineNumber int) {
	if len(line) == 0 {
		s.flushParagraph()
		return
	}
	if item, isList := listItem(line); isList {
		s.flushParagraph()
		s.paragraphList = true
		line = item
	}
	if len(line) == 0 {
		return
	}
	if level, text := atxHeading(line); level > 0 {
		s.flushParagraph()
		s.emit(level, string(text), lineNumber)
		return
	}
	if level := setextLevel(line); level > 0 && s.paragraph.Len() > 0 && !s.paragraphList {
		text, firstLine := s.paragraph.String(), s.paragraphLine
		s.paragraph.Reset()
		s.emit(level, text, firstLine)
		return
	}
	if thematicBreak(line) || referenceDefinition(line) || tagOnly(line) {
		s.flushParagraph()
		return
	}
	if s.paragraph.Len() == 0 {
		s.paragraphLine = lineNumber
	} else {
		s.paragraph.WriteByte(' ')
	}
	s.paragraph.Write(line)
}

func (s *scanner) emit(level int, value string, line int) {
	rendered := s.inline.extract(value)
	if rendered == "" {
		return
	}
	s.result.Headings = append(s.result.Headings, Heading{Level: level, Text: rendered, Line: line})
	if level == 1 && s.firstH1 == "" {
		s.firstH1 = rendered
	}
}

func (s *scanner) flushParagraph() {
	if s.paragraph.Len() == 0 {
		s.paragraphList = false
		return
	}
	if s.firstParagraph == "" {
		value := s.inline.extract(s.paragraph.String())
		if value != "" {
			s.firstParagraph = value
			if s.firstH1 != "" {
				s.paragraphAfterH1 = value
			}
		}
	}
	s.paragraph.Reset()
	s.paragraphList = false
}

func (s *scanner) composeDescription() string {
	if s.title != "" && s.description != "" {
		return s.title + " - " + s.description
	}
	if s.title != "" {
		return s.title
	}
	if s.description != "" {
		return s.description
	}
	if s.firstH1 != "" {
		if s.paragraphAfterH1 != "" {
			return s.firstH1 + " - " + s.paragraphAfterH1
		}
		return s.firstH1
	}
	return s.firstParagraph
}
