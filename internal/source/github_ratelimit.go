package source

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math/big"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"time"

	"github.com/g5becks/dox/internal/errs"
)

const (
	defaultMaxRateLimitWait = 60 * time.Second
	defaultRetryBaseWait    = 100 * time.Millisecond
	defaultRetryMaxWait     = 5 * time.Second
	maxBackoffShift         = 6
	jitterDivisor           = 2
)

type githubRequestSpec struct {
	endpoint   string
	query      map[string]string
	accept     string       // defaults to githubJSONMediaType
	result     any          // JSON destination for a 2xx response
	raw        *[]byte      // raw destination for a 2xx response (githubRawMediaType)
	header     *http.Header // receives the 2xx response headers (pagination)
	errContext string
	statusDesc string
	statusHint string
	legacyCode string
	fields     map[string]any
}

func (s *githubSource) doGitHubGet(ctx context.Context, spec githubRequestSpec) error {
	maxRetries := s.effectiveMaxRetries()
	waitedForRateLimit := false

	for attempt := 0; ; attempt++ {
		response, err := s.sendGitHubRequest(ctx, spec)
		if err != nil {
			if !s.shouldRetryNetworkError(ctx, err, attempt, maxRetries) {
				return s.wrapNetworkError(spec, err)
			}
			if waitErr := sleepWithContext(ctx, s.jitteredBackoff(attempt)); waitErr != nil {
				return s.wrapNetworkError(spec, waitErr)
			}
			continue
		}

		retryDelay, shouldRetry, respErr := s.evaluateGitHubResponse(
			response,
			spec,
			attempt,
			maxRetries,
			&waitedForRateLimit,
		)
		if shouldRetry {
			if waitErr := sleepWithContext(ctx, retryDelay); waitErr != nil {
				return s.wrapNetworkError(spec, waitErr)
			}
			continue
		}
		if respErr != nil {
			return respErr
		}
		if spec.header != nil {
			*spec.header = response.header
		}
		return s.decodeGitHubBody(spec, response)
	}
}

// githubResponse is a fully read API response; bodies are small JSON documents
// or single raw blobs, and reading them releases the per-host slot promptly.
type githubResponse struct {
	status int
	header http.Header
	body   []byte
}

func (r *githubResponse) isSuccess() bool {
	return r.status >= http.StatusOK && r.status < http.StatusMultipleChoices
}

func (s *githubSource) sendGitHubRequest(ctx context.Context, spec githubRequestSpec) (*githubResponse, error) {
	target := strings.TrimRight(s.apiBase, "/") + spec.endpoint
	if len(spec.query) > 0 {
		values := neturl.Values{}
		for k, v := range spec.query {
			values.Set(k, v)
		}
		target += "?" + values.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", userAgent)
	s.provider.decorateAPI(request, spec.accept)
	response, err := s.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	return &githubResponse{status: response.StatusCode, header: response.Header, body: body}, nil
}

func (s *githubSource) decodeGitHubBody(spec githubRequestSpec, response *githubResponse) error {
	switch {
	case spec.raw != nil:
		*spec.raw = response.body
	case spec.result != nil:
		if err := json.Unmarshal(response.body, spec.result); err != nil {
			return errs.Wrap(
				errs.Network,
				fmt.Errorf("%s: decoding response: %w", spec.errContext, err),
				errs.WithFields(spec.fields),
			)
		}
	}
	return nil
}

func (s *githubSource) shouldRetryNetworkError(ctx context.Context, err error, attempt, maxRetries int) bool {
	return !isContextTermination(ctx, err) && attempt < maxRetries
}

func (s *githubSource) wrapNetworkError(spec githubRequestSpec, err error) error {
	return errs.Wrap(
		errs.Network,
		fmt.Errorf("%s: %w", spec.errContext, err),
		errs.WithFields(spec.fields),
	)
}

func (s *githubSource) evaluateGitHubResponse(
	response *githubResponse,
	spec githubRequestSpec,
	attempt, maxRetries int,
	waitedForRateLimit *bool,
) (time.Duration, bool, error) {
	now := time.Now().UTC()
	if isRateLimitedResponse(response) {
		resetTime, waitDuration, hasWait := parseRateLimitReset(response.header, now)
		if !*waitedForRateLimit && hasWait && waitDuration > 0 && waitDuration <= s.effectiveMaxRateLimitWait() {
			*waitedForRateLimit = true
			return waitDuration, true, nil
		}
		return 0, false, s.newRateLimitError(response.header, resetTime)
	}

	status := response.status
	if status >= http.StatusInternalServerError && attempt < maxRetries {
		// A server asking for a longer pause than the wait bound is not worth
		// blocking the sync on; fail with the status error instead.
		if delay := s.retryDelayForResponse(attempt, response.header, now); delay <= s.effectiveMaxRateLimitWait() {
			return delay, true, nil
		}
	}

	if !response.isSuccess() {
		return 0, false, s.newStatusError(status, spec)
	}

	s.observeQuotaHeader(response.header)
	return 0, false, nil
}

func (s *githubSource) effectiveMaxRetries() int {
	if s.retriesConfigured {
		return max(0, s.maxRetries)
	}
	return httpRetryCount
}

func (s *githubSource) effectiveMaxRateLimitWait() time.Duration {
	if s.maxRateLimitWait > 0 {
		return s.maxRateLimitWait
	}
	return defaultMaxRateLimitWait
}

func (s *githubSource) jitteredBackoff(attempt int) time.Duration {
	base := s.retryBaseWait
	if base <= 0 {
		base = defaultRetryBaseWait
	}
	shift := min(attempt, maxBackoffShift)
	backoff := min(base*time.Duration(1<<shift), defaultRetryMaxWait)
	jitter := cryptoJitter(max(base/jitterDivisor, time.Nanosecond))
	return min(backoff+jitter, defaultRetryMaxWait)
}

func cryptoJitter(limit time.Duration) time.Duration {
	if limit <= 1 {
		return 0
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(limit)))
	if err != nil {
		return 0
	}
	return time.Duration(n.Int64())
}

