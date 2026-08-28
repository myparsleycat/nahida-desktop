//go:build windows

package platform

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
)

const (
	esSystemRequired = 0x00000001
	esContinuous     = 0x80000000
)

var setThreadExecutionState = syscall.NewLazyDLL("kernel32.dll").NewProc("SetThreadExecutionState")

type powerCommand struct {
	block  bool
	result chan error
}

// PowerBlocker owns a dedicated locked OS thread because Windows execution
// state is attached to the calling thread, not to the process as a whole.
type PowerBlocker struct {
	mu       sync.Mutex
	commands chan powerCommand
	stop     chan chan error
	closed   bool
	apply    func(bool) error
}

func NewPowerBlocker() *PowerBlocker {
	return newPowerBlocker(applyExecutionState)
}

func newPowerBlocker(apply func(bool) error) *PowerBlocker {
	p := &PowerBlocker{
		commands: make(chan powerCommand),
		stop:     make(chan chan error),
		apply:    apply,
	}
	go p.run()
	return p
}

func (p *PowerBlocker) Set(block bool) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errors.New("power blocker is closed")
	}
	result := make(chan error, 1)
	p.commands <- powerCommand{block: block, result: result}
	return <-result
}

func (p *PowerBlocker) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	result := make(chan error, 1)
	p.stop <- result
	return <-result
}

func (p *PowerBlocker) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	blocked := false
	for {
		select {
		case command := <-p.commands:
			if command.block == blocked {
				command.result <- nil
				continue
			}
			err := p.apply(command.block)
			command.result <- err
			if err == nil {
				blocked = command.block
			}
		case result := <-p.stop:
			if blocked {
				result <- p.apply(false)
			} else {
				result <- nil
			}
			return
		}
	}
}

func applyExecutionState(block bool) error {
	state := uintptr(esContinuous)
	if block {
		state |= esSystemRequired
	}
	result, _, callErr := setThreadExecutionState.Call(state)
	if result != 0 {
		return nil
	}
	return fmt.Errorf("SetThreadExecutionState returned zero: %w", callErr)
}
