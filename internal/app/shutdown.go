package app

import (
	"sync/atomic"
	"time"

	"nahida.live/desktop/internal/infra"
)

const (
	// shutdownLimit leaves room for the 15 seconds backup.ServiceShutdown waits for a running backup, and with
	// shutdownLogWait stays under the 30 seconds a further launch waits in fairy.SingleInstance, so a launch right
	// after a hung shutdown still starts.
	shutdownLimit   = 25 * time.Second
	shutdownLogWait = 2 * time.Second
)

// closeWithin runs closeRuntime and calls exit if it has not returned within limit. After fairy.Main returns, a
// further launch waits for this process to exit, so a process stuck in shutdown makes every later launch fail. The
// running stage is logged before exit, but for at most logWait, because a hung log write would otherwise also
// hold the exit.
func closeWithin(
	closeRuntime func() error,
	watch *infra.StallWatch,
	limit, logWait time.Duration,
	exit func(),
) error {
	var finished atomic.Bool
	timer := time.AfterFunc(limit, func() {
		if finished.Load() {
			return
		}
		logged := make(chan struct{})
		go func() {
			watch.Expire()
			close(logged)
		}()
		select {
		case <-logged:
		case <-time.After(logWait):
		}
		if !finished.Load() {
			exit()
		}
	})

	err := closeRuntime()
	finished.Store(true)
	timer.Stop()
	return err
}
