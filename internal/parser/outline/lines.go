package outline

import (
	"bytes"
	"strings"
)

const (
	setextH1Level   = 1
	setextH2Level   = 2
	minFenceLength  = 3
	listMarkerWidth = 2
)

func prepareLine(raw []byte) ([]byte, bool) {
	spaces := 0
	for spaces < len(raw) && raw[spaces] == ' ' {
		spaces++
	}
	if spaces >= 4 || (spaces < len(raw) && raw[spaces] == '\t') {
		return nil, true
	}
	line := raw[spaces:]
	for len(line) > 0 && line[0] == '>' {
		line = line[1:]
		if len(line) > 0 && line[0] == ' ' {
			line = line[1:]
		}
		line = bytes.TrimLeft(line, " ")
	}
	return bytes.TrimSpace(line), false
}

func fenceMarker(line []byte) (byte, int) {
	if len(line) == 0 || (line[0] != '`' && line[0] != '~') {
		return 0, 0
	}
	count := 0
	for count < len(line) && line[count] == line[0] {
		count++
	}
	return line[0], count
}

func closesFence(line []byte, marker byte, length int) bool {
	candidate, count := fenceMarker(line)
	return candidate == marker && count >= length && len(bytes.TrimSpace(line[count:])) == 0
}

func atxHeading(line []byte) (int, []byte) {
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level < 1 || level > 6 {
		return 0, nil
	}
	if level < len(line) && line[level] != ' ' && line[level] != '\t' {
		return 0, nil
	}
	value := bytes.TrimSpace(line[level:])
	end := len(value)
	for end > 0 && value[end-1] == '#' {
		end--
	}
	if end < len(value) && (end == 0 || value[end-1] == ' ' || value[end-1] == '\t') {
		value = bytes.TrimSpace(value[:end])
	}
	// Preserve the old explicit-header-ID behavior without putting IDs in text.
	if index := bytes.LastIndex(value, []byte(" {#")); index >= 0 && bytes.HasSuffix(value, []byte{'}'}) {
		value = bytes.TrimSpace(value[:index])
	}
	return level, value
}

func setextLevel(line []byte) int {
	if allCharacters(line, '=') {
		return setextH1Level
	}
	if allCharacters(line, '-') {
		return setextH2Level
	}
	return 0
}

func allCharacters(line []byte, char byte) bool {
	if len(line) == 0 {
		return false
	}
	for _, b := range line {
		if b != char {
			return false
		}
	}
	return true
}

func thematicBreak(line []byte) bool {
	if len(line) == 0 || (line[0] != '-' && line[0] != '*' && line[0] != '_') {
		return false
	}
	compact := bytes.ReplaceAll(bytes.ReplaceAll(line, []byte{' '}, nil), []byte{'\t'}, nil)
	return len(compact) >= 3 &&
		(allCharacters(compact, '-') || allCharacters(compact, '*') || allCharacters(compact, '_'))
}

func referenceDefinition(line []byte) bool {
	return line[0] == '[' && bytes.Contains(line, []byte("]:"))
}
func tagOnly(line []byte) bool {
	return line[0] == '<' && line[len(line)-1] == '>' && !bytes.Contains(line, []byte("</"))
}

func (s *scanner) skipMDX(line []byte) bool {
	if s.mdxDepth > 0 {
		s.mdxDepth += statementDepth(line)
		return true
	}
	text := string(line)
	statement := strings.HasPrefix(text, "import ") || strings.HasPrefix(text, "import\t") ||
		strings.HasPrefix(text, "export const ") ||
		strings.HasPrefix(text, "export let ") ||
		strings.HasPrefix(text, "export var ")
	if !statement {
		return false
	}
	s.mdxDepth = max(0, statementDepth(line))
	return true
}

func statementDepth(line []byte) int {
	return bytes.Count(
		line,
		[]byte{'{'},
	) - bytes.Count(
		line,
		[]byte{'}'},
	) + bytes.Count(
		line,
		[]byte{'('},
	) - bytes.Count(
		line,
		[]byte{')'},
	)
}

func listItem(line []byte) ([]byte, bool) {
	if len(line) < listMarkerWidth {
		return line, false
	}
	if (line[0] == '-' || line[0] == '+' || line[0] == '*') && (line[1] == ' ' || line[1] == '\t') {
		return bytes.TrimSpace(line[2:]), true
	}
	end := 0
	for end < len(line) && line[end] >= '0' && line[end] <= '9' {
		end++
	}
	if end > 0 && end+1 < len(line) && (line[end] == '.' || line[end] == ')') &&
		(line[end+1] == ' ' || line[end+1] == '\t') {
		return bytes.TrimSpace(line[end+2:]), true
	}
	return line, false
}

func (s *scanner) skipRawHTML(line []byte) bool {
	if s.rawHTML != "" {
		if bytes.Contains(bytes.ToLower(line), []byte("</"+s.rawHTML+">")) {
			s.rawHTML = ""
		}
		return true
	}
	if len(line) == 0 || line[0] != '<' {
		return false
	}
	lower := bytes.ToLower(line)
	for _, tag := range []string{"script", "style", "pre"} {
		if bytes.HasPrefix(lower, []byte("<"+tag+">")) || bytes.HasPrefix(lower, []byte("<"+tag+" ")) {
			if !bytes.Contains(lower, []byte("</"+tag+">")) {
				s.rawHTML = tag
			}
			return true
		}
	}
	return false
}
