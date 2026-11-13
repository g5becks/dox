package dox

import (
	"context"
	"errors"

	"github.com/g5becks/dox/internal/errs"
)

// Code classifies a failure independently of its message. The values match
// the "code" field of the CLI's JSON error output.
type Code string

// Failure classes.
const (
	CodeUsage          = Code(errs.Usage)          // invalid arguments or options
	CodeConfig         = Code(errs.Config)         // invalid or unreadable configuration
	CodeNotFound       = Code(errs.NotFound)       // a config, collection, file or source does not exist
	CodeNetwork        = Code(errs.Network)        // a request failed or timed out
	CodeRateLimited    = Code(errs.RateLimited)    // the provider rejected the request for quota reasons
	CodePartialFailure = Code(errs.PartialFailure) // some sources failed while others succeeded
	CodeStale          = Code(errs.Stale)          // Check found sources behind upstream
	CodeInternal       = Code(errs.Internal)       // anything else
	CodeInterrupted    = Code(errs.Interrupted)    // the context was cancelled
)

// Sentinel errors for errors.Is. They are wrapped by an *Error.
var (
	// ErrCollectionNotFound reports an unknown collection name.
	ErrCollectionNotFound = errs.ErrCollectionNotFound
	// ErrFileNotFound reports a path that is not in the collection.
	ErrFileNotFound = errs.ErrFileNotFound
	// ErrSourceNotFound reports a source name that is not in the configuration.
	ErrSourceNotFound = errs.ErrSourceNotFound
	// ErrNotSynced reports that nothing has been synced yet; call Project.Sync.
	ErrNotSynced = errs.ErrNotSynced
)

// Error is the error type returned by every Project method. Use errors.AsType
// to read its Code and Hint, and errors.Is with the Err* sentinels or
// context.Canceled to branch on specific causes.
type Error struct {
	Code    Code   // failure class
	Message string // what went wrong
	Hint    string // what to do about it; may be empty
	cause   error
}

// Error returns the message.
func (e *Error) Error() string { return e.Message }

// Unwrap returns the underlying cause, so errors.Is and errors.As see through
// an *Error.
func (e *Error) Unwrap() error { return e.cause }

// wrapError turns any error from the implementation into an *Error.
func wrapError(err error) error {
	if err == nil {
		return nil
	}
	if existing, ok := errors.AsType[*Error](err); ok {
		return existing
	}
	apiErr := &Error{Code: CodeInternal, Message: err.Error(), cause: err}
	if detail, ok := errors.AsType[*errs.Error](err); ok {
		apiErr.Code, apiErr.Hint = Code(detail.Code), detail.Hint
	} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		apiErr.Code = CodeInterrupted
	}
	return apiErr
}

func usageError(message, hint string) error {
	return &Error{Code: CodeUsage, Message: message, Hint: hint}
}
