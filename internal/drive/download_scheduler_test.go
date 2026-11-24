package drive

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"nahida.live/desktop/internal/transfer"
)

const schedulerTestMiB = 1024 * 1024

func schedulerTestPlan(t *testing.T, small int, large []transfer.DownloadFile) *downloadPlan {
	t.Helper()
	files := make([]transfer.DownloadFile, 0, small+len(large))
	for index := range small {
		files = append(files, transfer.DownloadFile{ID: fmt.Sprintf("small-%d", index), Size: 1})
	}
	return spoolTestPlan(t, append(files, large...))
}

func largeTestFiles(sizes ...int64) []transfer.DownloadFile {
	files := make([]transfer.DownloadFile, len(sizes))
	for index, size := range sizes {
		files[index] = transfer.DownloadFile{ID: fmt.Sprintf("large-%d", index), Size: size}
	}
	return files
}

func compressedLargeTestFiles(count int) []transfer.DownloadFile {
	algorithm := "zstd"
	files := largeTestFiles(make([]int64, count)...)
	for index := range files {
		files[index].Size = parallelDownloadThreshold
		files[index].CompAlg = &algorithm
	}
	return files
}

func uncompressedLargeTestFiles(count int) []transfer.DownloadFile {
	sizes := make([]int64, count)
	for index := range sizes {
		sizes[index] = parallelDownloadThreshold + int64(index)*schedulerTestMiB
	}
	return largeTestFiles(sizes...)
}

// schedulerProbe takes files as fast as the scheduler allows and holds them
// until the test finishes them, so what it holds after synctest.Wait is what
// the lane limits admit.
type schedulerProbe struct {
	scheduler *downloadScheduler
	taken     chan scheduledDownload
	held      []scheduledDownload
	cancel    context.CancelFunc
}

func startSchedulerProbe(t *testing.T, plan *downloadPlan, budget int) *schedulerProbe {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	probe := &schedulerProbe{
		scheduler: newDownloadScheduler(plan, budget),
		taken:     make(chan scheduledDownload, plan.fileCount),
		cancel:    cancel,
	}
	go func() {
		defer close(probe.taken)
		for {
			item, ok := probe.scheduler.take(ctx)
			if !ok {
				return
			}
			probe.taken <- item
		}
	}()
	return probe
}

func (p *schedulerProbe) admitted() (small, large int) {
	synctest.Wait()
	for {
		select {
		case item, ok := <-p.taken:
			if !ok {
				return small, large
			}
			p.held = append(p.held, item)
			if item.lane == largeDownloadLane {
				large++
				continue
			}
			small++
		default:
			return small, large
		}
	}
}

func (p *schedulerProbe) finish(t *testing.T, lane downloadLane, count int) {
	t.Helper()
	for range count {
		index := slices.IndexFunc(p.held, func(item scheduledDownload) bool { return item.lane == lane })
		if index < 0 {
			t.Fatalf("no held file in lane %d to finish", lane)
		}
		p.scheduler.done(p.held[index])
		p.held = slices.Delete(p.held, index, index+1)
	}
}

func (p *schedulerProbe) stop(t *testing.T) {
	t.Helper()
	p.cancel()
	synctest.Wait()
	if _, open := <-p.taken; open {
		t.Fatal("take kept handing out files after cancellation")
	}
	p.scheduler.close()
}

