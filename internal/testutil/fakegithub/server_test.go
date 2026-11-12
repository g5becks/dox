package fakegithub_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/g5becks/dox/internal/testutil/fakegithub"
	"github.com/g5becks/dox/internal/testutil/githubfixture"
)

const repoPath = "/api/repos/acme/docs"

func TestStatusOverride(t *testing.T) {
	t.Parallel()
	server := fakegithub.New(t, 1)
	server.Set(repoPath, fakegithub.Response{Status: http.StatusServiceUnavailable, Body: []byte("unavailable")})
	response := get(t, server, repoPath)
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "unavailable" {
		t.Fatalf("body = %q, err = %v", body, err)
	}
}

func TestRateLimitHeaders(t *testing.T) {
	t.Parallel()
	server := fakegithub.New(t, 1)
	headers := http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {"1800000000"}}
	server.Set(repoPath, fakegithub.Response{Status: http.StatusForbidden, Headers: headers})
	response := get(t, server, repoPath)
	defer response.Body.Close()
	if response.Header.Get("X-Ratelimit-Remaining") != "0" || response.Header.Get("X-Ratelimit-Reset") != "1800000000" {
		t.Fatalf("rate-limit headers = %v", response.Header)
	}
}

func TestRetryAfter(t *testing.T) {
	t.Parallel()
	server := fakegithub.New(t, 1)
	server.Set(
		repoPath,
		fakegithub.Response{Status: http.StatusTooManyRequests, Headers: http.Header{"Retry-After": {"120"}}},
	)
	response := get(t, server, repoPath)
	defer response.Body.Close()
	if response.StatusCode != http.StatusTooManyRequests || response.Header.Get("Retry-After") != "120" {
		t.Fatalf("status = %d, Retry-After = %q", response.StatusCode, response.Header.Get("Retry-After"))
	}
}

func TestLatency(t *testing.T) {
	t.Parallel()
	const delay = 15 * time.Millisecond
	server := fakegithub.New(t, 1)
	server.Set(repoPath, fakegithub.Response{Latency: delay})
	start := time.Now()
	response := get(t, server, repoPath)
	defer response.Body.Close()
	if elapsed := time.Since(start); elapsed < delay {
		t.Fatalf("response arrived after %v, before configured latency %v", elapsed, delay)
	}
}

func TestNeverRespond(t *testing.T) {
	t.Parallel()
	server := fakegithub.New(t, 1)
	server.Set(repoPath, fakegithub.Response{NeverRespond: true})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+repoPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		response, requestErr := server.Client().Do(request)
		if response != nil {
			_ = response.Body.Close()
		}
		result <- requestErr
	}()
	select {
	case <-server.Started:
	case requestErr := <-result:
		t.Fatalf("request ended before reaching the server: %v", requestErr)
	case <-time.After(5 * time.Second):
		t.Fatal("request did not reach the server")
	}
	cancel()
	select {
	case requestErr := <-result:
		if !errors.Is(requestErr, context.Canceled) {
			t.Fatalf("request error = %v", requestErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled request did not terminate")
	}
	if len(server.Requests()) != 1 {
		t.Fatalf("observed requests = %v", server.Requests())
	}
}

func TestTruncatedTree(t *testing.T) {
	t.Parallel()
	server := fakegithub.New(t, 1)
	path := repoPath + "/git/trees/main"
	server.Set(path, fakegithub.Response{TruncatedTree: true})
	response := get(t, server, path)
	defer response.Body.Close()
	var tree struct {
		Truncated bool `json:"truncated"`
	}
	if err := json.NewDecoder(response.Body).Decode(&tree); err != nil {
		t.Fatal(err)
	}
	if !tree.Truncated {
		t.Fatal("expected truncated tree")
	}
}

func TestNormalEndpoints(t *testing.T) {
	t.Parallel()
	paths := []string{repoPath, repoPath + "/commits/HEAD", repoPath + "/git/trees/main",
		repoPath + "/git/blobs/0000000000000000000000000000000000000001",
		"/raw/acme/docs/" + githubfixture.Commit + "/docs/page-000.md",
		"/codeload/acme/docs/tar.gz/" + githubfixture.Commit}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			server := fakegithub.New(t, 1)
			response := get(t, server, path)
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status = %d", response.StatusCode)
			}
		})
	}
}

func get(t *testing.T, server *fakegithub.Server, path string) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func TestSuccessfulRateHeadersOverrideDefaults(t *testing.T) {
	t.Parallel()
	server := fakegithub.New(t, 1)
	server.Set(repoPath, fakegithub.Response{Headers: http.Header{"X-Ratelimit-Remaining": {"0"}}})
	response := get(t, server, repoPath)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("X-Ratelimit-Remaining") != "0" {
		t.Fatalf("status = %d, remaining = %q", response.StatusCode, response.Header.Get("X-Ratelimit-Remaining"))
	}
}
