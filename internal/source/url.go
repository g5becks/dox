package source

import (
	"context"
	"io"
	"maps"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/lockfile"
	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/samber/oops"
	"resty.dev/v3"
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

	client := resty.New()

	return &urlSource{
		name:     name,
		source:   cfg,
		filename: filename,
		client:   client,
	}, nil
}

func (s *urlSource) Sync(
	ctx context.Context,
	destDir string,
	prevLock *lockfile.LockEntry,
	opts SyncOptions,
	_ *progress.Tracker,
) (*SyncResult, error) {
	request := s.client.R().SetContext(ctx)
	if !opts.Force && prevLock != nil {
		if prevLock.ETag != "" {
			request.SetHeader("If-None-Match", prevLock.ETag)
		}
		if prevLock.LastMod != "" {
			request.SetHeader("If-Modified-Since", prevLock.LastMod)
		}
	}

	response, err := request.Get(s.source.URL)
	if err != nil {
		return nil, oops.
			Code("DOWNLOAD_FAILED").
			With("source", s.name).
			With("url", s.source.URL).
			Wrapf(err, "downloading url source")
	}

	if response.StatusCode() == http.StatusNotModified {
		lock := cloneLockEntry(prevLock)
		if lock == nil {
			lock = &lockfile.LockEntry{
				Type: "url",
			}
		}

		lock.Type = "url"
		lock.SyncedAt = time.Now().UTC()

		return &SyncResult{
			Skipped:   true,
			LockEntry: lock,
		}, nil
	}

	if response.StatusCode() < http.StatusOK || response.StatusCode() >= http.StatusMultipleChoices {
		return nil, oops.
			Code("DOWNLOAD_FAILED").
			With("source", s.name).
			With("url", s.source.URL).
			With("status", response.StatusCode()).
			Errorf("url source returned non-success status %d", response.StatusCode())
	}

	filePath := filepath.Join(destDir, s.filename)
	content, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, oops.
			Code("DOWNLOAD_FAILED").
			With("source", s.name).
			With("url", s.source.URL).
			Wrapf(err, "reading response body")
	}

	if !opts.DryRun {
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return nil, oops.
				Code("WRITE_FAILED").
				With("source", s.name).
				With("path", destDir).
				Wrapf(err, "creating destination directory")
		}

		if err := writeFileAtomic(filePath, content); err != nil {
			return nil, err
		}
	}

	return &SyncResult{
		Downloaded: 1,
		LockEntry: &lockfile.LockEntry{
			Type:     "url",
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

func writeFileAtomic(path string, content []byte) error {
	dir := filepath.Dir(path)
	tempFile, err := os.CreateTemp(dir, ".dox-url-*.tmp")
	if err != nil {
		return oops.
			Code("WRITE_FAILED").
			With("path", path).
			Wrapf(err, "creating temporary file")
	}

	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if _, err := tempFile.Write(content); err != nil {
		_ = tempFile.Close()
		return oops.
			Code("WRITE_FAILED").
			With("path", path).
			Wrapf(err, "writing temporary file")
	}

	if err := tempFile.Close(); err != nil {
		return oops.
			Code("WRITE_FAILED").
			With("path", path).
			Wrapf(err, "closing temporary file")
	}

	if err := os.Rename(tempPath, path); err != nil {
		return oops.
			Code("WRITE_FAILED").
			With("path", path).
			Wrapf(err, "replacing destination file")
	}

	return nil
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
