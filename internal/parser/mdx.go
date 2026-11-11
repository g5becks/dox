package parser

type MDXParser struct{}

func NewMDXParser() *MDXParser                 { return &MDXParser{} }
func (p *MDXParser) CanParse(path string) bool { return DetectFileType(path) == "mdx" }
func (p *MDXParser) Parse(_ string, content []byte) (*ParseResult, error) {
	return scanOutline(content, true), nil
}
