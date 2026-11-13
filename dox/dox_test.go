package dox_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/g5becks/dox/dox"
)

func TestSyncDeliversEventsOneAtATimeAndCounts(t *testing.T) {
	t.Parallel()
	project := unsyncedProject(t)
	// No locking on purpose: the race detector fails this test if events overlap.
	var events []dox.SyncEvent
	result, err := project.Sync(t.Context(), dox.SyncOptions{
		MaxParallel: 2,
		OnEvent:     func(event dox.SyncEvent) { events = append(events, event) },
	})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if result.Sources != 2 || result.Downloaded != 2 || result.Errors != 0 {
		t.Errorf("Sync() = %+v, want 2 sources, 2 downloads, 0 errors", result)
	}
	started, finished := map[string]bool{}, map[string]bool{}
	for _, event := range events {
		switch event.Kind {
		case dox.EventSourceStarted:
			started[event.Source] = true
		case dox.EventSourceFinished:
			finished[event.Source] = true
			if event.Err != nil || event.Downloaded != 1 {
				t.Errorf("finished event = %+v, want one download and no error", event)
			}
		case dox.EventWarning, dox.EventManifestFailed:
			t.Errorf("unexpected event %+v", event)
		}
	}
	for _, name := range []string{"api", "guide"} {
		if !started[name] || !finished[name] {
			t.Errorf("source %q: started=%v finished=%v, want both", name, started[name], finished[name])
		}
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	t.Parallel()
	project := unsyncedProject(t)
	result, err := project.Sync(t.Context(), dox.SyncOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Sync(DryRun) error = %v", err)
	}
	if result.Downloaded == 0 {
		t.Errorf("Sync(DryRun) = %+v, want it to report pending downloads", result)
	}
	if _, err = project.Collections(t.Context()); !errors.Is(err, dox.ErrNotSynced) {
		t.Errorf("Collections() after a dry run error = %v, want ErrNotSynced", err)
	}
}

func TestSyncUnknownSourceIsTyped(t *testing.T) {
	t.Parallel()
	project := unsyncedProject(t)
	_, err := project.Sync(t.Context(), dox.SyncOptions{Sources: []string{"nope"}})
	if !errors.Is(err, dox.ErrSourceNotFound) {
		t.Fatalf("Sync() error = %v, want ErrSourceNotFound", err)
	}
	if detail, ok := errors.AsType[*dox.Error](err); !ok || detail.Code != dox.CodeNotFound {
		t.Errorf("Sync() error = %#v, want a not_found *dox.Error", err)
	}
}

func TestSyncHonorsCancellation(t *testing.T) {
	t.Parallel()
	project := unsyncedProject(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := project.Sync(ctx, dox.SyncOptions{}); !errors.Is(err, context.Canceled) {
		t.Errorf("Sync() error = %v, want context.Canceled", err)
	}
}

func TestLoadMissingConfigIsTyped(t *testing.T) {
	t.Parallel()
	_, err := dox.Load(t.TempDir() + "/missing.toml")
	detail, ok := errors.AsType[*dox.Error](err)
	if !ok || detail.Code != dox.CodeNotFound || detail.Hint == "" {
		t.Errorf("Load() error = %#v, want a not_found *dox.Error with a hint", err)
	}
}

func TestLoadExternalOutputNeedsOptIn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	external := t.TempDir()
	configPath := filepath.Join(dir, "dox.toml")
	body := "output = " + strconv.Quote(
		external,
	) + "\n\n[sources.guide]\ntype = \"url\"\nurl = \"http://127.0.0.1:1/guide.md\"\n"
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := dox.Load(configPath); err == nil {
		t.Fatal("Load() accepted an output outside the config directory without WithExternalOutput")
	}
	project, err := dox.Load(configPath, dox.WithExternalOutput())
	if err != nil {
		t.Fatalf("Load(WithExternalOutput) error = %v", err)
	}
	if project.Output() != external {
		t.Errorf("Output() = %q, want %q", project.Output(), external)
	}
}

func TestCollectionsAndFiles(t *testing.T) {
	t.Parallel()
	project := syncedProject(t)
	collections, err := project.Collections(t.Context())
	if err != nil {
		t.Fatalf("Collections() error = %v", err)
	}
	names := make([]string, len(collections))
	for index, collection := range collections {
		names[index] = collection.Name
		if collection.FileCount != 1 || collection.Size == 0 || collection.LastSync.IsZero() {
			t.Errorf("collection %+v, want one synced file with a size and sync time", collection)
		}
	}
	if !slices.Equal(names, []string{"api", "guide"}) {
		t.Errorf("Collections() names = %v, want sorted [api guide]", names)
	}

	files, err := project.Files(t.Context(), "guide")
	if err != nil {
		t.Fatalf("Files() error = %v", err)
	}
	// The manifest counts the empty line after the final newline.
	if len(files) != 1 || files[0].Path != "guide.md" || files[0].Type != "md" ||
		files[0].Lines != 12 || files[0].Size != int64(len(guideMarkdown)) {
		t.Errorf("Files() = %+v, want guide.md, type md, 12 manifest lines, %d bytes", files, len(guideMarkdown))
	}
}

func TestOutlineHeadings(t *testing.T) {
	t.Parallel()
	project := syncedProject(t)
	outline, err := project.Outline(t.Context(), "guide", "guide.md")
	if err != nil {
		t.Fatalf("Outline() error = %v", err)
	}
	want := []dox.Heading{
		{Level: 1, Text: "Guide", Line: 1},
		{Level: 2, Text: "Install", Line: 5},
		{Level: 2, Text: "Usage", Line: 9},
	}
	if outline.Kind != dox.OutlineHeadings || !slices.Equal(outline.Headings, want) {
		t.Errorf("Outline() = %+v, want headings %+v", outline, want)
	}
}

func TestCatRanges(t *testing.T) {
	t.Parallel()
	project := syncedProject(t)
	tests := []struct {
		name      string
		rng       dox.Range
		wantFirst int
		want      []string
	}{
		{"whole file", dox.Range{}, 1, strings.Split(strings.TrimSuffix(guideMarkdown, "\n"), "\n")},
		{"explicit first line", dox.Range{Line: 1, Limit: 1}, 1, []string{"# Guide"}},
		{
			"window",
			dox.Range{Line: 5, Limit: 3},
			5,
			[]string{"## Install", "", "Run go install to get the binary."},
		},
		{"to the end", dox.Range{Line: 11}, 11, []string{"Run dox sync before you search."}},
		{"past the end", dox.Range{Line: 12}, 12, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content, err := project.Cat(t.Context(), "guide", "guide.md", tt.rng)
			if err != nil {
				t.Fatalf("Cat() error = %v", err)
			}
			if content.Line != tt.wantFirst || !slices.Equal(content.Lines, tt.want) {
				t.Errorf("Cat() line %d %q, want line %d %q", content.Line, content.Lines, tt.wantFirst, tt.want)
			}
			if content.Collection != "guide" || content.Path != "guide.md" || content.TotalLines != 11 {
				t.Errorf("Cat() = %+v, want guide/guide.md with 11 total lines", content)
			}
		})
	}
}

