package source

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	neturl "net/url"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/httpx"
)

const (
	sourceTypeGitea = "gitea"
	codebergHost    = "codeberg.org"
	giteaPageSize   = 1000
	queryFalse      = "false"
	// giteaMaxPages bounds tree pagination as a safety net.
	giteaMaxPages = 10000
)

// giteaAPIBase is the REST API root for Codeberg or a self-hosted Gitea or
// Forgejo instance.
func giteaAPIBase(host string) string {
	normalized := strings.ToLower(strings.TrimSpace(host))
	if normalized == "" || normalized == githubHost {
		normalized = codebergHost
	}
	return "https://" + normalized + "/api/v1"
}

// newGiteaSource builds a repository source backed by the Gitea API (Gitea,
// Forgejo and Codeberg share it).
func newGiteaSource(name string, cfg config.Source, token string, httpClient *http.Client) (Source, error) {
	owner, repo, err := parseRepo(cfg.Repo)
	if err != nil {
		return nil, err
	}
	ownsClient := httpClient == nil
	if ownsClient {
		httpClient = httpx.NewClient(httpx.DefaultTimeout)
	}
	src := &githubSource{
		name:       name,
		source:     cfg,
		owner:      owner,
		repo:       repo,
		http:       httpClient,
		ownsClient: ownsClient,
		apiBase:    giteaAPIBase(cfg.Host),
		token:      token,
		timeout:    httpx.NormalizeTimeout(httpClient.Timeout),
	}
	src.provider = &giteaProvider{s: src}
	return src, nil
}

// giteaProvider serves the Gitea API v1.
type giteaProvider struct {
	s *githubSource
	// folderSHAs caches per-commit folder lookups within one sync.
	folderSHAs map[string]string
}

type giteaCommit struct {
	SHA string `json:"sha"`
}

type giteaContent struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

func (p *giteaProvider) repoPath() string {
	return fmt.Sprintf("/repos/%s/%s", neturl.PathEscape(p.s.owner), neturl.PathEscape(p.s.repo))
}

func (p *giteaProvider) apiName() string { return sourceTypeGitea }

func (p *giteaProvider) tokenHint() string {
	return "Set tokens.<host> in dox.toml or GITEA_TOKEN to authenticate and raise limits"
}

func (p *giteaProvider) authorize(request *http.Request) {
	if p.s.token != "" {
		request.Header.Set("Authorization", "token "+p.s.token)
	}
}

func (p *giteaProvider) decorateAPI(request *http.Request, accept string) {
	if accept == "" {
		accept = "application/json"
	}
	request.Header.Set("Accept", accept)
	p.authorize(request)
}

func (p *giteaProvider) fields(extra map[string]any) map[string]any {
	fields := map[string]any{fieldRepo: p.s.source.Repo, errs.LegacyCodeField: errs.LegacyGithubAPIError}
	maps.Copy(fields, extra)
	return fields
}

func (p *giteaProvider) resolveCommit(ctx context.Context, ref string) (string, error) {
	var commits []giteaCommit
	if err := p.s.doGitHubGet(ctx, githubRequestSpec{
		endpoint: p.repoPath() + "/commits",
		query: map[string]string{
			"sha": ref, "limit": "1", "stat": queryFalse, "verification": queryFalse, "files": queryFalse,
		},
		result:     &commits,
		errContext: errResolvingRef,
		statusDesc: "commit " + ref,
		statusHint: "Check that the repository exists and 'ref' names a branch, tag, or commit SHA",
		fields:     p.fields(map[string]any{fieldRef: ref}),
	}); err != nil {
		return "", err
	}
	if len(commits) == 0 || !isHexSHA(commits[0].SHA) {
		return "", errs.New(
			errs.Network,
			fmt.Sprintf("gitea API returned no commit SHA for ref %q", ref),
			errs.WithFields(p.fields(map[string]any{fieldRef: ref})),
		)
	}
	return commits[0].SHA, nil
}

func (p *giteaProvider) contents(ctx context.Context, commit, repoPath string, result any) error {
	endpoint := p.repoPath() + "/contents"
	if repoPath != "" {
		endpoint += "/" + escapeRepoPath(repoPath)
	}
	return p.s.doGitHubGet(ctx, githubRequestSpec{
		endpoint:   endpoint,
		query:      map[string]string{fieldRef: commit},
		result:     result,
		errContext: "fetching contents",
		statusDesc: "contents of " + repoPath,
		statusHint: hintCheckRepoPathRef,
		fields:     p.fields(map[string]any{fieldPath: repoPath}),
	})
}

