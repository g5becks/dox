package source

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/fsutil"
)

const (
	githubRawBaseURL      = "https://raw.githubusercontent.com"
	githubCodeloadBaseURL = "https://codeload.github.com"
	githubSHAMediaType    = "application/vnd.github.sha"
	defaultRef            = "HEAD"
	gitSHALength          = 40

	// DefaultDownloadConcurrency bounds concurrent raw file downloads per source.
	DefaultDownloadConcurrency = 8

	// defaultTarballMinFiles is the changed-file count from which one tarball
	// beats per-file raw downloads. Measured against github.com (see dox-deb.22
	// notes): codeload needs roughly 0.4-0.8s to start a small repository
	// archive, while raw files cost ~60ms each at 8-way concurrency, so the
	// crossover is ~50-100 files; 64 sits in that range.
	defaultTarballMinFiles = 64
	// The tarball holds the whole repository, whose size is unknown up front.
	// Extraction stops after max(tarballSizeFactor x changed bytes,
	// tarballSizeFloor) compressed bytes and fetches the rest as raw files, so
	// a large repository costs at most that budget before falling back.
	tarballSizeFactor = 8
	tarballSizeFloor  = 16 << 20
)

// remoteFile is one tree blob selected for sync.
type remoteFile struct {
	repoPath string
	sha      string
	size     int64
}

// resolveCommit pins the configured ref (or HEAD) to a commit SHA with one REST
// call; later requests use the commit so a sync reads one consistent snapshot.
func (s *githubSource) resolveCommit(ctx context.Context) (string, string, error) {
	ref := s.source.Ref
	if ref == "" {
		ref = defaultRef
	}
	if s.resolvedCommit != "" {
		if s.resolvedRef != "" {
			ref = s.resolvedRef
		}
		return ref, s.resolvedCommit, nil
	}

	commit, err := s.provider.resolveCommit(ctx, ref)
	if err != nil {
		return "", "", err
	}
	s.resolvedRef, s.resolvedCommit = ref, commit
	return ref, commit, nil
}

// resolveCommit resolves ref to a commit SHA with one API call (the sha media
// type returns just the SHA).
func (p *githubProvider) resolveCommit(ctx context.Context, ref string) (string, error) {
	s := p.s
	var body []byte
	if err := s.doGitHubGet(ctx, githubRequestSpec{
		endpoint:   fmt.Sprintf("/repos/%s/%s/commits/%s", s.owner, s.repo, neturl.PathEscape(ref)),
		accept:     githubSHAMediaType,
		raw:        &body,
		errContext: errResolvingRef,
		statusDesc: "commit " + ref,
		statusHint: "Check that the repository exists and 'ref' names a branch, tag, or commit SHA",
		legacyCode: errs.LegacyGithubAPIError,
		fields: map[string]any{
			fieldRepo:            s.source.Repo,
			fieldRef:             ref,
			errs.LegacyCodeField: errs.LegacyGithubAPIError,
		},
	}); err != nil {
		return "", err
	}

	commit, ok := parseCommitSHA(body)
	if !ok {
		return "", errs.New(
			errs.Network,
			fmt.Sprintf("github API returned no commit SHA for ref %q", ref),
			errs.WithFields(map[string]any{
				fieldRepo:            s.source.Repo,
				fieldRef:             ref,
				errs.LegacyCodeField: errs.LegacyGithubAPIError,
			}),
		)
	}
	return commit, nil
}

// parseCommitSHA accepts the plain-text vnd.github.sha body or a JSON commit.
func parseCommitSHA(body []byte) (string, bool) {
	candidate := strings.TrimSpace(string(body))
	if !isHexSHA(candidate) {
		var commit struct {
			SHA string `json:"sha"`
		}
		if json.Unmarshal(body, &commit) != nil {
			return "", false
		}
		candidate = commit.SHA
	}
	return candidate, isHexSHA(candidate)
}

