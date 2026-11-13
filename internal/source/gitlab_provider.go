package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	sourceTypeGitLab = "gitlab"
	gitlabHost       = "gitlab.com"
	gitlabPageSize   = 100
	// gitlabMaxPages bounds pagination (100 entries per page) as a safety net.
	gitlabMaxPages = 10000
)

// gitlabAPIBase is the REST API root for gitlab.com or a self-managed host.
func gitlabAPIBase(host string) string {
	normalized := strings.ToLower(strings.TrimSpace(host))
	if normalized == "" || normalized == githubHost {
		normalized = gitlabHost
	}
	return "https://" + normalized + "/api/v4"
}

// newGitLabSource builds a repository source backed by the GitLab API. The
// repo is the project's full path ("group/project" or with subgroups).
func newGitLabSource(name string, cfg config.Source, token string, httpClient *http.Client) (Source, error) {
	segments := strings.Split(strings.Trim(cfg.Repo, "/"), "/")
	if len(segments) < 2 || slices.Contains(segments, "") {
		return nil, errs.New(
			errs.Config,
			fmt.Sprintf("invalid gitlab project path %q", cfg.Repo),
			errs.WithHint("Expected group/project (subgroups allowed: group/subgroup/project)"),
			errs.WithFields(map[string]any{fieldRepo: cfg.Repo, errs.LegacyCodeField: errs.LegacyConfigInvalid}),
		)
	}
	ownsClient := httpClient == nil
	if ownsClient {
		httpClient = httpx.NewClient(httpx.DefaultTimeout)
	}
	src := &githubSource{
		name:       name,
		source:     cfg,
		owner:      strings.Join(segments[:len(segments)-1], "/"),
		repo:       segments[len(segments)-1],
		http:       httpClient,
		ownsClient: ownsClient,
		apiBase:    gitlabAPIBase(cfg.Host),
		token:      token,
		timeout:    httpx.NormalizeTimeout(httpClient.Timeout),
	}
	src.provider = &gitlabProvider{s: src}
	return src, nil
}

// gitlabProvider serves gitlab.com and self-managed GitLab (API v4). Projects
// are addressed by their URL-encoded full path, so subgroups work.
type gitlabProvider struct {
	s *githubSource
	// folderSHAs caches folderSHA per commit within one sync.
	folderSHAs map[string]string
}

type gitlabCommit struct {
	ID string `json:"id"`
}

type gitlabTreeEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	Path string `json:"path"`
}

type gitlabFile struct {
	BlobID string `json:"blob_id"`
}

func (p *gitlabProvider) project() string {
	return "/projects/" + neturl.PathEscape(p.s.source.Repo)
}

func (p *gitlabProvider) apiName() string { return sourceTypeGitLab }

func (p *gitlabProvider) tokenHint() string {
	return "Set tokens.<host> in dox.toml or GITLAB_TOKEN to authenticate and raise limits"
}

func (p *gitlabProvider) authorize(request *http.Request) {
	if p.s.token != "" {
		request.Header.Set("Private-Token", p.s.token)
	}
}

func (p *gitlabProvider) decorateAPI(request *http.Request, accept string) {
	if accept == "" {
		accept = "application/json"
	}
	request.Header.Set("Accept", accept)
	p.authorize(request)
}

func (p *gitlabProvider) fields(extra map[string]any) map[string]any {
	fields := map[string]any{fieldRepo: p.s.source.Repo, errs.LegacyCodeField: errs.LegacyGithubAPIError}
	maps.Copy(fields, extra)
	return fields
}

func (p *gitlabProvider) resolveCommit(ctx context.Context, ref string) (string, error) {
	var commit gitlabCommit
	if err := p.s.doGitHubGet(ctx, githubRequestSpec{
		endpoint:   p.project() + "/repository/commits/" + neturl.PathEscape(ref),
		result:     &commit,
		errContext: errResolvingRef,
		statusDesc: "commit " + ref,
		statusHint: "Check that the project exists and 'ref' names a branch, tag, or commit SHA",
		fields:     p.fields(map[string]any{fieldRef: ref}),
	}); err != nil {
		return "", err
	}
	if !isHexSHA(commit.ID) {
		return "", errs.New(
			errs.Network,
			fmt.Sprintf("gitlab API returned no commit SHA for ref %q", ref),
			errs.WithFields(p.fields(map[string]any{fieldRef: ref})),
		)
	}
	return commit.ID, nil
}

// listTree returns every page of a tree listing at commit.
func (p *gitlabProvider) listTree(
	ctx context.Context,
	commit, folder string,
	recursive bool,
) ([]gitlabTreeEntry, error) {
	var all []gitlabTreeEntry
	for page := 1; page <= gitlabMaxPages; {
		query := map[string]string{
			fieldRef:   commit,
			"per_page": strconv.Itoa(gitlabPageSize),
			"page":     strconv.Itoa(page),
		}
		if folder != "" {
			query[fieldPath] = folder
		}
		if recursive {
			query["recursive"] = "true"
		}
		var entries []gitlabTreeEntry
		var header http.Header
		if err := p.s.doGitHubGet(ctx, githubRequestSpec{
			endpoint:   p.project() + "/repository/tree",
			query:      query,
			result:     &entries,
			header:     &header,
			errContext: "listing tree",
			statusDesc: "tree",
			statusHint: hintCheckRepoPathRef,
			fields:     p.fields(map[string]any{fieldPath: folder}),
		}); err != nil {
			return nil, p.notFound(err, folder, commit)
		}
		all = append(all, entries...)
		next, more := nextPage(header, page)
		if !more {
			return all, nil
		}
		page = next
	}
	return nil, errs.New(
		errs.Network,
		fmt.Sprintf("gitlab tree listing of %q exceeded %d pages", folder, gitlabMaxPages),
	)
}

