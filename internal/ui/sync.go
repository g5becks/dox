package ui

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/fatih/color"

	doxsync "github.com/g5becks/dox/internal/sync"
)

type styles struct {
	green  *color.Color
	red    *color.Color
	yellow *color.Color
	dim    *color.Color
	bold   *color.Color
}

func newStyles() styles {
	return styles{
		green:  color.New(color.FgGreen),
		red:    color.New(color.FgRed),
		yellow: color.New(color.FgYellow),
		dim:    color.New(color.Faint),
		bold:   color.New(color.Bold),
	}
}

// SyncPrinter renders sync progress events to stderr with colored output.
type SyncPrinter struct {
	w      io.Writer
	dryRun bool
	mu     sync.Mutex
	s      styles
}

// NewSyncPrinter creates a SyncPrinter that writes to stderr.
func NewSyncPrinter(dryRun bool) *SyncPrinter {
	return &SyncPrinter{
		w:      os.Stderr,
		dryRun: dryRun,
		s:      newStyles(),
	}
}

// NewSyncPrinterWithWriter creates a SyncPrinter that writes to the given writer.
func NewSyncPrinterWithWriter(w io.Writer, dryRun bool) *SyncPrinter {
	return &SyncPrinter{
		w:      w,
		dryRun: dryRun,
		s:      newStyles(),
	}
}

// HandleEvent is the callback wired into sync.Options.OnEvent.
func (p *SyncPrinter) HandleEvent(e doxsync.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch e.Kind {
	case doxsync.EventSourceStart:
		fmt.Fprintf(p.w, "%s syncing %s...\n",
			p.s.dim.Sprint("⟳"),
			p.s.bold.Sprint(e.Source),
		)

	case doxsync.EventSourceDone:
		p.handleDone(e)
	}
}

func (p *SyncPrinter) handleDone(e doxsync.Event) {
	if e.Err != nil {
		fmt.Fprintf(p.w, "%s %s: %s\n",
			p.s.red.Sprint("✗"),
			p.s.bold.Sprint(e.Source),
			e.Err,
		)
		return
	}

	if e.Result == nil {
		return
	}

	name := p.s.bold.Sprint(e.Source)

	if e.Result.Skipped {
		fmt.Fprintf(p.w, "%s %s %s\n",
			p.s.dim.Sprint("—"),
			name,
			p.s.dim.Sprint("(up to date)"),
		)
		return
	}

	detail := formatCounts(e.Result.Downloaded, e.Result.Deleted)
	fmt.Fprintf(p.w, "%s %s %s\n",
		p.s.green.Sprint("✓"),
		name,
		p.s.dim.Sprint(detail),
	)
}

func formatCounts(downloaded int, deleted int) string {
	switch {
	case downloaded > 0 && deleted > 0:
		return fmt.Sprintf("(%d downloaded, %d deleted)", downloaded, deleted)
	case downloaded > 0:
		return fmt.Sprintf("(%d downloaded)", downloaded)
	case deleted > 0:
		return fmt.Sprintf("(%d deleted)", deleted)
	default:
		return "(no changes)"
	}
}

// PrintSummary renders a final summary line after sync completes.
func (p *SyncPrinter) PrintSummary(r *doxsync.RunResult) {
	if r == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	fmt.Fprintln(p.w)

	label := "sync complete"
	if p.dryRun {
		label = p.s.yellow.Sprint("dry-run complete")
	}

	parts := fmt.Sprintf("%s: %d source(s), %d downloaded, %d deleted, %d up-to-date",
		label,
		r.Sources,
		r.Downloaded,
		r.Deleted,
		r.Skipped,
	)

	if r.Errors > 0 {
		parts += fmt.Sprintf(", %s",
			p.s.red.Sprintf("%d failed", r.Errors),
		)
	}

	fmt.Fprintln(p.w, parts)

	if p.dryRun {
		fmt.Fprintln(p.w, p.s.dim.Sprint("no files were written or removed"))
	}
}
