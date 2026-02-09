package manifest

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/lockfile"
	"github.com/g5becks/dox/internal/parser"
	"github.com/samber/oops"
)

const (
	maxParseSize = 50 * 1024 * 1024 // 50MB
)

// Generate creates a manifest by walking the output directory and parsing files.
func Generate(_ context.Context, cfg *config.Config, lock *lockfile.LockFile) error {
	outputDir := cfg.Output
	m := New()

	parsers := []parser.Parser{
		parser.NewMarkdownParser(),
		parser.NewMDXParser(),
		parser.NewTextParser(),
		parser.NewTypeScriptParser(),
	}

	for sourceName, sourceCfg := range cfg.Sources {
		sourceDir := resolveSourceDir(outputDir, sourceName, sourceCfg)

		if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
			continue
		}

		dirName := sourceName
		if sourceCfg.Out != "" {
			dirName = sourceCfg.Out
		}

		collection := &Collection{
			Name:     sourceName,
			Dir:      dirName,
			Type:     sourceCfg.Type,
			Source:   resolveSourceLocation(sourceCfg),
			Path:     sourceCfg.Path,
			Ref:      sourceCfg.Ref,
			LastSync: resolveLastSync(lock, sourceName),
		}

		var skipped int

		err := filepath.WalkDir(sourceDir, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil || d.IsDir() {
				return walkErr
			}

			if d.Name() == ManifestFile || d.Name() == ".dox.lock" {
				return nil
			}

			relPath, _ := filepath.Rel(sourceDir, path)
			fileInfo, parseErr := parseFile(path, relPath, parsers)
			if parseErr != nil {
				skipped++
				return nil //nolint:nilerr // intentionally skip unparseable files (binary, etc.)
			}

			collection.Files = append(collection.Files, *fileInfo)
			collection.TotalSize += fileInfo.Size
			return nil
		})

		if err != nil {
			return oops.
				Code("MANIFEST_GENERATION_ERROR").
				With("source", sourceName).
				Wrapf(err, "walking source directory")
		}

		collection.FileCount = len(collection.Files)
		collection.Skipped = skipped
		m.Collections[sourceName] = collection
	}

	return m.Save(outputDir)
}

func parseFile(absPath string, relPath string, parsers []parser.Parser) (*FileInfo, error) {
	stat, err := os.Stat(absPath)
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
		fileInfo.Type = "unknown"
		return fileInfo, nil
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}

	if parser.IsBinary(content) {
		return nil, oops.Errorf("binary file")
	}

	var matchedParser parser.Parser
	for _, p := range parsers {
		if p.CanParse(relPath) {
			matchedParser = p
			break
		}
	}

	if matchedParser == nil {
		fileInfo.Type = "unknown"
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

func resolveSourceDir(outputDir string, name string, src config.Source) string {
	if src.Out != "" {
		return filepath.Join(outputDir, src.Out)
	}
	return filepath.Join(outputDir, name)
}

func resolveSourceLocation(src config.Source) string {
	if src.Repo != "" {
		return src.Repo
	}
	return src.URL
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
