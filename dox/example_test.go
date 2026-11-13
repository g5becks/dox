package dox_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/g5becks/dox/dox"
)

func mustSynced() (*dox.Project, func()) {
	project, cleanup, err := newSyncedFixture()
	if err != nil {
		panic(err)
	}
	return project, cleanup
}

// Load reads a dox.toml; pass "" to search upward from the working directory
// like the CLI does.
func ExampleLoad() {
	project, cleanup, err := newFixture()
	if err != nil {
		panic(err)
	}
	defer cleanup()

	fmt.Println(filepath.Base(project.Output()))
	// Output: .dox
}

// Sync downloads the configured sources into the output directory. OnEvent
// reports progress; calls never overlap, so the callback needs no locking.
func ExampleProject_Sync() {
	project, cleanup, err := newFixture()
	if err != nil {
		panic(err)
	}
	defer cleanup()

	var finished []string
	result, err := project.Sync(context.Background(), dox.SyncOptions{
		OnEvent: func(event dox.SyncEvent) {
			if event.Kind == dox.EventSourceFinished && event.Err == nil {
				finished = append(finished, event.Source)
			}
		},
	})
	if err != nil {
		panic(err)
	}
	slices.Sort(finished)
	fmt.Println(result.Sources, "sources,", result.Downloaded, "files downloaded")
	fmt.Println(finished)
	// Output:
	// 2 sources, 2 files downloaded
	// [api guide]
}

func ExampleProject_Collections() {
	project, cleanup := mustSynced()
	defer cleanup()

	collections, err := project.Collections(context.Background())
	if err != nil {
		panic(err)
	}
	for _, collection := range collections {
		fmt.Println(collection.Name, collection.FileCount)
	}
	// Output:
	// api 1
	// guide 1
}

func ExampleProject_Files() {
	project, cleanup := mustSynced()
	defer cleanup()

	files, err := project.Files(context.Background(), "guide")
	if err != nil {
		panic(err)
	}
	for _, file := range files {
		fmt.Println(file.Path, file.Type)
	}
	// Output: guide.md md
}

func ExampleProject_Outline() {
	project, cleanup := mustSynced()
	defer cleanup()

	outline, err := project.Outline(context.Background(), "guide", "guide.md")
	if err != nil {
		panic(err)
	}
	for _, heading := range outline.Headings {
		fmt.Printf("%d:%s%s\n", heading.Line, strings.Repeat("  ", heading.Level-1), heading.Text)
	}
	// Output:
	// 1:Guide
	// 5:  Install
	// 9:  Usage
}

// Cat reads a window of a file: Line is 1-based and Limit 0 means "to the end".
func ExampleProject_Cat() {
	project, cleanup := mustSynced()
	defer cleanup()

	content, err := project.Cat(context.Background(), "guide", "guide.md", dox.Range{Line: 5, Limit: 3})
	if err != nil {
		panic(err)
	}
	for index, line := range content.Lines {
		fmt.Printf("%d:%s\n", content.Line+index, line)
	}
	// Output:
	// 5:## Install
	// 6:
	// 7:Run go install to get the binary.
}

// Search returns an iterator; the first error ends it. Content searches report
// the matching line, metadata searches (the default) report what matched.
func ExampleProject_Search() {
	project, cleanup := mustSynced()
	defer cleanup()

	query := dox.Query{Text: "dox sync", Content: true}
	for result, err := range project.Search(context.Background(), query) {
		if err != nil {
			panic(err)
		}
		fmt.Printf("%s/%s:%d: %s\n", result.Collection, result.Path, result.Line, result.Text)
	}
	// Output: guide/guide.md:11: Run dox sync before you search.
}

// Errors carry a stable Code and a hint, and wrap sentinels for errors.Is.
func ExampleError() {
	project, cleanup := mustSynced()
	defer cleanup()

	_, err := project.Cat(context.Background(), "gide", "guide.md", dox.Range{})
	fmt.Println(errors.Is(err, dox.ErrCollectionNotFound))
	if detail, ok := errors.AsType[*dox.Error](err); ok {
		fmt.Println(detail.Code)
		fmt.Println(detail.Hint)
	}
	// Output:
	// true
	// not_found
	// Did you mean "guide"? Run 'dox collections' to see available collections
}
