//go:build windows

package xxmi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotProcessNamesFindsCurrentProcess(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	processes, err := snapshotProcessNames(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !processes[strings.ToLower(filepath.Base(executable))] {
		t.Fatalf("snapshot did not include %s", executable)
	}
}

func TestSnapshotProcessNamesHonorsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := snapshotProcessNames(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
