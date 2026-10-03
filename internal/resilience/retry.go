package resilience

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"strings"
	"time"
)

var (
	ErrPermanentFailure   = errors.New("permanent failure: not retriable")
	ErrMaxRetriesExceeded = errors.New("maximum retries exceeded")
)

// RetryPolicy defines bounded retry behavior with exponential backoff and jitter.
type RetryPolicy struct {
	MaxAttempts    int           `json:"max_attempts"`
	InitialBackoff time.Duration `json:"initial_backoff"`
	MaxBackoff     time.Duration `json:"max_backoff"`
	Multiplier     float64       `json:"multiplier"`
}

// DefaultRetryPolicy returns standard bounded retry settings.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts:    3,
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     2 * time.Second,
		Multiplier:     2.0,
	}
}

// ComputeBackoff calculates backoff duration with full jitter for attempt (1-based).
func (p RetryPolicy) ComputeBackoff(attempt int) time.Duration {
	if attempt <= 1 {
		return p.InitialBackoff
	}
	backoff := float64(p.InitialBackoff)
	for i := 1; i < attempt; i++ {
		backoff *= p.Multiplier
		if backoff > float64(p.MaxBackoff) {
			backoff = float64(p.MaxBackoff)
			break
		}
	}
	// Apply full jitter [0, backoff]
	jittered := time.Duration(rand.Float64() * backoff)
	if jittered < p.InitialBackoff/2 {
		jittered = p.InitialBackoff / 2
	}
	return jittered
}

// IsTransient evaluates whether an error represents a temporary disruption suitable for retry.
func IsTransient(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, ErrPermanentFailure) {
		return false
	}

	msg := strings.ToLower(err.Error())

	// Permanent API errors
	if strings.Contains(msg, "401 unauthorized") ||
		strings.Contains(msg, "403 forbidden") ||
		strings.Contains(msg, "404 not found") ||
		strings.Contains(msg, "405 method not allowed") ||
		strings.Contains(msg, "insufficient_authority") {
		return false
	}

	// Transient API errors & rate limits
	if strings.Contains(msg, "429") ||
		strings.Contains(msg, "too many requests") ||
		strings.Contains(msg, "502") ||
		strings.Contains(msg, "bad gateway") ||
		strings.Contains(msg, "503") ||
		strings.Contains(msg, "service unavailable") ||
		strings.Contains(msg, "504") ||
		strings.Contains(msg, "gateway timeout") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "timeout") {
		return true
	}

	// Network temporary or timeout errors
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}

	return false
}
