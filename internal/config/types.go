package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"

	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/httpx"
)

const (
	DefaultOutput  = ".dox"
	DefaultTimeout = "30s"
	repoPartCount  = 2
	// maxPerHostLimit caps max_per_host, the per-host HTTP concurrency bound.
	maxPerHostLimit = 64
	// maxParallelLimit caps max_parallel, the concurrent source sync bound.
	maxParallelLimit = 100
	// maxDownloadConcurrency caps download_concurrency, the per-source file download bound.
	maxDownloadConcurrency = 64

	// Source type constants.
	sourceTypeGitHub   = "github"
	sourceTypeGitLab   = "gitlab"
	sourceTypeCodeberg = "codeberg"
	sourceTypeGitea    = "gitea"
	codebergHost       = "codeberg.org"
	sourceTypeGit      = "git"
	githubHost         = "github.com"
	gitlabHost         = "gitlab.com"
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
	DefaultLimit      int      `toml:"default_limit"`
	DescriptionLength int      `toml:"description_length"`
	LineNumbers       bool     `toml:"line_numbers"`
	Format            string   `toml:"format"             validate:"omitempty,oneof=table json jsonl csv"`
	ListFields        []string `toml:"list_fields"`
}

type Config struct {
	AllowExternalOutput bool   `toml:"-"`
	Output              string `toml:"output"       validate:"omitempty,dirpath"`
	Timeout             string `toml:"timeout"`
	GitHubToken         string `toml:"github_token"`
	// Tokens maps a git host (for example a GitHub Enterprise host) to its token.
	Tokens              map[string]string `toml:"tokens"`
	MaxParallel         int               `toml:"max_parallel"         validate:"omitempty,min=1,max=100"`
	MaxPerHost          int               `toml:"max_per_host"         validate:"omitempty,min=1,max=64"`
	DownloadConcurrency int               `toml:"download_concurrency"`
	Excludes            []string          `toml:"excludes"`
	Display             Display           `toml:"display"`
	Sources             map[string]Source `toml:"sources"              validate:"required,dive"`
	ConfigDir           string            `toml:"-"`
}

type Source struct {
	Type     string   `toml:"type"     validate:"omitempty,oneof=github url git gitlab gitea codeberg"`
	Repo     string   `toml:"repo"     validate:"omitempty,github_repo"`
	Host     string   `toml:"host"`
	Path     string   `toml:"path"`
	Ref      string   `toml:"ref"`
	Patterns []string `toml:"patterns"`
	Exclude  []string `toml:"exclude"`
	URL      string   `toml:"url"      validate:"omitempty,url"`
	Filename string   `toml:"filename"`
	Out      string   `toml:"out"`
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
	if c.Timeout == "" {
		c.Timeout = DefaultTimeout
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

// sourceDigestInput is the stable, explicitly tagged form hashed by Digest;
// renaming config keys must not silently change existing digests.
type sourceDigestInput struct {
	Type     string   `json:"type"`
	Repo     string   `json:"repo"`
	Host     string   `json:"host"`
	Path     string   `json:"path"`
	Ref      string   `json:"ref"`
	Patterns []string `json:"patterns"`
	Exclude  []string `json:"exclude"`
	URL      string   `json:"url"`
	Filename string   `json:"filename"`
	Out      string   `json:"out"`
}

// Digest fingerprints every field that determines what a source syncs (after
// defaults and global excludes are applied), as "sha256:<hex>".
func (s Source) Digest() string {
	canonical, err := json.Marshal(sourceDigestInput(s))
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:])
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
		// A host decides the provider: github.com and GitHub Enterprise hosts
		// whose name contains "github" infer github; other hosts need an
		// explicit type so a GitLab or Gitea server is never queried as GitHub.
		if host := strings.TrimSpace(src.Host); host != "" {
			return normalizeGitType(strings.ToLower(host))
		}
		return sourceTypeGitHub
	}
	return ""
}

func isGitSource(sourceType string) bool {
	return sourceType == sourceTypeGitHub ||
		sourceType == sourceTypeGit ||
		sourceType == sourceTypeGitLab ||
		sourceType == sourceTypeGitea ||
		sourceType == sourceTypeCodeberg
}

func applyGitSourceDefaults(src Source, globalExcludes []string) Source {
	// Default host to github.com if not specified
	if src.Host == "" {
		src.Host = githubHost
		switch src.Type {
		case sourceTypeGitLab:
			src.Host = gitlabHost
		case sourceTypeGitea, sourceTypeCodeberg:
			src.Host = codebergHost
		}
	}

	// Normalize type based on host if type is generic "git"
	if src.Type == sourceTypeGit {
		src.Type = normalizeGitType(src.Host)
	}
	// "codeberg" is an alias of the Gitea API type (Gitea, Forgejo, Codeberg).
	if src.Type == sourceTypeCodeberg {
		src.Type = sourceTypeGitea
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
	case strings.Contains(host, "codeberg"), strings.Contains(host, "gitea"), strings.Contains(host, "forgejo"):
		return sourceTypeGitea
	default:
		return sourceTypeGit
	}
}

