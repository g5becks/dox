package source_test

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"resty.dev/v3"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/source"
	"github.com/g5becks/dox/internal/testutil/fakegithub"
	"github.com/g5becks/dox/internal/testutil/regression"
)

func TestAuditSource(t *testing.T) {
	t.Parallel()
	cases := []struct {
		id    string
		check func(*testing.T) error
	}{
		{"R1_filename_escape", auditFilename},
		{"R3_forbidden", func(t *testing.T) error { return auditRateHint(t, http.StatusForbidden) }},
		{"R3_too_many", func(t *testing.T) error { return auditRateHint(t, http.StatusTooManyRequests) }},
		{"R3_success_last_request", auditLastQuota},
		{"C4_non_github_host", auditHost},
		{"P9_dry_run_body", auditDryRun},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			regression.Check(t, tc.id, tc.check(t))
		})
	}
}

func auditFilename(t *testing.T) error {
	t.Helper()
	root := t.TempDir()
	victim := filepath.Join(root, "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := fakegithub.New(t, 1)
	server.Set("/payload", fakegithub.Response{Status: http.StatusOK, Body: []byte("overwritten")})
	src, err := source.NewURL("docs", config.Source{URL: server.URL + "/payload", Filename: "../victim"})
	if err != nil {
		return nil
	}
	defer src.Close()
	_, syncErr := src.Sync(t.Context(), filepath.Join(root, "docs"), nil, source.SyncOptions{})
	body, readErr := os.ReadFile(victim)
	if syncErr == nil || readErr != nil || string(body) != "keep" {
		return fmt.Errorf("R1 filename escaped destination: sync=%w, sentinel=%q", syncErr, body)
	}
	return nil
}

func auditGitHub(t *testing.T, server *fakegithub.Server) source.Source {
	t.Helper()
	src, err := source.NewGitHub("docs", config.Source{Repo: "acme/docs", Path: "docs", Ref: "main"}, "")
	if err != nil {
		t.Fatal(err)
	}
	source.SetAuditGitHubEndpoint(src, server.APIURL())
	t.Cleanup(func() { _ = src.Close() })
	return src
}

func auditRateHint(t *testing.T, status int) error {
	t.Helper()
	server := fakegithub.New(t, 1)
	server.Set(
		"/api/repos/acme/docs/git/trees/main",
		fakegithub.Response{
			Status:  status,
			Headers: http.Header{"X-Ratelimit-Remaining": {"0"}, "Retry-After": {"120"}},
		},
	)
	_, err := auditGitHub(t, server).Sync(t.Context(), t.TempDir(), nil, source.SyncOptions{})
	if err == nil {
		t.Fatal("rate-limit response unexpectedly succeeded")
	}
	detailed, ok := errors.AsType[*errs.Error](err)
	if !ok ||
		(!strings.Contains(detailed.Hint, "GITHUB_TOKEN") && !strings.Contains(detailed.Hint, "github_token")) {
		return fmt.Errorf("R3 token hint missing for %d: %w", status, err)
	}
	return nil
}

func auditLastQuota(t *testing.T) error {
	t.Helper()
	server := fakegithub.New(t, 1)
	server.Set(
		"/api/repos/acme/docs/git/trees/main",
		fakegithub.Response{Headers: http.Header{"X-Ratelimit-Remaining": {"0"}}},
	)
	_, err := auditGitHub(t, server).Sync(t.Context(), t.TempDir(), nil, source.SyncOptions{})
	if err != nil {
		return fmt.Errorf("R3 successful request rejected: %w", err)
	}
	return nil
}

func auditHost(t *testing.T) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dox.toml")
	if err := os.WriteFile(
		path,
		[]byte("[sources.docs]\nrepo = \"acme/docs\"\npath = \"docs\"\nhost = \"gitlab.com\"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil
	}
	src, err := source.New("docs", cfg.Sources["docs"], "")
	if err != nil {
		return nil
	}
	defer src.Close()
	if !source.IsGitHubSource(src) {
		return nil
	}
	server := fakegithub.New(t, 1)
	source.SetAuditGitHubEndpoint(src, server.APIURL())
	_, err = src.Sync(t.Context(), t.TempDir(), nil, source.SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(server.Requests()) > 0 {
		return fmt.Errorf("C4 non-GitHub host contacted GitHub backend: %d requests", len(server.Requests()))
	}
	return nil
}

type countingBody struct {
	io.Reader

	read *atomic.Int64
}

func (body *countingBody) Read(data []byte) (int, error) {
	n, err := body.Reader.Read(data)
	body.read.Add(int64(n))
	return n, err
}
func (*countingBody) Close() error { return nil }

type auditTransport struct{ read *atomic.Int64 }

func (transport auditTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	var body io.ReadCloser = http.NoBody
	if request.Method != http.MethodHead {
		body = &countingBody{Reader: strings.NewReader("documentation body"), read: transport.read}
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/plain"}},
		Body:       body,
		Request:    request,
	}, nil
}

func auditDryRun(t *testing.T) error {
	t.Helper()
	var read atomic.Int64
	src, setClient := source.TestableURLSource(
		t,
		"docs",
		config.Source{URL: "https://example.invalid/guide.txt", Filename: "guide.txt"},
	)
	client := resty.New().SetTransport(auditTransport{read: &read})
	setClient(client)
	defer src.Close()
	_, err := src.Sync(t.Context(), t.TempDir(), nil, source.SyncOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if count := read.Load(); count != 0 {
		return fmt.Errorf("P9 dry-run read %d body bytes", count)
	}
	return nil
}
