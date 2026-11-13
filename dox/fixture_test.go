package dox_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/g5becks/dox/dox"
)

const guideMarkdown = `# Guide

Dox keeps documentation next to your code.

## Install

Run go install to get the binary.

## Usage

Run dox sync before you search.
`

const apiMarkdown = `# API

Functions for embedding dox.

## Load

Load reads a dox.toml file.
`

// newFixture serves two small documents, writes a dox.toml that syncs them and
// loads it. The returned function removes everything.
func newFixture() (*dox.Project, func(), error) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/guide.md":
			_, _ = w.Write([]byte(guideMarkdown))
		case "/api.md":
			_, _ = w.Write([]byte(apiMarkdown))
		default:
			http.NotFound(w, r)
		}
	}))
	dir, err := os.MkdirTemp("", "dox-api-*")
	if err != nil {
		server.Close()
		return nil, nil, err
	}
	cleanup := func() {
		server.Close()
		_ = os.RemoveAll(dir)
	}
	config := fmt.Sprintf(`
[sources.guide]
type = "url"
url = "%[1]s/guide.md"
filename = "guide.md"

[sources.api]
type = "url"
url = "%[1]s/api.md"
filename = "api.md"
`, server.URL)
	configPath := filepath.Join(dir, "dox.toml")
	if err = os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		cleanup()
		return nil, nil, err
	}
	project, err := dox.Load(configPath)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return project, cleanup, nil
}

// newSyncedFixture is newFixture followed by one sync.
func newSyncedFixture() (*dox.Project, func(), error) {
	project, cleanup, err := newFixture()
	if err != nil {
		return nil, nil, err
	}
	if _, err = project.Sync(context.Background(), dox.SyncOptions{}); err != nil {
		cleanup()
		return nil, nil, err
	}
	return project, cleanup, nil
}

func syncedProject(t *testing.T) *dox.Project {
	t.Helper()
	project, cleanup, err := newSyncedFixture()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	return project
}

func unsyncedProject(t *testing.T) *dox.Project {
	t.Helper()
	project, cleanup, err := newFixture()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	return project
}
