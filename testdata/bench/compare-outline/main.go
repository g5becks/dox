// compare-outline verifies the pinned corpus against captured v0.3.0 output.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/g5becks/dox/internal/parser"
	"github.com/g5becks/dox/internal/testutil/jsongolden"
)

type difference struct {
	Path    string   `json:"path"`
	Before  string   `json:"before_sha256"`
	After   string   `json:"after_sha256"`
	Reasons []string `json:"reasons"`
}
type captured struct {
	Path   string
	Result *parser.ParseResult
	Error  string
}

func main() {
	corpus := flag.String("corpus", "", "pinned corpus docs directory")
	baseline := flag.String("baseline", "", "v0.3.0 JSONL capture")
	expected := flag.String("expected", "testdata/bench/outline-differences.json", "reviewed differences")
	flag.Parse()
	if err := compare(*corpus, *baseline, *expected); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func compare(corpus, baseline, expected string) error {
	if corpus == "" || baseline == "" {
		return errors.New("-corpus and -baseline are required")
	}
	raw, err := os.ReadFile(expected)
	if err != nil {
		return err
	}
	var differences []difference
	if err = json.Unmarshal(raw, &differences); err != nil {
		return err
	}
	reviewed := make(map[string]difference, len(differences))
	for _, row := range differences {
		if len(row.Reasons) == 0 {
			return fmt.Errorf("missing explanation: %s", row.Path)
		}
		if _, duplicate := reviewed[row.Path]; duplicate {
			return fmt.Errorf("duplicate difference: %s", row.Path)
		}
		reviewed[row.Path] = row
	}
	file, err := os.Open(baseline)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	identical, changed := 0, 0
	for {
		var old captured
		if decodeErr := decoder.Decode(&old); decodeErr != nil {
			if errors.Is(decodeErr, io.EOF) {
				break
			}
			return decodeErr
		}
		if old.Error != "" {
			return fmt.Errorf("baseline parse failed for %s: %s", old.Path, old.Error)
		}
		if !filepath.IsLocal(old.Path) {
			return fmt.Errorf("unsafe baseline path %q", old.Path)
		}
		body, readErr := os.ReadFile(filepath.Join(corpus, filepath.FromSlash(old.Path)))
		if readErr != nil {
			return readErr
		}
		var p parser.Parser = parser.NewMarkdownParser()
		if strings.HasSuffix(old.Path, ".mdx") {
			p = parser.NewMDXParser()
		}
		current, parseErr := p.Parse(old.Path, body)
		if parseErr != nil {
			return parseErr
		}
		before, hashErr := digest(old.Result)
		if hashErr != nil {
			return hashErr
		}
		after, hashErr := digest(current)
		if hashErr != nil {
			return hashErr
		}
		explanation, known := reviewed[old.Path]
		if before == after {
			if known {
				return fmt.Errorf("stale reviewed difference, remove it: %s", old.Path)
			}
			identical++
			continue
		}
		if !known || before != explanation.Before || after != explanation.After {
			return fmt.Errorf("unexplained corpus change: %s (%s -> %s)", old.Path, before, after)
		}
		delete(reviewed, old.Path)
		changed++
	}
	if len(reviewed) != 0 {
		return fmt.Errorf("%d reviewed differences absent from corpus", len(reviewed))
	}
	fmt.Printf("PASS: %d corpus files, %d identical, %d individually explained differences\n", identical+changed, identical, changed)
	return nil
}

func digest(value *parser.ParseResult) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	canonical, err := jsongolden.Canonical(data)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(canonical)), nil
}
