package resilience

import (
	"context"
	"fmt"
	"time"
)

// ExecutorConfig holds settings for rate limiting, circuit breaking, and bounded retries.
type ExecutorConfig struct {
	Retry   RetryPolicy
	Breaker *CircuitBreaker
	Limiter *RateLimiter
}

// Execute wraps an operation with rate limiting, circuit breaker protection, and bounded retries.
func Execute(ctx context.Context, cfg ExecutorConfig, fn func(ctx context.Context) error) error {
	policy := cfg.Retry
	if policy.MaxAttempts <= 0 {
		policy.MaxAttempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		// 1. Wait for rate limiter token if configured
		if cfg.Limiter != nil {
			if err := cfg.Limiter.Wait(ctx); err != nil {
				return err
			}
		}

		// 2. Execute under circuit breaker if configured
		var execErr error
		if cfg.Breaker != nil {
			execErr = cfg.Breaker.Execute(ctx, func() error {
				return fn(ctx)
			})
		} else {
			execErr = fn(ctx)
		}

		// 3. Success
		if execErr == nil {
			return nil
		}

		lastErr = execErr

		// 4. Do not retry if non-transient or final attempt reached
		if !IsTransient(execErr) || attempt == policy.MaxAttempts {
			break
		}

		// 5. Compute jittered backoff and await next attempt
		backoff := policy.ComputeBackoff(attempt)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}

	if IsTransient(lastErr) && policy.MaxAttempts > 1 {
		return fmt.Errorf("%w: %v", ErrMaxRetriesExceeded, lastErr)
	}
	return lastErr
}
