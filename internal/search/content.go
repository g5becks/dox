package search

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/manifest"
	"github.com/g5becks/dox/internal/parser"
)

const maxFileSize = 50 * 1024 * 1024 // 50MB

// ContentResult represents a single match from content search.
type ContentResult struct {
	Collection string `json:"collection"`
	Path       string `json:"path"`
	Line       int    `json:"line"`
	Text       string `json:"text"`
}

// ContentOptions configures content search behavior.
type ContentOptions struct {
	OutputDir  string
	Query      string
	Collection string
	UseRegex   bool
	Limit      int
}

type matcher func([]byte) bool

// Content performs literal or regex search across synced file contents.
func Content(m *manifest.Manifest, opts ContentOptions) ([]ContentResult, error) {
	return ContentContext(context.Background(), m, opts)
}

// ContentContext performs literal or regex search across synced file contents, honoring ctx cancellation.
func ContentContext(ctx context.Context, m *manifest.Manifest, opts ContentOptions) ([]ContentResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		return nil, errs.New(
			errs.Usage,
			"search query cannot be empty",
			errs.WithHint("Provide a non-empty search query"),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyInvalidArgs}),
		)
	}

	if opts.Collection != "" {
		if _, exists := m.Collections[opts.Collection]; !exists {
			return nil, errs.New(
				errs.NotFound,
				fmt.Sprintf("collection %q not found", opts.Collection),
				errs.WithHint("Run 'dox collections' to see available collections"),
				errs.WithFields(
					map[string]any{"collection": opts.Collection, errs.LegacyCodeField: errs.LegacyCollectionNotFound},
				),
			)
		}
	}

	match, err := buildMatcher(query, opts.UseRegex)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(m.Collections))
	for name := range m.Collections {
		names = append(names, name)
	}
	sort.Strings(names)

	files, err := contentFiles(ctx, m, opts, names)
	if err != nil {
		return nil, err
	}
	return scanContentFiles(ctx, files, opts.Limit, match)
}

type contentFile struct {
	path       string
	collection string
	relative   string
}

func contentFiles(
	ctx context.Context,
	m *manifest.Manifest,
	opts ContentOptions,
	names []string,
) ([]contentFile, error) {
	var files []contentFile
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if opts.Collection != "" && name != opts.Collection {
			continue
		}
		coll := m.Collections[name]
		for _, file := range coll.Files {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			files = append(
				files,
				contentFile{
					path:       filepath.Join(opts.OutputDir, coll.Dir, file.Path),
					collection: name,
					relative:   file.Path,
				},
			)
		}
	}
	return files, nil
}

func scanContentFiles(ctx context.Context, files []contentFile, limit int, match matcher) ([]ContentResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	const maxWorkers = 8
	count := min(maxWorkers, runtime.GOMAXPROCS(0), len(files))
	chunks := make([][]ContentResult, len(files))
	jobs := make(chan int)
	var batch, workers sync.WaitGroup
	startContentWorkers(ctx, count, files, chunks, jobs, &batch, &workers, match, limit)
	defer func() { close(jobs); workers.Wait() }()
	total := 0
	// Process bounded windows so a limit stops scheduling after at most one window.
	const windowMultiplier = 8
	window := count
	if limit <= 0 {
		window *= windowMultiplier
	}
	for first := 0; first < len(files); first += window {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := min(first+window, len(files))
		batch.Add(end - first)
		for index := first; index < end; index++ {
			jobs <- index
		}
		batch.Wait()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, chunk := range chunks[first:end] {
			total += len(chunk)
		}
		if limit > 0 && total >= limit {
			total = limit
			break
		}
	}
	return flattenContentChunks(chunks, total), nil
}

func startContentWorkers(
	ctx context.Context,
	count int,
	files []contentFile,
	chunks [][]ContentResult,
	jobs <-chan int,
	batch, workers *sync.WaitGroup,
	match matcher,
	limit int,
) {
	for range count {
		workers.Go(func() {
			var scratch bytes.Buffer
			for index := range jobs {
				if ctx.Err() == nil {
					file := files[index]
					chunks[index], _ = scanFile(ctx, file.path, file.collection, file.relative, match, &scratch, limit)
				}
				batch.Done()
			}
		})
	}
}

func flattenContentChunks(chunks [][]ContentResult, total int) []ContentResult {
	results := make([]ContentResult, total)
	position := 0
	for _, chunk := range chunks {
		position += copy(results[position:], chunk)
		if position == total {
			break
		}
	}
	return results
}

func buildMatcher(query string, useRegex bool) (matcher, error) {
	if useRegex {
		pattern := "(?i)" + query
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, errs.Wrap(
				errs.Internal,
				fmt.Errorf("invalid regex pattern: %w", err),
				errs.WithHint("Check regex syntax"),
				errs.WithFields(map[string]any{"pattern": query, errs.LegacyCodeField: errs.LegacySearchError}),
			)
		}
		return re.Match, nil
	}

	lowerQuery := strings.ToLower(query)
	// The ASCII fast path is equivalent to the old lowercase comparison.
	// Unicode uses the exact original semantics (simple folding differs for İ/ſ/ς).
	asciiQuery := isASCII([]byte(lowerQuery))
	firstVariants := lowerQuery[:1]
	if firstVariants[0] >= 'a' && firstVariants[0] <= 'z' {
		firstVariants += string(firstVariants[0] - ('a' - 'A'))
	}
	return func(line []byte) bool {
		if asciiQuery && isASCII(line) {
			return containsASCII(line, []byte(lowerQuery), firstVariants)
		}
		return strings.Contains(strings.ToLower(string(line)), lowerQuery)
	}, nil
}

func isASCII(text []byte) bool {
	for _, b := range text {
		if b >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func scanFile(
	ctx context.Context,
	filePath, collection, relPath string,
	match matcher,
	scratch *bytes.Buffer,
	limit int,
) ([]ContentResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}

	if info.Size() > maxFileSize {
		return nil, nil
	}

	const readBufferSpare = 512
	scratch.Reset()
	scratch.Grow(int(info.Size()) + readBufferSpare)
	if _, readErr := scratch.ReadFrom(file); readErr != nil {
		return nil, readErr
	}
	if cancelErr := ctx.Err(); cancelErr != nil {
		return nil, cancelErr
	}
	content := scratch.Bytes()
	if parser.IsBinary(content) {
		return nil, nil
	}
	var results []ContentResult
	lineNumber := 0
	for raw := range bytes.Lines(content) {
		if cancelErr := ctx.Err(); cancelErr != nil {
			return nil, cancelErr
		}
		lineNumber++
		line := bytes.TrimSuffix(raw, []byte{'\n'})
		if match(line) {
			results = append(
				results,
				ContentResult{Collection: collection, Path: relPath, Line: lineNumber, Text: string(line)},
			)
			if limit > 0 && len(results) >= limit {
				break
			}
		}
	}

	return results, nil
}

func containsASCII(line, query []byte, firstVariants string) bool {
	for len(line) >= len(query) {
		index := bytes.IndexAny(line, firstVariants)
		if index < 0 || len(line)-index < len(query) {
			return false
		}
		line = line[index:]
		if equalASCII(line[:len(query)], query) {
			return true
		}
		line = line[1:]
	}
	return false
}

func equalASCII(text, lowerQuery []byte) bool {
	for index, b := range text {
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		if b != lowerQuery[index] {
			return false
		}
	}
	return true
}
