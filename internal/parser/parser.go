package parser

// Parser extracts description and outline from file content.
type Parser interface {
	Parse(path string, content []byte) (*ParseResult, error)
	CanParse(path string) bool
}

type ParseResult struct {
	Description   string
	ComponentType ComponentType
	Outline       *Outline
	Lines         int
}

type Outline struct {
	Type     OutlineType `json:"type"`
	Headings []Heading   `json:"headings,omitempty"`
	Exports  []Export    `json:"exports,omitempty"`
}

type OutlineType string

const (
	OutlineTypeHeadings OutlineType = "headings"
	OutlineTypeExports  OutlineType = "exports"
	OutlineTypeNone     OutlineType = "none"
)

type ComponentType string

const (
	ComponentTypeDocumentation ComponentType = "documentation"
	ComponentTypeCode          ComponentType = "code"
	ComponentTypeNone          ComponentType = ""
)

type Heading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	Line  int    `json:"line"`
}

type Export struct {
	Type string `json:"type"`
	Name string `json:"name"`
	Line int    `json:"line"`
}