// folderSHA walks non-recursive tree listings from the commit, one per path
// component, to the folder's tree SHA. (Gitea reports the commit SHA as the
// root tree id, so the repository root has no stable tree identity, and its
// contents API is far slower than git/trees.)
func (p *giteaProvider) folderSHA(ctx context.Context, commit string) (string, bool, error) {
	base := normalizeRepoPath(p.s.source.Path)
	if base == "" {
		return "", false, nil
	}
	if sha, ok := p.folderSHAs[commit]; ok {
		return sha, true, nil
	}
	treeID := commit
	for component := range strings.SplitSeq(base, "/") {
		entries, err := p.listTree(ctx, treeID, false)
		if err != nil {
			return "", false, p.notFound(err, base, commit)
		}
		index := slices.IndexFunc(entries, func(entry githubTreeEntry) bool {
			return entry.Path == component && entry.Type == gitObjectTree
		})
		if index < 0 {
			return "", false, p.notFound(&errs.HTTPStatusError{Code: http.StatusNotFound}, base, commit)
		}
		treeID = entries[index].SHA
	}
	if p.folderSHAs == nil {
		p.folderSHAs = map[string]string{}
	}
	p.folderSHAs[commit] = treeID
	return treeID, true, nil
}

func (p *giteaProvider) notFound(err error, folder, commit string) error {
	if status, ok := asHTTPStatus(err); ok && status == http.StatusNotFound && folder != "" {
		return errs.New(
			errs.NotFound,
			fmt.Sprintf("directory %q not found in %s at %s", folder, p.s.source.Repo, commit),
			errs.WithHint("Check 'path' (and 'ref') for this source in your config"),
			errs.WithCause(errs.ErrSourceNotFound),
			errs.WithFields(p.fields(map[string]any{fieldPath: folder})),
		)
	}
	return err
}

// listTree returns every page of a tree listing; Gitea reports
// truncated=true while more pages remain.
func (p *giteaProvider) listTree(ctx context.Context, treeID string, recursive bool) ([]githubTreeEntry, error) {
	var all []githubTreeEntry
	for page := 1; page <= giteaMaxPages; page++ {
		query := map[string]string{"per_page": strconv.Itoa(giteaPageSize), "page": strconv.Itoa(page)}
		if recursive {
			query["recursive"] = "true"
		}
		var listing githubTreeResponse
		if err := p.s.doGitHubGet(ctx, githubRequestSpec{
			endpoint:   p.repoPath() + "/git/trees/" + neturl.PathEscape(treeID),
			query:      query,
			result:     &listing,
			errContext: "listing tree",
			statusDesc: gitObjectTree,
			statusHint: hintCheckRepoPathRef,
			fields:     p.fields(map[string]any{fieldRef: treeID}),
		}); err != nil {
			return nil, err
		}
		all = append(all, listing.Tree...)
		if !listing.Truncated || len(listing.Tree) == 0 {
			return all, nil
		}
	}
	return nil, errs.New(errs.Network, fmt.Sprintf("gitea tree listing exceeded %d pages", giteaMaxPages))
}

// folderTree lists the folder recursively with repository-relative paths.
func (p *giteaProvider) folderTree(ctx context.Context, commit string) (*githubTreeResponse, error) {
	base := normalizeRepoPath(p.s.source.Path)
	treeID, known, err := p.folderSHA(ctx, commit)
	if err != nil {
		return nil, err
	}
	if !known {
		treeID = commit // the repository root
	}
	entries, err := p.listTree(ctx, treeID, true)
	if err != nil {
		return nil, err
	}
	tree := &githubTreeResponse{SHA: treeID}
	for _, entry := range entries {
		if base != "" {
			entry.Path = path.Join(base, entry.Path)
		}
		tree.Tree = append(tree.Tree, entry)
	}
	if !known {
		tree.SHA = listingDigest(tree.Tree)
	}
	return tree, nil
}

func (p *giteaProvider) fileSHA(ctx context.Context, commit, filePath string) (string, error) {
	var file giteaContent
	if err := p.contents(ctx, commit, normalizeRepoPath(filePath), &file); err != nil {
		return "", err
	}
	if file.Type != gitObjectFile || file.SHA == "" {
		return "", errs.New(
			errs.Network,
			fmt.Sprintf("expected file metadata for %q", filePath),
			errs.WithFields(p.fields(map[string]any{fieldPath: filePath})),
		)
	}
	return file.SHA, nil
}

func (p *giteaProvider) fileContent(ctx context.Context, commit, filePath, _ string) ([]byte, error) {
	var content []byte
	if err := p.s.doGitHubGet(ctx, githubRequestSpec{
		endpoint:   p.repoPath() + "/raw/" + escapeRepoPath(filePath),
		query:      map[string]string{fieldRef: commit},
		accept:     "*/*",
		raw:        &content,
		errContext: "downloading file",
		statusDesc: gitObjectFile,
		fields:     p.fields(map[string]any{fieldPath: filePath}),
	}); err != nil {
		return nil, err
	}
	return content, nil
}

func (p *giteaProvider) rawRequest(ctx context.Context, commit, repoPath string) (*http.Request, error) {
	target := strings.TrimRight(p.s.apiBase, "/") + p.repoPath() + "/raw/" + escapeRepoPath(repoPath) +
		"?ref=" + neturl.QueryEscape(commit)
	return http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
}

// archiveRequest fetches the whole repository at commit; entries sit below
// one "<repo>/" directory with full repository paths.
func (p *giteaProvider) archiveRequest(ctx context.Context, commit string) (*http.Request, error) {
	target := strings.TrimRight(p.s.apiBase, "/") + p.repoPath() + "/archive/" + neturl.PathEscape(commit) + ".tar.gz"
	return http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
}
