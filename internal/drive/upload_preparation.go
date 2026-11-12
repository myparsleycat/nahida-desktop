package drive

import (
	"context"
	"time"

	"nahida.live/desktop/internal/transfer"
)

type uploadPreparationProgress struct {
	ctx     context.Context
	now     func() time.Time
	publish func(transfer.PreparationProgress)
	value   transfer.PreparationProgress
	last    time.Time
	started bool
}

func (p *uploadPreparationProgress) add(bytes int64, completed bool) {
	p.value.ProcessedBytes += bytes
	if completed {
		p.value.ProcessedFiles++
	}
	p.report(false)
}

func (p *uploadPreparationProgress) report(force bool) {
	if p.ctx.Err() != nil {
		return
	}
	now := p.now()
	if !force && p.started && now.Sub(p.last) < 500*time.Millisecond {
		return
	}
	p.last, p.started = now, true
	p.publish(p.value)
}
