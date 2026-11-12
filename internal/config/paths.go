package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/fsutil"
	"github.com/g5becks/dox/internal/runlock"
)

const RootSentinel = ".dox-root"

// Paths resolves every output/source path relative to the config directory.
type Paths struct {
	configDir string
	output    string
	relative  string
	external  bool
}

// ValidatePath accepts a local path below, and distinct from, its root.
func ValidatePath(path string) error {
	if !filepath.IsLocal(path) || filepath.Clean(path) == "." {
		return errs.New(
			errs.Config,
			fmt.Sprintf("unsafe local path %q", path),
			errs.WithHint("Use a relative path inside the configured output directory"),
		)
	}
	return nil
}

// NewPaths validates raw output configuration and resolves its output root.
func NewPaths(configDir, output string, allowExternal bool) (*Paths, error) {
	base, err := filepath.Abs(configDir)
	if err != nil {
		return nil, err
	}
	if !allowExternal {
		if operationErr := ValidatePath(output); operationErr != nil {
			return nil, operationErr
		}
	}
	resolved := output
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(base, resolved)
	}
	resolved = filepath.Clean(resolved)
	if resolved == base || filepath.Dir(resolved) == resolved {
		return nil, errs.New(
			errs.Config,
			fmt.Sprintf("unsafe output directory %q", output),
			errs.WithHint(
				"Choose a directory below the config directory; the config directory and filesystem root are always protected",
			),
		)
	}
	relative, err := filepath.Rel(base, resolved)
	if err != nil && !allowExternal {
		return nil, err
	}
	external := err != nil || !filepath.IsLocal(relative)
	if external && !allowExternal {
		return nil, errs.New(errs.Config, "output escapes the config directory")
	}
	return &Paths{configDir: base, output: resolved, relative: relative, external: external}, nil
}

// Paths returns normalized paths for a loaded or programmatically built config.
func (c *Config) Paths() (*Paths, error) {
	output := c.Output
	if filepath.IsAbs(output) {
		base, err := filepath.Abs(c.ConfigDir)
		if err != nil {
			return nil, err
		}
		relative, relErr := filepath.Rel(base, output)
		if relErr == nil && filepath.IsLocal(relative) {
			output = relative
		}
	}
	return NewPaths(c.ConfigDir, output, c.AllowExternalOutput)
}

// Output returns the absolute output directory for display and read-only APIs.
func (p *Paths) Output() string { return p.output }

// SourceDir returns a validated source directory relative to the output root.
func (p *Paths) SourceDir(name string, source Source) (string, error) {
	if err := validateSourcePaths(name, source); err != nil {
		return "", err
	}
	directory := source.Out
	if directory == "" {
		directory = name
	}
	if !filepath.IsLocal(directory) {
		return "", errs.New(errs.Config, fmt.Sprintf("unsafe source output %q", directory))
	}
	return filepath.Clean(directory), nil
}

// Source returns the resolved directory for a validated source.
func (p *Paths) Source(name string, source Source) (string, error) {
	relative, err := p.SourceDir(name, source)
	if err != nil {
		return "", err
	}
	return filepath.Join(p.output, relative), nil
}

// OpenOutput anchors local paths to the config root before creating or opening.
func (p *Paths) OpenOutput(create bool) (*os.Root, error) {
	if p.external {
		var root *os.Root
		var err error
		if create {
			root, err = fsutil.OpenDir(p.output)
		} else {
			root, err = os.OpenRoot(p.output)
		}
		if err != nil {
			return nil, err
		}
		return p.protectConfig(root)
	}
	base, err := os.OpenRoot(p.configDir)
	if err != nil {
		return nil, err
	}
	defer base.Close()
	if create {
		if mkdirAllErr := base.MkdirAll(p.relative, fsutil.DirMode); mkdirAllErr != nil {
			return nil, mkdirAllErr
		}
	}
	root, err := base.OpenRoot(p.relative)
	if err != nil {
		return nil, err
	}
	return p.protectConfig(root)
}

// MarkOutput records ownership before the first non-dry sync writes documents.
func MarkOutput(root *os.Root) error {
	return fsutil.AtomicWrite(root, RootSentinel, []byte("dox output\n"))
}

// RequireOwned refuses cleanup of directories without a regular ownership file.
func RequireOwned(root *os.Root) error {
	for _, name := range []string{RootSentinel, ".dox.lock"} {
		info, err := root.Lstat(name)
		if err == nil && info.Mode().IsRegular() {
			return nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return errs.New(
		errs.Config,
		"refusing to clean an unowned output directory",
		errs.WithHint("Run 'dox sync' to create .dox-root, or choose an existing dox output with .dox.lock"),
	)
}

// CleanOutput removes contents through the root while retaining its sentinel.
func CleanOutput(root *os.Root) error {
	if err := RequireOwned(root); err != nil {
		return err
	}
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	entries, err := directory.ReadDir(-1)
	closeErr := directory.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	for _, entry := range entries {
		// The ownership marker stays, and so does the run lock the caller holds.
		if entry.Name() == RootSentinel || entry.Name() == runlock.FileName {
			continue
		}
		if removeAllErr := root.RemoveAll(entry.Name()); removeAllErr != nil {
			return removeAllErr
		}
	}
	return MarkOutput(root)
}

// protectConfig catches aliases which resolve to the config directory or an ancestor.
func (p *Paths) protectConfig(root *os.Root) (*os.Root, error) {
	outputInfo, err := root.Stat(".")
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(p.configDir)
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	for current := canonical; ; current = filepath.Dir(current) {
		info, statErr := os.Stat(current)
		if statErr != nil {
			_ = root.Close()
			return nil, statErr
		}
		if os.SameFile(outputInfo, info) {
			_ = root.Close()
			return nil, errs.New(
				errs.Config,
				"output resolves to the config directory or its ancestor",
				errs.WithHint("Choose a separate output directory; protected directories cannot be opted into"),
			)
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return root, nil
}

func validateSourcePaths(name string, source Source) error {
	if !filepath.IsLocal(name) {
		return errs.New(errs.Config, fmt.Sprintf("unsafe source name %q", name))
	}
	for _, path := range []string{source.Out, source.Filename} {
		if path != "" && !filepath.IsLocal(path) {
			return errs.New(errs.Config, fmt.Sprintf("unsafe source path %q", path))
		}
	}
	return nil
}
