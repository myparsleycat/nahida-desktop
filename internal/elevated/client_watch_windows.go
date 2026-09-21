//go:build windows

package elevated

import (
	"golang.org/x/sys/windows"
)

func (c *Client) watch(process windows.Handle, generation uint64) {
	if process == 0 {
		return
	}
	dup, err := duplicateHandle(process)
	if err != nil {
		return
	}
	stop, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		_ = windows.CloseHandle(dup)
		return
	}

	c.mu.Lock()
	if c.generation != generation || !c.wanted {
		c.mu.Unlock()
		_ = windows.CloseHandle(dup)
		_ = windows.CloseHandle(stop)
		return
	}
	c.signalWatchLocked()
	c.watchStop = stop
	c.mu.Unlock()

	go c.watchProcess(dup, stop, generation)
}

func (c *Client) watchProcess(process, stop windows.Handle, generation uint64) {
	defer func() {
		_ = windows.CloseHandle(process)
		_ = windows.CloseHandle(stop)
	}()

	event, err := windows.WaitForMultipleObjects([]windows.Handle{process, stop}, false, windows.INFINITE)
	if err != nil || event != uint32(windows.WAIT_OBJECT_0) {
		return
	}
	// WaitForMultipleObjects returns the first signaled handle. Close and
	// closeLocked signal stop while the process may already be exiting, so
	// treat a signaled stop as a requested teardown rather than an unexpected
	// drop of a later connection.
	if wait, waitErr := windows.WaitForSingleObject(stop, 0); waitErr == nil && wait == uint32(windows.WAIT_OBJECT_0) {
		return
	}
	c.dropUnexpected(generation)
}

func (c *Client) dropUnexpected(generation uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.generation != generation || !c.wanted {
		return
	}
	_ = c.closeLocked()
}

func (c *Client) signalWatchLocked() {
	if c.watchStop == 0 {
		return
	}
	_ = windows.SetEvent(c.watchStop)
	c.watchStop = 0
}

func duplicateHandle(handle windows.Handle) (windows.Handle, error) {
	var dup windows.Handle
	err := windows.DuplicateHandle(
		windows.CurrentProcess(),
		handle,
		windows.CurrentProcess(),
		&dup,
		0,
		false,
		windows.DUPLICATE_SAME_ACCESS,
	)
	return dup, err
}