func (s *githubSource) retryDelayForResponse(attempt int, header http.Header, now time.Time) time.Duration {
	if delay, ok := parseRetryAfterHeader(header.Get("Retry-After"), now); ok {
		return delay
	}
	return s.jitteredBackoff(attempt)
}

func (s *githubSource) newRateLimitError(header http.Header, resetTime time.Time) error {
	resetLabel := rateLimitHeader(header, "Reset")
	if resetLabel == "" && !resetTime.IsZero() {
		resetLabel = strconv.FormatInt(resetTime.Unix(), 10)
	}
	return errs.New(
		errs.RateLimited,
		s.provider.apiName()+" API rate limit exhausted",
		errs.WithCause(&errs.RateLimitError{Reset: resetTime}),
		errs.WithHint(s.provider.tokenHint()),
		errs.WithFields(
			map[string]any{
				fieldRepo:            s.source.Repo,
				"reset":              resetLabel,
				errs.LegacyCodeField: errs.LegacyGithubRateLimit,
			},
		),
	)
}

func (s *githubSource) newStatusError(status int, spec githubRequestSpec) error {
	fields := make(map[string]any, len(spec.fields)+1)
	maps.Copy(fields, spec.fields)
	fields[fieldStatus] = status
	fields[errs.LegacyCodeField] = errs.LegacyGithubAPIError

	opts := []errs.Option{
		errs.WithFields(fields),
		errs.WithCause(&errs.HTTPStatusError{Code: status}),
	}
	if spec.statusHint != "" {
		opts = append(opts, errs.WithHint(spec.statusHint))
	}
	return errs.New(
		errs.Network,
		fmt.Sprintf("%s API returned status %d for %s", s.provider.apiName(), status, spec.statusDesc),
		opts...,
	)
}

func (s *githubSource) observeQuotaHeader(header http.Header) {
	remainingRaw := rateLimitHeader(header, "Remaining")
	if remainingRaw == "" {
		return
	}
	remaining, err := strconv.Atoi(remainingRaw)
	if err != nil {
		return
	}
	if remaining <= rateLimitWarnThresh && s.warnedLowRL.CompareAndSwap(false, true) {
		if s.onWarning != nil {
			s.onWarning(
				fmt.Sprintf(
					"%s API rate limit low (%d remaining); %s",
					s.provider.apiName(),
					remaining,
					s.provider.tokenHint(),
				),
			)
		}
	}
}

func isRateLimitedResponse(response *githubResponse) bool {
	status := response.status
	if status != http.StatusForbidden && status != http.StatusTooManyRequests {
		return false
	}
	header := response.header
	if rateLimitHeader(header, "Remaining") == "0" {
		return true
	}
	if strings.TrimSpace(header.Get("Retry-After")) != "" {
		return true
	}
	return status == http.StatusTooManyRequests && rateLimitHeader(header, "Reset") != ""
}

// rateLimitHeader reads a quota header in GitHub (X-RateLimit-*) or GitLab /
// IETF draft (RateLimit-*) spelling.
func rateLimitHeader(header http.Header, field string) string {
	if value := strings.TrimSpace(header.Get("X-Ratelimit-" + field)); value != "" {
		return value
	}
	return strings.TrimSpace(header.Get("Ratelimit-" + field))
}

func parseRateLimitReset(header http.Header, now time.Time) (time.Time, time.Duration, bool) {
	var resetTime time.Time
	var waitDuration time.Duration
	hasWait := false

	if delay, ok := parseRetryAfterHeader(header.Get("Retry-After"), now); ok {
		waitDuration = delay
		resetTime = now.Add(delay).UTC()
		hasWait = true
	}

	if resetRaw := rateLimitHeader(header, "Reset"); resetRaw != "" {
		if unixSec, err := strconv.ParseInt(resetRaw, 10, 64); err == nil {
			headerReset := time.Unix(unixSec, 0).UTC()
			if resetTime.IsZero() {
				resetTime = headerReset
				waitDuration = max(0, headerReset.Sub(now))
				hasWait = true
			}
		}
	}

	return resetTime, waitDuration, hasWait
}

func parseRetryAfterHeader(raw string, now time.Time) (time.Duration, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(trimmed); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second, true
	}
	if parsedTime, err := http.ParseTime(trimmed); err == nil {
		return max(0, parsedTime.UTC().Sub(now)), true
	}
	return 0, false
}

func isContextTermination(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
