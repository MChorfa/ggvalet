package parallel

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPool_New_ZeroClampsToOne(t *testing.T) {
	p := New(0)
	if p == nil {
		t.Fatal("New(0) returned nil")
	}
	if cap(p.sem) != 1 {
		t.Errorf("New(0) clamped to concurrency %d, expected 1", cap(p.sem))
	}
}

func TestPool_New_NegativeClampsToOne(t *testing.T) {
	p := New(-5)
	if p == nil {
		t.Fatal("New(-5) returned nil")
	}
	if cap(p.sem) != 1 {
		t.Errorf("New(-5) clamped to concurrency %d, expected 1", cap(p.sem))
	}
}

func TestPool_New_PositiveValue(t *testing.T) {
	tests := []int{1, 3, 10, 100}

	for _, n := range tests {
		t.Run(string(rune(n)), func(t *testing.T) {
			p := New(n)
			if cap(p.sem) != n {
				t.Errorf("New(%d) concurrency = %d, expected %d", n, cap(p.sem), n)
			}
		})
	}
}

func TestPool_Go_SimpleExecution(t *testing.T) {
	p := New(3)
	var count int32

	for i := 0; i < 5; i++ {
		p.Go(func() {
			atomic.AddInt32(&count, 1)
		})
	}

	errs := p.Wait()
	if errs != nil && len(errs) != 0 {
		t.Fatalf("Wait() returned non-empty error slice: %v", errs)
	}

	if atomic.LoadInt32(&count) != 5 {
		t.Errorf("Go() executed %d functions, expected 5", count)
	}
}

func TestPool_Go_BoundedConcurrency(t *testing.T) {
	var (
		active  int64
		maxSeen int64
		mu      sync.Mutex
	)

	p := New(3)

	for i := 0; i < 10; i++ {
		p.Go(func() {
			cur := atomic.AddInt64(&active, 1)
			mu.Lock()
			if cur > maxSeen {
				maxSeen = cur
			}
			mu.Unlock()

			time.Sleep(20 * time.Millisecond)

			atomic.AddInt64(&active, -1)
		})
	}

	p.Wait()

	if atomic.LoadInt64(&maxSeen) != 3 {
		t.Errorf("max concurrent goroutines = %d, expected 3", maxSeen)
	}
}

func TestPool_GoErr_CollectsErrors(t *testing.T) {
	p := New(5)

	err1 := errors.New("error1")
	err2 := errors.New("error2")

	p.GoErr(func() error { return nil })
	p.GoErr(func() error { return err1 })
	p.GoErr(func() error { return nil })
	p.GoErr(func() error { return err2 })
	p.GoErr(func() error { return nil })

	errs := p.Wait()
	if len(errs) != 2 {
		t.Fatalf("Wait() returned %d errors, expected 2", len(errs))
	}

	errMap := make(map[string]bool)
	for _, err := range errs {
		errMap[err.Error()] = true
	}

	if !errMap["error1"] || !errMap["error2"] {
		t.Errorf("collected errors mismatch: %v", errs)
	}
}

func TestPool_GoErr_NoErrors(t *testing.T) {
	p := New(3)

	for i := 0; i < 5; i++ {
		p.GoErr(func() error { return nil })
	}

	errs := p.Wait()
	if errs != nil && len(errs) != 0 {
		t.Fatalf("Wait() returned non-empty error slice: %v", errs)
	}
}

func TestPool_Wait_BlocksUntilComplete(t *testing.T) {
	p := New(1)

	start := time.Now()

	p.Go(func() {
		time.Sleep(100 * time.Millisecond)
	})

	errs := p.Wait()
	elapsed := time.Since(start)

	if errs != nil && len(errs) != 0 {
		t.Fatalf("Wait() returned non-empty error slice: %v", errs)
	}

	if elapsed < 100*time.Millisecond {
		t.Errorf("Wait() returned too early: %v, expected >= 100ms", elapsed)
	}
}

func TestPool_Wait_EmptyPool(t *testing.T) {
	p := New(5)

	start := time.Now()
	errs := p.Wait()
	elapsed := time.Since(start)

	if errs != nil && len(errs) != 0 {
		t.Fatalf("Wait() on empty pool returned non-empty error slice: %v", errs)
	}

	if elapsed > 10*time.Millisecond {
		t.Errorf("Wait() on empty pool took %v, expected < 10ms", elapsed)
	}
}

