package ui

import (
	"sync"

	"github.com/myparsleycat/fairy"
)

// Events carries backend events to the UI. Backend packages emit through Emit, which may be called from any
// goroutine, also before the message loop starts; handlers registered with On run on the UI thread.
type Events struct {
	mu       sync.RWMutex
	handlers map[string][]func(data []any)
}

func NewEvents() *Events { return &Events{handlers: map[string][]func([]any){}} }

// Emit queues the event for its handlers. An event nobody handles is dropped here, so frequent progress
// events do not rebuild the windows.
func (e *Events) Emit(name string, data ...any) {
	e.mu.RLock()
	handlers := e.handlers[name]
	e.mu.RUnlock()
	if len(handlers) == 0 {
		return
	}
	fairy.Go(func() {
		for _, handle := range handlers {
			handle(data)
		}
	})
}

func (e *Events) On(name string, handler func(data []any)) {
	e.mu.Lock()
	e.handlers[name] = append(e.handlers[name], handler)
	e.mu.Unlock()
}