func TestCatTextJoinsLines(t *testing.T) {
	t.Parallel()
	project := syncedProject(t)
	content, err := project.Cat(t.Context(), "api", "api.md", dox.Range{Line: 1, Limit: 3})
	if err != nil {
		t.Fatalf("Cat() error = %v", err)
	}
	if want := "# API\n\nFunctions for embedding dox."; content.Text() != want {
		t.Errorf("Text() = %q, want %q", content.Text(), want)
	}
}

func TestCatRejectsNegativeRanges(t *testing.T) {
	t.Parallel()
	project := syncedProject(t)
	for _, rng := range []dox.Range{{Line: -1}, {Limit: -1}} {
		_, err := project.Cat(t.Context(), "guide", "guide.md", rng)
		if detail, ok := errors.AsType[*dox.Error](err); !ok || detail.Code != dox.CodeUsage {
			t.Errorf("Cat(%+v) error = %#v, want a usage *dox.Error", rng, err)
		}
	}
}

func TestLookupMissesAreTypedWithSuggestions(t *testing.T) {
	t.Parallel()
	project := syncedProject(t)
	ctx := t.Context()

	_, collectionErr := project.Files(ctx, "gide")
	if !errors.Is(collectionErr, dox.ErrCollectionNotFound) {
		t.Fatalf("Files(gide) error = %v, want ErrCollectionNotFound", collectionErr)
	}
	detail, ok := errors.AsType[*dox.Error](collectionErr)
	if !ok || detail.Code != dox.CodeNotFound || !strings.Contains(detail.Hint, `Did you mean "guide"?`) {
		t.Errorf("Files(gide) error = %#v, want a not_found error suggesting guide", collectionErr)
	}

	_, fileErr := project.Cat(ctx, "guide", "gide.md", dox.Range{})
	if !errors.Is(fileErr, dox.ErrFileNotFound) {
		t.Fatalf("Cat(gide.md) error = %v, want ErrFileNotFound", fileErr)
	}
	if detail, ok = errors.AsType[*dox.Error](fileErr); !ok || !strings.Contains(detail.Hint, `"guide.md"`) {
		t.Errorf("Cat(gide.md) error = %#v, want a suggestion for guide.md", fileErr)
	}

	if _, outlineErr := project.Outline(ctx, "nope", "guide.md"); !errors.Is(outlineErr, dox.ErrCollectionNotFound) {
		t.Errorf("Outline(nope) error = %v, want ErrCollectionNotFound", outlineErr)
	}
}

