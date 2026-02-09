package manifest

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/samber/oops"

	"github.com/g5becks/dox/internal/parser"
)

const (
	CurrentVersion = "1.0.0"
	ManifestFile   = "manifest.json"
)

type Manifest struct {
	Version     string                 `json:"version"`
	Generated   time.Time              `json:"generated"`
	Collections map[string]*Collection `json:"collections"`
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
			return nil, oops.
				Code("MANIFEST_NOT_FOUND").
				With("path", manifestPath).
				Hint("Run 'dox sync' to generate the manifest").
				Errorf("manifest not found at %q", manifestPath)
		}

		return nil, oops.
			Code("MANIFEST_READ_ERROR").
			With("path", manifestPath).
			Wrapf(err, "reading manifest file")
	}

	m := &Manifest{}
	if unmarshalErr := json.Unmarshal(data, m); unmarshalErr != nil {
		return nil, oops.
			Code("MANIFEST_CORRUPTED").
			With("path", manifestPath).
			Hint("Delete .dox/manifest.json and run 'dox sync'").
			Wrapf(unmarshalErr, "parsing manifest file")
	}

	if m.Collections == nil {
		m.Collections = make(map[string]*Collection)
	}

	return m, nil
}

func (m *Manifest) Save(outputDir string) error {
	if m == nil {
		return oops.
			Code("MANIFEST_WRITE_ERROR").
			Hint("Initialize manifest before saving").
			Errorf("cannot save nil manifest")
	}

	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return oops.
			Code("MANIFEST_WRITE_ERROR").
			With("path", outputDir).
			Wrapf(err, "creating manifest directory")
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return oops.
			Code("MANIFEST_WRITE_ERROR").
			Wrapf(err, "encoding manifest")
	}

	data = append(data, '\n')
	manifestPath := Path(outputDir)

	tempFile, err := os.CreateTemp(outputDir, ManifestFile+".*.tmp")
	if err != nil {
		return oops.
			Code("MANIFEST_WRITE_ERROR").
			With("path", outputDir).
			Wrapf(err, "creating temporary manifest file")
	}

	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if _, writeErr := tempFile.Write(data); writeErr != nil {
		_ = tempFile.Close()
		return oops.
			Code("MANIFEST_WRITE_ERROR").
			With("path", tempPath).
			Wrapf(writeErr, "writing temporary manifest file")
	}

	if closeErr := tempFile.Close(); closeErr != nil {
		return oops.
			Code("MANIFEST_WRITE_ERROR").
			With("path", tempPath).
			Wrapf(closeErr, "closing temporary manifest file")
	}

	if renameErr := os.Rename(tempPath, manifestPath); renameErr != nil {
		return oops.
			Code("MANIFEST_WRITE_ERROR").
			With("from", tempPath).
			With("to", manifestPath).
			Wrapf(renameErr, "replacing manifest file")
	}

	return nil
}

func Path(outputDir string) string {
	return filepath.Join(outputDir, ManifestFile)
}
