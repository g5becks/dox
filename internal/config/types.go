package config

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/samber/oops"
)

const (
	DefaultOutput = ".dox"
	repoPartCount = 2

	// Source type constants.
	sourceTypeGitHub   = "github"
	sourceTypeGitLab   = "gitlab"
	sourceTypeCodeberg = "codeberg"
	sourceTypeGit      = "git"
	sourceTypeURL      = "url"
)

func DefaultPatterns() []string {
	return []string{"**/*.md", "**/*.mdx", "**/*.txt"}
}

// DefaultExcludes returns common patterns to exclude from syncing.
// These are populated in the config template by `dox init`.
func DefaultExcludes() []string {
	return []string{
		// Hidden/config directories
		".vitepress/**",
		".github/**",
		".git/**",
		".idea/**",
		".vscode/**",

		// Dependencies
		"node_modules/**",
		"vendor/**",

		// Images (common formats)
		"**/*.png",
		"**/*.jpg",
		"**/*.jpeg",
		"**/*.gif",
		"**/*.svg",
		"**/*.ico",
		"**/*.webp",

		// Build artifacts
		"dist/**",
		"build/**",
		".next/**",
		".nuxt/**",
		"out/**",

		// Compiled/binary files
		"**/*.wasm",
		"**/*.so",
		"**/*.dylib",
		"**/*.dll",
		"**/*.exe",

		// Archives
		"**/*.zip",
		"**/*.tar",
		"**/*.gz",
		"**/*.tgz",

		// Media
		"**/*.mp4",
		"**/*.mov",
		"**/*.avi",
		"**/*.mp3",
		"**/*.wav",

		// Fonts
		"**/*.woff",
		"**/*.woff2",
		"**/*.ttf",
		"**/*.eot",
		"**/*.otf",
	}
}

type Display struct {
	DefaultLimit      int      `koanf:"default_limit"`
	DescriptionLength int      `koanf:"description_length"`
	LineNumbers       bool     `koanf:"line_numbers"`
	Format            string   `koanf:"format"            validate:"omitempty,oneof=table json csv"`
	ListFields        []string `koanf:"list_fields"`
}

type Config struct {
	Output      string            `koanf:"output"       validate:"omitempty,dirpath"`
	GitHubToken string            `koanf:"github_token"`
	MaxParallel int               `koanf:"max_parallel" validate:"omitempty,min=1,max=100"`
	Excludes    []string          `koanf:"excludes"`
	Display     Display           `koanf:"display"`
	Sources     map[string]Source `koanf:"sources"      validate:"required,dive"`
	ConfigDir   string            `koanf:"-"`
}

type Source struct {
	Type     string   `koanf:"type"     validate:"omitempty,oneof=github url git gitlab codeberg"`
	Repo     string   `koanf:"repo"     validate:"omitempty,github_repo"`
	Host     string   `koanf:"host"`
	Path     string   `koanf:"path"`
	Ref      string   `koanf:"ref"`
	Patterns []string `koanf:"patterns"`
	Exclude  []string `koanf:"exclude"`
	URL      string   `koanf:"url"      validate:"omitempty,url"`
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

// mergeExcludes returns the union of global and source-specific excludes.
// Duplicates are removed using map deduplication.
func mergeExcludes(global, source []string) []string {
	if len(global) == 0 && len(source) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(global)+len(source))

	for _, pattern := range global {
		seen[pattern] = struct{}{}
	}

	for _, pattern := range source {
		seen[pattern] = struct{}{}
	}

	result := make([]string, 0, len(seen))
	for pattern := range seen {
		result = append(result, pattern)
	}

	return result
}

func (c *Config) ApplyDefaults() {
	if c.Output == "" {
		c.Output = DefaultOutput
	}

	// Apply display defaults
	if c.Display.DefaultLimit == 0 {
		c.Display.DefaultLimit = 50
	}
	if c.Display.DescriptionLength == 0 {
		c.Display.DescriptionLength = 200
	}
	if c.Display.Format == "" {
		c.Display.Format = "table"
	}
	if len(c.Display.ListFields) == 0 {
		c.Display.ListFields = []string{"path", "type", "lines", "size", "description"}
	}

	for sourceName, sourceCfg := range c.Sources {
		sourceCfg = applySourceDefaults(sourceCfg, c.Excludes)
		c.Sources[sourceName] = sourceCfg
	}
}

