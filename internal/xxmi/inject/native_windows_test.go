//go:build windows

package inject

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestSnapshotNativeModulesRetriesTransientFailures(t *testing.T) {
	t.Parallel()
	for _, failure := range []windows.Errno{windows.ERROR_BAD_LENGTH, windows.ERROR_PARTIAL_COPY} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var calls int
			want := windows.Handle(123)
			snapshot, err := snapshotNativeModules(
				ctx,
				windows.CurrentProcess(),
				42,
				func(flags, pid uint32) (windows.Handle, error) {
					if flags != windows.TH32CS_SNAPMODULE || pid != 42 {
						t.Fatalf("snapshot arguments = %#x, %d", flags, pid)
					}
					calls++
					if calls < 3 {
						return windows.InvalidHandle, fmt.Errorf("loader is changing modules: %w", failure)
					}
					return want, nil
				},
			)
			if err != nil || snapshot != want || calls != 3 {
				t.Fatalf("snapshot = %v, err = %v, attempts = %d", snapshot, err, calls)
			}
		})
	}
}

func TestSnapshotNativeModulesDoesNotRetryPermanentFailure(t *testing.T) {
	t.Parallel()
	var calls int
	_, err := snapshotNativeModules(
		context.Background(),
		windows.CurrentProcess(),
		42,
		func(uint32, uint32) (windows.Handle, error) {
			calls++
			return windows.InvalidHandle, windows.ERROR_ACCESS_DENIED
		},
	)
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) || calls != 1 {
		t.Fatalf("snapshot err = %v, attempts = %d", err, calls)
	}
}

func TestSnapshotNativeModulesStopsOnCancellationAndDeadline(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"cancellation", "deadline"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := context.Canceled
			if name == "deadline" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 75*time.Millisecond)
				defer stop()
				want = context.DeadlineExceeded
			}
			var calls int
			_, err := snapshotNativeModules(
				ctx,
				windows.CurrentProcess(),
				42,
				func(uint32, uint32) (windows.Handle, error) {
					calls++
					if name == "cancellation" {
						cancel()
					}
					return windows.InvalidHandle, windows.ERROR_BAD_LENGTH
				},
			)
			if !errors.Is(err, want) {
				t.Fatalf("snapshot err = %v, want %v", err, want)
			}
			if name == "cancellation" && calls != 1 {
				t.Fatalf("snapshot retried after cancellation: %d attempts", calls)
			}
		})
	}
}

func TestSnapshotNativeModulesDoesNotStartAfterCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := snapshotNativeModules(ctx, windows.CurrentProcess(), 42, func(uint32, uint32) (windows.Handle, error) {
		t.Fatal("snapshot attempted after cancellation")
		return 0, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("snapshot err = %v", err)
	}
}

func TestSnapshotNativeModulesStopsWhenTargetExits(t *testing.T) {
	t.Parallel()
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = windows.CloseHandle(event) }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var calls int
	_, err = snapshotNativeModules(ctx, event, 42, func(uint32, uint32) (windows.Handle, error) {
		calls++
		if err := windows.SetEvent(event); err != nil {
			t.Fatal(err)
		}
		return windows.InvalidHandle, windows.ERROR_PARTIAL_COPY
	})
	if err == nil || err.Error() != "target process exited before module lookup" || calls != 1 {
		t.Fatalf("snapshot err = %v, attempts = %d", err, calls)
	}
}

func TestWaitNativeModuleFindsLoadedModule(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entry, err := waitNativeModule(ctx, windows.CurrentProcess(), windows.GetCurrentProcessId(), "KERNEL32.DLL")
	if err != nil || entry.ModBaseAddr == 0 {
		t.Fatalf("module base = %#x, err = %v", entry.ModBaseAddr, err)
	}
}

func TestWaitNativeModuleWaitsForMissingModuleUntilDeadline(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	_, err := waitNativeModule(ctx, windows.CurrentProcess(), windows.GetCurrentProcessId(), "nahida-missing-test.dll")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("missing module err = %v, want deadline exceeded", err)
	}
}

func TestNativeThreadWaitCancellationAndTimeout(t *testing.T) {
	t.Parallel()
	for _, cancel := range []bool{false, true} {
		name := "timeout"
		if cancel {
			name = "cancel"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			event, err := windows.CreateEvent(nil, 1, 0, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = windows.CloseHandle(event) }()
			ctx, stop := context.WithCancel(context.Background())
			defer stop()
			want := context.DeadlineExceeded
			if cancel {
				stop()
				want = context.Canceled
			}
			if err := waitNativeThread(ctx, event, time.Millisecond); !errors.Is(err, want) {
				t.Fatalf("wait error = %v, want %v", err, want)
			}
			if err := windows.SetEvent(event); err != nil {
				t.Fatal(err)
			}
			if err := waitNativeThread(context.Background(), event, time.Second); err != nil {
				t.Fatalf("completed wait failed: %v", err)
			}
		})
	}
}

func TestNativeInjectionHonorsCancellationBeforeOpeningProcess(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := injectNativeDLL(ctx, 0, "", time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled injection error = %v", err)
	}
}
