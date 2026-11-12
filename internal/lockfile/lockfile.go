package lockfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/fsutil"
)

const (
	fileName = ".dox.lock"
	// currentVersion 2 adds LockEntry.Commit; version 1 locks load unchanged.
	currentVersion = 2
)

type LockFile struct {
	Version int                   `json:"version"`
	Sources map[string]*LockEntry `json:"sources"`
}

type LockEntry struct {
	Type        string            `json:"type"`
	TreeSHA     string            `json:"tree_sha,omitempty"`
	RefResolved string            `json:"ref_resolved,omitempty"`
	Commit      string            `json:"commit,omitempty"`
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

		return nil, errs.Wrap(
			errs.Internal,
			fmt.Errorf("reading lock file: %w", err),
			errs.WithFields(map[string]any{fieldPath: lockPath, errs.LegacyCodeField: errs.LegacyLockError}),
		)
	}

	lock := &LockFile{}
	if unmarshalErr := json.Unmarshal(data, lock); unmarshalErr != nil {
		return nil, errs.Wrap(
			errs.Internal,
			fmt.Errorf("parsing lock file: %w", unmarshalErr),
			errs.WithHint("Delete the lock file and run 'dox sync' to regenerate it"),
			errs.WithFields(map[string]any{fieldPath: lockPath, errs.LegacyCodeField: errs.LegacyLockError}),
		)
	}

	// Older versions are subsets of the current schema; upgrade them in place.
	if lock.Version < currentVersion {
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
		return l.SaveRoot(nil)
	}
	root, err := fsutil.OpenDir(outputDir)
	if err != nil {
		return errs.Wrap(
			errs.Internal,
			fmt.Errorf("creating lock file directory: %w", err),
			errs.WithFields(map[string]any{fieldPath: outputDir, errs.LegacyCodeField: errs.LegacyLockError}),
		)
	}
	defer root.Close()
	return l.SaveRoot(root)
}

// SaveRoot writes state through an already anchored output directory.
func (l *LockFile) SaveRoot(root *os.Root) error {
	if l == nil {
		return errs.New(
			errs.Internal,
			"cannot save nil lock file",
			errs.WithHint("Initialize lock file before saving"),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyLockError}),
		)
	}
	if l.Version == 0 {
		l.Version = currentVersion
	}
	if l.Sources == nil {
		l.Sources = map[string]*LockEntry{}
	}

	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return errs.Wrap(
			errs.Internal,
			fmt.Errorf("encoding lock file: %w", err),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyLockError}),
		)
	}
	data = append(data, '\n')
	if atomicWriteErr := fsutil.AtomicWrite(root, fileName, data); atomicWriteErr != nil {
		return errs.Wrap(
			errs.Internal,
			fmt.Errorf("writing lock file: %w", atomicWriteErr),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyLockError}),
		)
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
