package source

import (
	"context"
	"encoding/base64"
	"fmt"
	"maps"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"resty.dev/v3"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/fsutil"
	"github.com/g5becks/dox/internal/httpx"
	"github.com/g5becks/dox/internal/lockfile"
)

const (
	sourceTypeGitHub    = "github"
	githubAPIBaseURL    = "https://api.github.com"
	userAgent           = "dox"
	httpRetryCount      = 3
	httpRetryMaxWaitSec = 5
	rateLimitWarnThresh = 10
)

type githubSource struct {
	name        string
	source      config.Source
	owner       string
	repo        string
	client      *resty.Client
	timeout     time.Duration
	resolvedRef string
	warnedLowRL bool
}

type githubTreeResponse struct {
	SHA       string            `json:"sha"`
	Truncated bool              `json:"truncated"`
	Tree      []githubTreeEntry `json:"tree"`
}

type githubTreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

type githubRepoResponse struct {
	DefaultBranch string `json:"default_branch"`
}

type githubContentResponse struct {
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

type githubBlobResponse struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

func newGitHubSource(name string, cfg config.Source, token string) (Source, error) {
	return newGitHubSourceWithHTTP(name, cfg, token, httpx.NewClient(httpx.DefaultTimeout))
}

func newGitHubSourceWithHTTP(name string, cfg config.Source, token string, httpClient *http.Client) (Source, error) {
	owner, repo, err := parseRepo(cfg.Repo)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = httpx.NewClient(httpx.DefaultTimeout)
	}

	return &githubSource{
		name:    name,
		source:  cfg,
		owner:   owner,
		repo:    repo,
		client:  newGitHubClientWithHTTP(token, httpClient),
		timeout: httpx.NormalizeTimeout(httpClient.Timeout),
	}, nil
}

func (s *githubSource) Close() error {
	return s.client.Close()
}

func (s *githubSource) Sync(
	ctx context.Context,
	destDir string,
	prevLock *lockfile.LockEntry,
	opts SyncOptions,
) (*SyncResult, error) {
	sourceCtx, cancel := httpx.SourceContext(ctx, s.timeout)
	defer cancel()

	if isSingleFilePath(s.source.Path) {
		return s.syncSingleFile(sourceCtx, destDir, prevLock, opts)
	}

	return s.syncDirectory(sourceCtx, destDir, prevLock, opts)
}

func (s *githubSource) syncSingleFile(
	ctx context.Context,
	destDir string,
	prevLock *lockfile.LockEntry,
	opts SyncOptions,
) (*SyncResult, error) {
	ref, err := s.resolveRef(ctx)
	if err != nil {
		return nil, err
	}

	filePath := normalizeRepoPath(s.source.Path)
	relativePath := path.Base(filePath)
	sha, err := s.fetchContentSHA(ctx, ref, filePath)
	if err != nil {
		return nil, err
	}

	oldSHA := ""
	if prevLock != nil && prevLock.Files != nil {
		oldSHA = prevLock.Files[relativePath]
	}

	if !opts.Force && oldSHA != "" && oldSHA == sha {
		lockEntry := cloneLockEntry(prevLock)
		if lockEntry == nil {
			lockEntry = &lockfile.LockEntry{Type: sourceTypeGitHub}
		}

		lockEntry.Type = sourceTypeGitHub
		lockEntry.RefResolved = ref
		lockEntry.SyncedAt = time.Now().UTC()

		return &SyncResult{
			Skipped:   true,
			LockEntry: lockEntry,
		}, nil
	}

	if !opts.DryRun {
		content, fetchErr := s.fetchBlobContent(ctx, sha)
		if fetchErr != nil {
			return nil, fetchErr
		}

		root, rootErr := destinationRoot(destDir, opts)
		if rootErr != nil {
			return nil, rootErr
		}
		defer root.Close()
		if writeErr := fsutil.AtomicWrite(root, filepath.FromSlash(relativePath), content); writeErr != nil {
			return nil, writeErr
		}
	}

	return &SyncResult{
		Downloaded: 1,
		LockEntry: &lockfile.LockEntry{
			Type:        sourceTypeGitHub,
			RefResolved: ref,
			SyncedAt:    time.Now().UTC(),
			Files: map[string]string{
				relativePath: sha,
			},
		},
	}, nil
}