func TestPool_ErrorOrderNotGuaranteed(t *testing.T) {
	p := New(5)

	errors := []error{
		errors.New("a"),
		errors.New("b"),
		errors.New("c"),
	}

	for _, err := range errors {
		e := err
		p.GoErr(func() error { return e })
	}

	gotErrors := p.Wait()
	if len(gotErrors) != 3 {
		t.Fatalf("Wait() returned %d errors, expected 3", len(gotErrors))
	}

	gotMap := make(map[string]bool)
	for _, err := range gotErrors {
		gotMap[err.Error()] = true
	}

	for _, err := range errors {
		if !gotMap[err.Error()] {
			t.Errorf("missing error: %v", err)
		}
	}
}

func TestPool_Go_MixedWithGoErr(t *testing.T) {
	p := New(5)

	err1 := errors.New("err1")
	var count int32

	p.Go(func() {
		atomic.AddInt32(&count, 1)
	})
	p.GoErr(func() error {
		atomic.AddInt32(&count, 1)
		return err1
	})
	p.Go(func() {
		atomic.AddInt32(&count, 1)
	})

	errs := p.Wait()

	if len(errs) != 1 {
		t.Fatalf("Wait() returned %d errors, expected 1", len(errs))
	}
	if errs[0].Error() != "err1" {
		t.Errorf("error mismatch: %v, expected 'err1'", errs[0])
	}

	if atomic.LoadInt32(&count) != 3 {
		t.Errorf("Go() executed %d functions, expected 3", count)
	}
}

func TestPool_Concurrency_BoundaryTest(t *testing.T) {
	tests := []int{1, 2, 5, 10}

	for _, n := range tests {
		t.Run(string(rune(n)), func(t *testing.T) {
			var (
				active  int64
				maxSeen int64
				mu      sync.Mutex
			)

			p := New(n)

			for i := 0; i < n*4; i++ {
				p.Go(func() {
					cur := atomic.AddInt64(&active, 1)
					mu.Lock()
					if cur > maxSeen {
						maxSeen = cur
					}
					mu.Unlock()

					time.Sleep(10 * time.Millisecond)

					atomic.AddInt64(&active, -1)
				})
			}

			p.Wait()

			if atomic.LoadInt64(&maxSeen) != int64(n) {
				t.Errorf("pool size %d: max concurrent = %d, expected %d", n, maxSeen, n)
			}
		})
	}
}

func TestPool_LargeWorkload(t *testing.T) {
	p := New(10)
	var count int32

	for i := 0; i < 100; i++ {
		p.Go(func() {
			atomic.AddInt32(&count, 1)
			time.Sleep(1 * time.Millisecond)
		})
	}

	errs := p.Wait()
	if len(errs) != 0 {
		t.Fatalf("Wait() returned errors: %v", errs)
	}

	if atomic.LoadInt32(&count) != 100 {
		t.Errorf("executed %d functions, expected 100", count)
	}
}

func TestPool_MultipleWaitCalls(t *testing.T) {
	p := New(3)

	p.Go(func() {
		time.Sleep(10 * time.Millisecond)
	})

	errs1 := p.Wait()
	if len(errs1) != 0 {
		t.Fatalf("first Wait() returned errors: %v", errs1)
	}

	errs2 := p.Wait()
	if len(errs2) != 0 {
		t.Fatalf("second Wait() returned errors: %v", errs2)
	}
}

func TestPool_GoErr_PartialErrors(t *testing.T) {
	p := New(5)

	err1 := errors.New("fail1")
	err2 := errors.New("fail2")

	p.GoErr(func() error { return nil })
	p.GoErr(func() error { return err1 })
	p.GoErr(func() error { return nil })
	p.GoErr(func() error { return nil })
	p.GoErr(func() error { return err2 })
	p.GoErr(func() error { return nil })

	errs := p.Wait()

	if len(errs) != 2 {
		t.Fatalf("Wait() returned %d errors, expected 2", len(errs))
	}

	errSet := make(map[string]bool)
	for _, err := range errs {
		errSet[err.Error()] = true
	}

	if !errSet["fail1"] || !errSet["fail2"] {
		t.Errorf("error set mismatch: %v", errs)
	}
}
