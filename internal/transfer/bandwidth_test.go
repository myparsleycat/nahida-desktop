package transfer

import (
	"context"
	"errors"
	"sync"
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
	limiter.SetRateBPS(10_000)
	if err := limiter.Take(context.Background(), 10_000, nil); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	order := make([]int, 0, 2)
	done := make(chan struct{}, 2)
	for i := 1; i <= 2; i++ {
		go func() {
			if err := limiter.Take(context.Background(), 500, nil); err != nil {
				t.Errorf("Take() error = %v", err)
			}
			mu.Lock()
			order = append(order, i)
			mu.Unlock()
			done <- struct{}{}
		}()
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("first waiter did not finish")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("second waiter did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("completion order = %v", order)
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
