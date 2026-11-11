package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/go-playground/validator/v10"

	"github.com/g5becks/dox/internal/errs"
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
	Format            string   `koanf:"format"             validate:"omitempty,oneof=table json csv"`
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
		c.Display.ListFields = []string{fieldPath, fieldType, "lines", "size", "description"}
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
	if err := validateDisplay(v, c.Display); err != nil {
		return err
	}
	for name, sourceCfg := range c.Sources {
		if err := validateSource(v, name, sourceCfg); err != nil {
			return err
		}
	}
	return nil
}

func validateDisplay(v *validator.Validate, display Display) error {
	// Validate Display config
	if err := v.Struct(display); err != nil {
		if validationErrors, ok := errors.AsType[validator.ValidationErrors](err); ok {
			for _, fe := range validationErrors {
				field := strings.ToLower(fe.Field())
				if fe.Tag() == "oneof" && field == "format" {
					return errs.New(
						errs.Config,
						fmt.Sprintf("invalid display format %q", display.Format),
						errs.WithHint("Supported formats: table, json, csv"),
						errs.WithFields(
							map[string]any{
								fieldName:            "display.format",
								fieldValue:           display.Format,
								errs.LegacyCodeField: errs.LegacyConfigInvalid,
							},
						),
					)
				}
			}
		}
		return errs.Wrap(
			errs.Config,
			fmt.Errorf("validating display config: %w", err),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyConfigInvalid}),
		)
	}

	return nil
}

func validateSource(v *validator.Validate, sourceName string, sourceCfg Source) error {
	// Validate that source has either repo or url (not both, not neither)
	hasRepo := sourceCfg.Repo != ""
	hasURL := sourceCfg.URL != ""

	if !hasRepo && !hasURL {
		return errs.New(
			errs.Config,
			fmt.Sprintf("source %q has neither 'repo' nor 'url'", sourceName),
			errs.WithHint("Each source must have either 'repo' (for git hosting) or 'url' (for direct downloads)"),
			errs.WithFields(map[string]any{fieldSource: sourceName, errs.LegacyCodeField: errs.LegacyConfigInvalid}),
		)
	}

	if hasRepo && hasURL {
		return errs.New(
			errs.Config,
			fmt.Sprintf("source %q has both 'repo' and 'url'", sourceName),
			errs.WithHint("Use 'repo' for git hosting OR 'url' for direct downloads, not both"),
			errs.WithFields(map[string]any{fieldSource: sourceName, errs.LegacyCodeField: errs.LegacyConfigInvalid}),
		)
	}

	// For git hosting sources, require path
	if hasRepo && sourceCfg.Path == "" {
		return errs.New(
			errs.Config,
			fmt.Sprintf("missing 'path' for source %q", sourceName),
			errs.WithHint("Set path to a file or directory in the repository"),
			errs.WithFields(
				map[string]any{
					fieldSource:          sourceName,
					fieldName:            fieldPath,
					errs.LegacyCodeField: errs.LegacyConfigInvalid,
				},
			),
		)
	}

	// Struct validation for URL format, repo format, etc.
	valErr := v.Struct(sourceCfg)
	if valErr == nil {
		return nil
	}

	var validationErrors validator.ValidationErrors
	if !errors.As(valErr, &validationErrors) {
		return errs.Wrap(
			errs.Config,
			fmt.Errorf("validating source %q: %w", sourceName, valErr),
			errs.WithFields(map[string]any{fieldSource: sourceName, errs.LegacyCodeField: errs.LegacyConfigInvalid}),
		)
	}

	for _, fe := range validationErrors {
		return mapValidationError(sourceName, sourceCfg, fe)
	}
	return nil
}

func mapValidationError(sourceName string, sourceCfg Source, fe validator.FieldError) error {
	field := strings.ToLower(fe.Field())

	switch {
	case fe.Tag() == "oneof" && field == fieldType:
		return errs.New(
			errs.Config,
			fmt.Sprintf("unknown source type %q for source %q", sourceCfg.Type, sourceName),
			errs.WithHint("Supported types: github, gitlab, codeberg, git, url (or omit 'type' to infer)"),
			errs.WithFields(
				map[string]any{
					fieldSource:          sourceName,
					fieldType:            sourceCfg.Type,
					errs.LegacyCodeField: errs.LegacyUnknownSourceType,
				},
			),
		)

	case fe.Tag() == "github_repo":
		return errs.New(
			errs.Config,
			fmt.Sprintf("invalid repo format %q for source %q", sourceCfg.Repo, sourceName),
			errs.WithHint("Expected repo format: owner/repo"),
			errs.WithFields(
				map[string]any{
					fieldSource:          sourceName,
					fieldName:            "repo",
					fieldValue:           sourceCfg.Repo,
					errs.LegacyCodeField: errs.LegacyConfigInvalid,
				},
			),
		)

	case fe.Tag() == sourceTypeURL && field == sourceTypeURL:
		return errs.New(
			errs.Config,
			fmt.Sprintf("invalid url %q for source %q", sourceCfg.URL, sourceName),
			errs.WithHint("URL must be a valid HTTP/HTTPS URL"),
			errs.WithFields(
				map[string]any{
					fieldSource:          sourceName,
					fieldName:            sourceTypeURL,
					fieldValue:           sourceCfg.URL,
					errs.LegacyCodeField: errs.LegacyConfigInvalid,
				},
			),
		)

	default:
		return errs.New(
			errs.Config,
			fmt.Sprintf("validation failed for field %q in source %q", field, sourceName),
			errs.WithFields(
				map[string]any{
					fieldSource:          sourceName,
					fieldName:            field,
					"tag":                fe.Tag(),
					errs.LegacyCodeField: errs.LegacyConfigInvalid,
				},
			),
		)
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
