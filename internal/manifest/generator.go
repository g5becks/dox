package manifest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/g5becks/dox/internal/config"
	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/lockfile"
	"github.com/g5becks/dox/internal/parser"
)

const (
	maxParseSize    = 50 * 1024 * 1024
	unknownFileType = "unknown"
)

type collectionWork struct {
	collection *Collection
	previous   map[string]*FileInfo
	sha        map[string]string
}
type fileWork struct {
	collection     int
	path, relative string
	entry          fs.DirEntry
	previous       *FileInfo
	sha            string
}

// Generate creates a versioned, deterministic manifest for the current files.
func Generate(ctx context.Context, cfg *config.Config, lock *lockfile.LockFile) error {
	if cancelErr := ctx.Err(); cancelErr != nil {
		return cancelErr
	}
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

// GenerateRoot reuses unchanged entries and publishes only a changed snapshot.
func GenerateRoot(ctx context.Context, cfg *config.Config, lock *lockfile.LockFile, root *os.Root) error {
	if cancelErr := ctx.Err(); cancelErr != nil {
		return cancelErr
	}
	previous := cachedManifest(root)
	collections, files, err := collectWork(ctx, cfg, lock, root, previous)
	if err != nil {
		return err
	}
	results, err := parseWork(ctx, root, files)
	if err != nil {
		return err
	}
	current := assembleManifest(collections, files, results)
	if cancelErr := ctx.Err(); cancelErr != nil {
		return cancelErr
	}
	if reflect.DeepEqual(previous, current) {
		return nil
	}
	return current.SaveRootContext(ctx, root)
}

func cachedManifest(root *os.Root) *Manifest {
	data, err := root.ReadFile(ManifestFile)
	if err != nil {
		return nil
	}
	var previous Manifest
	if err = json.Unmarshal(data, &previous); err != nil || previous.ParserVersion != parser.CurrentVersion {
		return nil
	}
	return &previous
}

func collectWork(
	ctx context.Context,
	cfg *config.Config,
	lock *lockfile.LockFile,
	root *os.Root,
	previous *Manifest,
) ([]collectionWork, []fileWork, error) {
	paths, err := cfg.Paths()
	if err != nil {
		return nil, nil, err
	}
	var collections []collectionWork
	var files []fileWork
	for _, name := range slices.Sorted(maps.Keys(cfg.Sources)) {
		if cancelErr := ctx.Err(); cancelErr != nil {
			return nil, nil, cancelErr
		}
		source := cfg.Sources[name]
		directory, pathErr := paths.SourceDir(name, source)
		if pathErr != nil {
			return nil, nil, pathErr
		}
		if _, statErr := root.Stat(directory); os.IsNotExist(statErr) {
			continue
		}
		collection := &Collection{
			Name:     name,
			Dir:      filepath.ToSlash(directory),
			Type:     source.Type,
			Source:   resolveSourceLocation(source),
			Path:     source.Path,
			Ref:      source.Ref,
			LastSync: resolveLastSync(lock, name),
		}
		work := collectionWork{collection: collection, previous: previousFiles(previous, collection)}
		if entry := lock.GetEntry(name); entry != nil {
			work.sha = entry.Files
		}
		sourceFiles, walkErr := walkCollection(ctx, root, directory, work, len(collections))
		if walkErr != nil {
			return nil, nil, errs.Wrap(
				errs.Internal,
				fmt.Errorf("walking source directory: %w", walkErr),
				errs.WithFields(
					map[string]any{"source": name, errs.LegacyCodeField: errs.LegacyManifestGenerationError},
				),
			)
		}
		files = append(files, sourceFiles...)

		collections = append(collections, work)
	}
	return collections, files, nil
}

func stateFile(name string) bool {
	return name == ManifestFile || name == ".dox.lock" || name == config.RootSentinel ||
		strings.HasPrefix(name, ".dox-") && strings.HasSuffix(name, ".tmp")
}

func previousFiles(previous *Manifest, current *Collection) map[string]*FileInfo {
	if previous == nil {
		return nil
	}
	old := previous.Collections[current.Name]
	if old == nil || old.Dir != current.Dir || old.Type != current.Type || old.Source != current.Source ||
		old.Path != current.Path ||
		old.Ref != current.Ref {
		return nil
	}
	files := make(map[string]*FileInfo, len(old.Files))
	for index := range old.Files {
		file := &old.Files[index]
		files[file.Path] = file
	}
	return files
}

func parseWork(ctx context.Context, root *os.Root, files []fileWork) ([]*FileInfo, error) {
	results := make([]*FileInfo, len(files))
	parsers := []parser.Parser{
		parser.NewMarkdownParser(),
		parser.NewMDXParser(),
		parser.NewTextParser(),
		parser.NewTypeScriptParser(),
	}
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(runtime.GOMAXPROCS(0))
	for index, work := range files {
		if err := groupCtx.Err(); err != nil {
			break
		}
		group.Go(func() error {
			if err := groupCtx.Err(); err != nil {
				return err
			}
			result, err := parseWorkFile(groupCtx, root, work, parsers)
			if err != nil {
				if groupCtx.Err() != nil {
					return groupCtx.Err()
				}
				return nil // Non-document files remain skipped, as in the previous generator.
			}
			results[index] = result
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	if cancelErr := ctx.Err(); cancelErr != nil {
		return nil, cancelErr
	}
	return results, nil
}

func parseWorkFile(ctx context.Context, root *os.Root, work fileWork, parsers []parser.Parser) (*FileInfo, error) {
	info, err := work.entry.Info()
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		info, err = root.Stat(work.path)
		if err != nil {
			return nil, err
		}
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("unsupported file type: %s", work.relative)
	}
	if reusableFile(work, info) {
		cached := *work.previous
		return &cached, nil
	}
	if cancelErr := ctx.Err(); cancelErr != nil {
		return nil, cancelErr
	}
	return parseFile(ctx, root, work.path, work.relative, info, work.sha, parsers)
}

func reusableFile(work fileWork, info fs.FileInfo) bool {
	old := work.previous
	return old != nil && old.Size == info.Size() && old.Modified.Equal(info.ModTime()) && old.SHA == work.sha
}

func assembleManifest(collections []collectionWork, files []fileWork, results []*FileInfo) *Manifest {
	current := New()
	current.ParserVersion = parser.CurrentVersion
	current.Generated = time.Time{}
	for index, work := range files {
		collection := collections[work.collection].collection
		if results[index] == nil {
			collection.Skipped++
			continue
		}
		collection.Files = append(collection.Files, *results[index])
		collection.TotalSize += results[index].Size
	}
	for _, work := range collections {
		collection := work.collection
		slices.SortFunc(collection.Files, func(a, b FileInfo) int { return strings.Compare(a.Path, b.Path) })
		collection.FileCount = len(collection.Files)
		for _, file := range collection.Files {
			if file.Modified.After(current.Generated) {
				current.Generated = file.Modified
			}
		}
		if collection.LastSync.IsZero() {
			collection.LastSync = latestModified(collection.Files)
		}
		if collection.LastSync.After(current.Generated) {
			current.Generated = collection.LastSync
		}
		current.Collections[collection.Name] = collection
	}
	return current
}

func latestModified(files []FileInfo) time.Time {
	var latest time.Time
	for _, file := range files {
		if file.Modified.After(latest) {
			latest = file.Modified
		}
	}
	return latest
}

func parseFile(
	ctx context.Context,
	root *os.Root,
	path, relative string,
	info fs.FileInfo,
	sha string,
	parsers []parser.Parser,
) (*FileInfo, error) {
	file := &FileInfo{Path: relative, Size: info.Size(), Modified: info.ModTime().UTC(), SHA: sha}
	if info.Size() > maxParseSize {
		file.Warning = "file_too_large"
		file.Type = unknownFileType
		return file, nil
	}
	content, err := root.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if cancelErr := ctx.Err(); cancelErr != nil {
		return nil, cancelErr
	}
	if parser.IsBinary(content) {
		return nil, errs.New(errs.Internal, "binary file")
	}
	for _, p := range parsers {
		if !p.CanParse(relative) {
			continue
		}
		result, parseErr := p.Parse(relative, content)
		if parseErr != nil {
			return nil, parseErr
		}
		file.Type = parser.DetectFileType(relative)
		file.Lines = result.Lines
		file.Description = result.Description
		file.ComponentType = result.ComponentType
		file.Outline = result.Outline
		return file, nil
	}
	file.Type = unknownFileType
	file.Lines = countLines(content)
	return file, nil
}

func resolveSourceLocation(source config.Source) string {
	if source.Repo != "" {
		return source.Repo
	}
	if source.URL != "" {
		return source.URL
	}
	return unknownFileType
}
func countLines(content []byte) int { return bytes.Count(content, []byte{'\n'}) + 1 }
func resolveLastSync(lock *lockfile.LockFile, name string) time.Time {
	if entry := lock.GetEntry(name); entry != nil {
		return entry.SyncedAt.UTC()
	}
	return time.Time{}
}

func walkCollection(
	ctx context.Context,
	root *os.Root,
	directory string,
	work collectionWork,
	index int,
) ([]fileWork, error) {
	var files []fileWork
	err := fs.WalkDir(
		root.FS(),
		filepath.ToSlash(directory),
		func(path string, entry fs.DirEntry, entryErr error) error {
			if entryErr != nil {
				return entryErr
			}
			if cancelErr := ctx.Err(); cancelErr != nil {
				return cancelErr
			}
			if entry.IsDir() || stateFile(entry.Name()) {
				return nil
			}
			relative, relErr := filepath.Rel(directory, filepath.FromSlash(path))
			if relErr != nil {
				return relErr
			}
			relative = filepath.ToSlash(relative)
			files = append(
				files,
				fileWork{
					collection: index,
					path:       path,
					relative:   relative,
					entry:      entry,
					previous:   work.previous[relative],
					sha:        work.sha[relative],
				},
			)
			return nil
		},
	)
	return files, err
}
