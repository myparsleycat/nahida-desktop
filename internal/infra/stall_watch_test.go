package infra

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestStallWatchLogsOnlyTheStageStillRunning(t *testing.T) {
	var output bytes.Buffer
	log := NewLogWithOptions(LogOptions{Writer: &output, DisableFile: true})
	watch := NewStallWatch(log, "Test", "install-importer-package", map[string]any{"importer": "ZZMI"})
	watch.warnAfter, watch.dumpAfter = time.Hour, time.Hour

	// A timer of a stage that has ended can fire while the next stage waits for the lock.
	watch.Stage("download")
	ended := watch.generation
	watch.Stage("pause-importer-watchers")
	watch.fire(ended, false)
	if output.Len() != 0 {
		t.Fatalf("ended stage was logged: %s", output.String())
	}

	watch.fire(watch.generation, false)
	watch.Stop()
	watch.fire(watch.generation-1, false)

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want stalled and finished: %s", len(lines), output.String())
	}
	for i, status := range []string{"stalled", "finished"} {
		for _, want := range []string{`"stage":"pause-importer-watchers"`, `"status":"` + status + `"`, `"importer":"ZZMI"`} {
			if !strings.Contains(lines[i], want) {
				t.Fatalf("line %d lacks %s: %s", i, want, lines[i])
			}
		}
	}
}

func TestSummarizeGoroutinesOrdersOthersByTheStagePackages(t *testing.T) {
	dump := `goroutine 1 [running]:
nahida.live/desktop/internal/infra.allGoroutineStacks()
	D:/a/desktop/internal/infra/stall_watch.go:130 +0x45

goroutine 5 [chan receive]:
nahida.live/desktop/internal/xxmi.(*XXMI).watchRunning(0xc000300)
	D:/a/desktop/internal/xxmi/running_state.go:80 +0x1a

goroutine 7 [syscall, 4 minutes]:
syscall.SyscallN(0x7ffb, {0xc000010, 0x2, 0x2})
	C:/Program Files/Go/src/runtime/syscall_windows.go:519 +0x46
nahida.live/desktop/internal/mod.(*compressionCoordinator).reconcile(0xc000123, {0x1, 0x2})
	D:/a/desktop/internal/mod/compression.go:452 +0x9c
created by nahida.live/desktop/internal/mod.(*compressionCoordinator).startPendingLocked in goroutine 1
	D:/a/desktop/internal/mod/compression.go:340 +0x45

goroutine 8 [select, 9 minutes]:
net/http.(*persistConn).writeLoop(0xc000200)
	C:/Program Files/Go/src/net/http/transport.go:2519 +0xe7

goroutine 9 [chan receive]:
nahida.live/desktop/internal/mod.(*compressionCoordinator).stop(0xc000123)
	D:/a/desktop/internal/mod/compression.go:722 +0x9c
nahida.live/desktop/internal/xxmi.(*XXMI).installImporterPackage(0xc000400, {0x1, 0x2})
	D:/a/desktop/internal/xxmi/install.go:112 +0x1a

goroutine 12 [running]:
	goroutine running on other thread; stack unavailable
`
	// The worker in the package the stage waits in comes first although the runtime lists it later.
	wantStage := "goroutine 9 [chan receive] | mod.(*compressionCoordinator).stop compression.go:722" +
		" | xxmi.(*XXMI).installImporterPackage install.go:112"
	wantOthers := []string{
		"goroutine 7 [syscall, 4 minutes] | syscall.SyscallN syscall_windows.go:519" +
			" | mod.(*compressionCoordinator).reconcile compression.go:452" +
			" | created by mod.(*compressionCoordinator).startPendingLocked in goroutine 1 compression.go:340",
		"goroutine 5 [chan receive] | xxmi.(*XXMI).watchRunning running_state.go:80",
	}
	stage, others := summarizeGoroutines(dump, "9")
	if stage != wantStage || !slices.Equal(others, wantOthers) {
		t.Fatalf("got %q and %q, want %q and %q", stage, others, wantStage, wantOthers)
	}

	if stage, _ := summarizeGoroutines(dump, "12"); stage != "goroutine 12 [running]" {
		t.Fatalf("running stage goroutine = %q", stage)
	}
}
