// Package parallel provides a bounded goroutine pool for concurrent API calls.
package parallel

import "sync"

// Pool runs at most N goroutines concurrently.
type Pool struct {
	sem  chan struct{}
	wg   sync.WaitGroup
	mu   sync.Mutex
	errs []error
}

// New creates a Pool with concurrency limit n (minimum 1).
func New(n int) *Pool {
	if n < 1 {
		n = 1
	}
	return &Pool{sem: make(chan struct{}, n)}
}

// Go submits fn for execution, blocking until a slot is available.
func (p *Pool) Go(fn func()) {
	p.sem <- struct{}{}
	p.wg.Add(1)
	go func() {
		defer func() { <-p.sem; p.wg.Done() }()
		fn()
	}()
}

// GoErr submits fn; any returned error is collected and returned by Wait.
func (p *Pool) GoErr(fn func() error) {
	p.sem <- struct{}{}
	p.wg.Add(1)
	go func() {
		defer func() { <-p.sem; p.wg.Done() }()
		if err := fn(); err != nil {
			p.mu.Lock()
			p.errs = append(p.errs, err)
			p.mu.Unlock()
		}
	}()
}

// Wait blocks until all submitted functions complete.
// Returns any errors collected from GoErr calls.
func (p *Pool) Wait() []error {
	p.wg.Wait()
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.errs
}
