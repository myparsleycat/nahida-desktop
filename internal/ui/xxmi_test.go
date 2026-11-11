package ui

import (
	"context"
	"testing"
	"time"
)

// CheckUpdates emits the event that sets the updates query; a value set that way must count as fresh, or every
// frame starts another check.
func TestFetchFreshKeepsValueSetByEvent(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	shell := &Shell{deps: Deps{Ready: func(context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}}
	var q query[int]
	q.set(1)
	fetchFresh(shell, &q, time.Hour, func(context.Context) (int, error) { return 2, nil })
	if q.loading {
		t.Error("a value set by an event was read again at once")
	}
}
