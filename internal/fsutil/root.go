// Package fsutil provides confined output-directory and atomic-file operations.
package fsutil

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DirMode is the permission policy for documentation output directories.
const DirMode os.FileMode = 0o750
const privateFileMode os.FileMode = 0o600

// OpenDir opens or creates a directory beneath its nearest existing ancestor.
// Callers accepting configuration paths must validate and anchor them first.
func OpenDir(path string) (*os.Root, error) {
	current := filepath.Clean(path)
	relative := "."
	for {
		root, err := os.OpenRoot(current)
		if err == nil {
			defer root.Close()
			if mkdirErr := root.MkdirAll(relative, DirMode); mkdirErr != nil {
				return nil, mkdirErr
			}
			return root.OpenRoot(relative)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil, err
		}
		relative = filepath.Join(filepath.Base(current), relative)
		current = parent
	}
}

// AtomicWrite replaces a file through an opened root, including temp cleanup.
func AtomicWrite(root *os.Root, path string, data []byte) error {
	return AtomicWriteContext(context.Background(), root, path, data)
}

// AtomicWriteContext preserves the previous file when cancellation precedes publication.
func AtomicWriteContext(ctx context.Context, root *os.Root, path string, data []byte) error {
	if cancelErr := ctx.Err(); cancelErr != nil {
		return cancelErr
	}
	if !filepath.IsLocal(path) || filepath.Clean(path) == "." {
		return fmt.Errorf("invalid output filename %q", path)
	}
	directory := filepath.Dir(path)
	if err := root.MkdirAll(directory, DirMode); err != nil {
		return err
	}
	temporary := filepath.Join(directory, ".dox-"+rand.Text()+".tmp")
	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, privateFileMode)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(temporary) }()
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if cancelErr := ctx.Err(); cancelErr != nil {
		return cancelErr
	}
	return root.Rename(temporary, path)
}
