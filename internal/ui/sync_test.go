package ui_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/g5becks/dox/internal/source"
	"github.com/g5becks/dox/internal/sync"
	"github.com/g5becks/dox/internal/ui"
)

var errMock = errors.New("mock error")

func newTestPrinter(buf *bytes.Buffer, dryRun bool) *ui.SyncPrinter {
	return ui.NewSyncPrinterWithWriter(buf, dryRun)
}

func TestHandleEventStart(t *testing.T) {
	var buf bytes.Buffer
	p := newTestPrinter(&buf, false)

	p.HandleEvent(sync.Event{
		Kind:   sync.EventSourceStart,
		Source: "my-lib",
	})

	out := buf.String()
	if !strings.Contains(out, "my-lib") {
		t.Errorf("start event output missing source name, got: %q", out)
	}
	if !strings.Contains(out, "syncing") {
		t.Errorf("start event output missing 'syncing', got: %q", out)
	}
}

func TestHandleEventDoneSuccess(t *testing.T) {
	var buf bytes.Buffer
	p := newTestPrinter(&buf, false)

	p.HandleEvent(sync.Event{
		Kind:   sync.EventSourceDone,
		Source: "my-lib",
		Result: &source.SyncResult{Downloaded: 5, Deleted: 2},
	})

	out := buf.String()
	if !strings.Contains(out, "my-lib") {
		t.Errorf("done event output missing source name, got: %q", out)
	}
	if !strings.Contains(out, "5 downloaded") {
		t.Errorf("done event output missing download count, got: %q", out)
	}
	if !strings.Contains(out, "2 deleted") {
		t.Errorf("done event output missing delete count, got: %q", out)
	}
}

func TestHandleEventDoneSkipped(t *testing.T) {
	var buf bytes.Buffer
	p := newTestPrinter(&buf, false)

	p.HandleEvent(sync.Event{
		Kind:   sync.EventSourceDone,
		Source: "my-lib",
		Result: &source.SyncResult{Skipped: true},
	})

	out := buf.String()
	if !strings.Contains(out, "up to date") {
		t.Errorf("skipped event output missing 'up to date', got: %q", out)
	}
}

func TestHandleEventDoneError(t *testing.T) {
	var buf bytes.Buffer
	p := newTestPrinter(&buf, false)

	p.HandleEvent(sync.Event{
		Kind:   sync.EventSourceDone,
		Source: "my-lib",
		Err:    errMock,
	})

	out := buf.String()
	if !strings.Contains(out, "my-lib") {
		t.Errorf("error event output missing source name, got: %q", out)
	}
	if !strings.Contains(out, "mock error") {
		t.Errorf("error event output missing error text, got: %q", out)
	}
}

func TestPrintSummary(t *testing.T) {
	var buf bytes.Buffer
	p := newTestPrinter(&buf, false)

	p.PrintSummary(&sync.RunResult{
		Sources:    3,
		Downloaded: 10,
		Deleted:    1,
		Skipped:    1,
		Errors:     0,
	})

	out := buf.String()
	if !strings.Contains(out, "sync complete") {
		t.Errorf("summary missing 'sync complete', got: %q", out)
	}
	if !strings.Contains(out, "3 source(s)") {
		t.Errorf("summary missing source count, got: %q", out)
	}
}

func TestPrintSummaryDryRun(t *testing.T) {
	var buf bytes.Buffer
	p := newTestPrinter(&buf, true)

	p.PrintSummary(&sync.RunResult{
		Sources:    2,
		Downloaded: 5,
	})

	out := buf.String()
	if !strings.Contains(out, "dry-run complete") {
		t.Errorf("dry-run summary missing label, got: %q", out)
	}
	if !strings.Contains(out, "no files were written or removed") {
		t.Errorf("dry-run summary missing disclaimer, got: %q", out)
	}
}

func TestPrintSummaryWithErrors(t *testing.T) {
	var buf bytes.Buffer
	p := newTestPrinter(&buf, false)

	p.PrintSummary(&sync.RunResult{
		Sources: 3,
		Errors:  2,
	})

	out := buf.String()
	if !strings.Contains(out, "2 failed") {
		t.Errorf("summary missing error count, got: %q", out)
	}
}

func TestPrintSummaryNilResult(t *testing.T) {
	var buf bytes.Buffer
	p := newTestPrinter(&buf, false)

	p.PrintSummary(nil)

	if buf.Len() != 0 {
		t.Errorf("expected no output for nil result, got: %q", buf.String())
	}
}
