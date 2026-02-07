package lockfile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/samber/oops"
)

const (
	fileName       = ".dox.lock"
	currentVersion = 1
)

type LockFile struct {
	Version int                   `json:"version"`
	Sources map[string]*LockEntry `json:"sources"`
}

type LockEntry struct {
	Type        string            `json:"type"`
	TreeSHA     string            `json:"tree_sha,omitempty"`
	RefResolved string            `json:"ref_resolved,omitempty"`
	ETag        string            `json:"etag,omitempty"`
	LastMod     string            `json:"last_modified,omitempty"`
	SyncedAt    time.Time         `json:"synced_at"`
	Files       map[string]string `json:"files,omitempty"`
}

func Load(outputDir string) (*LockFile, error) {
	lockPath := filepath.Join(outputDir, fileName)
	data, err := os.ReadFile(lockPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return New(), nil
		}

		return nil, oops.
			Code("LOCK_ERROR").
			With("path", lockPath).
			Wrapf(err, "reading lock file")
	}

	lock := &LockFile{}
	if unmarshalErr := json.Unmarshal(data, lock); unmarshalErr != nil {
		return nil, oops.
			Code("LOCK_ERROR").
			With("path", lockPath).
			Hint("Delete the lock file and run 'dox sync' to regenerate it").
			Wrapf(unmarshalErr, "parsing lock file")
	}

	if lock.Version == 0 {
		lock.Version = currentVersion
	}

	if lock.Sources == nil {
		lock.Sources = map[string]*LockEntry{}
	}

	return lock, nil
}

func New() *LockFile {
	return &LockFile{
		Version: currentVersion,
		Sources: map[string]*LockEntry{},
	}
}

func (l *LockFile) Save(outputDir string) error {
	if l == nil {
		return oops.
			Code("LOCK_ERROR").
			Hint("Initialize lock file state before saving").
			Errorf("cannot save nil lock file")
	}

	if l.Version == 0 {
		l.Version = currentVersion
	}

	if l.Sources == nil {
		l.Sources = map[string]*LockEntry{}
	}

	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return oops.
			Code("LOCK_ERROR").
			With("path", outputDir).
			Wrapf(err, "creating lock directory")
	}

	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return oops.
			Code("LOCK_ERROR").
			Wrapf(err, "encoding lock file")
	}

	data = append(data, '\n')
	lockPath := filepath.Join(outputDir, fileName)

	tempFile, err := os.CreateTemp(outputDir, fileName+".*.tmp")
	if err != nil {
		return oops.
			Code("LOCK_ERROR").
			With("path", outputDir).
			Wrapf(err, "creating temporary lock file")
	}

	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if _, writeErr := tempFile.Write(data); writeErr != nil {
		_ = tempFile.Close()
		return oops.
			Code("LOCK_ERROR").
			With("path", tempPath).
			Wrapf(writeErr, "writing temporary lock file")
	}

	if closeErr := tempFile.Close(); closeErr != nil {
		return oops.
			Code("LOCK_ERROR").
			With("path", tempPath).
			Wrapf(closeErr, "closing temporary lock file")
	}

	if renameErr := os.Rename(tempPath, lockPath); renameErr != nil {
		return oops.
			Code("LOCK_ERROR").
			With("from", tempPath).
			With("to", lockPath).
			Wrapf(renameErr, "replacing lock file")
	}

	return nil
}

func (l *LockFile) GetEntry(name string) *LockEntry {
	if l == nil {
		return nil
	}

	return l.Sources[name]
}

func (l *LockFile) SetEntry(name string, entry *LockEntry) {
	if l == nil {
		return
	}

	if l.Sources == nil {
		l.Sources = map[string]*LockEntry{}
	}

	l.Sources[name] = entry
}

func (l *LockFile) RemoveEntry(name string) {
	if l == nil || l.Sources == nil {
		return
	}

	delete(l.Sources, name)
}
