package outline

import (
	"bytes"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"go.yaml.in/yaml/v3"
)

const maxMetadataBytes = 64 * 1024

type metadata struct {
	Title       string `yaml:"title"       toml:"title"`
	Description string `yaml:"description" toml:"description"`
}

// Frontmatter strips a closed YAML/TOML block and reports its source line offset.
// Decode work is bounded even when a file contains a very large metadata block.
func Frontmatter(content []byte) ([]byte, string, string, int) {
	first, rest, found := bytes.Cut(content, []byte{'\n'})
	delimiter := string(bytes.TrimSuffix(first, []byte{'\r'}))
	if !found || (delimiter != "---" && delimiter != "+++") {
		return content, "", "", 0
	}
	consumed := len(content) - len(rest)
	offset := 1
	for raw := range bytes.Lines(rest) {
		offset++
		line := bytes.TrimSuffix(bytes.TrimSuffix(raw, []byte{'\n'}), []byte{'\r'})
		if string(line) == delimiter {
			bodyStart := consumed + len(raw)
			data := content[len(content)-len(rest) : consumed]
			value := decodeMetadata(data, delimiter)
			return content[bodyStart:], value.Title, value.Description, offset
		}
		consumed += len(raw)
	}
	return content, "", "", 0
}

func decodeMetadata(data []byte, delimiter string) metadata {
	var value metadata
	if len(data) > maxMetadataBytes {
		return value
	}
	var err error
	if delimiter == "---" {
		err = yaml.Unmarshal(data, &value)
	} else {
		err = toml.Unmarshal(data, &value)
	}
	if err != nil {
		return metadata{}
	}
	value.Title = strings.Join(strings.Fields(value.Title), " ")
	value.Description = strings.Join(strings.Fields(value.Description), " ")
	return value
}
