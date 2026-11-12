package httpx

import (
	"io"
	"net/http"
	"sync"
)

// DefaultPerHostLimit bounds concurrent in-flight requests to one host.
const DefaultPerHostLimit = 16

// LimitedTransport bounds in-flight requests per URL host. A slot is held from
// RoundTrip until the response body is closed or fully read, so it caps work
// that is still streaming as well as requests waiting for headers.
type LimitedTransport struct {
	base  *http.Transport
	limit int
	mu    sync.Mutex
	hosts map[string]chan struct{}
}

// NewLimitedTransport wraps base with a per-host limit (DefaultPerHostLimit when limit <= 0).
func NewLimitedTransport(base *http.Transport, limit int) *LimitedTransport {
	if limit <= 0 {
		limit = DefaultPerHostLimit
	}
	return &LimitedTransport{base: base, limit: limit, hosts: make(map[string]chan struct{})}
}

// Unwrap returns the pooled transport shared by all hosts.
func (t *LimitedTransport) Unwrap() *http.Transport { return t.base }

// Limit returns the per-host in-flight request bound.
func (t *LimitedTransport) Limit() int { return t.limit }

// CloseIdleConnections closes idle pooled connections.
func (t *LimitedTransport) CloseIdleConnections() { t.base.CloseIdleConnections() }

func (t *LimitedTransport) slots(host string) chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	slots, ok := t.hosts[host]
	if !ok {
		slots = make(chan struct{}, t.limit)
		t.hosts[host] = slots
	}
	return slots
}

// RoundTrip waits for a slot for the request's host, honoring cancellation.
func (t *LimitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	slots := t.slots(req.URL.Host)
	select {
	case slots <- struct{}{}:
	case <-req.Context().Done():
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, req.Context().Err()
	}
	release := sync.OnceFunc(func() { <-slots })
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		release()
		return nil, err
	}
	resp.Body = &releasingBody{ReadCloser: resp.Body, release: release}
	return resp, nil
}

type releasingBody struct {
	io.ReadCloser

	release func()
}

func (b *releasingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.release()
	}
	return n, err
}

func (b *releasingBody) Close() error {
	err := b.ReadCloser.Close()
	b.release()
	return err
}
