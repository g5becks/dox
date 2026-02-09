package parser

import (
	"bytes"
	"path/filepath"
	"strings"
	"unicode/utf8"
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
	if !bytes.HasPrefix(content, []byte("---\n")) && !bytes.HasPrefix(content, []byte("---\r\n")) {
		return content, "", ""
	}

	// Find the closing ---
	start := bytes.Index(content, []byte("\n"))
	if start == -1 {
		return content, "", ""
	}
	start++ // Move past the first newline

	skipBytes := 5 // Default for "\n---\n"
	end := bytes.Index(content[start:], []byte("\n---\n"))
	if end == -1 {
		end = bytes.Index(content[start:], []byte("\n---\r\n"))
		if end == -1 {
			return content, "", ""
		}
		skipBytes = 6 // For "\n---\r\n"
	}

	frontmatter := content[start : start+end]
	body := content[start+end+skipBytes:]

	// Extract title and description
	var title, description string
	lines := bytes.Split(frontmatter, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if titleAfter, titleFound := bytes.CutPrefix(line, []byte("title:")); titleFound {
			title = strings.TrimSpace(string(titleAfter))
			title = strings.Trim(title, `"'`)
		} else if descAfter, descFound := bytes.CutPrefix(line, []byte("description:")); descFound {
			description = strings.TrimSpace(string(descAfter))
			description = strings.Trim(description, `"'`)
		}
	}

	return body, title, description
}
