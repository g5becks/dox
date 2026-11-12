package source

import (
	"testing"

	"github.com/g5becks/dox/internal/config"
)

// NewBenchmarkGitHubSource injects the fixture endpoint; the caller must close it.
func NewBenchmarkGitHubSource(tb testing.TB, serverURL string) Source {
	tb.Helper()
	src, err := newGitHubSource("docs", config.Source{Repo: "acme/docs", Path: "docs"}, "")
	if err != nil {
		tb.Fatal(err)
	}
	github, ok := src.(*githubSource)
	if !ok {
		tb.Fatal("expected a GitHub source")
	}
	setFixtureBases(github, serverURL+"/api")
	return src
}
