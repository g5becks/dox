package parser_test

import (
	"strings"
	"testing"

	"github.com/g5becks/dox/internal/parser"
)

// catLineCount is how `dox cat` addresses a file: split on "\n" and drop the
// empty element that follows a final newline.
func catLineCount(content string) int {
	lines := strings.Split(content, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return len(lines)
}

type lineCountCase struct {
	name    string
	content string
	want    int
}

func lineCountCases() []lineCountCase {
	return []lineCountCase{
		{"empty", "", 0},
		{"single line without newline", "a", 1},
		{"single line with newline", "a\n", 1},
		{"two lines without newline", "a\nb", 2},
		{"two lines with newline", "a\nb\n", 2},
		{"lone newline", "\n", 1},
		{"trailing blank line", "a\n\n", 2},
		{"crlf endings", "a\r\nb\r\n", 2},
		{"bom then text", "\xef\xbb\xbfa\nb\n", 2},
	}
}

func TestCountLines(t *testing.T) {
	t.Parallel()
	for _, tt := range lineCountCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := parser.CountLines([]byte(tt.content)); got != tt.want {
				t.Errorf("CountLines(%q) = %d, want %d", tt.content, got, tt.want)
			}
			if got := catLineCount(tt.content); got != tt.want {
				t.Errorf("catLineCount(%q) = %d, want %d (test table disagrees with cat)", tt.content, got, tt.want)
			}
		})
	}
}

func TestCountLinesAgreesWithCatExhaustively(t *testing.T) {
	t.Parallel()
	alphabet := []string{"a", "\n", "\r"}
	level := []string{""}
	for range 7 {
		for _, content := range level {
			if got, want := parser.CountLines([]byte(content)), catLineCount(content); got != want {
				t.Fatalf("CountLines(%q) = %d, cat addresses %d", content, got, want)
			}
		}
		var next []string
		for _, content := range level {
			for _, letter := range alphabet {
				next = append(next, content+letter)
			}
		}
		level = next
	}
}

func TestParsersReportCatLineCount(t *testing.T) {
	t.Parallel()
	parsers := []struct {
		name string
		path string
		p    parser.Parser
	}{
		{"markdown", "doc.md", parser.NewMarkdownParser()},
		{"mdx", "doc.mdx", parser.NewMDXParser()},
		{"text", "doc.txt", parser.NewTextParser()},
		{"typescript", "doc.ts", parser.NewTypeScriptParser()},
	}
	for _, pc := range parsers {
		for _, tt := range lineCountCases() {
			t.Run(pc.name+"/"+tt.name, func(t *testing.T) {
				t.Parallel()
				result, err := pc.p.Parse(pc.path, []byte(tt.content))
				if err != nil {
					t.Fatalf("Parse() error = %v", err)
				}
				if result.Lines != tt.want {
					t.Errorf("Lines = %d, want %d", result.Lines, tt.want)
				}
			})
		}
	}
}
