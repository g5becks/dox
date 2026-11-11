package errs_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/g5becks/dox/internal/errs"
)

func TestMetadataAndWrapping(t *testing.T) {
	t.Parallel()
	cause := errors.New("disk full")
	fields := map[string]any{"path": "docs", "legacy_code": "WRITE_FAILED"}
	failure := errs.Wrap(errs.Internal, fmt.Errorf("writing docs: %w", cause),
		errs.WithHint("Free disk space"), errs.WithFields(fields))
	wrapped := fmt.Errorf("sync: %w", failure)
	if !errors.Is(wrapped, cause) {
		t.Fatal("underlying cause was lost")
	}
	detail, ok := errors.AsType[*errs.Error](wrapped)
	if !ok || detail.Code != errs.Internal || detail.Msg != "writing docs: disk full" ||
		detail.Hint != "Free disk space" || !reflect.DeepEqual(detail.Fields, fields) {
		t.Fatalf("unexpected metadata: %+v", detail)
	}
	if detail.Error() != detail.Msg {
		t.Fatalf("error text changed: %q", detail.Error())
	}
}

func TestSourceNotFound(t *testing.T) {
	t.Parallel()
	failure := errs.New(errs.NotFound, "unknown source", errs.WithCause(errs.ErrSourceNotFound))
	if !errors.Is(fmt.Errorf("sync: %w", failure), errs.ErrSourceNotFound) {
		t.Fatal("source sentinel lost")
	}
}

func TestRateLimit(t *testing.T) {
	t.Parallel()
	reset := time.Unix(1_800_000_000, 0)
	failure := errs.New(errs.RateLimited, "quota exhausted", errs.WithCause(&errs.RateLimitError{Reset: reset}))
	limit, ok := errors.AsType[*errs.RateLimitError](fmt.Errorf("request: %w", failure))
	if !ok || !limit.Reset.Equal(reset) || limit.Error() == "" {
		t.Fatalf("rate-limit metadata lost: %+v", limit)
	}
	if (&errs.RateLimitError{}).Error() != "rate limit exhausted" {
		t.Fatal("unknown reset text changed")
	}
}

func TestHTTPStatus(t *testing.T) {
	t.Parallel()
	failure := errs.New(errs.Network, "request failed", errs.WithCause(&errs.HTTPStatusError{Code: 503}))
	status, ok := errors.AsType[*errs.HTTPStatusError](fmt.Errorf("operation: %w", failure))
	if !ok || status.Code != 503 || status.Error() == "" {
		t.Fatalf("HTTP status lost: %+v", status)
	}
}
