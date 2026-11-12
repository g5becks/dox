// Package httpx provides shared HTTP clients and timeout helpers for source syncs.
package httpx

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/g5becks/dox/internal/errs"
)

const (
	// DefaultTimeout is the default per-request and per-source sync timeout.
	DefaultTimeout = 30 * time.Second
	// DefaultMaxIdleConnsPerHost bounds idle pooled connections per host.
	DefaultMaxIdleConnsPerHost = 16
)

// ParseTimeout parses a positive duration string for config or flag values.
func ParseTimeout(code errs.Code, raw string) (time.Duration, error) {
	trimmed := strings.TrimSpace(raw)
	d, err := time.ParseDuration(trimmed)
	if err != nil || d <= 0 {
		legacy := errs.LegacyConfigInvalid
		if code == errs.Usage {
			legacy = errs.LegacyInvalidArgs
		}
		return 0, errs.New(
			code,
			fmt.Sprintf("invalid timeout %q", raw),
			errs.WithHint("Use a positive duration such as \"30s\" or \"1m\""),
			errs.WithFields(map[string]any{"timeout": raw, errs.LegacyCodeField: legacy}),
		)
	}
	return d, nil
}

// NormalizeTimeout returns DefaultTimeout when d is non-positive.
func NormalizeTimeout(d time.Duration) time.Duration {
	if d <= 0 {
		return DefaultTimeout
	}
	return d
}

// Options configures a shared client.
type Options struct {
	// Timeout bounds each request and its response headers (DefaultTimeout when <= 0).
	Timeout time.Duration
	// PerHostLimit bounds in-flight requests per host (DefaultPerHostLimit when <= 0).
	PerHostLimit int
}

// NewClient constructs a shared HTTP client with request, response-header,
// idle-connection and per-host concurrency bounds.
func NewClient(timeout time.Duration) *http.Client {
	return NewClientWithOptions(Options{Timeout: timeout})
}

// NewClientWithOptions constructs one client for all sources of a run: a single
// pooled transport (HTTP/2 where offered) keeps connections per host, and a
// LimitedTransport bounds concurrent requests to each host.
func NewClientWithOptions(opts Options) *http.Client {
	effective := NormalizeTimeout(opts.Timeout)
	var transport *http.Transport
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = base.Clone()
	} else {
		transport = &http.Transport{}
	}
	transport.ResponseHeaderTimeout = effective
	transport.MaxIdleConnsPerHost = DefaultMaxIdleConnsPerHost
	transport.ForceAttemptHTTP2 = true

	return &http.Client{
		Timeout:   effective,
		Transport: NewLimitedTransport(transport, opts.PerHostLimit),
	}
}

// SourceContext applies a per-source deadline to ctx without canceling sibling sources.
func SourceContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, NormalizeTimeout(timeout))
}
