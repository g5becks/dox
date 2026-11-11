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

	"resty.dev/v3"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/fsutil"
	"github.com/g5becks/dox/internal/lockfile"
)

type urlSource struct {
	name     string
	source   config.Source
	filename string
	client   *resty.Client
}

func NewURL(name string, cfg config.Source) (Source, error) {
	filename := cfg.Filename
	if filename == "" {
		filename = filenameFromURL(name, cfg.URL)
	}

	if err := config.ValidatePath(filename); err != nil {
		return nil, err
	}
	client := resty.New()

	return &urlSource{
		name:     name,
		source:   cfg,
		filename: filename,
		client:   client,
	}, nil
}

func (s *urlSource) Close() error {
	return s.client.Close()
}

func (s *urlSource) Sync(
	ctx context.Context,
	destDir string,
	prevLock *lockfile.LockEntry,
	opts SyncOptions,
) (*SyncResult, error) {
	request := s.syncRequest(ctx, prevLock, opts)

	response, err := request.Get(s.source.URL)
	if err != nil {
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

	if response.StatusCode() == http.StatusNotModified {
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

	if response.StatusCode() < http.StatusOK || response.StatusCode() >= http.StatusMultipleChoices {
		return nil, errs.New(
			errs.Network,
			fmt.Sprintf("url source returned non-success status %d", response.StatusCode()),
			errs.WithFields(
				map[string]any{
					fieldSource:          s.name,
					sourceTypeURL:        s.source.URL,
					fieldStatus:          response.StatusCode(),
					errs.LegacyCodeField: errs.LegacyDownloadFailed,
				},
			),
			errs.WithCause(&errs.HTTPStatusError{Code: response.StatusCode()}),
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
			ETag:     response.Header().Get("ETag"),
			LastMod:  response.Header().Get("Last-Modified"),
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

func (s *urlSource) syncRequest(ctx context.Context, prevLock *lockfile.LockEntry, opts SyncOptions) *resty.Request {
	request := s.client.R().SetContext(ctx)
	if !opts.Force && prevLock != nil {
		if prevLock.ETag != "" {
			request.SetHeader("If-None-Match", prevLock.ETag)
		}
		if prevLock.LastMod != "" {
			request.SetHeader("If-Modified-Since", prevLock.LastMod)
		}
	}

	return request
}
