// Package errs provides error categories, guidance and inspectable causes.
package errs

import (
	"errors"
	"fmt"
	"time"
)

// Code identifies the kind of failure independently of its displayed message.
type Code string

const (
	Usage          Code = "usage"
	Config         Code = "config"
	NotFound       Code = "not_found"
	Network        Code = "network"
	RateLimited    Code = "rate_limited"
	PartialFailure Code = "partial_failure"
	Internal       Code = "internal"
	Interrupted    Code = "interrupted"
)

// Error carries user guidance and context while retaining a standard error chain.
type Error struct {
	Code   Code
	Msg    string
	Hint   string
	Fields map[string]any
	Err    error
}

func (e *Error) Error() string { return e.Msg }
func (e *Error) Unwrap() error { return e.Err }

// Option adds optional metadata to an Error.
type Option func(*Error)

// New constructs an error without changing its message.
func New(code Code, msg string, options ...Option) *Error {
	failure := &Error{Code: code, Msg: msg}
	for _, option := range options {
		option(failure)
	}
	return failure
}

// Wrap adds metadata to an error already wrapped with fmt.Errorf and %w.
func Wrap(code Code, cause error, options ...Option) *Error {
	failure := New(code, cause.Error(), options...)
	failure.Err = cause
	return failure
}

// WithHint sets actionable guidance, separate from the message.
func WithHint(hint string) Option {
	return func(failure *Error) { failure.Hint = hint }
}

// WithFields attaches context. Callers must not modify fields after construction.
func WithFields(fields map[string]any) Option {
	return func(failure *Error) { failure.Fields = fields }
}

// WithCause attaches a typed or sentinel cause without changing the message.
func WithCause(cause error) Option {
	return func(failure *Error) { failure.Err = cause }
}

// ErrSourceNotFound allows callers to distinguish unknown configured sources.
var ErrSourceNotFound = errors.New("source not found")

// RateLimitError records when the provider quota resets, if known.
type RateLimitError struct{ Reset time.Time }

func (e *RateLimitError) Error() string {
	if e.Reset.IsZero() {
		return "rate limit exhausted"
	}
	return "rate limit exhausted until " + e.Reset.UTC().Format(time.RFC3339)
}

// HTTPStatusError exposes a failed HTTP response status.
type HTTPStatusError struct{ Code int }

func (e *HTTPStatusError) Error() string { return fmt.Sprintf("HTTP status %d", e.Code) }
