package source_test

import (
	"path"
	"strings"
	"testing"

	"github.com/g5becks/dox/internal/source"
)

// FuzzNormalizeRepoPath checks the shape of every normalized repository path,
// since the result is spliced into API URLs and archive prefixes.
func FuzzNormalizeRepoPath(f *testing.F) {
	for _, seed := range []string{
		"", " ", "/", "//", ".", "./", "docs", "/docs/", "docs//guide", "docs/./guide",
		"a/../b", "../x", "/../x", "a/b/../../..", " docs ", "docs/guide.md", "\x00", "a\\b", "é/ü",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, repoPath string) {
		got := source.NormalizeRepoPath(repoPath)
		if got == "" {
			return
		}
		if got == "." || strings.HasPrefix(got, "/") || strings.HasSuffix(got, "/") {
			t.Fatalf("NormalizeRepoPath(%q) = %q, want no dot or edge slash", repoPath, got)
		}
		if path.Clean(got) != got {
			t.Fatalf("NormalizeRepoPath(%q) = %q, want a cleaned path", repoPath, got)
		}
	})
}

// FuzzShouldIncludeFile checks the include/exclude contract for arbitrary
// paths and glob patterns: errors never include a file, an exclusion always
// wins, and pattern order does not matter.
func FuzzShouldIncludeFile(f *testing.F) {
	for _, seed := range [][3]string{
		{"docs/guide.md", "**/*.md", "**/draft*"},
		{"README.md", "*.md", ""},
		{"a/b/c.mdx", "a/**", "a/b/*"},
		{"x", "[", "x"},
		{"x", "{a,b", "*"},
		{"", "", ""},
		{"é/ü.md", "*/*.md", "ü*"},
	} {
		f.Add(seed[0], seed[1], seed[2])
	}
	f.Fuzz(func(t *testing.T, relativePath, include, exclude string) {
		included, err := source.ShouldIncludeFile(relativePath, []string{include}, nil)
		if err != nil && included {
			t.Fatalf("included %q on error %v", relativePath, err)
		}

		// A pattern that includes a file also excludes it when listed as an exclusion.
		excluded, excludeErr := source.ShouldIncludeFile(relativePath, []string{include}, []string{include})
		if excludeErr == nil && excluded {
			t.Fatalf("%q matched include %q and the same exclude %q", relativePath, include, include)
		}

		both, bothErr := source.ShouldIncludeFile(relativePath, []string{include}, []string{exclude})
		if bothErr == nil && both && !included {
			t.Fatalf("%q excluded by %q was included without it", relativePath, exclude)
		}

		forward, forwardErr := source.ShouldIncludeFile(relativePath, []string{include, exclude}, nil)
		reverse, reverseErr := source.ShouldIncludeFile(relativePath, []string{exclude, include}, nil)
		if forwardErr == nil && reverseErr == nil && forward != reverse {
			t.Fatalf("pattern order changed the result for %q: %q,%q", relativePath, include, exclude)
		}
	})
}