func isHexSHA(value string) bool {
	if len(value) != gitSHALength {
		return false
	}
	for _, r := range value {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

func (s *githubSource) downloadConcurrency(opts SyncOptions) int {
	if opts.DownloadConcurrency > 0 {
		return opts.DownloadConcurrency
	}
	return DefaultDownloadConcurrency
}

func (s *githubSource) effectiveTarballMinFiles() int {
	if s.tarballMinFiles > 0 {
		return s.tarballMinFiles
	}
	return defaultTarballMinFiles
}

// useTarball reports whether the change set is large enough for one archive
// download to beat per-file raw downloads.
func (s *githubSource) useTarball(toDownload map[string]remoteFile) bool {
	return len(toDownload) >= s.effectiveTarballMinFiles()
}

// tarballBudget bounds the compressed archive bytes read before giving up on
// the archive for this change set.
func (s *githubSource) tarballBudget(toDownload map[string]remoteFile) int64 {
	if s.tarballByteBudget > 0 {
		return s.tarballByteBudget
	}
	var changedBytes int64
	for _, file := range toDownload {
		changedBytes += file.size
	}
	return max(changedBytes*tarballSizeFactor, tarballSizeFloor)
}

// downloadChanged writes every changed file at commit, via one tarball when
// that is cheaper and raw per-file downloads otherwise (or for anything the
// archive did not provide).
func (s *githubSource) downloadChanged(
	ctx context.Context,
	root *os.Root,
	commit string,
	toDownload map[string]remoteFile,
	concurrency int,
) error {
	for relativePath := range toDownload {
		if err := config.ValidatePath(filepath.FromSlash(relativePath)); err != nil {
			return err
		}
	}
	remaining := toDownload
	if s.useTarball(toDownload) {
		var err error
		remaining, err = s.extractTarball(ctx, root, commit, toDownload)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			// The archive is an optimization; fetch whatever it did not deliver.
			remaining = s.notWritten(toDownload, remaining)
		}
	}
	return s.downloadRaw(ctx, root, commit, remaining, concurrency)
}

// notWritten returns remaining when extraction reported it, else everything.
func (s *githubSource) notWritten(all, remaining map[string]remoteFile) map[string]remoteFile {
	if remaining == nil {
		return all
	}
	return remaining
}

func (s *githubSource) downloadRaw(
	ctx context.Context,
	root *os.Root,
	commit string,
	files map[string]remoteFile,
	concurrency int,
) error {
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(max(1, concurrency))
	for _, relativePath := range slices.Sorted(maps.Keys(files)) {
		file := files[relativePath]
		group.Go(func() error {
			return s.fetchRawFile(groupCtx, root, commit, relativePath, file)
		})
	}
	return group.Wait()
}

func (s *githubSource) fetchRawFile(
	ctx context.Context,
	root *os.Root,
	commit, relativePath string,
	file remoteFile,
) error {
	fields := map[string]any{
		fieldRepo:            s.source.Repo,
		fieldPath:            file.repoPath,
		errs.LegacyCodeField: errs.LegacyDownloadFailed,
	}
	response, err := s.getWithRetry(ctx, func(ctx context.Context) (*http.Request, error) {
		return s.provider.rawRequest(ctx, commit, file.repoPath)
	})
	if err != nil {
		return errs.Wrap(errs.Network, fmt.Errorf("downloading %s: %w", file.repoPath, err), errs.WithFields(fields))
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		fields[fieldStatus] = response.StatusCode
		return errs.New(
			errs.Network,
			fmt.Sprintf("raw download returned status %d for %s", response.StatusCode, file.repoPath),
			errs.WithFields(fields),
			errs.WithCause(&errs.HTTPStatusError{Code: response.StatusCode}),
		)
	}
	if writeErr := fsutil.AtomicWriteReader(
		ctx,
		root,
		filepath.FromSlash(relativePath),
		response.Body,
	); writeErr != nil {
		return errs.Wrap(errs.Network, fmt.Errorf("writing %s: %w", relativePath, writeErr), errs.WithFields(fields))
	}
	return nil
}

// getWithRetry sends a GET for non-API content (raw files, archives), retrying
// network errors and 5xx before any body is consumed, like the API client.
func (s *githubSource) getWithRetry(
	ctx context.Context,
	build func(context.Context) (*http.Request, error),
) (*http.Response, error) {
	maxRetries := s.effectiveMaxRetries()
	for attempt := 0; ; attempt++ {
		request, err := build(ctx)
		if err != nil {
			return nil, err
		}
		request.Header.Set("User-Agent", userAgent)
		s.provider.authorize(request)
		response, err := s.http.Do(request)
		retryable := err != nil && !isContextTermination(ctx, err) ||
			err == nil && response.StatusCode >= http.StatusInternalServerError
		if !retryable || attempt >= maxRetries {
			return response, err
		}
		if response != nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
		if waitErr := sleepWithContext(ctx, s.jitteredBackoff(attempt)); waitErr != nil {
			return nil, waitErr
		}
	}
}

// extractTarball streams the commit archive and writes only the wanted files.
// It returns the files the archive did not provide (nil when it failed before
// reading any entry).
func (s *githubSource) extractTarball(
	ctx context.Context,
	root *os.Root,
	commit string,
	toDownload map[string]remoteFile,
) (map[string]remoteFile, error) {
	wanted := make(map[string]string, len(toDownload))
	for relativePath, file := range toDownload {
		wanted[file.repoPath] = relativePath
	}
	response, err := s.getWithRetry(ctx, func(ctx context.Context) (*http.Request, error) {
		return s.provider.archiveRequest(ctx, commit)
	})
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, &errs.HTTPStatusError{Code: response.StatusCode}
	}
	budget := &budgetReader{reader: response.Body, remaining: s.tarballBudget(toDownload)}
	compressed, err := gzip.NewReader(budget)
	if err != nil {
		return nil, err
	}
	defer compressed.Close()

	remaining := maps.Clone(toDownload)
	archive := tar.NewReader(compressed)
	for len(remaining) > 0 {
		header, nextErr := archive.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return remaining, nextErr
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		// Archive entries are "<repo>-<commit>/<path>"; drop the top directory.
		_, repoPath, found := strings.Cut(header.Name, "/")
		relativePath, ok := wanted[repoPath]
		if !found || !ok {
			continue
		}
		if _, pending := remaining[relativePath]; !pending {
			continue
		}
		if writeErr := fsutil.AtomicWriteReader(ctx, root, filepath.FromSlash(relativePath), archive); writeErr != nil {
			return remaining, writeErr
		}
		delete(remaining, relativePath)
	}
	return remaining, nil
}

