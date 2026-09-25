package transfer

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestBandwidthLimiterUnlimited(t *testing.T) {
	limiter := NewBandwidthLimiter()
	defer limiter.Close()
	if err := limiter.Take(context.Background(), 1024*1024, nil); err != nil {
		t.Fatalf("Take() error = %v", err)
	}
}

func TestBandwidthLimiterWaitsAndPreservesFIFO(t *testing.T) {
	limiter := NewBandwidthLimiter()
	defer limiter.Close()
	limiter.SetRateBPS(0.01)
	type result struct {
		id  int
		err error
	}
	results := make(chan result, 2)
	for i := 1; i <= 2; i++ {
		queued := make(chan struct{})
		go func() {
			results <- result{i, limiter.Take(t.Context(), 1, func() { close(queued) })}
		}()
		select {
		case <-queued:
		case <-time.After(time.Second):
			t.Fatalf("waiter %d did not enter the queue", i)
		}
	}

	limiter.mu.Lock()
	if len(limiter.waiters) != 2 {
		limiter.mu.Unlock()
		t.Fatalf("queued waiters = %d, want 2", len(limiter.waiters))
	}
	first, second := limiter.waiters[0], limiter.waiters[1]
	limiter.stopTimerLocked()
	limiter.tokens = 1
	limiter.lastFill = time.Now()
	limiter.drainLocked()
	firstGranted := false
	select {
	case <-first.done:
		firstGranted = true
	default:
	}
	secondGranted := false
	select {
	case <-second.done:
		secondGranted = true
	default:
	}
	limiter.mu.Unlock()
	if !firstGranted || secondGranted {
		t.Fatalf("first grant = %v, second grant = %v", firstGranted, secondGranted)
	}
	select {
	case got := <-results:
		if got.id != 1 || got.err != nil {
			t.Fatalf("first result = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("first granted waiter did not finish")
	}

	limiter.mu.Lock()
	limiter.stopTimerLocked()
	limiter.tokens = 1
	limiter.lastFill = time.Now()
	limiter.drainLocked()
	limiter.mu.Unlock()
	select {
	case got := <-results:
		if got.id != 2 || got.err != nil {
			t.Fatalf("second result = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("second granted waiter did not finish")
	}
}

func TestBandwidthLimiterCancellationReleasesNextWaiter(t *testing.T) {
	limiter := NewBandwidthLimiter()
	defer limiter.Close()
	limiter.SetRateBPS(1_000)
	if err := limiter.Take(context.Background(), 1_000, nil); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	head := make(chan error, 1)
	go func() { head <- limiter.Take(ctx, 1_000, nil) }()
	time.Sleep(10 * time.Millisecond)
	next := make(chan error, 1)
	go func() { next <- limiter.Take(context.Background(), 10, nil) }()
	cancel()
	if err := <-head; !errors.Is(err, context.Canceled) {
		t.Fatalf("head error = %v", err)
	}
	select {
	case err := <-next:
		if err != nil {
			t.Fatalf("next error = %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("next waiter did not consume accrued tokens")
	}
}

func TestBandwidthLimiterDisablingRateReleasesWaiters(t *testing.T) {
	limiter := NewBandwidthLimiter()
	defer limiter.Close()
	limiter.SetRateBPS(1)
	done := make(chan error, 1)
	go func() { done <- limiter.Take(context.Background(), 1_000_000, nil) }()
	time.Sleep(10 * time.Millisecond)
	limiter.SetRateBPS(0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter was not released")
	}
}

func TestBandwidthLimiterRejectsAlreadyAborted(t *testing.T) {
	limiter := NewBandwidthLimiter()
	defer limiter.Close()
	limiter.SetRateBPS(1000)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := limiter.Take(ctx, 100, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Take() error = %v", err)
	}
}

func TestBandwidthLimiterAllowsTakeLargerThanBucketAfterRefill(t *testing.T) {
	limiter := NewBandwidthLimiter()
	defer limiter.Close()
	limiter.SetRateBPS(1000)
	done := make(chan error, 1)
	go func() { done <- limiter.Take(context.Background(), 2500, nil) }()
	select {
	case err := <-done:
		t.Fatalf("large take finished too early: %v", err)
	case <-time.After(1400 * time.Millisecond):
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(800 * time.Millisecond):
		t.Fatal("large take did not finish after refill")
	}
}

func TestBandwidthLimiterCallsOnWaitOnlyWhenWaiting(t *testing.T) {
	limiter := NewBandwidthLimiter()
	defer limiter.Close()
	limiter.SetRateBPS(1000)
	var waits atomic.Int32
	onWait := func() { waits.Add(1) }
	if err := limiter.Take(context.Background(), 1000, onWait); err != nil {
		t.Fatal(err)
	}
	if waits.Load() != 0 {
		t.Fatalf("immediate take called onWait %d times", waits.Load())
	}
	done := make(chan error, 1)
	go func() { done <- limiter.Take(context.Background(), 1000, onWait) }()
	time.Sleep(20 * time.Millisecond)
	if waits.Load() != 1 {
		t.Fatalf("waiting take onWait = %d", waits.Load())
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("waiting take did not finish")
	}
}
