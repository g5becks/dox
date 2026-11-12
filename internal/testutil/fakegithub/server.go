// Package fakegithub adds fault injection to deterministic GitHub test routes.
package fakegithub

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/g5becks/dox/internal/testutil/githubfixture"
)

// Response configures one path's behavior; its zero value uses normal responses.
type Response struct {
	Status        int
	Headers       http.Header
	Body          []byte
	Latency       time.Duration
	NeverRespond  bool
	TruncatedTree bool
}

// Server owns a local fake API and concurrent-safe response overrides.
type Server struct {
	URL     string
	Started <-chan struct{}
	client  *http.Client
	base    *githubfixture.Server
	mu      sync.RWMutex
	routes  map[string]Response
	calls   []string
	started chan struct{}
	// newConns counts accepted TCP connections, to prove connection reuse.
	newConns atomic.Int64
}

func New(tb testing.TB, files int) *Server {
	tb.Helper()
	base := githubfixture.New(tb, files)
	base.Latency = 0
	started := make(chan struct{}, 1)
	f := &Server{base: base, routes: make(map[string]Response), started: started, Started: started}
	server := httptest.NewUnstartedServer(http.HandlerFunc(f.serveHTTP))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			f.newConns.Add(1)
		}
	}
	server.Start()
	f.URL = server.URL
	f.client = server.Client()
	tb.Cleanup(server.Close)
	return f
}

// Set replaces one path's response without racing concurrent requests.
func (f *Server) Set(path string, response Response) {
	response.Headers = response.Headers.Clone()
	response.Body = slices.Clone(response.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[path] = response
}

func (f *Server) Client() *http.Client { return f.client }

// NewConnections returns how many TCP connections the server has accepted.
func (f *Server) NewConnections() int64 { return f.newConns.Load() }
func (f *Server) APIURL() string        { return f.URL + "/api" }

// Requests returns a copy of the observed paths, including canceled requests.
func (f *Server) Requests() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return slices.Clone(f.calls)
}

func (f *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.calls = append(f.calls, r.URL.Path)
	response := f.routes[r.URL.Path]
	f.mu.Unlock()
	select {
	case f.started <- struct{}{}:
	default:
	}
	if !waitResponse(r, response) {
		return
	}
	w = &headerWriter{ResponseWriter: w, overrides: response.Headers}
	if response.TruncatedTree && strings.Contains(r.URL.Path, "/git/trees/") {
		writeJSON(w, map[string]any{"sha": githubfixture.Tree, "truncated": true, "tree": []any{}})
		return
	}
	if response.Status != 0 || response.Body != nil {
		status := response.Status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_, _ = w.Write(response.Body) // Canceled clients may stop reading.
		return
	}
	f.base.ServeHTTP(w, r)
}

func waitResponse(r *http.Request, response Response) bool {
	if response.NeverRespond {
		<-r.Context().Done()
		return false
	}
	if response.Latency > 0 {
		select {
		case <-r.Context().Done():
			return false
		case <-time.After(response.Latency):
		}
	}
	return true
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value) // A canceled request may close its response.
}

// headerWriter applies fault headers after the normal handler's defaults.
type headerWriter struct {
	http.ResponseWriter

	overrides http.Header
	written   bool
}

func (w *headerWriter) WriteHeader(status int) {
	if w.written {
		return
	}
	for key, values := range w.overrides {
		w.Header()[key] = slices.Clone(values)
	}
	w.written = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *headerWriter) Write(body []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *headerWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
