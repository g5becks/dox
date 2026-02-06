package config

import (
	"path/filepath"
	"strings"

	"github.com/samber/oops"
)

const DefaultOutput = ".dox"

var DefaultPatterns = []string{"**/*.md", "**/*.mdx", "**/*.txt"}

type Config struct {
	Output      string            `koanf:"output"`
	GitHubToken string            `koanf:"github_token"`
	Sources     map[string]Source `koanf:"sources"`

	ConfigDir string `koanf:"-"`
}

type Source struct {
	Type     string   `koanf:"type"`
	Repo     string   `koanf:"repo"`
	Path     string   `koanf:"path"`
	Ref      string   `koanf:"ref"`
	Host     string   `koanf:"host"`
	Patterns []string `koanf:"patterns"`
	Exclude  []string `koanf:"exclude"`
	URL      string   `koanf:"url"`
	Filename string   `koanf:"filename"`
	Out      string   `koanf:"out"`
}

func (c *Config) ApplyDefaults() {
	if c.Output == "" {
		c.Output = DefaultOutput
	}

	for sourceName, sourceCfg := range c.Sources {
		if sourceCfg.Type == "github" && len(sourceCfg.Patterns) == 0 {
			sourceCfg.Patterns = append([]string(nil), DefaultPatterns...)
		}

		c.Sources[sourceName] = sourceCfg
	}
}

func (c *Config) Validate() error {
	for sourceName, sourceCfg := range c.Sources {
		switch sourceCfg.Type {
		case "github":
			if sourceCfg.Repo == "" {
				return oops.
					Code("CONFIG_INVALID").
					With("source", sourceName).
					With("field", "repo").
					Hint("Set repo in owner/repo format for github sources").
					Errorf("missing repo for source %q", sourceName)
			}

			if !isValidRepo(sourceCfg.Repo) {
				return oops.
					Code("CONFIG_INVALID").
					With("source", sourceName).
					With("field", "repo").
					With("value", sourceCfg.Repo).
					Hint("Expected repo format: owner/repo").
					Errorf("invalid repo format %q for source %q", sourceCfg.Repo, sourceName)
			}

			if sourceCfg.Path == "" {
				return oops.
					Code("CONFIG_INVALID").
					With("source", sourceName).
					With("field", "path").
					Hint("Set path to a file or directory in the repository").
					Errorf("missing path for source %q", sourceName)
			}
		case "url":
			if sourceCfg.URL == "" {
				return oops.
					Code("CONFIG_INVALID").
					With("source", sourceName).
					With("field", "url").
					Hint("Set url for url sources").
					Errorf("missing url for source %q", sourceName)
			}
		default:
			return oops.
				Code("UNKNOWN_SOURCE_TYPE").
				With("source", sourceName).
				With("type", sourceCfg.Type).
				Hint("Supported types: github, url").
				Errorf("unknown source type %q for source %q", sourceCfg.Type, sourceName)
		}
	}

	return nil
}

func (c *Config) OutputDir(sourceName string, sourceCfg Source) string {
	baseOutputDir := c.Output
	if !filepath.IsAbs(baseOutputDir) {
		baseOutputDir = filepath.Join(c.ConfigDir, c.Output)
	}

	if sourceCfg.Out != "" {
		return filepath.Join(baseOutputDir, sourceCfg.Out)
	}

	return filepath.Join(baseOutputDir, sourceName)
}

func isValidRepo(repo string) bool {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return false
	}

	return parts[0] != "" && parts[1] != ""
}
