package source

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/fsutil"
	"github.com/g5becks/dox/internal/httpx"
	"github.com/g5becks/dox/internal/lockfile"
)

const (
	sourceTypeGitHub    = "github"
	githubAPIBaseURL    = "https://api.github.com"
	githubAPIVersion    = "2022-11-28"
	githubJSONMediaType = "application/vnd.github+json"
	githubRawMediaType  = "application/vnd.github.raw"
	gitObjectBlob       = "blob"
	userAgent           = "dox"
	httpRetryCount      = 3
	rateLimitWarnThresh = 10
)

type githubSource struct {
	name              string
	source            config.Source
	owner             string
	repo              string
	http              *http.Client
	ownsClient        bool
	apiBase           string
	rawBase           string
	codeloadBase      string
	token             string
	timeout           time.Duration
	maxRetries        int
	retriesConfigured bool
	retryBaseWait     time.Duration
	maxRateLimitWait  time.Duration
	resolvedRef       string
	resolvedCommit    string
	tarballMinFiles   int
	tarballByteBudget int64
	warnedLowRL       atomic.Bool
	onWarning         func(string)
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
	Size int64  `json:"size"`
}

type githubContentResponse struct {
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

func newGitHubSource(name string, cfg config.Source, token string) (Source, error) {
	return newGitHubSourceWithHTTP(name, cfg, token, nil)
}

func newGitHubSourceWithHTTP(name string, cfg config.Source, token string, httpClient *http.Client) (Source, error) {
	owner, repo, err := parseRepo(cfg.Repo)
	if err != nil {
		return nil, err
	}
	ownsClient := httpClient == nil
	if ownsClient {
		httpClient = httpx.NewClient(httpx.DefaultTimeout)
	}

	return &githubSource{
		name:         name,
		source:       cfg,
		owner:        owner,
		repo:         repo,
		http:         httpClient,
		ownsClient:   ownsClient,
		apiBase:      githubAPIBaseURL,
		rawBase:      githubRawBaseURL,
		codeloadBase: githubCodeloadBaseURL,
		token:        token,
		timeout:      httpx.NormalizeTimeout(httpClient.Timeout),
	}, nil
}

// Close releases idle connections of a client this source created; a shared
// client belongs to the caller.
func (s *githubSource) Close() error {
	if s.ownsClient {
		s.http.CloseIdleConnections()
	}
	return nil
}

func (s *githubSource) Sync(
	ctx context.Context,
	destDir string,
	prevLock *lockfile.LockEntry,
	opts SyncOptions,
) (*SyncResult, error) {
	s.onWarning = opts.OnWarning
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
	ref, commit, err := s.resolveCommit(ctx)
	if err != nil {
		return nil, err
	}

	filePath := normalizeRepoPath(s.source.Path)
	relativePath := path.Base(filePath)
	sha, err := s.fetchContentSHA(ctx, commit, filePath)
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
		lockEntry.Commit = commit
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
			Commit:      commit,
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
	ref, commit, err := s.resolveCommit(ctx)
	if err != nil {
		return nil, err
	}

	// Fetch only the configured folder's tree ("<commit>:<path>") and compare
	// its own SHA, so commits elsewhere in the repository keep the fast path.
	tree, err := s.fetchFolderTree(ctx, commit)
	if err != nil {
		return nil, err
	}
	folderSHA := tree.SHA

	if !opts.Force && prevLock != nil && prevLock.TreeSHA == folderSHA {
		lockEntry := cloneLockEntry(prevLock)
		if lockEntry == nil {
			lockEntry = &lockfile.LockEntry{Type: sourceTypeGitHub}
		}

		lockEntry.Type = sourceTypeGitHub
		lockEntry.RefResolved = ref
		lockEntry.Commit = commit
		lockEntry.SyncedAt = time.Now().UTC()

		return &SyncResult{
			Skipped:   true,
			LockEntry: lockEntry,
		}, nil
	}

	remoteFiles, err := s.buildFileMap(tree.Tree)
	if err != nil {
		return nil, err
	}
	newFiles := make(map[string]string, len(remoteFiles))
	for relativePath, file := range remoteFiles {
		newFiles[relativePath] = file.sha
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
		changed := make(map[string]remoteFile, len(toDownload))
		for relativePath := range toDownload {
			changed[relativePath] = remoteFiles[relativePath]
		}
		if downloadErr := s.downloadChanged(
			ctx, root, commit, changed, s.downloadConcurrency(opts),
		); downloadErr != nil {
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
			TreeSHA:     folderSHA,
			RefResolved: ref,
			Commit:      commit,
			SyncedAt:    time.Now().UTC(),
			Files:       newFiles,
		},
	}, nil
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

func (s *githubSource) fetchTree(ctx context.Context, ref string, recursive bool) (*githubTreeResponse, error) {
	endpoint := fmt.Sprintf("/repos/%s/%s/git/trees/%s", s.owner, s.repo, neturl.PathEscape(ref))
	result := &githubTreeResponse{}
	var query map[string]string
	if recursive {
		query = map[string]string{"recursive": "1"}
	}

	if err := s.doGitHubGet(ctx, githubRequestSpec{
		endpoint:   endpoint,
		query:      query,
		result:     result,
		errContext: "fetching tree",
		statusDesc: "tree",
		statusHint: "Check repository, path, and ref in your config",
		legacyCode: errs.LegacyGithubAPIError,
		fields: map[string]any{
			fieldRepo:            s.source.Repo,
			fieldRef:             ref,
			errs.LegacyCodeField: errs.LegacyGithubAPIError,
		},
	}); err != nil {
		return nil, err
	}

	return result, nil
}

func (s *githubSource) fetchContentSHA(ctx context.Context, ref string, filePath string) (string, error) {
	endpoint := fmt.Sprintf("/repos/%s/%s/contents/%s", s.owner, s.repo, escapeRepoPath(filePath))
	result := &githubContentResponse{}

	if err := s.doGitHubGet(ctx, githubRequestSpec{
		endpoint:   endpoint,
		query:      map[string]string{fieldRef: ref},
		result:     result,
		errContext: "fetching content metadata",
		statusDesc: "content metadata",
		statusHint: "Check repository path and ref in your config",
		legacyCode: errs.LegacyGithubAPIError,
		fields: map[string]any{
			fieldRepo:            s.source.Repo,
			fieldPath:            filePath,
			errs.LegacyCodeField: errs.LegacyGithubAPIError,
		},
	}); err != nil {
		return "", err
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

// fetchBlobContent downloads one blob as raw bytes (no JSON or base64).
func (s *githubSource) fetchBlobContent(ctx context.Context, sha string) ([]byte, error) {
	endpoint := fmt.Sprintf("/repos/%s/%s/git/blobs/%s", s.owner, s.repo, sha)
	var content []byte

	if err := s.doGitHubGet(ctx, githubRequestSpec{
		endpoint:   endpoint,
		accept:     githubRawMediaType,
		raw:        &content,
		errContext: "downloading blob",
		statusDesc: gitObjectBlob,
		legacyCode: errs.LegacyDownloadFailed,
		fields: map[string]any{
			fieldRepo:            s.source.Repo,
			fieldSHA:             sha,
			errs.LegacyCodeField: errs.LegacyDownloadFailed,
		},
	}); err != nil {
		return nil, err
	}

	return content, nil
}

func (s *githubSource) buildFileMap(treeEntries []githubTreeEntry) (map[string]remoteFile, error) {
	basePath := normalizeRepoPath(s.source.Path)
	patterns := s.source.Patterns
	if len(patterns) == 0 {
		patterns = config.DefaultPatterns()
	}

	files := make(map[string]remoteFile)
	for _, entry := range treeEntries {
		if entry.Type != gitObjectBlob || entry.Path == "" || entry.SHA == "" {
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

		files[relativePath] = remoteFile{repoPath: normalizeRepoPath(entry.Path), sha: entry.SHA, size: entry.Size}
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
