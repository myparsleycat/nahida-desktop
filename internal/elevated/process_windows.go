//go:build windows

package elevated

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sys/windows"
)

var errHelperAlreadyRunning = errors.New("elevated helper is already running for this user")

const helperShutdownGrace = 5 * time.Second

// A cancellation is normally silent at the application boundary. Failed
// cleanup must still be reported, so only the cleanup error is unwrapped then.
func helperStartupError(startErr, cleanupErr error) error {
	if cleanupErr != nil && errors.Is(startErr, context.Canceled) {
		return fmt.Errorf("cleanup cancelled elevated helper startup (%s): %w", startErr.Error(), cleanupErr)
	}
	return errors.Join(startErr, cleanupErr)
}

// checkHelperInstance avoids another UAC prompt for a helper owned by another
// client. The server also reserves the mutex atomically to cover startup races.
func checkHelperInstance(userSID string) error {
	name, err := windows.UTF16PtrFromString(`Local\nahida-elevated-` + userSID)
	if err != nil {
		return fmt.Errorf("encode elevated helper mutex: %w", err)
	}
	handle, err := windows.OpenMutex(windows.SYNCHRONIZE, false, name)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return nil
	}
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		// A helper elevated using another administrator's credentials can have
		// an ACL that excludes the parent. Its reservation still blocks launch.
		return errors.Join(errHelperAlreadyRunning, err)
	}
	if err != nil {
		return fmt.Errorf("check elevated helper mutex: %w", err)
	}
	return errors.Join(errHelperAlreadyRunning, windows.CloseHandle(handle))
}

// lockHelperInstance reserves one helper for the verified parent's user in the
// current Windows session, including helpers from other application versions.
// Keep the handle open for the server lifetime; no thread owns the mutex.
func lockHelperInstance(userSID string) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(`Local\nahida-elevated-` + userSID)
	if err != nil {
		return 0, fmt.Errorf("encode elevated helper mutex: %w", err)
	}
	handle, err := windows.CreateMutex(nil, false, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return 0, errors.Join(errHelperAlreadyRunning, windows.CloseHandle(handle))
	}
	if err != nil {
		return 0, fmt.Errorf("create elevated helper mutex: %w", err)
	}
	return handle, nil
}

// stopHelperProcess first allows a stopped/disconnected server to exit normally.
// A hung server is terminated, and its handle is retained by the caller if exit
// cannot be confirmed. Closing a handle alone must never permit a replacement.
func stopHelperProcess(process windows.Handle) error {
	event, err := windows.WaitForSingleObject(process, uint32(helperShutdownGrace/time.Millisecond))
	if err != nil {
		return fmt.Errorf("wait for elevated helper exit: %w", err)
	}
	if event == windows.WAIT_OBJECT_0 {
		return nil
	}
	if event != uint32(windows.WAIT_TIMEOUT) {
		return fmt.Errorf("unexpected elevated helper wait result %d", event)
	}

	if err := windows.TerminateProcess(process, 1); err != nil {
		// Exit can race TerminateProcess, which then reports access denied.
		if event, waitErr := windows.WaitForSingleObject(process, 0); waitErr == nil && event == windows.WAIT_OBJECT_0 {
			return nil
		}
		return fmt.Errorf("terminate elevated helper: %w", err)
	}
	event, err = windows.WaitForSingleObject(process, 1000)
	if err != nil {
		return fmt.Errorf("wait for terminated elevated helper: %w", err)
	}
	if event != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("elevated helper exit was not confirmed: wait result %d", event)
	}
	return nil
}
