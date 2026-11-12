// Package regression tracks the audited baseline failures until their fixes land.
package regression

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// KnownFailing returns a fresh copy of the fixed audit registry. Later fixes
// remove entries here; new entries must not be added to hide regressions.
func KnownFailing() map[string]string {
	knownFailing := map[string]string{}
	return knownFailing
}

// Classify inverts only registered failures, preserving ordinary new failures.
func Classify(registry map[string]string, id string, failure error) error {
	issue, known := registry[id]
	if !known {
		return failure
	}
	if failure == nil {
		return fmt.Errorf("%s passed: remove from knownFailing (%s)", id, issue)
	}
	return nil
}

// Verify also checks that an expected failure matches the documented repro.
// DOX_AUDIT_ENFORCE=1 exposes raw failures for baseline verification.
func Verify(id string, failure error) error {
	registry := KnownFailing()
	if _, known := registry[id]; known && failure != nil {
		pattern := evidencePattern(id)
		if !regexp.MustCompile(pattern).MatchString(failure.Error()) {
			return fmt.Errorf("%s failed for an unrelated reason: %w", id, failure)
		}
	}
	if os.Getenv("DOX_AUDIT_ENFORCE") == "1" {
		return failure
	}
	return Classify(registry, id, failure)
}

func evidencePattern(id string) string {
	switch {
	case strings.HasPrefix(id, "R1_"):
		return `^R1 (unsafe|accepted|filename)`
	case strings.HasPrefix(id, "R2_"):
		return `^R2 request exceeded`
	case strings.HasPrefix(id, "R3_"):
		return `^R3 (token hint missing|successful request rejected)`
	case strings.HasPrefix(id, "P2_"):
		return `^P2 concurrency =`
	case strings.HasPrefix(id, "C3_"):
		return `(?s)(Required flag "type" not set|flag provided but not defined: -[rp])`
	case strings.HasPrefix(id, "C4_"):
		return `^C4 non-GitHub host contacted GitHub`
	case strings.HasPrefix(id, "C5_"):
		return `^C5 (exit code|missing hint)`
	case strings.HasPrefix(id, "C6_"):
		return `(?s)(flag provided but not defined: -line|^C6 line mismatch)`
	case strings.HasPrefix(id, "P9_"):
		return `^P9 dry-run read`
	default:
		return `a^` // Unknown expected-failure evidence must never match.
	}
}

// Check reports the raw evidence while leaving the Go test result to Verify.
func Check(tb testing.TB, id string, failure error) {
	tb.Helper()
	if err := Verify(id, failure); err != nil {
		tb.Fatal(err)
	}
	if failure != nil {
		tb.Logf("KNOWN FAILING %s: %v", id, failure)
	}
}
