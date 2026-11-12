package source_test

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/source"
	"github.com/g5becks/dox/internal/testutil/fakegithub"
)

func TestRateLimitClassificationProducesTypedResetAndTokenHint(t *testing.T) {
	t.Parallel()
	resetUnix := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second)

	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()
			assertRateLimitStatus(t, status, resetUnix)
		})
	}
}

func assertRateLimitStatus(t *testing.T, status int, resetUnix time.Time) {
	t.Helper()
	server := fakegithub.New(t, 1)
	server.Set("/api/repos/acme/docs/git/trees/main", fakegithub.Response{
		Status: status,
		Headers: http.Header{
			"X-Ratelimit-Remaining": {"0"},
			"X-Ratelimit-Reset":     {strconv.FormatInt(resetUnix.Unix(), 10)},
		},
	})

	src := newTestGitHubSource(t, server.APIURL())
	_, syncErr := src.Sync(t.Context(), t.TempDir(), nil, source.SyncOptions{})
	if syncErr == nil {
		t.Fatal("expected rate-limit error")
	}

	detail, ok := errors.AsType[*errs.Error](syncErr)
	if !ok || detail.Code != errs.RateLimited {
		t.Fatalf("expected RateLimited error, got %+v", detail)
	}
	if !strings.Contains(detail.Hint, "GITHUB_TOKEN") || !strings.Contains(detail.Hint, "GH_TOKEN") {
		t.Fatalf("missing token hint: %q", detail.Hint)
	}

	rlErr, ok := errors.AsType[*errs.RateLimitError](syncErr)
	if !ok || !rlErr.Reset.Equal(resetUnix) {
		t.Fatalf("RateLimitError.Reset = %v, want %v", rlErr, resetUnix)
	}

	if got := len(server.Requests()); got != 1 {
		t.Fatalf("requests = %d, want 1 (rate-limited 4xx must not retry)", got)
	}
}

func TestSuccessWithZeroRemainingSucceedsAndWarnsOnce(t *testing.T) {
	t.Parallel()
	server := fakegithub.New(t, 3)
	server.Set("/api/repos/acme/docs/git/trees/main", fakegithub.Response{
		Headers: http.Header{"X-Ratelimit-Remaining": {"0"}},
	})

	src := newTestGitHubSource(t, server.APIURL())
	var warnings []string
	result, syncErr := src.Sync(t.Context(), t.TempDir(), nil, source.SyncOptions{
		OnWarning: func(msg string) {
			warnings = append(warnings, msg)
		},
	})
	if syncErr != nil {
		t.Fatalf("Sync() unexpected error = %v", syncErr)
	}
	if result == nil || result.Downloaded != 3 {
		t.Fatalf("unexpected SyncResult = %+v", result)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings count = %d (%v), want 1", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "GITHUB_TOKEN") {
		t.Fatalf("unexpected warning message: %q", warnings[0])
	}
}

func TestRetryPolicyRetries5xxWithRetryAfter(t *testing.T) {
	t.Parallel()
	server := fakegithub.New(t, 1)
	server.Set("/api/repos/acme/docs/git/trees/main", fakegithub.Response{
		Status:  http.StatusServiceUnavailable,
		Headers: http.Header{"Retry-After": {"0"}},
	})

	src := newTestGitHubSource(t, server.APIURL())
	_, syncErr := src.Sync(t.Context(), t.TempDir(), nil, source.SyncOptions{})
	if syncErr == nil {
		t.Fatal("expected 503 failure after exhausting retries")
	}
	if got := len(server.Requests()); got != 4 {
		t.Fatalf("5xx request count = %d, want 4 (1 initial + 3 retries)", got)
	}
}

func TestRetryPolicyStopsWhenRetryAfterExceedsBound(t *testing.T) {
	t.Parallel()
	server := fakegithub.New(t, 1)
	server.Set("/api/repos/acme/docs/git/trees/main", fakegithub.Response{
		Status:  http.StatusServiceUnavailable,
		Headers: http.Header{"Retry-After": {"3600"}},
	})

	src := newTestGitHubSource(t, server.APIURL())
	start := time.Now()
	_, syncErr := src.Sync(t.Context(), t.TempDir(), nil, source.SyncOptions{})
	if syncErr == nil {
		t.Fatal("expected 503 failure")
	}
	if got := len(server.Requests()); got != 1 {
		t.Fatalf("request count = %d, want 1 (Retry-After beyond bound must not be waited)", got)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("sync blocked for %v honoring an unbounded Retry-After", elapsed)
	}
}

func TestRetryPolicyNeverRetries4xx(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		server := fakegithub.New(t, 1)
		server.Set("/api/repos/acme/docs/git/trees/main", fakegithub.Response{
			Status: status,
		})

		src := newTestGitHubSource(t, server.APIURL())
		_, syncErr := src.Sync(t.Context(), t.TempDir(), nil, source.SyncOptions{})
		if syncErr == nil {
			t.Fatalf("expected failure for status %d", status)
		}
		if got := len(server.Requests()); got != 1 {
			t.Fatalf("status %d request count = %d, want 1 (4xx must never retry)", status, got)
		}
	}
}

func TestRetryPolicyRetriesNetworkError(t *testing.T) {
	t.Parallel()
	var attempts atomic.Int64
	src, err := source.NewGitHub("docs", config.Source{Repo: "acme/docs", Path: "docs", Ref: "main"}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	source.ConfigureGitHubTestNetworkRetry(src, &attempts, 2, time.Millisecond)

	_, syncErr := src.Sync(t.Context(), t.TempDir(), nil, source.SyncOptions{})
	if syncErr == nil {
		t.Fatal("expected network failure")
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("network attempt count = %d, want 3 (1 initial + 2 retries)", got)
	}
}

func newTestGitHubSource(t *testing.T, apiURL string) source.Source {
	t.Helper()
	src, err := source.NewGitHub("docs", config.Source{Repo: "acme/docs", Path: "docs", Ref: "main"}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	source.ConfigureGitHubTestRetry(src, apiURL, 3, time.Millisecond, time.Second)
	return src
}