// nextPage reads GitLab's X-Next-Page header; an empty value ends the listing.
func nextPage(header http.Header, current int) (int, bool) {
	next, err := strconv.Atoi(strings.TrimSpace(header.Get("X-Next-Page")))
	if err != nil || next <= current {
		return 0, false
	}
	return next, true
}

// notFound turns a 404 for a configured folder into a NotFound error.
func (p *gitlabProvider) notFound(err error, folder, commit string) error {
	if status, ok := errors.AsType[*errs.HTTPStatusError](
		err,
	); ok && status.Code == http.StatusNotFound &&
		folder != "" {
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

// folderSHA reads the folder's tree id from one listing of its parent.
func (p *gitlabProvider) folderSHA(ctx context.Context, commit string) (string, bool, error) {
	base := normalizeRepoPath(p.s.source.Path)
	if base == "" {
		return "", false, nil
	}
	if sha, ok := p.folderSHAs[commit]; ok {
		return sha, true, nil
	}
	parent := path.Dir(base)
	if parent == "." {
		parent = ""
	}
	entries, err := p.listTree(ctx, commit, parent, false)
	if err != nil {
		return "", false, err
	}
	for _, entry := range entries {
		if entry.Path == base && entry.Type == gitObjectTree {
			if p.folderSHAs == nil {
				p.folderSHAs = map[string]string{}
			}
			p.folderSHAs[commit] = entry.ID
			return entry.ID, true, nil
		}
	}
	return "", false, p.notFound(&errs.HTTPStatusError{Code: http.StatusNotFound}, base, commit)
}

func (p *gitlabProvider) folderTree(ctx context.Context, commit string) (*githubTreeResponse, error) {
	base := normalizeRepoPath(p.s.source.Path)
	sha, known, err := p.folderSHA(ctx, commit)
	if err != nil {
		return nil, err
	}
	entries, err := p.listTree(ctx, commit, base, true)
	if err != nil {
		return nil, err
	}
	tree := &githubTreeResponse{}
	for _, entry := range entries {
		// Paths are already repository-relative.
		tree.Tree = append(tree.Tree, githubTreeEntry{Path: entry.Path, Type: entry.Type, SHA: entry.ID})
	}
	tree.SHA = sha
	if !known {
		tree.SHA = listingDigest(tree.Tree)
	}
	return tree, nil
}

// listingDigest identifies a root listing, which has no parent to read a tree
// id from; it changes exactly when a listed path or blob changes.
func listingDigest(entries []githubTreeEntry) string {
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		lines = append(lines, entry.Type+" "+entry.SHA+" "+entry.Path)
	}
	slices.Sort(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return "listing:" + hex.EncodeToString(sum[:])
}

func (p *gitlabProvider) fileEndpoint(filePath string) string {
	return p.project() + "/repository/files/" + neturl.PathEscape(normalizeRepoPath(filePath))
}

func (p *gitlabProvider) fileSHA(ctx context.Context, commit, filePath string) (string, error) {
	var file gitlabFile
	if err := p.s.doGitHubGet(ctx, githubRequestSpec{
		endpoint:   p.fileEndpoint(filePath),
		query:      map[string]string{fieldRef: commit},
		result:     &file,
		errContext: "fetching file metadata",
		statusDesc: "file metadata",
		statusHint: "Check repository path and ref in your config",
		fields:     p.fields(map[string]any{fieldPath: filePath}),
	}); err != nil {
		return "", err
	}
	if file.BlobID == "" {
		return "", errs.New(
			errs.Network,
			fmt.Sprintf("expected file metadata for %q", filePath),
			errs.WithFields(p.fields(map[string]any{fieldPath: filePath})),
		)
	}
	return file.BlobID, nil
}

func (p *gitlabProvider) fileContent(ctx context.Context, commit, filePath, _ string) ([]byte, error) {
	var content []byte
	if err := p.s.doGitHubGet(ctx, githubRequestSpec{
		endpoint:   p.fileEndpoint(filePath) + "/raw",
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

func (p *gitlabProvider) rawRequest(ctx context.Context, commit, repoPath string) (*http.Request, error) {
	target := strings.TrimRight(p.s.apiBase, "/") + p.fileEndpoint(repoPath) + "/raw?ref=" + neturl.QueryEscape(commit)
	return http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
}

// archiveRequest asks for an archive of just the configured folder; entries
// keep their repository paths below one top-level directory.
func (p *gitlabProvider) archiveRequest(ctx context.Context, commit string) (*http.Request, error) {
	query := neturl.Values{"sha": {commit}}
	if base := normalizeRepoPath(p.s.source.Path); base != "" {
		query.Set(fieldPath, base)
	}
	target := strings.TrimRight(p.s.apiBase, "/") + p.project() + "/repository/archive.tar.gz?" + query.Encode()
	return http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
}
