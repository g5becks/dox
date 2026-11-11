package lockfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/g5becks/dox/internal/errs"
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
		return errs.New(
			errs.Internal,
			"cannot save nil lock file",
			errs.WithHint("Initialize lock file state before saving"),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyLockError}),
		)
	}

	if l.Version == 0 {
		l.Version = currentVersion
	}

	if l.Sources == nil {
		l.Sources = map[string]*LockEntry{}
	}

	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return errs.Wrap(
			errs.Internal,
			fmt.Errorf("creating lock directory: %w", err),
			errs.WithFields(map[string]any{fieldPath: outputDir, errs.LegacyCodeField: errs.LegacyLockError}),
		)
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
	lockPath := filepath.Join(outputDir, fileName)

	tempFile, err := os.CreateTemp(outputDir, fileName+".*.tmp")
	if err != nil {
		return errs.Wrap(
			errs.Internal,
			fmt.Errorf("creating temporary lock file: %w", err),
			errs.WithFields(map[string]any{fieldPath: outputDir, errs.LegacyCodeField: errs.LegacyLockError}),
		)
	}

	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if _, writeErr := tempFile.Write(data); writeErr != nil {
		_ = tempFile.Close()
		return errs.Wrap(
			errs.Internal,
			fmt.Errorf("writing temporary lock file: %w", writeErr),
			errs.WithFields(map[string]any{fieldPath: tempPath, errs.LegacyCodeField: errs.LegacyLockError}),
		)
	}

	if closeErr := tempFile.Close(); closeErr != nil {
		return errs.Wrap(
			errs.Internal,
			fmt.Errorf("closing temporary lock file: %w", closeErr),
			errs.WithFields(map[string]any{fieldPath: tempPath, errs.LegacyCodeField: errs.LegacyLockError}),
		)
	}

	if renameErr := os.Rename(tempPath, lockPath); renameErr != nil {
		return errs.Wrap(
			errs.Internal,
			fmt.Errorf("replacing lock file: %w", renameErr),
			errs.WithFields(
				map[string]any{"from": tempPath, "to": lockPath, errs.LegacyCodeField: errs.LegacyLockError},
			),
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
