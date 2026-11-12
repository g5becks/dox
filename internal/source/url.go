package source

import (
	"context"
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

	request, err := s.syncRequest(sourceCtx, prevLock, opts)
	if err == nil {
		var response *http.Response
		response, err = s.http.Do(request)
		if err == nil {
			defer response.Body.Close()
			return s.handleResponse(response, destDir, prevLock, opts)
		}
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

	content, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, errs.Wrap(
			errs.Network,
			fmt.Errorf("reading response body: %w", err),
			errs.WithFields(
				map[string]any{
					fieldSource:          s.name,
					sourceTypeURL:        s.source.URL,
					errs.LegacyCodeField: errs.LegacyDownloadFailed,
				},
			),
		)
	}

	if !opts.DryRun {
		root, rootErr := destinationRoot(destDir, opts)
		if rootErr != nil {
			return nil, rootErr
		}
		defer root.Close()
		if writeErr := fsutil.AtomicWrite(root, s.filename, content); writeErr != nil {
			return nil, fmt.Errorf("writing URL source: %w", writeErr)
		}
	}

	return &SyncResult{
		Downloaded: 1,
		LockEntry: &lockfile.LockEntry{
			Type:     sourceTypeURL,
			ETag:     response.Header.Get("ETag"),
			LastMod:  response.Header.Get("Last-Modified"),
			SyncedAt: time.Now().UTC(),
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

func (s *urlSource) syncRequest(
	ctx context.Context,
	prevLock *lockfile.LockEntry,
	opts SyncOptions,
) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.source.URL, nil)
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

	return request, nil
}
