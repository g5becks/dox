package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
)

const replacementSection = "[sources.x]\ntype = \"github\"\nrepo = \"new/repo\"\npath = \"docs\"\n"

func TestReplaceSourceSectionKeepsNeighbors(t *testing.T) {
	t.Parallel()
	input := `output = ".dox"

[sources.x]
type = "github"
repo = "old/repo"
path = "guide"
patterns = ["**/*.md"]

# The display table belongs after x.
[display]
format = "json"

[sources.y]
url = "https://example.com/llms.txt"
`
	want := `output = ".dox"

[sources.x]
type = "github"
repo = "new/repo"
path = "docs"

# The display table belongs after x.
[display]
format = "json"

[sources.y]
url = "https://example.com/llms.txt"
`
	got, err := replaceSourceSection(input, "x", replacementSection)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("replaceSourceSection() =\n%s\nwant\n%s", got, want)
	}
}

func TestReplaceSourceSectionRefusesUnrecognizedShapes(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"comment inside":     "[sources.x]\nrepo = \"a/b\"\n# keep me\npath = \"docs\"\n",
		"inline comment":     "[sources.x]\nrepo = \"a/b\" # keep me\npath = \"docs\"\n",
		"multi-line array":   "[sources.x]\nrepo = \"a/b\"\npatterns = [\n  \"*.md\",\n]\n",
		"inline table":       "[sources]\nx = { repo = \"a/b\", path = \"docs\" }\n",
		"dotted key":         "[sources.x]\nrepo = \"a/b\"\nmeta.note = \"n\"\n",
		"unknown key":        "[sources.x]\nrepo = \"a/b\"\nmirror = \"m\"\n",
		"sub-table":          "[sources.x]\nrepo = \"a/b\"\n[sources.x.extra]\nk = \"v\"\n",
		"non-string value":   "[sources.x]\nrepo = \"a/b\"\nout = 5\n",
		"quoted header":      "[sources.\"x\"]\nrepo = \"a/b\"\n",
		"multi-line string":  "[sources.x]\nrepo = \"\"\"\na/b\"\"\"\n",
		"array of tables":    "[[sources]]\nrepo = \"a/b\"\n",
		"duplicate sections": "[sources.x]\nrepo = \"a/b\"\n[sources.x]\nrepo = \"c/d\"\n",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := replaceSourceSection(input, "x", replacementSection)
			detail, ok := errors.AsType[*errs.Error](err)
			if !ok || detail.Code != errs.Usage || !strings.Contains(detail.Hint, "by hand") {
				t.Fatalf(
					"replaceSourceSection(%q) error = %v, want a Usage refusal with an edit-by-hand hint",
					input,
					err,
				)
			}
		})
	}
}

func TestValidateAddSourceName(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"docs", "my-lib", "lib_2"} {
		if err := validateAddSourceName(name); err != nil {
			t.Errorf("validateAddSourceName(%q) = %v", name, err)
		}
	}
	for _, name := range []string{"a.b", "has space", "x]", `q"`} {
		if err := validateAddSourceName(name); err == nil {
			t.Errorf("validateAddSourceName(%q) accepted an invalid name", name)
		}
	}
}

// FuzzReplaceSourceSection checks that a successful replacement of a valid
// config yields a valid config where only sources.x changed.
func FuzzReplaceSourceSection(f *testing.F) {
	seeds := []string{
		"[sources.x]\nrepo = \"a/b\"\npath = \"docs\"\n",
		"output = \".dox\"\n[sources.x]\nrepo = \"a/b\"\n\n# next\n[sources.y]\nurl = \"https://e.com/a\"\n",
		"[sources.x]\nrepo = \"a/b\"\n[display]\nformat = \"json\"\n",
		"[sources]\nx = { repo = \"a/b\" }\n",
		"[sources.x]\npatterns = ['a', \"b\"]\n[sources.x.sub]\nk = 1\n",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	section := renderSourceSection("x", config.Source{Type: "github", Repo: "new/repo", Path: "docs"})
	var wantSource map[string]any
	if err := toml.Unmarshal([]byte(strings.TrimPrefix(section, "[sources.x]\n")), &wantSource); err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, input string) {
		var before map[string]any
		if toml.Unmarshal([]byte(input), &before) != nil {
			return
		}
		out, err := replaceSourceSection(input, "x", section)
		if err != nil {
			return
		}
		var after map[string]any
		if parseErr := toml.Unmarshal([]byte(out), &after); parseErr != nil {
			t.Fatalf("replacement produced invalid TOML: %v\ninput:\n%s\noutput:\n%s", parseErr, input, out)
		}
		afterSources, _ := after["sources"].(map[string]any)
		if !reflect.DeepEqual(afterSources["x"], wantSource) {
			t.Fatalf("sources.x = %#v, want %#v\ninput:\n%s\noutput:\n%s", afterSources["x"], wantSource, input, out)
		}
		delete(afterSources, "x")
		if beforeSources, ok := before["sources"].(map[string]any); ok {
			delete(beforeSources, "x")
		}
		if len(afterSources) == 0 {
			delete(after, "sources")
		}
		if beforeSources, ok := before["sources"].(map[string]any); ok && len(beforeSources) == 0 {
			delete(before, "sources")
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatalf(
				"replacement changed other settings\nbefore: %#v\nafter:  %#v\ninput:\n%s\noutput:\n%s",
				before,
				after,
				input,
				out,
			)
		}
	})
}
