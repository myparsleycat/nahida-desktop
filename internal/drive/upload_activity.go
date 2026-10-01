package drive

import (
	"context"
	"sync"

	"nahida.live/desktop/internal/transfer"
)

type uploadActivityKey struct{}

// uploadActivity aggregates concurrent requests. Waiting for one response must
// not hide another request that is still sending its body.
type uploadActivity struct {
	mu         sync.Mutex
	preparing  bool
	sending    int
	phase      transfer.UploadPhase
	onProgress func(UploadExecutionProgress)
}

func (a *uploadActivity) reportLocked() {
	phase := transfer.UploadWaiting
	if a.sending > 0 {
		phase = transfer.UploadTransferring
	} else if a.preparing {
		phase = transfer.UploadPreparing
	}
	if phase == a.phase {
		return
	}
	a.phase = phase
	if a.onProgress != nil {
		a.onProgress(UploadExecutionProgress{Phase: phase})
	}
}

func (a *uploadActivity) setPreparing(preparing bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.preparing = preparing
	a.reportLocked()
}

func beginUploadRequest(ctx context.Context) func() {
	a, _ := ctx.Value(uploadActivityKey{}).(*uploadActivity)
	if a == nil {
		return func() {}
	}
	a.mu.Lock()
	a.sending++
	a.reportLocked()
	a.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			a.sending--
			a.reportLocked()
		})
	}
}
