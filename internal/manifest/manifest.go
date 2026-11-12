package manifest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/fsutil"
	"github.com/g5becks/dox/internal/parser"
)

const (
	CurrentVersion = "1.0.0"
	ManifestFile   = "manifest.json"
)

type Manifest struct {
	ParserVersion int                    `json:"parser_version,omitempty"`
	Version       string                 `json:"version"`
	Generated     time.Time              `json:"generated"`
	Collections   map[string]*Collection `json:"collections"`
}

type Collection struct {
	Name      string     `json:"name"`
	Dir       string     `json:"dir"`
	Type      string     `json:"type"`
	Source    string     `json:"source"`
	Path      string     `json:"path,omitempty"`
	Ref       string     `json:"ref,omitempty"`
	LastSync  time.Time  `json:"last_sync"`
	FileCount int        `json:"file_count"`
	TotalSize int64      `json:"total_size"`
	Skipped   int        `json:"skipped,omitempty"`
	Files     []FileInfo `json:"files"`
}

type FileInfo struct {
	SHA           string               `json:"sha,omitempty"`
	Path          string               `json:"path"`
	Type          string               `json:"type"`
	Size          int64                `json:"size"`
	Lines         int                  `json:"lines"`
	Modified      time.Time            `json:"modified"`
	Description   string               `json:"description"`
	ComponentType parser.ComponentType `json:"component_type,omitempty"`
	Warning       string               `json:"warning,omitempty"`
	Outline       *parser.Outline      `json:"outline,omitempty"`
}

func New() *Manifest {
	return &Manifest{
		Version:     CurrentVersion,
		Generated:   time.Now(),
		Collections: make(map[string]*Collection),
	}
}

func Load(outputDir string) (*Manifest, error) {
	manifestPath := Path(outputDir)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errs.New(
				errs.NotFound,
				fmt.Sprintf("manifest not found at %q", manifestPath),
				errs.WithHint("Run 'dox sync' to generate the manifest"),
				errs.WithFields(
					map[string]any{fieldPath: manifestPath, errs.LegacyCodeField: errs.LegacyManifestNotFound},
				),
			)
		}

		return nil, errs.Wrap(
			errs.Internal,
			fmt.Errorf("reading manifest file: %w", err),
			errs.WithFields(
				map[string]any{fieldPath: manifestPath, errs.LegacyCodeField: errs.LegacyManifestReadError},
			),
		)
	}

	m := &Manifest{}
	if unmarshalErr := json.Unmarshal(data, m); unmarshalErr != nil {
		return nil, errs.Wrap(
			errs.Internal,
			fmt.Errorf("parsing manifest file: %w", unmarshalErr),
			errs.WithHint("Delete .dox/manifest.json and run 'dox sync'"),
			errs.WithFields(
				map[string]any{fieldPath: manifestPath, errs.LegacyCodeField: errs.LegacyManifestCorrupted},
			),
		)
	}

	if m.Collections == nil {
		m.Collections = make(map[string]*Collection)
	}

	return m, nil
}

func (m *Manifest) Save(outputDir string) error {
	if m == nil {
		return m.SaveRoot(nil)
	}
	root, err := fsutil.OpenDir(outputDir)
	if err != nil {
		return errs.Wrap(
			errs.Internal,
			fmt.Errorf("creating manifest directory: %w", err),
			errs.WithFields(map[string]any{fieldPath: outputDir, errs.LegacyCodeField: errs.LegacyManifestWriteError}),
		)
	}
	defer root.Close()
	return m.SaveRoot(root)
}

// SaveRoot writes state through an already anchored output directory.
func (m *Manifest) SaveRoot(root *os.Root) error {
	return m.SaveRootContext(context.Background(), root)
}

// SaveRootContext checks cancellation before atomically publishing a compact snapshot.
func (m *Manifest) SaveRootContext(ctx context.Context, root *os.Root) error {
	if m == nil {
		return errs.New(
			errs.Internal,
			"cannot save nil manifest",
			errs.WithHint("Initialize manifest before saving"),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyManifestWriteError}),
		)
	}

	data, err := json.Marshal(m)
	if err != nil {
		return errs.Wrap(
			errs.Internal,
			fmt.Errorf("encoding manifest: %w", err),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyManifestWriteError}),
		)
	}
	data = append(data, '\n')
	if atomicWriteErr := fsutil.AtomicWriteContext(ctx, root, ManifestFile, data); atomicWriteErr != nil {
		return errs.Wrap(
			errs.Internal,
			fmt.Errorf("writing manifest: %w", atomicWriteErr),
			errs.WithFields(map[string]any{errs.LegacyCodeField: errs.LegacyManifestWriteError}),
		)
	}
	return nil
}

func Path(outputDir string) string {
	return filepath.Join(outputDir, ManifestFile)
}
