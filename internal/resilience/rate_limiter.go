package resilience

import (
	"context"
	"math"
	"sync"
	"time"
)

// RateLimiter implements a bounded token-bucket rate limiter.
type RateLimiter struct {
	mu            sync.Mutex
	rate          float64 // tokens per second
	burst         float64 // max token capacity
	tokens        float64
	lastTokenTime time.Time
}

// NewRateLimiter creates a rate limiter with rate (tokens/sec) and burst capacity.
func NewRateLimiter(rate float64, burst int) *RateLimiter {
	if rate <= 0 {
		rate = 50.0 // default 50 req/sec
	}
	if burst <= 0 {
		burst = 10
	}
	now := time.Now().UTC()
	return &RateLimiter{
		rate:          rate,
		burst:         float64(burst),
		tokens:        float64(burst),
		lastTokenTime: now,
	}
}

// Wait blocks until a token is available or context expires.
func (rl *RateLimiter) Wait(ctx context.Context) error {
	for {
		rl.mu.Lock()
		now := time.Now().UTC()
		elapsed := now.Sub(rl.lastTokenTime).Seconds()
		rl.lastTokenTime = now

		// Refill tokens
		rl.tokens = math.Min(rl.burst, rl.tokens+elapsed*rl.rate)

		if rl.tokens >= 1.0 {
			rl.tokens -= 1.0
			rl.mu.Unlock()
			return nil
		}

		// Calculate sleep time needed for 1 token
		needed := 1.0 - rl.tokens
		sleepDur := time.Duration(needed/rl.rate*float64(time.Second)) + time.Millisecond
		rl.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleepDur):
		}
	}
}

// Allow reports whether a request can proceed immediately without blocking.
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now().UTC()
	elapsed := now.Sub(rl.lastTokenTime).Seconds()
	rl.lastTokenTime = now
	rl.tokens = math.Min(rl.burst, rl.tokens+elapsed*rl.rate)

	if rl.tokens >= 1.0 {
		rl.tokens -= 1.0
		return true
	}
	return false
}
