package resilience

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestRetryPolicy_TransientRetries(t *testing.T) {
	ctx := context.Background()
	policy := RetryPolicy{
		MaxAttempts:    3,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
		Multiplier:     2.0,
	}

	attempts := 0
	err := Execute(ctx, ExecutorConfig{Retry: policy}, func(ctx context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("503 Service Unavailable")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("expected retry to succeed on 3rd attempt, got %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetryPolicy_PermanentErrorDoesNotRetry(t *testing.T) {
	ctx := context.Background()
	policy := DefaultRetryPolicy()

	attempts := 0
	err := Execute(ctx, ExecutorConfig{Retry: policy}, func(ctx context.Context) error {
		attempts++
		return errors.New("403 Forbidden: insufficient_authority")
	})

	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if attempts != 1 {
		t.Fatalf("expected exactly 1 attempt for permanent error, got %d", attempts)
	}
}

func TestCircuitBreaker_TripsAndRecovers(t *testing.T) {
	ctx := context.Background()
	cb := NewCircuitBreaker("test-host", 2, 1, 50*time.Millisecond)

	if cb.State() != StateClosed {
		t.Errorf("expected initial state CLOSED, got %s", cb.State())
	}

	// 1st transient failure
	_ = cb.Execute(ctx, func() error { return errors.New("504 Gateway Timeout") })
	if cb.State() != StateClosed {
		t.Errorf("expected state CLOSED after 1 failure, got %s", cb.State())
	}

	// 2nd transient failure -> trips to OPEN
	_ = cb.Execute(ctx, func() error { return errors.New("504 Gateway Timeout") })
	if cb.State() != StateOpen {
		t.Errorf("expected state OPEN after 2 failures, got %s", cb.State())
	}

	// Fast-fail while OPEN
	err := cb.Execute(ctx, func() error { return nil })
	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("expected ErrCircuitOpen while OPEN, got %v", err)
	}

	// Wait for cooldown
	time.Sleep(60 * time.Millisecond)
	if cb.State() != StateHalfOpen {
		t.Errorf("expected state HALF_OPEN after cooldown, got %s", cb.State())
	}

	// Success probe recovers to CLOSED
	err = cb.Execute(ctx, func() error { return nil })
	if err != nil {
		t.Errorf("unexpected error on half-open probe: %v", err)
	}
	if cb.State() != StateClosed {
		t.Errorf("expected state CLOSED after successful probe, got %s", cb.State())
	}
}

func TestRateLimiter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	rl := NewRateLimiter(10.0, 2) // 10 tokens/sec, burst 2
	if !rl.Allow() {
		t.Errorf("expected 1st allow to pass")
	}
	if !rl.Allow() {
		t.Errorf("expected 2nd allow to pass (burst 2)")
	}

	// 3rd allow without sleep should fail
	if rl.Allow() {
		t.Errorf("expected 3rd allow to fail before refill")
	}

	// Wait blocks until refill
	err := rl.Wait(ctx)
	if err != nil {
		t.Errorf("wait failed: %v", err)
	}
}

func TestExecuteIntegration(t *testing.T) {
	ctx := context.Background()
	cb := NewCircuitBreaker("test-api", 3, 1, 100*time.Millisecond)
	rl := NewRateLimiter(100.0, 5)
	policy := RetryPolicy{
		MaxAttempts:    2,
		InitialBackoff: 5 * time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
		Multiplier:     2.0,
	}

	calls := 0
	err := Execute(ctx, ExecutorConfig{
		Retry:   policy,
		Breaker: cb,
		Limiter: rl,
	}, func(ctx context.Context) error {
		calls++
		if calls == 1 {
			return fmt.Errorf("connection reset by peer")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("expected Execute to succeed on 2nd attempt, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}
