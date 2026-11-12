package httpx_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/g5becks/dox/internal/httpx"
)

func TestPerHostLimitBoundsInFlightRequests(t *testing.T) {
	t.Parallel()
	const limit, requests = 3, 12
	var active, peak atomic.Int64
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		now := active.Add(1)
		for {
			old := peak.Load()
			if now <= old || peak.CompareAndSwap(old, now) {
				break
			}
		}
		<-release
		active.Add(-1)
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(server.Close)

	client := httpx.NewClientWithOptions(httpx.Options{Timeout: 10 * time.Second, PerHostLimit: limit})
	t.Cleanup(client.CloseIdleConnections)
	var wg sync.WaitGroup
	errCh := make(chan error, requests)
	for range requests {
		wg.Go(func() {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
			if err != nil {
				errCh <- err
				return
			}
			resp, err := client.Do(req)
			if err != nil {
				errCh <- err
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			errCh <- resp.Body.Close()
		})
	}
	// Let the first wave arrive, then confirm nobody beyond the limit got in.
	deadline := time.Now().Add(5 * time.Second)
	for active.Load() < limit && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if got := active.Load(); got != limit {
		t.Fatalf("in-flight requests = %d, want %d", got, limit)
	}
	close(release)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := peak.Load(); got > limit {
		t.Fatalf("peak in-flight requests = %d, want <= %d", got, limit)
	}
}

func TestPerHostLimitWaitHonorsCancellation(t *testing.T) {
	t.Parallel()
	hold := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-hold
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(hold) })

	client := httpx.NewClientWithOptions(httpx.Options{Timeout: 10 * time.Second, PerHostLimit: 1})
	go func() {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
		if resp, err := client.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}()
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = client.Do(req)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued request error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("queued request waited %v after cancellation", elapsed)
	}
}

func TestPerHostLimitIsPerHost(t *testing.T) {
	t.Parallel()
	hold := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-hold
	}))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(hold) })
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(fast.Close)

	client := httpx.NewClientWithOptions(httpx.Options{Timeout: 10 * time.Second, PerHostLimit: 1})
	go func() {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, slow.URL, nil)
		if resp, err := client.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}()
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fast.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("other host blocked by a busy host: %v", err)
	}
	_ = resp.Body.Close()
}
