package sync_test

import (
	"testing"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/sync"
)

//nolint:paralleltest // t.Setenv cannot be used in parallel tests.
func TestResolveTokenPerHost(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "env-github")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GH_ENTERPRISE_TOKEN", "env-enterprise")
	cfg := &config.Config{
		GitHubToken: "",
		Tokens:      map[string]string{"github.special.com": "config-special"},
	}
	cases := []struct {
		name   string
		source config.Source
		want   string
	}{
		{"github.com from env", config.Source{Type: "github", Repo: "a/b"}, "env-github"},
		{"explicit github.com host", config.Source{Type: "github", Repo: "a/b", Host: "GitHub.com"}, "env-github"},
		{
			"enterprise from env",
			config.Source{Type: "github", Repo: "a/b", Host: "github.mycorp.com"},
			"env-enterprise",
		},
		{"tokens map wins", config.Source{Type: "github", Repo: "a/b", Host: "github.special.com"}, "config-special"},
		{"url sources get none", config.Source{Type: "url", URL: "https://example.com/a.txt"}, ""},
	}
	for _, tc := range cases {
		if got := sync.ResolveToken(cfg, tc.source); got != tc.want {
			t.Errorf("%s: token = %q, want %q", tc.name, got, tc.want)
		}
	}
	cfg.GitHubToken = "config-github"
	if got := sync.ResolveToken(cfg, config.Source{Type: "github", Repo: "a/b"}); got != "config-github" {
		t.Errorf("github_token should win over GITHUB_TOKEN, got %q", got)
	}
}
