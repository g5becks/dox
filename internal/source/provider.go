package source

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	neturl "net/url"
	"strings"

	"github.com/g5becks/dox/internal/errs"
)

// provider is one git hosting API. githubSource orchestrates a sync (ref
// pinning, diffing against the lock, concurrent or archive downloads, atomic
// writes) and asks the provider only for hosting-specific operations.
type provider interface {
	// resolveCommit pins ref to a commit SHA.
	resolveCommit(ctx context.Context, ref string) (string, error)
	// folderTree lists the configured folder at commit with repository-relative
	// paths and the folder's own tree SHA, handling truncated listings.
	folderTree(ctx context.Context, commit string) (*githubTreeResponse, error)
	// fileSHA returns the blob SHA of one file at commit.
	fileSHA(ctx context.Context, commit, filePath string) (string, error)
	// fileContent returns one file's bytes (sha from fileSHA).
	fileContent(ctx context.Context, commit, filePath, sha string) ([]byte, error)
	// rawRequest builds the request for one file's raw bytes at commit.
	rawRequest(ctx context.Context, commit, repoPath string) (*http.Request, error)
	// archiveRequest builds the request for the commit's tar.gz archive.
	archiveRequest(ctx context.Context, commit string) (*http.Request, error)
	// authorize adds credentials to a content (raw or archive) request.
	authorize(request *http.Request)
	// decorateAPI sets Accept (accept, or the provider's JSON default) and
	// credentials on an API request.
	decorateAPI(request *http.Request, accept string)
	// apiName labels errors and warnings ("github", "gitlab").
	apiName() string
	// tokenHint tells users how to supply credentials for this host.
	tokenHint() string
}

// folderSHAProvider can report a folder's tree SHA without listing it, so an
// unchanged folder costs no listing calls.
type folderSHAProvider interface {
	folderSHA(ctx context.Context, commit string) (string, bool, error)
}

const githubHost = "github.com"

// isEnterpriseHost reports whether a GitHub source names a GitHub Enterprise
// Server host rather than github.com.
func isEnterpriseHost(host string) bool {
	normalized := strings.ToLower(strings.TrimSpace(host))
	return normalized != "" && normalized != githubHost
}

// enterpriseAPIBase is GitHub Enterprise Server's REST API root.
func enterpriseAPIBase(host string) string {
	return "https://" + strings.ToLower(strings.TrimSpace(host)) + "/api/v3"
}

// githubProvider serves github.com and GitHub Enterprise Server. It shares
// the source's HTTP plumbing (retries, rate-limit classification, warnings).
type githubProvider struct {
	s *githubSource
	// enterprise routes content through the API (contents with the raw media
	// type, and the tarball endpoint): GHE raw web URLs need a browser session.
	enterprise bool
}

func (p *githubProvider) authorize(request *http.Request) {
	if p.s.token != "" {
		request.Header.Set("Authorization", "Bearer "+p.s.token)
	}
}

func (p *githubProvider) decorateAPI(request *http.Request, accept string) {
	if accept == "" {
		accept = githubJSONMediaType
	}
	request.Header.Set("Accept", accept)
	request.Header.Set("X-Github-Api-Version", githubAPIVersion)
	p.authorize(request)
}

func (p *githubProvider) apiName() string { return "github" }

func (p *githubProvider) tokenHint() string {
	if p.enterprise {
		return "Set tokens.<host> in dox.toml or GH_ENTERPRISE_TOKEN to authenticate"
	}
	return "Set github_token, GITHUB_TOKEN, or GH_TOKEN to increase limits"
}

func (p *githubProvider) rawRequest(ctx context.Context, commit, repoPath string) (*http.Request, error) {
	s := p.s
	if !p.enterprise {
		target := fmt.Sprintf("%s/%s/%s/%s/%s",
			strings.TrimRight(s.rawBase, "/"), s.owner, s.repo, commit, escapeRepoPath(repoPath))
		return http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	}
	target := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s",
		strings.TrimRight(s.apiBase, "/"), s.owner, s.repo, escapeRepoPath(repoPath), neturl.QueryEscape(commit))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", githubRawMediaType)
	request.Header.Set("X-Github-Api-Version", githubAPIVersion)
	return request, nil
}

func (p *githubProvider) archiveRequest(ctx context.Context, commit string) (*http.Request, error) {
	s := p.s
	target := fmt.Sprintf("%s/%s/%s/tar.gz/%s", strings.TrimRight(s.codeloadBase, "/"), s.owner, s.repo, commit)
	if p.enterprise || s.token != "" {
		// The API endpoint redirects to a signed archive URL: needed for
		// private repositories and for GHE, whose codeload host differs (one
		// REST call; the token is not forwarded across hosts).
		target = fmt.Sprintf("%s/repos/%s/%s/tarball/%s", strings.TrimRight(s.apiBase, "/"), s.owner, s.repo, commit)
	}
	return http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
}

// asHTTPStatus extracts the HTTP status of a failed API request.
func asHTTPStatus(err error) (int, bool) {
	if status, ok := errors.AsType[*errs.HTTPStatusError](err); ok {
		return status.Code, true
	}
	return 0, false
}