// TimeoutDuration returns the validated timeout duration or the 30s default.
func (c *Config) TimeoutDuration() time.Duration {
	if c == nil || c.Timeout == "" {
		return httpx.DefaultTimeout
	}
	d, err := httpx.ParseTimeout(errs.Config, c.Timeout)
	if err != nil {
		return httpx.DefaultTimeout
	}
	return d
}

func (c *Config) Validate() error {
	if c.Timeout != "" {
		if _, err := httpx.ParseTimeout(errs.Config, c.Timeout); err != nil {
			return err
		}
	}
	if err := validateOptionalRange(
		"max_parallel", c.MaxParallel, maxParallelLimit, "4 × CPU cores, min 10, max 16",
	); err != nil {
		return err
	}
	if err := validateOptionalRange("max_per_host", c.MaxPerHost, maxPerHostLimit, "16"); err != nil {
		return err
	}
	if err := validateOptionalRange(
		"download_concurrency", c.DownloadConcurrency, maxDownloadConcurrency, "8",
	); err != nil {
		return err
	}
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

// validateOptionalRange accepts 0 (unset, use the default) or 1..upper.
func validateOptionalRange(name string, value, upper int, defaultLabel string) error {
	if value == 0 || (value >= 1 && value <= upper) {
		return nil
	}
	return errs.New(
		errs.Config,
		fmt.Sprintf("invalid %s %d", name, value),
		errs.WithHint(fmt.Sprintf("Use a value from 1 to %d, or omit it for the default of %s", upper, defaultLabel)),
		errs.WithFields(
			map[string]any{
				fieldName:            name,
				fieldValue:           value,
				errs.LegacyCodeField: errs.LegacyConfigInvalid,
			},
		),
	)
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
						errs.WithHint("Supported formats: table, json, jsonl, csv"),
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
	if err := validateSourcePaths(sourceName, sourceCfg); err != nil {
		return err
	}
	if err := validateSupportedProvider(sourceName, sourceCfg); err != nil {
		return err
	}
	if sourceCfg.Type != sourceTypeGitLab && strings.Count(sourceCfg.Repo, "/") > 1 {
		return errs.New(
			errs.Config,
			fmt.Sprintf("invalid repo format %q for source %q", sourceCfg.Repo, sourceName),
			errs.WithHint("Expected repo format: owner/repo (group/subgroup/project paths are GitLab-only)"),
			errs.WithFields(map[string]any{fieldSource: sourceName, fieldName: "repo", fieldValue: sourceCfg.Repo,
				errs.LegacyCodeField: errs.LegacyConfigInvalid}),
		)
	}
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

// validateSupportedProvider rejects git hosting providers that dox cannot
// sync yet. Before v0.5.0 every repo source is fetched from api.github.com,
// so accepting another host would query (or sync) the wrong repository.
func validateSupportedProvider(sourceName string, sourceCfg Source) error {
	// Only the generic "git" type (an unknown host) has no provider: github,
	// gitlab and gitea sources, including self-hosted ones, are supported.
	if sourceCfg.Type != sourceTypeGit {
		return nil
	}
	what := fmt.Sprintf("source type %q", sourceCfg.Type)
	if host := strings.TrimSpace(sourceCfg.Host); host != "" && !strings.EqualFold(host, githubHost) {
		what += fmt.Sprintf(" (host %q)", host)
	}
	return unsupportedProviderError(sourceName, what, map[string]any{fieldType: sourceCfg.Type})
}

func unsupportedProviderError(sourceName, what string, fields map[string]any) error {
	fields[fieldSource] = sourceName
	fields[errs.LegacyCodeField] = errs.LegacyConfigInvalid
	return errs.New(
		errs.Config,
		fmt.Sprintf("%s for source %q is not supported yet (planned for v0.5.0)", what, sourceName),
		errs.WithHint(
			"Set type to github (github.com or Enterprise), gitlab or gitea (Gitea, Forgejo, Codeberg) "+
				"for this host, or use a 'url' source",
		),
		errs.WithFields(fields),
	)
}

func mapValidationError(sourceName string, sourceCfg Source, fe validator.FieldError) error {
	field := strings.ToLower(fe.Field())

	switch {
	case fe.Tag() == "oneof" && field == fieldType:
		return errs.New(
			errs.Config,
			fmt.Sprintf("unknown source type %q for source %q", sourceCfg.Type, sourceName),
			errs.WithHint("Supported types: github, gitlab, gitea (alias codeberg), url (or omit 'type' to infer)"),
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

// isValidRepo accepts owner/repo, and deeper group/subgroup/project paths,
// which only GitLab sources may use (validateSource checks the type).
func isValidRepo(repo string) bool {
	parts := strings.Split(repo, "/")
	if len(parts) < repoPartCount {
		return false
	}
	return !slices.Contains(parts, "")
}
