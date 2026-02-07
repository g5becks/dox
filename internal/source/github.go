package source

import (
	"context"
	"encoding/base64"
	"fmt"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/samber/oops"
	"resty.dev/v3"

	"github.com/g5becks/dox/internal/config"
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
	owner, repo, err := parseRepo(cfg.Repo)
	if err != nil {
		return nil, err
	}

	return &githubSource{
		name:   name,
		source: cfg,
		owner:  owner,
		repo:   repo,
		client: newGitHubClient(token),
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
	if isSingleFilePath(s.source.Path) {
		return s.syncSingleFile(ctx, destDir, prevLock, opts)
	}

	return s.syncDirectory(ctx, destDir, prevLock, opts)
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

		localPath := filepath.Join(destDir, filepath.FromSlash(relativePath))
		if mkdirErr := os.MkdirAll(filepath.Dir(localPath), 0o750); mkdirErr != nil {
			return nil, oops.
				Code("WRITE_FAILED").
				With("source", s.name).
				With("path", filepath.Dir(localPath)).
				Wrapf(mkdirErr, "creating destination directory")
		}

		if writeErr := writeFileAtomic(localPath, content); writeErr != nil {
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
		if mkdirErr := os.MkdirAll(destDir, 0o750); mkdirErr != nil {
			return nil, oops.
				Code("WRITE_FAILED").
				With("source", s.name).
				With("path", destDir).
				Wrapf(mkdirErr, "creating destination directory")
		}

		if downloadErr := s.downloadFiles(ctx, destDir, toDownload); downloadErr != nil {
			return nil, downloadErr
		}

		if deleteErr := s.deleteStaleFiles(destDir, toDelete); deleteErr != nil {
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

func (s *githubSource) downloadFiles(ctx context.Context, destDir string, toDownload map[string]string) error {
	for _, relativePath := range sortedKeys(toDownload) {
		sha := toDownload[relativePath]
		content, fetchErr := s.fetchBlobContent(ctx, sha)
		if fetchErr != nil {
			return fetchErr
		}

		localPath := filepath.Join(destDir, filepath.FromSlash(relativePath))
		if mkdirErr := os.MkdirAll(filepath.Dir(localPath), 0o750); mkdirErr != nil {
			return oops.
				Code("WRITE_FAILED").
				With("source", s.name).
				With("path", filepath.Dir(localPath)).
				Wrapf(mkdirErr, "creating destination directory")
		}

		if writeErr := writeFileAtomic(localPath, content); writeErr != nil {
			return writeErr
		}
	}

	return nil
}

func (s *githubSource) deleteStaleFiles(destDir string, toDelete map[string]struct{}) error {
	for _, relativePath := range sortedKeys(toDelete) {
		localPath := filepath.Join(destDir, filepath.FromSlash(relativePath))
		if removeErr := os.Remove(localPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return oops.
				Code("WRITE_FAILED").
				With("source", s.name).
				With("path", localPath).
				Wrapf(removeErr, "deleting stale file")
		}

		cleanupEmptyDirs(filepath.Dir(localPath), destDir)
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
		return "", oops.
			Code("GITHUB_API_ERROR").
			With("repo", s.source.Repo).
			Wrapf(err, "fetching repository metadata")
	}

	if !response.IsSuccess() {
		return "", oops.
			Code("GITHUB_API_ERROR").
			With("repo", s.source.Repo).
			With("status", response.StatusCode()).
			Hint("Check that the repository exists and is accessible").
			Errorf("github API returned status %d for repository metadata", response.StatusCode())
	}

	if rlErr := s.checkRateLimit(response); rlErr != nil {
		return "", rlErr
	}

	if result.DefaultBranch == "" {
		return "", oops.
			Code("GITHUB_API_ERROR").
			With("repo", s.source.Repo).
			Errorf("github repository metadata did not include default branch")
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
		return nil, oops.
			Code("GITHUB_API_ERROR").
			With("repo", s.source.Repo).
			With("ref", ref).
			Wrapf(err, "fetching tree")
	}

	if !response.IsSuccess() {
		return nil, oops.
			Code("GITHUB_API_ERROR").
			With("repo", s.source.Repo).
			With("ref", ref).
			With("status", response.StatusCode()).
			Hint("Check repository, path, and ref in your config").
			Errorf("github API returned status %d for tree", response.StatusCode())
	}

	if rlErr := s.checkRateLimit(response); rlErr != nil {
		return nil, rlErr
	}

	if result.Truncated {
		return nil, oops.
			Code("GITHUB_API_ERROR").
			With("repo", s.source.Repo).
			With("ref", ref).
			Hint("Narrow the configured path to reduce tree size").
			Errorf("github returned a truncated tree; contents fallback is not implemented")
	}

	return result, nil
}

func (s *githubSource) fetchContentSHA(ctx context.Context, ref string, filePath string) (string, error) {
	endpoint := fmt.Sprintf("/repos/%s/%s/contents/%s", s.owner, s.repo, escapeRepoPath(filePath))
	result := &githubContentResponse{}

	response, err := s.client.R().
		SetContext(ctx).
		SetQueryParam("ref", ref).
		SetResult(result).
		Get(endpoint)
	if err != nil {
		return "", oops.
			Code("GITHUB_API_ERROR").
			With("repo", s.source.Repo).
			With("path", filePath).
			Wrapf(err, "fetching content metadata")
	}

	if !response.IsSuccess() {
		return "", oops.
			Code("GITHUB_API_ERROR").
			With("repo", s.source.Repo).
			With("path", filePath).
			With("status", response.StatusCode()).
			Hint("Check repository path and ref in your config").
			Errorf("github API returned status %d for content metadata", response.StatusCode())
	}

	if rlErr := s.checkRateLimit(response); rlErr != nil {
		return "", rlErr
	}

	if result.Type != "file" || result.SHA == "" {
		return "", oops.
			Code("GITHUB_API_ERROR").
			With("repo", s.source.Repo).
			With("path", filePath).
			Errorf("expected file metadata for %q", filePath)
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
		return nil, oops.
			Code("DOWNLOAD_FAILED").
			With("repo", s.source.Repo).
			With("sha", sha).
			Wrapf(err, "downloading blob")
	}

	if !response.IsSuccess() {
		return nil, oops.
			Code("GITHUB_API_ERROR").
			With("repo", s.source.Repo).
			With("sha", sha).
			With("status", response.StatusCode()).
			Errorf("github API returned status %d for blob", response.StatusCode())
	}

	if rlErr := s.checkRateLimit(response); rlErr != nil {
		return nil, rlErr
	}

	if result.Encoding != "base64" {
		return nil, oops.
			Code("DOWNLOAD_FAILED").
			With("repo", s.source.Repo).
			With("sha", sha).
			Errorf("unsupported blob encoding %q", result.Encoding)
	}

	normalized := strings.ReplaceAll(result.Content, "\n", "")
	content, err := base64.StdEncoding.DecodeString(normalized)
	if err != nil {
		return nil, oops.
			Code("DOWNLOAD_FAILED").
			With("repo", s.source.Repo).
			With("sha", sha).
			Wrapf(err, "decoding blob content")
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

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}

	slices.Sort(keys)
	return keys
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
			return false, oops.
				Code("CONFIG_INVALID").
				With("pattern", pattern).
				With("path", candidate).
				Wrapf(err, "invalid glob pattern")
		}

		if matched {
			return true, nil
		}
	}

	return false, nil
}

func cleanupEmptyDirs(startDir string, stopDir string) {
	current := startDir
	cleanStop := filepath.Clean(stopDir)

	for current != cleanStop && current != "." && current != string(filepath.Separator) {
		entries, err := os.ReadDir(current)
		if err != nil || len(entries) != 0 {
			return
		}

		if removeErr := os.Remove(current); removeErr != nil {
			return
		}

		current = filepath.Dir(current)
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
		return "", "", oops.
			Code("CONFIG_INVALID").
			With("repo", repo).
			Hint("Expected repo format: owner/repo").
			Errorf("invalid github repo format %q", repo)
	}

	return parts[0], parts[1], nil
}

func newGitHubClient(token string) *resty.Client {
	client := resty.New()
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
		return oops.
			Code("GITHUB_RATE_LIMIT").
			With("repo", s.source.Repo).
			With("reset", reset).
			Hint("Set github_token, GITHUB_TOKEN, or GH_TOKEN to increase limits").
			Errorf("github API rate limit exhausted")
	}

	if remaining <= rateLimitWarnThresh && !s.warnedLowRL {
		s.warnedLowRL = true
	}

	return nil
}
