package transfer

import (
	"context"
	"math"
	"sync"
	"time"
)

type bandwidthWaiter struct {
	bytes float64
	done  chan struct{}
	err   error
}

// BandwidthLimiter is a FIFO token bucket matching the Electron limiter.
// A zero rate is unlimited. The bucket initially carries one second of data,
// and may grow to fit the first queued request so large chunks cannot starve.
type BandwidthLimiter struct {
	mu       sync.Mutex
	rateBPS  float64
	tokens   float64
	lastFill time.Time
	waiters  []*bandwidthWaiter
	timer    *time.Timer
	closed   bool
}

func NewBandwidthLimiter() *BandwidthLimiter {
	return &BandwidthLimiter{lastFill: time.Now()}
}

func (l *BandwidthLimiter) SetRateBPS(rateBPS float64) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refillLocked(time.Now())
	if rateBPS <= 0 || math.IsNaN(rateBPS) || math.IsInf(rateBPS, 0) {
		l.rateBPS = 0
		l.tokens = 0
		l.stopTimerLocked()
		l.resolveAllLocked(nil)
		return
	}
	l.rateBPS = rateBPS
	l.tokens = rateBPS
	l.lastFill = time.Now()
	l.drainLocked()
}

func (l *BandwidthLimiter) RateBPS() float64 {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rateBPS
}

func (l *BandwidthLimiter) Take(ctx context.Context, bytes int64, onWait func()) error {
	if l == nil || bytes <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	l.mu.Lock()
	if l.closed || l.rateBPS <= 0 {
		l.mu.Unlock()
		return nil
	}
	l.refillLocked(time.Now())
	requested := float64(bytes)
	if len(l.waiters) == 0 && l.tokens >= requested {
		l.tokens -= requested
		l.mu.Unlock()
		return nil
	}
	waiter := &bandwidthWaiter{bytes: requested, done: make(chan struct{})}
	l.waiters = append(l.waiters, waiter)
	l.drainLocked()
	l.mu.Unlock()

	if onWait != nil {
		onWait()
	}
	select {
	case <-waiter.done:
		return waiter.err
	case <-ctx.Done():
		if l.cancelWaiter(waiter, ctx.Err()) {
			return ctx.Err()
		}
		<-waiter.done
		return waiter.err
	}
}

func (l *BandwidthLimiter) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	l.closed = true
	l.stopTimerLocked()
	l.resolveAllLocked(nil)
}

func (l *BandwidthLimiter) cancelWaiter(target *bandwidthWaiter, err error) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, waiter := range l.waiters {
		if waiter != target {
			continue
		}
		l.waiters = append(l.waiters[:i], l.waiters[i+1:]...)
		waiter.err = err
		close(waiter.done)
		l.drainLocked()
		return true
	}
	return false
}

func (l *BandwidthLimiter) refillLocked(now time.Time) {
	if l.rateBPS <= 0 {
		l.lastFill = now
		return
	}
	elapsed := now.Sub(l.lastFill).Seconds()
	if elapsed <= 0 {
		return
	}
	capacity := l.rateBPS
	if len(l.waiters) > 0 && l.waiters[0].bytes > capacity {
		capacity = l.waiters[0].bytes
	}
	l.tokens = min(capacity, l.tokens+l.rateBPS*elapsed)
	l.lastFill = now
}

func (l *BandwidthLimiter) drainLocked() {
	if l.rateBPS <= 0 || l.closed {
		l.resolveAllLocked(nil)
		return
	}
	l.refillLocked(time.Now())
	for len(l.waiters) > 0 {
		waiter := l.waiters[0]
		if l.tokens < waiter.bytes {
			break
		}
		l.waiters = l.waiters[1:]
		l.tokens -= waiter.bytes
		close(waiter.done)
	}
	l.scheduleLocked()
}

func (l *BandwidthLimiter) scheduleLocked() {
	l.stopTimerLocked()
	if l.rateBPS <= 0 || len(l.waiters) == 0 || l.closed {
		return
	}
	deficit := l.waiters[0].bytes - l.tokens
	if deficit <= 0 {
		return
	}
	delay := time.Duration(deficit/l.rateBPS*float64(time.Second)) + time.Nanosecond
	if delay < time.Millisecond {
		delay = time.Millisecond
	}
	l.timer = time.AfterFunc(delay, func() {
		l.mu.Lock()
		l.timer = nil
		l.drainLocked()
		l.mu.Unlock()
	})
}

func (l *BandwidthLimiter) stopTimerLocked() {
	if l.timer == nil {
		return
	}
	l.timer.Stop()
	l.timer = nil
}

func (l *BandwidthLimiter) resolveAllLocked(err error) {
	for _, waiter := range l.waiters {
		waiter.err = err
		close(waiter.done)
	}
	l.waiters = nil
}