func (s *githubSource) syncDirectory(
	ctx context.Context,
	destDir string,
	prevLock *lockfile.LockEntry,
	opts SyncOptions,
) (*SyncResult, error) {
	ref, err := s.resolveRef(ctx)
	if err != nil {
		return nil, err
	}

	tree, err := s.fetchTree(ctx, ref)
	if err != nil {
		return nil, err
	}

	if !opts.Force && prevLock != nil && prevLock.TreeSHA == tree.SHA {
		lockEntry := cloneLockEntry(prevLock)
		if lockEntry == nil {
			lockEntry = &lockfile.LockEntry{Type: sourceTypeGitHub}
		}

		lockEntry.Type = sourceTypeGitHub
		lockEntry.RefResolved = ref
		lockEntry.SyncedAt = time.Now().UTC()

		return &SyncResult{
			Skipped:   true,
			LockEntry: lockEntry,
		}, nil
	}

	newFiles, err := s.buildFileMap(tree.Tree)
	if err != nil {
		return nil, err
	}

	oldFiles := map[string]string{}
	if prevLock != nil && prevLock.Files != nil {
		oldFiles = prevLock.Files
	}

	toDownload := diffDownloads(newFiles, oldFiles, opts.Force)
	toDelete := diffDeletes(oldFiles, newFiles)

	if !opts.DryRun {
		root, rootErr := destinationRoot(destDir, opts)
		if rootErr != nil {
			return nil, rootErr
		}
		defer root.Close()
		if downloadErr := s.downloadFiles(ctx, root, toDownload); downloadErr != nil {
			return nil, downloadErr
		}
		if deleteErr := s.deleteStaleFiles(root, toDelete); deleteErr != nil {
			return nil, deleteErr
		}
	}

	return &SyncResult{
		Downloaded: len(toDownload),
		Deleted:    len(toDelete),
		LockEntry: &lockfile.LockEntry{
			Type:        sourceTypeGitHub,
			TreeSHA:     tree.SHA,
			RefResolved: ref,
			SyncedAt:    time.Now().UTC(),
			Files:       newFiles,
		},
	}, nil
}

func (s *githubSource) downloadFiles(ctx context.Context, root *os.Root, toDownload map[string]string) error {
	for _, relativePath := range slices.Sorted(maps.Keys(toDownload)) {
		if err := config.ValidatePath(filepath.FromSlash(relativePath)); err != nil {
			return err
		}
		content, err := s.fetchBlobContent(ctx, toDownload[relativePath])
		if err != nil {
			return err
		}
		if atomicWriteErr := fsutil.AtomicWrite(
			root,
			filepath.FromSlash(relativePath),
			content,
		); atomicWriteErr != nil {
			return atomicWriteErr
		}
	}
	return nil
}

func (s *githubSource) deleteStaleFiles(root *os.Root, toDelete map[string]struct{}) error {
	for _, relativePath := range slices.Sorted(maps.Keys(toDelete)) {
		local := filepath.FromSlash(relativePath)
		if err := config.ValidatePath(local); err != nil {
			return err
		}
		if err := root.Remove(local); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("deleting stale file: %w", err)
		}
		cleanupEmptyDirs(root, filepath.Dir(local))
	}
	return nil
}

func (s *githubSource) resolveRef(ctx context.Context) (string, error) {
	if s.resolvedRef != "" {
		return s.resolvedRef, nil
	}

	if s.source.Ref != "" {
		s.resolvedRef = s.source.Ref
		return s.resolvedRef, nil
	}

	endpoint := fmt.Sprintf("/repos/%s/%s", s.owner, s.repo)
	result := &githubRepoResponse{}

	response, err := s.client.R().
		SetContext(ctx).
		SetResult(result).
		Get(endpoint)
	if err != nil {
		return "", errs.Wrap(
			errs.Network,
			fmt.Errorf("fetching repository metadata: %w", err),
			errs.WithFields(map[string]any{fieldRepo: s.source.Repo, errs.LegacyCodeField: errs.LegacyGithubAPIError}),
		)
	}

	if !response.IsSuccess() {
		return "", errs.New(
			errs.Network,
			fmt.Sprintf("github API returned status %d for repository metadata", response.StatusCode()),
			errs.WithHint("Check that the repository exists and is accessible"),
			errs.WithFields(
				map[string]any{
					fieldRepo:            s.source.Repo,
					fieldStatus:          response.StatusCode(),
					errs.LegacyCodeField: errs.LegacyGithubAPIError,
				},
			),
			errs.WithCause(&errs.HTTPStatusError{Code: response.StatusCode()}),
		)
	}

	if rlErr := s.checkRateLimit(response); rlErr != nil {
		return "", rlErr
	}

	if result.DefaultBranch == "" {
		return "", errs.New(
			errs.Network,
			"github repository metadata did not include default branch",
			errs.WithFields(map[string]any{fieldRepo: s.source.Repo, errs.LegacyCodeField: errs.LegacyGithubAPIError}),
		)
	}

	s.resolvedRef = result.DefaultBranch
	return s.resolvedRef, nil
}

