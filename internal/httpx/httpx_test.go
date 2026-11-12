package httpx_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/g5becks/dox/internal/errs"
	"github.com/g5becks/dox/internal/httpx"
)

func TestNewClientConfiguresTimeoutsAndPool(t *testing.T) {
	t.Parallel()
	const timeout = 250 * time.Millisecond
	client := httpx.NewClient(timeout)
	if client.Timeout != timeout {
		t.Fatalf("client.Timeout = %v, want %v", client.Timeout, timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected transport type %T", client.Transport)
	}
	if transport.ResponseHeaderTimeout != timeout {
		t.Fatalf("ResponseHeaderTimeout = %v, want %v", transport.ResponseHeaderTimeout, timeout)
	}
	if transport.MaxIdleConnsPerHost != httpx.DefaultMaxIdleConnsPerHost {
		t.Fatalf("MaxIdleConnsPerHost = %d, want %d", transport.MaxIdleConnsPerHost, httpx.DefaultMaxIdleConnsPerHost)
	}
}

func TestParseTimeout(t *testing.T) {
	t.Parallel()
	got, err := httpx.ParseTimeout(errs.Config, "15s")
	if err != nil || got != 15*time.Second {
		t.Fatalf("ParseTimeout(15s) = %v, %v", got, err)
	}
	for _, invalid := range []string{"", "0s", "-5s", "nope"} {
		if _, parseErr := httpx.ParseTimeout(errs.Usage, invalid); parseErr == nil {
			t.Fatalf("expected error for invalid timeout %q", invalid)
		}
	}
}

func TestSourceContextAppliesDeadline(t *testing.T) {
	t.Parallel()
	ctx, cancel := httpx.SourceContext(context.Background(), 50*time.Millisecond)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 50*time.Millisecond {
		t.Fatalf("unexpected deadline: ok=%t remaining=%v", ok, time.Until(deadline))
	}
}
