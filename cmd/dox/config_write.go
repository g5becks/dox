package main

import (
	"os"
	"path/filepath"
)

// writeConfig replaces the selected config without truncating an existing inode.
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
	if closeErr := file.Close(); closeErr != nil {
		return closeErr
	}
	// Both names are basenames under the explicitly selected config directory.
	return root.Rename(tempName, filepath.Base(path))
}