func (s *githubSource) fetchTree(ctx context.Context, ref string) (*githubTreeResponse, error) {
	endpoint := fmt.Sprintf("/repos/%s/%s/git/trees/%s", s.owner, s.repo, neturl.PathEscape(ref))
	result := &githubTreeResponse{}

	response, err := s.client.R().
		SetContext(ctx).
		SetQueryParam("recursive", "1").
		SetResult(result).
		Get(endpoint)
	if err != nil {
		return nil, errs.Wrap(
			errs.Network,
			fmt.Errorf("fetching tree: %w", err),
			errs.WithFields(
				map[string]any{
					fieldRepo:            s.source.Repo,
					fieldRef:             ref,
					errs.LegacyCodeField: errs.LegacyGithubAPIError,
				},
			),
		)
	}

	if !response.IsSuccess() {
		return nil, errs.New(
			errs.Network,
			fmt.Sprintf("github API returned status %d for tree", response.StatusCode()),
			errs.WithHint("Check repository, path, and ref in your config"),
			errs.WithFields(
				map[string]any{
					fieldRepo:            s.source.Repo,
					fieldRef:             ref,
					fieldStatus:          response.StatusCode(),
					errs.LegacyCodeField: errs.LegacyGithubAPIError,
				},
			),
			errs.WithCause(&errs.HTTPStatusError{Code: response.StatusCode()}),
		)
	}

	if rlErr := s.checkRateLimit(response); rlErr != nil {
		return nil, rlErr
	}

	if result.Truncated {
		return nil, errs.New(
			errs.Network,
			"github returned a truncated tree; contents fallback is not implemented",
			errs.WithHint("Narrow the configured path to reduce tree size"),
			errs.WithFields(
				map[string]any{
					fieldRepo:            s.source.Repo,
					fieldRef:             ref,
					errs.LegacyCodeField: errs.LegacyGithubAPIError,
				},
			),
		)
	}

	return result, nil
}

func (s *githubSource) fetchContentSHA(ctx context.Context, ref string, filePath string) (string, error) {
	endpoint := fmt.Sprintf("/repos/%s/%s/contents/%s", s.owner, s.repo, escapeRepoPath(filePath))
	result := &githubContentResponse{}

	response, err := s.client.R().
		SetContext(ctx).
		SetQueryParam(fieldRef, ref).
		SetResult(result).
		Get(endpoint)
	if err != nil {
		return "", errs.Wrap(
			errs.Network,
			fmt.Errorf("fetching content metadata: %w", err),
			errs.WithFields(
				map[string]any{
					fieldRepo:            s.source.Repo,
					fieldPath:            filePath,
					errs.LegacyCodeField: errs.LegacyGithubAPIError,
				},
			),
		)
	}

	if !response.IsSuccess() {
		return "", errs.New(
			errs.Network,
			fmt.Sprintf("github API returned status %d for content metadata", response.StatusCode()),
			errs.WithHint("Check repository path and ref in your config"),
			errs.WithFields(
				map[string]any{
					fieldRepo:            s.source.Repo,
					fieldPath:            filePath,
					fieldStatus:          response.StatusCode(),
					errs.LegacyCodeField: errs.LegacyGithubAPIError,
				},
			),
			errs.WithCause(&errs.HTTPStatusError{Code: response.StatusCode()}),
		)
	}

	if rlErr := s.checkRateLimit(response); rlErr != nil {
		return "", rlErr
	}

	if result.Type != "file" || result.SHA == "" {
		return "", errs.New(
			errs.Network,
			fmt.Sprintf("expected file metadata for %q", filePath),
			errs.WithFields(
				map[string]any{
					fieldRepo:            s.source.Repo,
					fieldPath:            filePath,
					errs.LegacyCodeField: errs.LegacyGithubAPIError,
				},
			),
		)
	}

	return result.SHA, nil
}

