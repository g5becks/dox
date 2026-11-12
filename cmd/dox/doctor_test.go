package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func collectChecks(t *testing.T, rateLimitHandler http.HandlerFunc, token string) []doctorCheck {
	t.Helper()
	server := httptest.NewServer(rateLimitHandler)
	t.Cleanup(server.Close)
	var checks []doctorCheck
	checkRateLimit(t.Context(), server.URL, token, func(name, status, detail string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Detail: detail})
	})
	return checks
}

func TestCheckRateLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantStatus string
		wantDetail string
	}{
		{
			name: "plenty remaining",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"resources":{"core":{"limit":60,"remaining":42,"reset":1893456000}}}`))
			},
			wantStatus: checkOK,
			wantDetail: "42 of 60 requests remaining (resets 2030-01-01T00:00:00Z)",
		},
		{
			name: "nearly exhausted",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"resources":{"core":{"limit":60,"remaining":3,"reset":1893456000}}}`))
			},
			wantStatus: checkWarn,
			wantDetail: "3 of 60 requests remaining",
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
			},
			wantStatus: checkWarn,
			wantDetail: "unexpected response (status 502)",
		},
		{
			name: "malformed body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`not json`))
			},
			wantStatus: checkWarn,
			wantDetail: "unexpected response (status 200)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			checks := collectChecks(t, tt.handler, "")
			if len(checks) != 1 {
				t.Fatalf("got %d checks, want 1: %+v", len(checks), checks)
			}
			if checks[0].Status != tt.wantStatus || !strings.Contains(checks[0].Detail, tt.wantDetail) {
				t.Errorf("check = %+v, want status %q containing %q", checks[0], tt.wantStatus, tt.wantDetail)
			}
		})
	}
}

func TestCheckRateLimitSendsTokenOnlyWhenConfigured(t *testing.T) {
	t.Parallel()
	var authorization atomic.Value
	handler := func(w http.ResponseWriter, r *http.Request) {
		authorization.Store(r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"resources":{"core":{"limit":5000,"remaining":4999,"reset":0}}}`))
	}
	collectChecks(t, handler, "")
	if got := authorization.Load(); got != "" {
		t.Errorf("Authorization = %q without a token, want empty", got)
	}
	collectChecks(t, handler, "tok")
	if got := authorization.Load(); got != "Bearer tok" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer tok")
	}
}

func TestCheckRateLimitUnreachable(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close()
	var checks []doctorCheck
	checkRateLimit(t.Context(), url, "", func(name, status, detail string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Detail: detail})
	})
	if len(checks) != 1 || checks[0].Status != checkWarn || !strings.HasPrefix(checks[0].Detail, "unreachable:") {
		t.Errorf("checks = %+v, want one unreachable warning", checks)
	}
}
