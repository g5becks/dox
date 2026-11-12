package source

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/g5becks/dox/internal/config"
)

// IsSingleFilePath exports isSingleFilePath for testing.
//
//nolint:gochecknoglobals // Test-only exports
var IsSingleFilePath = isSingleFilePath

// FilenameFromURL exports filenameFromURL for testing.
//
//nolint:gochecknoglobals // Test-only exports
var FilenameFromURL = filenameFromURL

// TestableGitHubSource creates a githubSource for external tests.
func TestableGitHubSource(
	t *testing.T,
	name string,
	cfg config.Source,
	client *http.Client,
	resolvedRef string,
) Source {
	t.Helper()

	owner, repo, err := parseRepo(cfg.Repo)
	if err != nil {
		t.Fatalf("parseRepo() error = %v", err)
	}

	return &githubSource{
		name:        name,
		source:      cfg,
		owner:       owner,
		repo:        repo,
		http:        client,
		apiBase:     mockGitHubAPIBase,
		resolvedRef: resolvedRef,
		// Mocks key routes by the ref name, so treat it as the resolved commit.
		resolvedCommit: resolvedRef,
	}
}

// TestableURLSource creates a urlSource and returns it as a Source + a setter for the client.
func TestableURLSource(t *testing.T, name string, cfg config.Source) (Source, func(*http.Client)) {
	t.Helper()

	src, err := NewURL(name, cfg)
	if err != nil {
		t.Fatalf("NewURL() error = %v", err)
	}

	u := src.(*urlSource)

	return u, func(client *http.Client) {
		u.http = client
	}
}

// MockHTTPResponse is an exported version for external tests.
type MockHTTPResponse struct {
	Status int
	Body   string
	Header http.Header
	// WantAccept, when set, fails the test unless the request carries this Accept header.
	WantAccept string
}

const mockGitHubAPIBase = "https://api.github.test"

// NewMockGitHubClient creates an HTTP client with a mock GitHub API transport for external tests.
func NewMockGitHubClient(t *testing.T, responses map[string]MockHTTPResponse) *http.Client {
	t.Helper()
	return &http.Client{Transport: &exportedMockTransport{
		t:         t,
		responses: responses,
	}}
}

type exportedMockTransport struct {
	t         *testing.T
	responses map[string]MockHTTPResponse
}

func (m *exportedMockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m.t.Helper()

	key := req.URL.Path
	if req.URL.RawQuery != "" {
		key += "?" + req.URL.RawQuery
	}

	response, ok := m.responses[key]
	if !ok {
		m.t.Fatalf("unexpected request %s %s", req.Method, key)
	}
	if response.WantAccept != "" && req.Header.Get("Accept") != response.WantAccept {
		m.t.Fatalf("request %s Accept = %q, want %q", key, req.Header.Get("Accept"), response.WantAccept)
	}

	status := response.Status
	if status == 0 {
		status = http.StatusOK
	}

	header := response.Header
	if header == nil {
		header = make(http.Header)
	}

	if header.Get("Content-Type") == "" {
		header.Set("Content-Type", "application/json")
	}

	return &http.Response{
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		StatusCode:    status,
		Header:        header,
		Body:          io.NopCloser(strings.NewReader(response.Body)),
		ContentLength: int64(len(response.Body)),
		Request:       req,
	}, nil
}

// RoundTripFunc is an exported version of roundTripFunc for URL test mocking.
type RoundTripFunc func(*http.Request) *http.Response

// RoundTrip implements http.RoundTripper.
func (f RoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req), nil
}

// NewMockHTTPClient creates an HTTP client with a custom round-trip handler.
func NewMockHTTPClient(handler RoundTripFunc) *http.Client {
	return &http.Client{Transport: handler}
}

// NewHTTPResponse creates a mock HTTP response for tests.
func NewHTTPResponse(
	req *http.Request,
	status int,
	body string,
	header http.Header,
) *http.Response {
	if header == nil {
		header = make(http.Header)
	}

	return &http.Response{
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		StatusCode:    status,
		Header:        header,
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}
}
