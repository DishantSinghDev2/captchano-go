package captchano

import (
	"errors"
	"fmt"
	"time"
)

// Sentinel errors, one per API error code, for use with errors.Is:
//
//	if errors.Is(err, captchano.ErrInsufficientBalance) { ... }
var (
	ErrInvalidAPIKey       = errors.New("captchano: invalid_api_key")
	ErrInsufficientBalance = errors.New("captchano: insufficient_balance")
	ErrUnsupportedType     = errors.New("captchano: unsupported_type")
	ErrInvalidSitekey      = errors.New("captchano: invalid_sitekey")
	ErrProxyRequired       = errors.New("captchano: proxy_required")
	ErrProxyUnreachable    = errors.New("captchano: proxy_unreachable")
	ErrScoreNotMet         = errors.New("captchano: score_not_met")
	ErrUnsolvable          = errors.New("captchano: unsolvable")
	ErrCapacityUnavailable = errors.New("captchano: capacity_unavailable")
	ErrRateLimited         = errors.New("captchano: rate_limited")
	ErrTaskNotFound        = errors.New("captchano: task_not_found")
	ErrUpstreamError       = errors.New("captchano: upstream_error")
)

// Error codes, as sent by the API in error.code.
const (
	CodeInvalidAPIKey       = "invalid_api_key"
	CodeInsufficientBalance = "insufficient_balance"
	CodeUnsupportedType     = "unsupported_type"
	CodeInvalidSitekey      = "invalid_sitekey"
	CodeProxyRequired       = "proxy_required"
	CodeProxyUnreachable    = "proxy_unreachable"
	CodeScoreNotMet         = "score_not_met"
	CodeUnsolvable          = "unsolvable"
	CodeCapacityUnavailable = "capacity_unavailable"
	CodeRateLimited         = "rate_limited"
	CodeTaskNotFound        = "task_not_found"
	CodeUpstreamError       = "upstream_error"
)

var sentinels = map[string]error{
	CodeInvalidAPIKey:       ErrInvalidAPIKey,
	CodeInsufficientBalance: ErrInsufficientBalance,
	CodeUnsupportedType:     ErrUnsupportedType,
	CodeInvalidSitekey:      ErrInvalidSitekey,
	CodeProxyRequired:       ErrProxyRequired,
	CodeProxyUnreachable:    ErrProxyUnreachable,
	CodeScoreNotMet:         ErrScoreNotMet,
	CodeUnsolvable:          ErrUnsolvable,
	CodeCapacityUnavailable: ErrCapacityUnavailable,
	CodeRateLimited:         ErrRateLimited,
	CodeTaskNotFound:        ErrTaskNotFound,
	CodeUpstreamError:       ErrUpstreamError,
}

// APIError is returned for every error response and for tasks that end in
// status "failed". Match it with errors.Is against the sentinels above, or
// errors.As to read the fields.
type APIError struct {
	// Code is the stable machine-readable code, e.g. "invalid_sitekey".
	Code string
	// Message is the API's human-readable text.
	Message string
	// Status is the HTTP status (0 when the error came from a polled task).
	Status int
	// TaskID is set when the error belongs to a task.
	TaskID string
	// RetryAfter is set on 429 when the API said how long to wait.
	RetryAfter time.Duration
	// Body is the raw response body, when there was one.
	Body []byte
}

func (e *APIError) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("captchano: %s: %s (HTTP %d)", e.Code, e.Message, e.Status)
	}
	return fmt.Sprintf("captchano: %s: %s", e.Code, e.Message)
}

// Is makes errors.Is(err, ErrInvalidSitekey) and friends work.
func (e *APIError) Is(target error) bool {
	s, ok := sentinels[e.Code]
	return ok && s == target
}

// Retryable reports whether the same request may succeed if sent again later.
func (e *APIError) Retryable() bool {
	switch e.Code {
	case CodeRateLimited, CodeCapacityUnavailable, CodeUpstreamError, CodeUnsolvable, CodeScoreNotMet:
		return true
	}
	return false
}

// codeForStatus is the best-effort code when a response carries none.
func codeForStatus(status int) string {
	switch {
	case status == 401 || status == 403:
		return CodeInvalidAPIKey
	case status == 402:
		return CodeInsufficientBalance
	case status == 404:
		return CodeTaskNotFound
	case status == 429:
		return CodeRateLimited
	case status == 503:
		return CodeCapacityUnavailable
	case status >= 500:
		return CodeUpstreamError
	}
	return "http_error"
}

// ConnectionError means the request never got a response (DNS, reset, timeout, offline).
type ConnectionError struct{ Err error }

func (e *ConnectionError) Error() string {
	return "captchano: could not reach the API: " + e.Err.Error()
}

// Unwrap returns the underlying network error.
func (e *ConnectionError) Unwrap() error { return e.Err }
