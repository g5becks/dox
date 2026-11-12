package source

import (
	"errors"
	"net/http"
	"sync/atomic"
	"time"
)

// IsGitHubSource identifies the backend without making any request.
func IsGitHubSource(src Source) bool {
	_, ok := src.(*githubSource)
	return ok
}

// SetAuditGitHubEndpoint confines audit repros to their local server.
func SetAuditGitHubEndpoint(src Source, url string) {
	github := src.(*githubSource)
	github.apiBase = url
	github.maxRetries = 0
	github.retriesConfigured = true
}

// ConfigureGitHubTestRetry configures retry count, base backoff, and rate-limit wait bound for tests.
func ConfigureGitHubTestRetry(src Source, url string, maxRetries int, baseWait, maxRateLimitWait time.Duration) {
	github := src.(*githubSource)
	github.apiBase = url
	github.maxRetries = maxRetries
	github.retriesConfigured = true
	github.retryBaseWait = baseWait
	github.maxRateLimitWait = maxRateLimitWait
}

type failingTransport struct {
	attempts *atomic.Int64
}

func (f failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	f.attempts.Add(1)
	return nil, errors.New("simulated connection reset")
}

// ConfigureGitHubTestNetworkRetry injects a failing transport to count network retry attempts.
func ConfigureGitHubTestNetworkRetry(src Source, attempts *atomic.Int64, maxRetries int, baseWait time.Duration) {
	github := src.(*githubSource)
	github.http = &http.Client{Transport: failingTransport{attempts: attempts}}
	github.maxRetries = maxRetries
	github.retriesConfigured = true
	github.retryBaseWait = baseWait
}