func (s *githubSource) fetchBlobContent(ctx context.Context, sha string) ([]byte, error) {
	endpoint := fmt.Sprintf("/repos/%s/%s/git/blobs/%s", s.owner, s.repo, sha)
	result := &githubBlobResponse{}

	response, err := s.client.R().
		SetContext(ctx).
		SetResult(result).
		Get(endpoint)
	if err != nil {
		return nil, errs.Wrap(
			errs.Network,
			fmt.Errorf("downloading blob: %w", err),
			errs.WithFields(
				map[string]any{
					fieldRepo:            s.source.Repo,
					fieldSHA:             sha,
					errs.LegacyCodeField: errs.LegacyDownloadFailed,
				},
			),
		)
	}

	if !response.IsSuccess() {
		return nil, errs.New(
			errs.Network,
			fmt.Sprintf("github API returned status %d for blob", response.StatusCode()),
			errs.WithFields(
				map[string]any{
					fieldRepo:            s.source.Repo,
					fieldSHA:             sha,
					fieldStatus:          response.StatusCode(),
					errs.LegacyCodeField: errs.LegacyGithubAPIError,
				},
			),
			errs.WithCause(&errs.HTTPStatusError{Code: response.StatusCode()}),
		)
	}

	if rlErr := s.checkRateLimit(response); rlErr != nil {
		return nil, rlErr
	}

	if result.Encoding != "base64" {
		return nil, errs.New(
			errs.Network,
			fmt.Sprintf("unsupported blob encoding %q", result.Encoding),
			errs.WithFields(
				map[string]any{
					fieldRepo:            s.source.Repo,
					fieldSHA:             sha,
					errs.LegacyCodeField: errs.LegacyDownloadFailed,
				},
			),
		)
	}

	normalized := strings.ReplaceAll(result.Content, "\n", "")
	content, err := base64.StdEncoding.DecodeString(normalized)
	if err != nil {
		return nil, errs.Wrap(
			errs.Network,
			fmt.Errorf("decoding blob content: %w", err),
			errs.WithFields(
				map[string]any{
					fieldRepo:            s.source.Repo,
					fieldSHA:             sha,
					errs.LegacyCodeField: errs.LegacyDownloadFailed,
				},
			),
		)
	}

	return content, nil
}

func (s *githubSource) buildFileMap(treeEntries []githubTreeEntry) (map[string]string, error) {
	basePath := normalizeRepoPath(s.source.Path)
	patterns := s.source.Patterns
	if len(patterns) == 0 {
		patterns = config.DefaultPatterns()
	}

	files := make(map[string]string)
	for _, entry := range treeEntries {
		if entry.Type != "blob" || entry.Path == "" || entry.SHA == "" {
			continue
		}

		relativePath, ok := relativePathWithinBase(entry.Path, basePath)
		if !ok {
			continue
		}

		include, err := shouldIncludeFile(relativePath, patterns, s.source.Exclude)
		if err != nil {
			return nil, err
		}

		if !include {
			continue
		}

		files[relativePath] = entry.SHA
	}

	return files, nil
}

func diffDownloads(newFiles map[string]string, oldFiles map[string]string, force bool) map[string]string {
	toDownload := make(map[string]string)

	for relativePath, newSHA := range newFiles {
		oldSHA, existed := oldFiles[relativePath]
		if force || !existed || oldSHA != newSHA {
			toDownload[relativePath] = newSHA
		}
	}

	return toDownload
}

func diffDeletes(oldFiles map[string]string, newFiles map[string]string) map[string]struct{} {
	toDelete := make(map[string]struct{})

	for relativePath := range oldFiles {
		if _, exists := newFiles[relativePath]; !exists {
			toDelete[relativePath] = struct{}{}
		}
	}

	return toDelete
}

func relativePathWithinBase(remotePath string, basePath string) (string, bool) {
	cleanRemote := normalizeRepoPath(remotePath)
	cleanBase := normalizeRepoPath(basePath)

	if cleanBase == "" {
		return cleanRemote, cleanRemote != ""
	}

	if cleanRemote == cleanBase {
		return path.Base(cleanRemote), true
	}

	prefix := cleanBase + "/"
	if !strings.HasPrefix(cleanRemote, prefix) {
		return "", false
	}

	relativePath := strings.TrimPrefix(cleanRemote, prefix)
	return relativePath, relativePath != ""
}

func shouldIncludeFile(relativePath string, patterns []string, exclude []string) (bool, error) {
	included, err := matchesAny(patterns, relativePath)
	if err != nil || !included {
		return false, err
	}

	excluded, err := matchesAny(exclude, relativePath)
	if err != nil {
		return false, err
	}

	return !excluded, nil
}

