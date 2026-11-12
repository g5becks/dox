package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	neturl "net/url"
	"path"
	"time"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/fsutil"
	"github.com/g5becks/dox/internal/httpx"
	"github.com/g5becks/dox/internal/lockfile"
)

type urlSource struct {
	name       string
	source     config.Source
	filename   string
	http       *http.Client
	ownsClient bool
	timeout    time.Duration
}

func NewURL(name string, cfg config.Source) (Source, error) {
	return newURLSourceWithHTTP(name, cfg, nil)
}

func newURLSourceWithHTTP(name string, cfg config.Source, httpClient *http.Client) (Source, error) {
	filename := cfg.Filename
	if filename == "" {
		filename = filenameFromURL(name, cfg.URL)
	}

	if err := config.ValidatePath(filename); err != nil {
		return nil, err
	}
	ownsClient := httpClient == nil
	if ownsClient {
		httpClient = httpx.NewClient(httpx.DefaultTimeout)
	}

	return &urlSource{
		name:       name,
		source:     cfg,
		filename:   filename,
		http:       httpClient,
		ownsClient: ownsClient,
		timeout:    httpx.NormalizeTimeout(httpClient.Timeout),
	}, nil
}

// Close releases idle connections of a client this source created; a shared
// client belongs to the caller.
func (s *urlSource) Close() error {
	if s.ownsClient {
		s.http.CloseIdleConnections()
	}
	return nil
}

func (s *urlSource) Sync(
	ctx context.Context,
	destDir string,
	prevLock *lockfile.LockEntry,
	opts SyncOptions,
) (*SyncResult, error) {
	sourceCtx, cancel := httpx.SourceContext(ctx, s.timeout)
	defer cancel()

	if opts.Check && !hasValidators(prevLock) && prevLock != nil && prevLock.Files[s.filename] != "" {
		return s.checkDigest(sourceCtx, prevLock, opts)
	}
	method := http.MethodGet
	if opts.DryRun {
		// A dry run only needs to know whether the document changed.
		method = http.MethodHead
	}
	response, err := s.send(sourceCtx, method, prevLock, opts)
	if err == nil && opts.DryRun &&
		(response.StatusCode == http.StatusMethodNotAllowed || response.StatusCode == http.StatusNotImplemented) {
		// HEAD unsupported: ask for at most one byte and never read it.
		_ = response.Body.Close()
		response, err = s.send(sourceCtx, http.MethodGet, prevLock, opts, "Range", "bytes=0-0")
	}
	if err == nil {
		defer response.Body.Close()
		return s.handleResponse(sourceCtx, response, destDir, prevLock, opts)
	}
	return nil, errs.Wrap(
		errs.Network,
		fmt.Errorf("downloading url source: %w", err),
		errs.WithFields(
			map[string]any{
				fieldSource:          s.name,
				sourceTypeURL:        s.source.URL,
				errs.LegacyCodeField: errs.LegacyDownloadFailed,
			},
		),
	)
}

func (s *urlSource) handleResponse(
	ctx context.Context,
	response *http.Response,
	destDir string,
	prevLock *lockfile.LockEntry,
	opts SyncOptions,
) (*SyncResult, error) {
	if response.StatusCode == http.StatusNotModified {
		lock := cloneLockEntry(prevLock)
		if lock == nil {
			lock = &lockfile.LockEntry{
				Type: sourceTypeURL,
			}
		}

		lock.Type = sourceTypeURL
		lock.SyncedAt = time.Now().UTC()

		return &SyncResult{
			Skipped:   true,
			LockEntry: lock,
		}, nil
	}

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, errs.New(
			errs.Network,
			fmt.Sprintf("url source returned non-success status %d", response.StatusCode),
			errs.WithFields(
				map[string]any{
					fieldSource:          s.name,
					sourceTypeURL:        s.source.URL,
					fieldStatus:          response.StatusCode,
					errs.LegacyCodeField: errs.LegacyDownloadFailed,
				},
			),
			errs.WithCause(&errs.HTTPStatusError{Code: response.StatusCode}),
		)
	}

	digest := ""
	if !opts.DryRun {
		root, rootErr := destinationRoot(destDir, opts)
		if rootErr != nil {
			return nil, rootErr
		}
		defer root.Close()
		// Stream to a temporary file and rename, never holding the body in memory.
		hasher := sha256.New()
		body := io.TeeReader(response.Body, hasher)
		verify := func() error {
			digest = "sha256:" + hex.EncodeToString(hasher.Sum(nil))
			return s.verifyLocked(prevLock, opts, digest)
		}
		if writeErr := fsutil.AtomicWriteReaderVerified(ctx, root, s.filename, body, verify); writeErr != nil {
			if detail, ok := errors.AsType[*errs.Error](writeErr); ok {
				return nil, detail
			}
			return nil, errs.Wrap(
				errs.Network,
				fmt.Errorf("downloading url source: %w", writeErr),
				errs.WithFields(
					map[string]any{
						fieldSource:          s.name,
						sourceTypeURL:        s.source.URL,
						errs.LegacyCodeField: errs.LegacyDownloadFailed,
					},
				),
			)
		}
	}

	return &SyncResult{
		Downloaded: 1,
		LockEntry: &lockfile.LockEntry{
			Type:     sourceTypeURL,
			ETag:     response.Header.Get("ETag"),
			LastMod:  response.Header.Get("Last-Modified"),
			SyncedAt: time.Now().UTC(),
			Files:    urlFiles(s.filename, digest),
		},
	}, nil
}