func TestDownloadSchedulerLimitsEachLane(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		small      int
		large      []transfer.DownloadFile
		budget     int
		wantSmall  int
		wantLarge  int
		finishLane downloadLane
		finish     int
		thenSmall  int
		thenLarge  int
	}{
		{
			name: "mixed lanes split the budget", small: 40, large: uncompressedLargeTestFiles(6), budget: 32,
			wantSmall: 16, wantLarge: 2,
			finishLane: largeDownloadLane, finish: 1, thenLarge: 1,
		},
		{
			name: "large only takes the whole budget", large: uncompressedLargeTestFiles(6), budget: 32,
			wantLarge:  4,
			finishLane: largeDownloadLane, finish: 1, thenLarge: 1,
		},
		{
			name: "compressed large files count one connection each", large: compressedLargeTestFiles(40),
			budget: 32, wantLarge: 32,
			finishLane: largeDownloadLane, finish: 2, thenLarge: 2,
		},
		{
			name: "cheaper large files fill the room the next one does not fit",
			large: append(
				largeTestFiles(30*schedulerTestMiB, 30*schedulerTestMiB, 30*schedulerTestMiB),
				compressedLargeTestFiles(6)...),
			budget:     20,
			wantLarge:  6,
			finishLane: largeDownloadLane,
			finish:     1,
			thenLarge:  1,
		},
		{
			name: "small only takes the whole budget", small: 40, budget: 32,
			wantSmall:  32,
			finishLane: smallDownloadLane, finish: 3, thenSmall: 3,
		},
		{
			name: "drained small lane widens the large lane", small: 3, large: uncompressedLargeTestFiles(6),
			budget: 32, wantSmall: 3, wantLarge: 2,
			finishLane: smallDownloadLane, finish: 3, thenLarge: 2,
		},
		{
			name: "small lane borrows what a lone large file leaves", small: 40,
			large: uncompressedLargeTestFiles(1), budget: 32, wantSmall: 24, wantLarge: 1,
			finishLane: largeDownloadLane, finish: 1, thenSmall: 8,
		},
		{
			name: "a large file over the budget still runs alone", small: 40, large: uncompressedLargeTestFiles(2),
			budget: 4, wantSmall: 1, wantLarge: 1,
			finishLane: largeDownloadLane, finish: 1, thenLarge: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				probe := startSchedulerProbe(t, schedulerTestPlan(t, test.small, test.large), test.budget)

				small, large := probe.admitted()
				if small != test.wantSmall || large != test.wantLarge {
					t.Fatalf("admitted small=%d large=%d, want small=%d large=%d",
						small, large, test.wantSmall, test.wantLarge)
				}

				probe.finish(t, test.finishLane, test.finish)
				small, large = probe.admitted()
				if small != test.thenSmall || large != test.thenLarge {
					t.Fatalf("after finishing %d, admitted small=%d large=%d, want small=%d large=%d",
						test.finish, small, large, test.thenSmall, test.thenLarge)
				}
				probe.stop(t)
			})
		})
	}
}

func TestDownloadSchedulerHandsOutEveryFileOnceLargestFirst(t *testing.T) {
	t.Parallel()

	sizes := []int64{
		parallelDownloadThreshold, 90 * schedulerTestMiB, 40 * schedulerTestMiB, 300 * schedulerTestMiB,
	}
	plan := schedulerTestPlan(t, 5, largeTestFiles(sizes...))
	scheduler := newDownloadScheduler(plan, 32)
	defer scheduler.close()

	var indexes []int
	var largeSizes []int64
	for {
		item, ok := scheduler.take(t.Context())
		if !ok {
			break
		}
		indexes = append(indexes, item.index)
		if item.lane == largeDownloadLane {
			largeSizes = append(largeSizes, item.file.Size)
		}
		scheduler.done(item)
	}

	slices.Sort(indexes)
	if want := []int{0, 1, 2, 3, 4, 5, 6, 7, 8}; !slices.Equal(indexes, want) {
		t.Fatalf("indexes = %v, want %v", indexes, want)
	}
	if want := []int64{
		300 * schedulerTestMiB, 90 * schedulerTestMiB, 40 * schedulerTestMiB, parallelDownloadThreshold,
	}; !slices.Equal(largeSizes, want) {
		t.Fatalf("large sizes = %v, want %v", largeSizes, want)
	}
	if err := scheduler.spoolErr(); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadSchedulerStopsHandingOutFilesAfterSpoolError(t *testing.T) {
	t.Parallel()

	plan := schedulerTestPlan(t, 3, uncompressedLargeTestFiles(3))
	plan.spoolPath = filepath.Join(t.TempDir(), "missing"+downloadSpoolExt)
	scheduler := newDownloadScheduler(plan, 32)
	defer scheduler.close()

	// Two large files fill the large lane's half of the budget, so the next
	// take reads the spool and fails while both are still running. Nothing
	// finishes them meanwhile, so a take that waits for room would never
	// return without the deadline.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var held []scheduledDownload
	for {
		item, ok := scheduler.take(ctx)
		if !ok {
			break
		}
		held = append(held, item)
	}
	if ctx.Err() != nil {
		t.Fatal("take waited for room instead of stopping after the spool error")
	}
	if len(held) != 2 || held[0].lane != largeDownloadLane || held[1].lane != largeDownloadLane {
		t.Fatalf("handed out %+v, want the two largest files only", held)
	}
	if scheduler.spoolErr() == nil {
		t.Fatal("spool read failure was not reported")
	}
	for _, item := range held {
		scheduler.done(item)
	}
}