func matchesAny(patterns []string, candidate string) (bool, error) {
	for _, pattern := range patterns {
		matched, err := doublestar.PathMatch(pattern, candidate)
		if err != nil {
			return false, errs.Wrap(
				errs.Config,
				fmt.Errorf("invalid glob pattern: %w", err),
				errs.WithFields(
					map[string]any{
						"pattern":            pattern,
						fieldPath:            candidate,
						errs.LegacyCodeField: errs.LegacyConfigInvalid,
					},
				),
			)
		}

		if matched {
			return true, nil
		}
	}

	return false, nil
}

func cleanupEmptyDirs(root *os.Root, directory string) {
	for directory != "." && filepath.IsLocal(directory) {
		if err := root.Remove(directory); err != nil {
			return
		}
		directory = filepath.Dir(directory)
	}
}

func isSingleFilePath(repoPath string) bool {
	trimmed := strings.TrimSpace(repoPath)
	if strings.HasSuffix(trimmed, "/") {
		return false
	}

	switch strings.ToLower(filepath.Ext(trimmed)) {
	case ".md", ".mdx", ".txt", ".rst":
		return true
	default:
		return false
	}
}

func normalizeRepoPath(repoPath string) string {
	trimmed := strings.TrimSpace(repoPath)
	trimmed = strings.TrimPrefix(trimmed, "/")
	trimmed = strings.TrimSuffix(trimmed, "/")
	if trimmed == "" {
		return ""
	}

	cleaned := path.Clean(trimmed)
	if cleaned == "." {
		return ""
	}

	return strings.TrimPrefix(cleaned, "/")
}

func escapeRepoPath(repoPath string) string {
	cleaned := normalizeRepoPath(repoPath)
	if cleaned == "" {
		return ""
	}

	parts := strings.Split(cleaned, "/")
	for i, part := range parts {
		parts[i] = neturl.PathEscape(part)
	}

	return strings.Join(parts, "/")
}

func parseRepo(repo string) (string, string, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", errs.New(
			errs.Config,
			fmt.Sprintf("invalid github repo format %q", repo),
			errs.WithHint("Expected repo format: owner/repo"),
			errs.WithFields(map[string]any{fieldRepo: repo, errs.LegacyCodeField: errs.LegacyConfigInvalid}),
		)
	}

	return parts[0], parts[1], nil
}

func newGitHubClientWithHTTP(token string, httpClient *http.Client) *resty.Client {
	if httpClient == nil {
		httpClient = httpx.NewClient(httpx.DefaultTimeout)
	}
	client := resty.NewWithClient(httpClient)
	client.SetBaseURL(githubAPIBaseURL)
	client.SetHeader("Accept", "application/vnd.github.v3+json")
	client.SetHeader("User-Agent", userAgent)
	client.SetRetryCount(httpRetryCount)
	client.SetRetryWaitTime(1 * time.Second)
	client.SetRetryMaxWaitTime(httpRetryMaxWaitSec * time.Second)

	if token != "" {
		client.SetAuthToken(token)
	}

	return client
}

func (s *githubSource) checkRateLimit(response *resty.Response) error {
	remainingRaw := response.Header().Get("X-Ratelimit-Remaining")
	if remainingRaw == "" {
		return nil
	}

	remaining, err := strconv.Atoi(remainingRaw)
	if err != nil {
		return nil //nolint:nilerr // Non-critical header parsing; malformed value is safely ignored.
	}

	if remaining == 0 {
		reset := response.Header().Get("X-Ratelimit-Reset")
		var resetTime time.Time
		if unix, parseErr := strconv.ParseInt(reset, 10, 64); parseErr == nil {
			resetTime = time.Unix(unix, 0)
		}
		return errs.New(
			errs.RateLimited,
			"github API rate limit exhausted",
			errs.WithCause(&errs.RateLimitError{Reset: resetTime}),
			errs.WithHint("Set github_token, GITHUB_TOKEN, or GH_TOKEN to increase limits"),
			errs.WithFields(
				map[string]any{
					fieldRepo:            s.source.Repo,
					"reset":              reset,
					errs.LegacyCodeField: errs.LegacyGithubRateLimit,
				},
			),
		)
	}

	if remaining <= rateLimitWarnThresh && !s.warnedLowRL {
		s.warnedLowRL = true
	}

	return nil
}
