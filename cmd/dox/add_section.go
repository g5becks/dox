package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/pelletier/go-toml/v2"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
)

// bareKeyPattern matches TOML bare keys, so `[sources.<name>]` stays a valid
// header without quoting.
var bareKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// isSimpleSourceKey reports whether key is one `dox add` renders; a section
// using anything else is not one it can rewrite without losing information.
func isSimpleSourceKey(key string) bool {
	switch key {
	case fieldType, "repo", "host", fieldPath, "ref", "patterns", "exclude", fieldURL, "filename", "out":
		return true
	default:
		return false
	}
}

func validateAddSourceName(sourceName string) error {
	if bareKeyPattern.MatchString(sourceName) {
		return nil
	}
	return errs.New(
		errs.Usage,
		fmt.Sprintf("invalid source name %q", sourceName),
		errs.WithHint("Source names may contain only letters, digits, '-' and '_'"),
		errs.WithFields(map[string]any{fieldSource: sourceName, errs.LegacyCodeField: errs.LegacyConfigInvalid}),
	)
}

func renderSourceSection(sourceName string, sourceCfg config.Source) string {
	lines := []string{
		fmt.Sprintf("[sources.%s]", sourceName),
		fmt.Sprintf("type = %q", sourceCfg.Type),
	}

	if sourceCfg.Repo != "" {
		lines = append(lines, fmt.Sprintf("repo = %q", sourceCfg.Repo))
		if sourceCfg.Host != "" {
			lines = append(lines, fmt.Sprintf("host = %q", sourceCfg.Host))
		}
		lines = append(lines, fmt.Sprintf("path = %q", sourceCfg.Path))

		if sourceCfg.Ref != "" {
			lines = append(lines, fmt.Sprintf("ref = %q", sourceCfg.Ref))
		}
		if len(sourceCfg.Patterns) > 0 {
			lines = append(lines, "patterns = "+formatTOMLStringArray(sourceCfg.Patterns))
		}
		if len(sourceCfg.Exclude) > 0 {
			lines = append(lines, "exclude = "+formatTOMLStringArray(sourceCfg.Exclude))
		}
	}

	if sourceCfg.Type == fieldURL {
		lines = append(lines, fmt.Sprintf("url = %q", sourceCfg.URL))
		if sourceCfg.Filename != "" {
			lines = append(lines, fmt.Sprintf("filename = %q", sourceCfg.Filename))
		}
	}

	if sourceCfg.Out != "" {
		lines = append(lines, fmt.Sprintf("out = %q", sourceCfg.Out))
	}

	return strings.Join(lines, "\n") + "\n"
}

func formatTOMLStringArray(values []string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%q", value))
	}

	return "[" + strings.Join(parts, ", ") + "]"
}

func appendSourceSection(fileContent string, section string) string {
	trimmed := strings.TrimRight(fileContent, "\n")
	if trimmed == "" {
		return section
	}

	return trimmed + "\n\n" + section
}

// replaceSourceSection rewrites `[sources.<name>]` only when it has the simple
// shape `dox add` itself writes: single-line `key = value` pairs for known keys,
// with string or string-array values and no comments between them. Anything
// else is refused rather than risk dropping comments or settings.
// Comments and blank lines after the last key belong to whatever follows and
// are kept.
func replaceSourceSection(fileContent string, sourceName string, section string) (string, error) {
	lines := strings.Split(fileContent, "\n")
	header := fmt.Sprintf("[sources.%s]", sourceName)

	start := -1
	for idx, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == header && start == -1:
			start = idx
		case trimmed == header, splitsSourceSection(trimmed, sourceName):
			return "", unrecognizedSectionError(sourceName, "it is split across several tables")
		}
	}
	if start == -1 {
		return "", unrecognizedSectionError(sourceName, "it is not written as a "+header+" table")
	}

	lastKey, err := scanSimpleSection(lines, start, sourceName)
	if err != nil {
		return "", err
	}

	newLines := make([]string, 0, len(lines))
	newLines = append(newLines, lines[:start]...)
	newLines = append(newLines, strings.Split(strings.TrimRight(section, "\n"), "\n")...)
	newLines = append(newLines, lines[lastKey+1:]...)

	return strings.TrimRight(strings.Join(newLines, "\n"), "\n") + "\n", nil
}

// splitsSourceSection reports whether a trimmed line opens a table that would
// extend sources.<name> or make sources an array of tables. TOML allows
// whitespace and quotes around header keys, so `[ sources.x.0]`,
// `[sources . x . a]` and `[sources."x".a]` all open a sub-table of x. Quotes
// and whitespace are dropped before comparing; that can only make unlike keys
// compare equal, which makes the caller refuse more, never less.
func splitsSourceSection(trimmed, sourceName string) bool {
	if !strings.HasPrefix(trimmed, "[") {
		return false
	}
	key := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '"' || r == '\'' {
			return -1
		}
		return r
	}, trimmed)
	if rest, isArray := strings.CutPrefix(key, "[["); isArray {
		return strings.HasPrefix(rest, "sources")
	}
	return strings.HasPrefix(key, "[sources."+sourceName+".")
}

// scanSimpleSection validates the body after the header at start and returns
// the index of its last key line (start when the body has no keys).
func scanSimpleSection(lines []string, start int, sourceName string) (int, error) {
	lastKey := start
	sawComment := false
	for idx := start + 1; idx < len(lines); idx++ {
		trimmed := strings.TrimSpace(lines[idx])
		switch {
		case strings.HasPrefix(trimmed, "["):
			return lastKey, nil
		case trimmed == "":
			continue
		case strings.HasPrefix(trimmed, "#"):
			sawComment = true
			continue
		case sawComment:
			return 0, unrecognizedSectionError(sourceName, "it contains comments")
		}
		if reason := simpleKeyValueProblem(trimmed); reason != "" {
			return 0, unrecognizedSectionError(sourceName, reason)
		}
		lastKey = idx
	}
	return lastKey, nil
}

func simpleKeyValueProblem(line string) string {
	key, value, ok := strings.Cut(line, "=")
	key = strings.TrimSpace(key)
	if !ok || !isSimpleSourceKey(key) {
		return fmt.Sprintf("line %q is not a supported key", line)
	}
	if strings.Contains(value, "#") {
		// Values never need '#' in practice; refusing avoids losing a trailing comment.
		return "it contains comments"
	}
	var parsed map[string]any
	if err := toml.Unmarshal([]byte("v = "+strings.TrimSpace(value)), &parsed); err != nil {
		return fmt.Sprintf("the value of %q is not a single-line string or array", key)
	}
	switch typed := parsed["v"].(type) {
	case string:
		return ""
	case []any:
		for _, item := range typed {
			if _, isString := item.(string); !isString {
				return fmt.Sprintf("the value of %q is not a string array", key)
			}
		}
		return ""
	default:
		return fmt.Sprintf("the value of %q is not a string or string array", key)
	}
}

func unrecognizedSectionError(sourceName, reason string) error {
	return errs.New(
		errs.Usage,
		fmt.Sprintf("cannot safely replace source %q: %s", sourceName, reason),
		errs.WithHint(fmt.Sprintf(
			"Edit [sources.%s] in your config by hand, or delete it and run 'dox add' again", sourceName,
		)),
		errs.WithFields(map[string]any{fieldSource: sourceName, errs.LegacyCodeField: errs.LegacyConfigWriteError}),
	)
}
