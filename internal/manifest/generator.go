package manifest

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/lockfile"
	"github.com/g5becks/dox/internal/parser"
)

const (
	maxParseSize    = 50 * 1024 * 1024 // 50MB
	unknownFileType = "unknown"
)

// Generate creates a manifest by walking the output directory and parsing files.
func Generate(ctx context.Context, cfg *config.Config, lock *lockfile.LockFile) error {
	paths, err := cfg.Paths()
	if err != nil {
		return err
	}
	root, err := paths.OpenOutput(true)
	if err != nil {
		return err
	}
	defer root.Close()
	return GenerateRoot(ctx, cfg, lock, root)
}

// GenerateRoot reads and writes a manifest inside an already anchored output.
func GenerateRoot(_ context.Context, cfg *config.Config, lock *lockfile.LockFile, root *os.Root) error {
	paths, err := cfg.Paths()
	if err != nil {
		return err
	}
	m := New()

	parsers := []parser.Parser{
		parser.NewMarkdownParser(),
		parser.NewMDXParser(),
		parser.NewTextParser(),
		parser.NewTypeScriptParser(),
	}

	for sourceName, sourceCfg := range cfg.Sources {
		sourceDir, pathErr := paths.SourceDir(sourceName, sourceCfg)
		if pathErr != nil {
			return pathErr
		}

		if _, statErr := root.Stat(sourceDir); os.IsNotExist(statErr) {
			continue
		}

		collection := &Collection{
			Name:     sourceName,
			Dir:      filepath.ToSlash(sourceDir),
			Type:     sourceCfg.Type,
			Source:   resolveSourceLocation(sourceCfg),
			Path:     sourceCfg.Path,
			Ref:      sourceCfg.Ref,
			LastSync: resolveLastSync(lock, sourceName),
		}

		var skipped int

		walkDirErr := fs.WalkDir(
			root.FS(),
			filepath.ToSlash(sourceDir),
			func(path string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil || d.IsDir() {
					return walkErr
				}

				if d.Name() == ManifestFile || d.Name() == ".dox.lock" {
					return nil
				}

				relPath, _ := filepath.Rel(sourceDir, path)
				fileInfo, parseErr := parseFile(root, path, relPath, parsers)
				if parseErr != nil {
					skipped++
					return nil //nolint:nilerr // intentionally skip unparseable files (binary, etc.)
				}

				collection.Files = append(collection.Files, *fileInfo)
				collection.TotalSize += fileInfo.Size
				return nil
			},
		)

		if walkDirErr != nil {
			return errs.Wrap(
				errs.Internal,
				fmt.Errorf("walking source directory: %w", walkDirErr),
				errs.WithFields(
					map[string]any{"source": sourceName, errs.LegacyCodeField: errs.LegacyManifestGenerationError},
				),
			)
		}

		collection.FileCount = len(collection.Files)
		collection.Skipped = skipped
		m.Collections[sourceName] = collection
	}

	return m.SaveRoot(root)
}

func parseFile(root *os.Root, absPath string, relPath string, parsers []parser.Parser) (*FileInfo, error) {
	stat, err := root.Stat(absPath)
	if err != nil {
		return nil, err
	}

	fileInfo := &FileInfo{
		Path:     relPath,
		Size:     stat.Size(),
		Modified: stat.ModTime(),
	}

	if stat.Size() > maxParseSize {
		fileInfo.Warning = "file_too_large"
		fileInfo.Type = unknownFileType
		return fileInfo, nil
	}

	content, err := root.ReadFile(absPath)
	if err != nil {
		return nil, err
	}

	if parser.IsBinary(content) {
		return nil, errs.New(errs.Internal, "binary file")
	}

	var matchedParser parser.Parser
	for _, p := range parsers {
		if p.CanParse(relPath) {
			matchedParser = p
			break
		}
	}

	if matchedParser == nil {
		fileInfo.Type = unknownFileType
		fileInfo.Lines = countLines(content)
		return fileInfo, nil
	}

	result, err := matchedParser.Parse(relPath, content)
	if err != nil {
		return nil, err
	}

	fileInfo.Type = parser.DetectFileType(relPath)
	fileInfo.Lines = result.Lines
	fileInfo.Description = result.Description
	fileInfo.ComponentType = result.ComponentType
	fileInfo.Outline = result.Outline

	return fileInfo, nil
}

func resolveSourceLocation(src config.Source) string {
	if src.Repo != "" {
		return src.Repo
	}
	if src.URL != "" {
		return src.URL
	}
	return unknownFileType
}

func countLines(content []byte) int {
	count := 0
	for _, b := range content {
		if b == '\n' {
			count++
		}
	}
	return count + 1
}

func resolveLastSync(lock *lockfile.LockFile, sourceName string) time.Time {
	if lock != nil {
		if entry := lock.GetEntry(sourceName); entry != nil {
			return entry.SyncedAt
		}
	}
	return time.Now()
}
