package resilience

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type CircuitState string

const (
	StateClosed   CircuitState = "CLOSED"
	StateOpen     CircuitState = "OPEN"
	StateHalfOpen CircuitState = "HALF_OPEN"
)

var ErrCircuitOpen = errors.New("circuit breaker is OPEN: fast-failing request")

// CircuitBreaker protects downstream remote hosts from cascading failures.
type CircuitBreaker struct {
	mu                   sync.RWMutex
	name                 string
	failureThreshold     int
	successThreshold     int
	cooldown             time.Duration
	state                CircuitState
	consecutiveFailures  int
	consecutiveSuccesses int
	lastStateChange      time.Time
}

// NewCircuitBreaker creates a circuit breaker for a given host or resource.
func NewCircuitBreaker(name string, failureThreshold, successThreshold int, cooldown time.Duration) *CircuitBreaker {
	if failureThreshold <= 0 {
		failureThreshold = 5
	}
	if successThreshold <= 0 {
		successThreshold = 2
	}
	if cooldown <= 0 {
		cooldown = 5 * time.Second
	}
	return &CircuitBreaker{
		name:             name,
		failureThreshold: failureThreshold,
		successThreshold: successThreshold,
		cooldown:         cooldown,
		state:            StateClosed,
		lastStateChange:  time.Now().UTC(),
	}
}

// State returns the current circuit state, automatically evaluating cooldown expiration.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.evaluateCooldown()
	return cb.state
}

func (cb *CircuitBreaker) evaluateCooldown() {
	if cb.state == StateOpen && time.Since(cb.lastStateChange) >= cb.cooldown {
		cb.state = StateHalfOpen
		cb.consecutiveSuccesses = 0
		cb.lastStateChange = time.Now().UTC()
	}
}

// Execute wraps an operation under circuit breaker protection.
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() error) error {
	cb.mu.Lock()
	cb.evaluateCooldown()

	if cb.state == StateOpen {
		cb.mu.Unlock()
		return fmt.Errorf("%w for target %s (cooldown expires in %v)",
			ErrCircuitOpen, cb.name, cb.cooldown-time.Since(cb.lastStateChange))
	}
	cb.mu.Unlock()

	err := fn()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err == nil {
		cb.onSuccess()
		return nil
	}

	cb.onFailure(err)
	return err
}

func (cb *CircuitBreaker) onSuccess() {
	if cb.state == StateHalfOpen {
		cb.consecutiveSuccesses++
		if cb.consecutiveSuccesses >= cb.successThreshold {
			cb.state = StateClosed
			cb.consecutiveFailures = 0
			cb.consecutiveSuccesses = 0
			cb.lastStateChange = time.Now().UTC()
		}
	} else if cb.state == StateClosed {
		cb.consecutiveFailures = 0
	}
}

func (cb *CircuitBreaker) onFailure(err error) {
	// If permanent client error, do not count against circuit health
	if !IsTransient(err) {
		return
	}

	cb.consecutiveFailures++
	if cb.state == StateHalfOpen || cb.consecutiveFailures >= cb.failureThreshold {
		cb.state = StateOpen
		cb.lastStateChange = time.Now().UTC()
	}
}

// Trip forces the circuit breaker into OPEN state for testing or emergency isolation.
func (cb *CircuitBreaker) Trip() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = StateOpen
	cb.lastStateChange = time.Now().UTC()
}

// Reset forces the circuit breaker into CLOSED state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = StateClosed
	cb.consecutiveFailures = 0
	cb.consecutiveSuccesses = 0
	cb.lastStateChange = time.Now().UTC()
}
