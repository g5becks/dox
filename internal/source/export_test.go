package source

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"resty.dev/v3"

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
	client *resty.Client,
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
		client:      client,
		resolvedRef: resolvedRef,
	}
}

// TestableURLSource creates a urlSource and returns it as a Source + a setter for the client.
func TestableURLSource(t *testing.T, name string, cfg config.Source) (Source, func(*resty.Client)) {
	t.Helper()

	src, err := NewURL(name, cfg)
	if err != nil {
		t.Fatalf("NewURL() error = %v", err)
	}

	u := src.(*urlSource)

	return u, func(client *resty.Client) {
		u.client = client
	}
}

// MockHTTPResponse is an exported version for external tests.
type MockHTTPResponse struct {
	Status int
	Body   string
	Header http.Header
}

// NewMockGitHubClient creates a resty client with mock transport for external tests.
func NewMockGitHubClient(t *testing.T, responses map[string]MockHTTPResponse) *resty.Client {
	t.Helper()

	client := resty.New().SetBaseURL("https://api.github.test")
	client.SetTransport(&exportedMockTransport{
		t:         t,
		responses: responses,
	})

	return client
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

// NewMockRestyClient creates a resty client with a custom round-trip handler.
func NewMockRestyClient(handler RoundTripFunc) *resty.Client {
	client := resty.New()
	client.SetTransport(handler)

	return client
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