func filenameFromURL(sourceName string, rawURL string) string {
	parsed, err := neturl.Parse(rawURL)
	if err == nil {
		baseName := path.Base(parsed.Path)
		if baseName != "" && baseName != "." && baseName != "/" {
			return baseName
		}
	}

	return sourceName + ".txt"
}

func cloneLockEntry(entry *lockfile.LockEntry) *lockfile.LockEntry {
	if entry == nil {
		return nil
	}

	cloned := *entry
	if entry.Files != nil {
		cloned.Files = make(map[string]string, len(entry.Files))
		maps.Copy(cloned.Files, entry.Files)
	}

	return &cloned
}

// send issues one request with the conditional headers from the previous
// sync; extra is optional header name/value pairs.
func (s *urlSource) send(
	ctx context.Context,
	method string,
	prevLock *lockfile.LockEntry,
	opts SyncOptions,
	extra ...string,
) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, s.source.URL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", userAgent)
	if !opts.Force && prevLock != nil {
		if prevLock.ETag != "" {
			request.Header.Set("If-None-Match", prevLock.ETag)
		}
		if prevLock.LastMod != "" {
			request.Header.Set("If-Modified-Since", prevLock.LastMod)
		}
	}
	for index := 0; index+1 < len(extra); index += 2 {
		request.Header.Set(extra[index], extra[index+1])
	}
	return s.http.Do(request)
}

// urlFiles records the downloaded file's content digest (none for dry runs).
func urlFiles(filename, digest string) map[string]string {
	if digest == "" {
		return nil
	}
	return map[string]string{filename: digest}
}

// verifyLocked requires, under --locked, that downloaded content matches the
// digest recorded in the lock; URLs cannot be pinned, only verified.
func (s *urlSource) verifyLocked(prevLock *lockfile.LockEntry, opts SyncOptions, digest string) error {
	if !opts.Locked {
		return nil
	}
	fields := map[string]any{
		fieldSource:          s.name,
		sourceTypeURL:        s.source.URL,
		errs.LegacyCodeField: errs.LegacyLockError,
	}
	expected := ""
	if prevLock != nil {
		expected = prevLock.Files[s.filename]
	}
	if expected == "" {
		return errs.New(
			errs.Config,
			fmt.Sprintf("source %q has no locked content digest", s.name),
			errs.WithHint("Run 'dox sync' once to record content digests, then use --locked"),
			errs.WithFields(fields),
		)
	}
	if expected != digest {
		fields["expected"], fields["actual"] = expected, digest
		return errs.New(
			errs.Network,
			fmt.Sprintf("url source %q changed since the lock was written", s.name),
			errs.WithHint("Run 'dox sync' without --locked to accept the new content"),
			errs.WithFields(fields),
		)
	}
	return nil
}

func hasValidators(prevLock *lockfile.LockEntry) bool {
	return prevLock != nil && (prevLock.ETag != "" || prevLock.LastMod != "")
}

// checkDigest downloads the document without writing it and compares its
// digest with the lock: the only staleness signal a server without
// ETag/Last-Modified offers.
func (s *urlSource) checkDigest(
	ctx context.Context,
	prevLock *lockfile.LockEntry,
	opts SyncOptions,
) (*SyncResult, error) {
	fields := map[string]any{
		fieldSource:          s.name,
		sourceTypeURL:        s.source.URL,
		errs.LegacyCodeField: errs.LegacyDownloadFailed,
	}
	response, err := s.send(ctx, http.MethodGet, prevLock, opts)
	if err != nil {
		return nil, errs.Wrap(errs.Network, fmt.Errorf("checking url source: %w", err), errs.WithFields(fields))
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		fields[fieldStatus] = response.StatusCode
		return nil, errs.New(
			errs.Network,
			fmt.Sprintf("url source returned non-success status %d", response.StatusCode),
			errs.WithFields(fields),
			errs.WithCause(&errs.HTTPStatusError{Code: response.StatusCode}),
		)
	}
	hasher := sha256.New()
	if _, copyErr := io.Copy(hasher, response.Body); copyErr != nil {
		return nil, errs.Wrap(errs.Network, fmt.Errorf("checking url source: %w", copyErr), errs.WithFields(fields))
	}
	lock := cloneLockEntry(prevLock)
	if "sha256:"+hex.EncodeToString(hasher.Sum(nil)) == prevLock.Files[s.filename] {
		return &SyncResult{Skipped: true, LockEntry: lock}, nil
	}
	return &SyncResult{Downloaded: 1, LockEntry: lock}, nil
}
