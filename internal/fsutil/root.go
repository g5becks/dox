// Package fsutil provides confined output-directory and atomic-file operations.
package fsutil

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
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
	return AtomicWriteReader(ctx, root, path, bytes.NewReader(data))
}

const copyBufferSize = 32 << 10

// copyBuffers reuses copy buffers across the many small files of a sync.
//
//nolint:gochecknoglobals // A process-wide pool is the point of sync.Pool.
var copyBuffers = sync.Pool{New: func() any {
	buffer := make([]byte, copyBufferSize)
	return &buffer
}}

// writerOnly hides *os.File's ReaderFrom, whose generic fallback would
// allocate a fresh buffer instead of using the pooled one.
type writerOnly struct{ io.Writer }

// AtomicWriteReader streams r into a temporary file beside path and renames it
// into place, so readers never observe a partial file and a failed or canceled
// copy leaves the previous file untouched.
func AtomicWriteReader(ctx context.Context, root *os.Root, path string, r io.Reader) error {
	return AtomicWriteReaderVerified(ctx, root, path, r, nil)
}

// AtomicWriteReaderVerified is AtomicWriteReader with a check that runs after
// the content is fully written and before it replaces path; a failing verify
// leaves the previous file untouched.
func AtomicWriteReaderVerified(
	ctx context.Context,
	root *os.Root,
	path string,
	r io.Reader,
	verify func() error,
) error {
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
	buffer, pooled := copyBuffers.Get().(*[]byte)
	if !pooled {
		fresh := make([]byte, copyBufferSize)
		buffer = &fresh
	}
	_, err = io.CopyBuffer(writerOnly{file}, r, *buffer)
	copyBuffers.Put(buffer)
	if err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if cancelErr := ctx.Err(); cancelErr != nil {
		return cancelErr
	}
	if verify != nil {
		if verifyErr := verify(); verifyErr != nil {
			return verifyErr
		}
	}
	return root.Rename(temporary, path)
}