// errTarballBudget stops an archive that is too large for its change set.
var errTarballBudget = errors.New("tarball exceeds its byte budget; fetching remaining files individually")

// budgetReader fails once more than remaining bytes have been read.
type budgetReader struct {
	reader    io.Reader
	remaining int64
}

func (b *budgetReader) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		return 0, errTarballBudget
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.reader.Read(p)
	b.remaining -= int64(n)
	return n, err
}

// fetchFolderTree lists every blob of the configured folder at commit with
// repository-relative paths. It requests the "<commit>:<path>" tree-ish, so
// GitHub returns only that folder and its own tree SHA in one call, and walks
// directory by directory when GitHub truncates the recursive listing.
func (p *githubProvider) folderTree(ctx context.Context, commit string) (*githubTreeResponse, error) {
	s := p.s
	base := normalizeRepoPath(s.source.Path)
	treeish := commit
	if base != "" {
		treeish = commit + ":" + base
	}
	tree, err := p.fetchTree(ctx, treeish, true)
	if err != nil {
		if status, ok := errors.AsType[*errs.HTTPStatusError](
			err,
		); ok && status.Code == http.StatusNotFound &&
			base != "" {
			return nil, errs.New(
				errs.NotFound,
				fmt.Sprintf("directory %q not found in %s at %s", base, s.source.Repo, commit),
				errs.WithHint("Check 'path' (and 'ref') for this source in your config"),
				errs.WithCause(errs.ErrSourceNotFound),
				errs.WithFields(map[string]any{
					fieldRepo:            s.source.Repo,
					fieldPath:            base,
					errs.LegacyCodeField: errs.LegacyGithubAPIError,
				}),
			)
		}
		return nil, err
	}
	if tree.Truncated {
		entries, walkErr := p.walkTree(ctx, tree.SHA, "")
		if walkErr != nil {
			return nil, walkErr
		}
		tree.Tree, tree.Truncated = entries, false
	}
	// A folder's tree lists paths relative to the folder; restore repo paths.
	if base != "" {
		for index := range tree.Tree {
			tree.Tree[index].Path = path.Join(base, tree.Tree[index].Path)
		}
	}
	return tree, nil
}

func (p *githubProvider) walkTree(ctx context.Context, treeID, directory string) ([]githubTreeEntry, error) {
	s := p.s
	tree, err := p.fetchTree(ctx, treeID, false)
	if err != nil {
		return nil, err
	}
	if tree.Truncated {
		return nil, errs.New(
			errs.Network,
			fmt.Sprintf("github truncated the listing of directory %q", directory),
			errs.WithHint("Narrow the configured path to a smaller directory"),
			errs.WithFields(map[string]any{fieldRepo: s.source.Repo, errs.LegacyCodeField: errs.LegacyGithubAPIError}),
		)
	}
	var entries []githubTreeEntry
	for _, entry := range tree.Tree {
		entry.Path = path.Join(directory, entry.Path)
		switch entry.Type {
		case gitObjectBlob:
			entries = append(entries, entry)
		case gitObjectTree:
			children, walkErr := p.walkTree(ctx, entry.SHA, entry.Path)
			if walkErr != nil {
				return nil, walkErr
			}
			entries = append(entries, children...)
		}
	}
	return entries, nil
}
