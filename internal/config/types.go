package config

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/samber/oops"
)

const (
	DefaultOutput           = ".dox"
	repoPartCount           = 2
	validationTagRequiredIf = "required_if"
)

func DefaultPatterns() []string {
	return []string{"**/*.md", "**/*.mdx", "**/*.txt"}
}

type Config struct {
	Output      string            `koanf:"output"       validate:"omitempty,dirpath"`
	GitHubToken string            `koanf:"github_token"`
	Sources     map[string]Source `koanf:"sources"      validate:"required,dive"`
	ConfigDir   string            `koanf:"-"`
}

type Source struct {
	Type     string   `koanf:"type"     validate:"required,oneof=github url"`
	Repo     string   `koanf:"repo"     validate:"required_if=Type github,omitempty,github_repo"`
	Path     string   `koanf:"path"     validate:"required_if=Type github"`
	Ref      string   `koanf:"ref"`
	Patterns []string `koanf:"patterns"`
	Exclude  []string `koanf:"exclude"`
	URL      string   `koanf:"url"      validate:"required_if=Type url,omitempty,url"`
	Filename string   `koanf:"filename"`
	Out      string   `koanf:"out"`
}

func newValidator() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())

	_ = v.RegisterValidation("github_repo", func(fl validator.FieldLevel) bool {
		return isValidRepo(fl.Field().String())
	})

	return v
}

func (c *Config) ApplyDefaults() {
	if c.Output == "" {
		c.Output = DefaultOutput
	}

	for sourceName, sourceCfg := range c.Sources {
		if sourceCfg.Type == "github" && len(sourceCfg.Patterns) == 0 {
			sourceCfg.Patterns = DefaultPatterns()
		}

		c.Sources[sourceName] = sourceCfg
	}
}

func (c *Config) Validate() error {
	v := newValidator()

	for sourceName, sourceCfg := range c.Sources {
		valErr := v.Struct(sourceCfg)
		if valErr == nil {
			continue
		}

		var validationErrors validator.ValidationErrors
		if !errors.As(valErr, &validationErrors) {
			return oops.
				Code("CONFIG_INVALID").
				With("source", sourceName).
				Wrapf(valErr, "validating source %q", sourceName)
		}

		for _, fe := range validationErrors {
			return mapValidationError(sourceName, sourceCfg, fe)
		}
	}

	return nil
}

func mapValidationError(sourceName string, sourceCfg Source, fe validator.FieldError) error {
	field := strings.ToLower(fe.Field())

	switch {
	case fe.Tag() == "oneof" && field == "type":
		return oops.
			Code("UNKNOWN_SOURCE_TYPE").
			With("source", sourceName).
			With("type", sourceCfg.Type).
			Hint("Supported types: github, url").
			Errorf("unknown source type %q for source %q", sourceCfg.Type, sourceName)

	case fe.Tag() == validationTagRequiredIf && field == "repo":
		return oops.
			Code("CONFIG_INVALID").
			With("source", sourceName).
			With("field", "repo").
			Hint("Set repo in owner/repo format for github sources").
			Errorf("missing repo for source %q", sourceName)

	case fe.Tag() == "github_repo":
		return oops.
			Code("CONFIG_INVALID").
			With("source", sourceName).
			With("field", "repo").
			With("value", sourceCfg.Repo).
			Hint("Expected repo format: owner/repo").
			Errorf("invalid repo format %q for source %q", sourceCfg.Repo, sourceName)

	case fe.Tag() == validationTagRequiredIf && field == "path":
		return oops.
			Code("CONFIG_INVALID").
			With("source", sourceName).
			With("field", "path").
			Hint("Set path to a file or directory in the repository").
			Errorf("missing path for source %q", sourceName)

	case fe.Tag() == validationTagRequiredIf && field == "url":
		return oops.
			Code("CONFIG_INVALID").
			With("source", sourceName).
			With("field", "url").
			Hint("Set url for url sources").
			Errorf("missing url for source %q", sourceName)

	default:
		return oops.
			Code("CONFIG_INVALID").
			With("source", sourceName).
			With("field", field).
			With("tag", fe.Tag()).
			Errorf("validation failed for field %q in source %q", field, sourceName)
	}
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
	if len(parts) != repoPartCount {
		return false
	}

	return parts[0] != "" && parts[1] != ""
}
