package source

import (
	"errors"
	"net/http"
	"strings"
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
	setFixtureBases(github, url)
	github.maxRetries = 0
	github.retriesConfigured = true
}

// ConfigureGitHubTestRetry configures retry count, base backoff, and rate-limit wait bound for tests.
func ConfigureGitHubTestRetry(src Source, url string, maxRetries int, baseWait, maxRateLimitWait time.Duration) {
	github := src.(*githubSource)
	setFixtureBases(github, url)
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

// setFixtureBases points API, raw and archive requests at a fake server whose
// API lives under "<server>/api" (fakegithub and githubfixture layouts).
func setFixtureBases(github *githubSource, apiURL string) {
	server := strings.TrimSuffix(apiURL, "/api")
	github.apiBase = apiURL
	github.rawBase = server + "/raw"
	github.codeloadBase = server + "/codeload"
}

// SetGitHubTarballMinFiles overrides the tarball threshold (0 restores the default).
func SetGitHubTarballMinFiles(src Source, minFiles int) {
	src.(*githubSource).tarballMinFiles = minFiles
}

// SetGitHubToken sets the token used for API, raw and archive requests.
func SetGitHubToken(src Source, token string) {
	src.(*githubSource).token = token
}
