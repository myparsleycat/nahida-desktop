//go:build windows

package app

import (
	"fmt"
	"math"
	gort "runtime"
	"sync"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/win"
	"github.com/rodrigocfd/windigo/x/cosh"
	"github.com/rodrigocfd/windigo/x/winsh"
)

const taskbarProgressTotal = 10_000

type taskbarUpdate struct {
	hwnd  uintptr
	value *float64
	mode  string
}

type nativeTaskbar struct {
	mu      sync.Mutex
	started bool
	closed  bool
	updates chan taskbarUpdate
	stop    chan struct{}
	done    chan struct{}
	report  func(error)
}

func newNativeTaskbar(report func(error)) *nativeTaskbar {
	return &nativeTaskbar{
		updates: make(chan taskbarUpdate, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
		report:  report,
	}
}

func (t *nativeTaskbar) Set(hwnd uintptr, value *float64, mode string) {
	if t == nil || hwnd == 0 {
		return
	}
	update := taskbarUpdate{hwnd: hwnd, value: cloneProgress(value), mode: mode}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	if !t.started {
		t.started = true
		go t.run()
	}
	select {
	case t.updates <- update:
	default:
		select {
		case <-t.updates:
		default:
		}
		t.updates <- update
	}
}

func (t *nativeTaskbar) Close() {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	started := t.started
	if started {
		close(t.stop)
	}
	t.mu.Unlock()
	if started {
		<-t.done
	}
}

func (t *nativeTaskbar) run() {
	defer close(t.done)
	gort.LockOSThread()
	defer gort.UnlockOSThread()

	_, err := win.CoInitializeEx(co.COINIT_APARTMENTTHREADED | co.COINIT_DISABLE_OLE1DDE)
	if err != nil {
		t.reportError(fmt.Errorf("initialize COM for taskbar progress: %w", err))
		return
	}
	defer win.CoUninitialize()

	releaser := win.NewOleReleaser()
	defer releaser.Release()
	var taskbar *winsh.ITaskbarList3
	if err := win.CoCreateInstance(
		releaser,
		&cosh.CLSID_TaskbarList,
		nil,
		co.CLSCTX_INPROC_SERVER,
		&taskbar,
	); err != nil {
		t.reportError(fmt.Errorf("create Windows taskbar list: %w", err))
		return
	}
	if err := taskbar.HrInit(); err != nil {
		t.reportError(fmt.Errorf("initialize Windows taskbar list: %w", err))
		return
	}

	var lastHWND win.HWND
	defer func() {
		if lastHWND != 0 {
			_ = taskbar.SetProgressState(lastHWND, cosh.TBPF_NOPROGRESS)
		}
	}()
	lastError := ""
	for {
		select {
		case <-t.stop:
			return
		case update := <-t.updates:
			hwnd := win.HWND(update.hwnd)
			if lastHWND != 0 && lastHWND != hwnd {
				_ = taskbar.SetProgressState(lastHWND, cosh.TBPF_NOPROGRESS)
			}
			lastHWND = hwnd
			err := applyTaskbarUpdate(taskbar, hwnd, update.value, update.mode)
			if err == nil {
				lastError = ""
				continue
			}
			message := err.Error()
			if message != lastError {
				t.reportError(err)
				lastError = message
			}
		}
	}
}

func (t *nativeTaskbar) reportError(err error) {
	if t.report != nil {
		t.report(err)
	}
}

func applyTaskbarUpdate(taskbar *winsh.ITaskbarList3, hwnd win.HWND, value *float64, mode string) error {
	if value == nil {
		return taskbar.SetProgressState(hwnd, cosh.TBPF_NOPROGRESS)
	}
	progress := *value
	if math.IsNaN(progress) || math.IsInf(progress, 0) {
		progress = 0
	}
	progress = max(0, min(progress, 100))
	completed := int(math.Round(progress * taskbarProgressTotal / 100))
	if err := taskbar.SetProgressValue(hwnd, completed, taskbarProgressTotal); err != nil {
		return fmt.Errorf("set taskbar progress value: %w", err)
	}
	if err := taskbar.SetProgressState(hwnd, taskbarProgressFlag(mode)); err != nil {
		return fmt.Errorf("set taskbar progress state: %w", err)
	}
	return nil
}

func taskbarProgressFlag(mode string) cosh.TBPF {
	switch mode {
	case "indeterminate":
		return cosh.TBPF_INDETERMINATE
	case "paused":
		return cosh.TBPF_PAUSED
	case "error":
		return cosh.TBPF_ERROR
	default:
		return cosh.TBPF_NORMAL
	}
}
