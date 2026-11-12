package main

import (
	"os"
	"path/filepath"
)

// writeConfig atomically replaces the selected config (temp file + rename)
// without truncating an existing inode, preserving its permission bits.
func writeConfig(path, content string) error {
	dir := filepath.Dir(path)
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()

	// CreateTemp uses exclusive creation and mode 0600, including for replacements.
	file, err := os.CreateTemp(dir, ".dox-config-*.tmp")
	if err != nil {
		return err
	}
	tempName := filepath.Base(file.Name())
	defer func() { _ = root.Remove(tempName) }()
	if _, writeErr := file.WriteString(content); writeErr != nil {
		_ = file.Close()
		return writeErr
	}
	if syncErr := file.Sync(); syncErr != nil {
		_ = file.Close()
		return syncErr
	}
	// Keep the existing file's permission bits; a new file keeps CreateTemp's 0600.
	if info, statErr := root.Stat(filepath.Base(path)); statErr == nil {
		if chmodErr := file.Chmod(info.Mode().Perm()); chmodErr != nil {
			_ = file.Close()
			return chmodErr
		}
	}
	if closeErr := file.Close(); closeErr != nil {
		return closeErr
	}
	// Both names are basenames under the explicitly selected config directory.
	return root.Rename(tempName, filepath.Base(path))
}