func applySourceDefaults(src Source, globalExcludes []string) Source {
	// Infer type if not explicitly set
	if src.Type == "" {
		src.Type = inferSourceType(src)
	}

	// Handle git hosting sources
	if isGitSource(src.Type) {
		src = applyGitSourceDefaults(src, globalExcludes)
	}

	return src
}

func inferSourceType(src Source) string {
	if src.URL != "" {
		return sourceTypeURL
	}
	if src.Repo != "" {
		return sourceTypeGitHub
	}
	return ""
}

func isGitSource(sourceType string) bool {
	return sourceType == sourceTypeGitHub ||
		sourceType == sourceTypeGit ||
		sourceType == sourceTypeGitLab ||
		sourceType == sourceTypeCodeberg
}

func applyGitSourceDefaults(src Source, globalExcludes []string) Source {
	// Default host to github.com if not specified
	if src.Host == "" {
		src.Host = "github.com"
	}

	// Normalize type based on host if type is generic "git"
	if src.Type == sourceTypeGit {
		src.Type = normalizeGitType(src.Host)
	}

	// Apply patterns if not set
	if len(src.Patterns) == 0 {
		src.Patterns = DefaultPatterns()
	}

	// Merge global excludes with per-source excludes
	if len(globalExcludes) > 0 || len(src.Exclude) > 0 {
		src.Exclude = mergeExcludes(globalExcludes, src.Exclude)
	}

	return src
}

func normalizeGitType(host string) string {
	switch {
	case strings.Contains(host, "github"):
		return sourceTypeGitHub
	case strings.Contains(host, "gitlab"):
		return sourceTypeGitLab
	case strings.Contains(host, "codeberg"):
		return sourceTypeCodeberg
	default:
		return sourceTypeGit
	}
}

func (c *Config) Validate() error {
	v := newValidator()

	// Validate Display config
	if err := v.Struct(c.Display); err != nil {
		var validationErrors validator.ValidationErrors
		if errors.As(err, &validationErrors) {
			for _, fe := range validationErrors {
				field := strings.ToLower(fe.Field())
				if fe.Tag() == "oneof" && field == "format" {
					return oops.
						Code("CONFIG_INVALID").
						With("field", "display.format").
						With("value", c.Display.Format).
						Hint("Supported formats: table, json, csv").
						Errorf("invalid display format %q", c.Display.Format)
				}
			}
		}
		return oops.
			Code("CONFIG_INVALID").
			Wrapf(err, "validating display config")
	}

	for sourceName, sourceCfg := range c.Sources {
		// Validate that source has either repo or url (not both, not neither)
		hasRepo := sourceCfg.Repo != ""
		hasURL := sourceCfg.URL != ""

		if !hasRepo && !hasURL {
			return oops.
				Code("CONFIG_INVALID").
				With("source", sourceName).
				Hint("Each source must have either 'repo' (for git hosting) or 'url' (for direct downloads)").
				Errorf("source %q has neither 'repo' nor 'url'", sourceName)
		}

		if hasRepo && hasURL {
			return oops.
				Code("CONFIG_INVALID").
				With("source", sourceName).
				Hint("Use 'repo' for git hosting OR 'url' for direct downloads, not both").
				Errorf("source %q has both 'repo' and 'url'", sourceName)
		}

		// For git hosting sources, require path
		if hasRepo && sourceCfg.Path == "" {
			return oops.
				Code("CONFIG_INVALID").
				With("source", sourceName).
				With("field", "path").
				Hint("Set path to a file or directory in the repository").
				Errorf("missing 'path' for source %q", sourceName)
		}

		// Struct validation for URL format, repo format, etc.
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
			Hint("Supported types: github, gitlab, codeberg, git, url (or omit 'type' to infer)").
			Errorf("unknown source type %q for source %q", sourceCfg.Type, sourceName)

	case fe.Tag() == "github_repo":
		return oops.
			Code("CONFIG_INVALID").
			With("source", sourceName).
			With("field", "repo").
			With("value", sourceCfg.Repo).
			Hint("Expected repo format: owner/repo").
			Errorf("invalid repo format %q for source %q", sourceCfg.Repo, sourceName)

	case fe.Tag() == "url" && field == "url":
		return oops.
			Code("CONFIG_INVALID").
			With("source", sourceName).
			With("field", "url").
			With("value", sourceCfg.URL).
			Hint("URL must be a valid HTTP/HTTPS URL").
			Errorf("invalid url %q for source %q", sourceCfg.URL, sourceName)

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
