package parser

import (
	"bytes"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/g5becks/dox/internal/parser/outline"
)

// IsBinary checks first 512 bytes for null bytes.
func IsBinary(content []byte) bool {
	const maxCheckSize = 512
	size := min(len(content), maxCheckSize)
	return bytes.IndexByte(content[:size], 0) != -1
}

// IsValidUTF8 validates the content is valid UTF-8.
func IsValidUTF8(content []byte) bool {
	return utf8.Valid(content)
}

// StripBOM removes UTF-8 BOM (0xEF, 0xBB, 0xBF) if present.
func StripBOM(content []byte) []byte {
	if len(content) >= 3 && content[0] == 0xEF && content[1] == 0xBB && content[2] == 0xBF {
		return content[3:]
	}
	return content
}

// DetectFileType maps file extension to type string.
// Returns: "md", "mdx", "txt", "tsx", "ts", or "unknown".
func DetectFileType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md":
		return "md"
	case ".mdx":
		return "mdx"
	case ".txt":
		return "txt"
	case ".tsx":
		return "tsx"
	case ".ts":
		return "ts"
	default:
		return "unknown"
	}
}

// StripFrontmatter removes YAML frontmatter (--- delimited) and returns
// the remaining content and extracted title/description if present.
func StripFrontmatter(content []byte) ([]byte, string, string) {
	body, title, description, _ := outline.Frontmatter(content)
	return body, title, description
}