func TestReadsBeforeSyncReportErrNotSynced(t *testing.T) {
	t.Parallel()
	project := unsyncedProject(t)
	_, err := project.Files(t.Context(), "guide")
	detail, ok := errors.AsType[*dox.Error](err)
	if !errors.Is(err, dox.ErrNotSynced) || !ok || detail.Code != dox.CodeNotFound || detail.Hint == "" {
		t.Errorf("Files() error = %#v, want ErrNotSynced with a hint", err)
	}
}

func collect(t *testing.T, project *dox.Project, query dox.Query) []dox.Result {
	t.Helper()
	var results []dox.Result
	for result, err := range project.Search(t.Context(), query) {
		if err != nil {
			t.Fatalf("Search(%+v) error = %v", query, err)
		}
		results = append(results, result)
	}
	return results
}

func TestSearchMetadata(t *testing.T) {
	t.Parallel()
	project := syncedProject(t)
	results := collect(t, project, dox.Query{Text: "guide"})
	if len(results) == 0 {
		t.Fatal("Search(guide) returned nothing")
	}
	first := results[0]
	if first.Collection != "guide" || first.Path != "guide.md" || first.Score == 0 || first.MatchField == "" ||
		first.Line != 0 {
		t.Errorf("first result = %+v, want a scored metadata match on guide.md", first)
	}
}

func TestSearchContent(t *testing.T) {
	t.Parallel()
	project := syncedProject(t)
	results := collect(t, project, dox.Query{Text: "dox sync", Content: true})
	want := dox.Result{
		Collection: "guide",
		Path:       "guide.md",
		Line:       11,
		Text:       "Run dox sync before you search.",
	}
	if len(results) != 1 || results[0] != want {
		t.Errorf("Search(content) = %+v, want [%+v]", results, want)
	}

	scoped := collect(t, project, dox.Query{Text: "dox", Content: true, Collection: "api"})
	for _, result := range scoped {
		if result.Collection != "api" {
			t.Errorf("collection-scoped search returned %+v", result)
		}
	}
	if len(scoped) == 0 {
		t.Error("collection-scoped search returned nothing")
	}
}

func TestSearchRegexAndLimit(t *testing.T) {
	t.Parallel()
	project := syncedProject(t)
	regex := collect(t, project, dox.Query{Text: `go (install|build)`, Content: true, Regex: true})
	if len(regex) != 1 || regex[0].Line != 7 {
		t.Errorf("regex search = %+v, want one match on line 7", regex)
	}
	if all := collect(t, project, dox.Query{Text: "dox", Content: true}); len(all) < 2 {
		t.Fatalf("unlimited search = %+v, want several matches", all)
	}
	if limited := collect(t, project, dox.Query{Text: "dox", Content: true, Limit: 1}); len(limited) != 1 {
		t.Errorf("limit 1 search = %+v, want exactly one result", limited)
	}
}

func TestSearchRejectsInvalidQueries(t *testing.T) {
	t.Parallel()
	project := syncedProject(t)
	queries := []dox.Query{
		{Text: "  "},
		{Text: "dox", Regex: true},
		{Text: "dox", Limit: -1},
		{Text: "(", Content: true, Regex: true},
	}
	for _, query := range queries {
		var gotErr error
		for _, err := range project.Search(t.Context(), query) {
			gotErr = err
		}
		if detail, ok := errors.AsType[*dox.Error](gotErr); !ok || detail.Code != dox.CodeUsage {
			t.Errorf("Search(%+v) error = %#v, want a usage *dox.Error", query, gotErr)
		}
	}
	for _, err := range project.Search(t.Context(), dox.Query{Text: "dox", Collection: "gide"}) {
		if !errors.Is(err, dox.ErrCollectionNotFound) {
			t.Errorf("Search(unknown collection) error = %v, want ErrCollectionNotFound", err)
		}
	}
}

func TestSearchStopsWhenTheConsumerBreaks(t *testing.T) {
	t.Parallel()
	project := syncedProject(t)
	seen := 0
	for _, err := range project.Search(t.Context(), dox.Query{Text: "dox", Content: true}) {
		if err != nil {
			t.Fatal(err)
		}
		seen++
		break
	}
	if seen != 1 {
		t.Errorf("seen = %d, want 1", seen)
	}
}

func TestSearchHonorsCancellation(t *testing.T) {
	t.Parallel()
	project := syncedProject(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var gotErr error
	for _, err := range project.Search(ctx, dox.Query{Text: "dox", Content: true}) {
		gotErr = err
	}
	if !errors.Is(gotErr, context.Canceled) {
		t.Errorf("Search() error = %v, want context.Canceled", gotErr)
	}
}

// readEverything exercises every read method once and reports failures with
// t.Errorf, so it is safe to call from several goroutines.
func readEverything(t *testing.T, project *dox.Project) {
	t.Helper()
	ctx := t.Context()
	if _, err := project.Collections(ctx); err != nil {
		t.Errorf("Collections() error = %v", err)
	}
	if _, err := project.Cat(ctx, "guide", "guide.md", dox.Range{Line: 2, Limit: 3}); err != nil {
		t.Errorf("Cat() error = %v", err)
	}
	if _, err := project.Outline(ctx, "api", "api.md"); err != nil {
		t.Errorf("Outline() error = %v", err)
	}
	for _, err := range project.Search(ctx, dox.Query{Text: "dox", Content: true}) {
		if err != nil {
			t.Errorf("Search() error = %v", err)
		}
	}
}

func TestProjectIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()
	project := syncedProject(t)
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() { readEverything(t, project) })
	}
	// Syncs of one output directory queue behind the advisory lock.
	for range 2 {
		wg.Go(func() {
			if _, err := project.Sync(t.Context(), dox.SyncOptions{Force: true}); err != nil {
				t.Errorf("Sync() error = %v", err)
			}
		})
	}
	wg.Wait()
}
