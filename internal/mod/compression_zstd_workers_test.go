package mod

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestZstdWorkersFillPoolAndSerializeResults(t *testing.T) {
	for _, tc := range []struct{ files, workers int }{{0, 4}, {1, 4}, {2, 4}, {12, 3}, {3, 0}} {
		t.Run(fmt.Sprintf("files=%d/workers=%d", tc.files, tc.workers), func(t *testing.T) {
			files := make([]compressionFile, tc.files)
			for i := range files {
				files[i] = compressionFile{path: fmt.Sprint(i), size: int64(i + 1)}
			}
			started := make(chan struct{}, tc.files)
			release := make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			var active, peak atomic.Int32
			seen := map[string]int{}
			var total int64
			var failures int
			failure := errors.New("one file failed")
			done := make(chan error, 1)
			go func() {
				done <- runZstdWorkers(t.Context(), files, tc.workers, func(file compressionFile) error {
					current := active.Add(1)
					defer active.Add(-1)
					for old := peak.Load(); current > old; old = peak.Load() {
						if peak.CompareAndSwap(old, current) {
							break
						}
					}
					started <- struct{}{}
					<-release
					if file.path == "0" {
						return failure
					}
					return nil
				}, func(path string, size int64, _ bool) {
					// These deliberately unguarded values also check callback serialization under -race.
					seen[path]++
					total += size
				}, func(_ string, err error) {
					if !errors.Is(err, failure) {
						t.Errorf("error = %v", err)
					}
					failures++
				})
			}()
			want := min(tc.files, max(1, tc.workers))
			for range want {
				awaitZstdSignal(t, started)
			}
			if int(peak.Load()) != want {
				t.Fatalf("peak = %d, want %d", peak.Load(), want)
			}
			once.Do(func() { close(release) })
			if err := awaitZstdResult(t, done); err != nil {
				t.Fatal(err)
			}
			if active.Load() != 0 || len(seen) != tc.files || total != int64(tc.files*(tc.files+1)/2) {
				t.Fatalf("active=%d seen=%v bytes=%d", active.Load(), seen, total)
			}
			for path, count := range seen {
				if count != 1 {
					t.Fatalf("%s processed %d times", path, count)
				}
			}
			if failures != min(1, tc.files) {
				t.Fatalf("failures = %d", failures)
			}
		})
	}
}

func TestZstdWorkersContinuePastSlowFile(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	fastFinished := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- runZstdWorkers(t.Context(), []compressionFile{{path: "slow"}, {path: "fast"}, {path: "next"}}, 2, func(file compressionFile) error {
			if file.path == "slow" {
				<-release
			}
			if file.path == "next" {
				fastFinished <- struct{}{}
			}
			return nil
		}, func(string, int64, bool) {}, ignoreCompressionFileErrors)
	}()
	awaitZstdSignal(t, fastFinished)
	once.Do(func() { close(release) })
	if err := awaitZstdResult(t, done); err != nil {
		t.Fatal(err)
	}
}

func TestZstdWorkersCancellationDrainsAndWaits(t *testing.T) {
	for _, count := range []int{1, 8} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			files := make([]compressionFile, count)
			started := make(chan struct{}, 1)
			release := make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			var calls atomic.Int32
			var progress, failures int
			done := make(chan error, 1)
			go func() {
				done <- runZstdWorkers(ctx, files, 1, func(compressionFile) error {
					calls.Add(1)
					started <- struct{}{}
					<-ctx.Done()
					<-release
					return fmt.Errorf("copy: %w", ctx.Err())
				}, func(string, int64, bool) { progress++ }, func(string, error) { failures++ })
			}()
			awaitZstdSignal(t, started)
			cancel()
			select {
			case err := <-done:
				t.Fatalf("returned before worker exit: %v", err)
			default:
			}
			once.Do(func() { close(release) })
			if err := awaitZstdResult(t, done); !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v", err)
			}
			if calls.Load() != 1 || progress != 0 || failures != 0 {
				t.Fatalf("calls=%d progress=%d errors=%d", calls.Load(), progress, failures)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := runZstdWorkers(ctx, []compressionFile{{}}, 4, func(compressionFile) error { t.Error("started after cancellation"); return nil }, func(string, int64, bool) {}, ignoreCompressionFileErrors); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestZstdWorkersKeepSuccessfulResultDuringCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var processed int
	err := runZstdWorkers(ctx, []compressionFile{{}}, 1, func(compressionFile) error {
		cancel()
		return nil
	}, func(string, int64, bool) { processed++ }, func(string, error) { t.Error("unexpected file error") })
	if !errors.Is(err, context.Canceled) || processed != 1 {
		t.Fatalf("error=%v processed=%d", err, processed)
	}
}

func TestZstdParallelRoundTripDeduplicatesRoots(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "DISABLED Parallel")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	want := bytes.Repeat([]byte("parallel-round-trip"), 16384)
	infos := map[string]os.FileInfo{}
	for i := range 12 {
		path := filepath.Join(folder, fmt.Sprintf("%02d.bin", i))
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
		mtime := time.Unix(1_700_000_000+int64(i), 0)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		infos[path] = info
	}
	var totals, processed int
	var totalBytes, processedBytes int64
	setTotals := func(count int, size int64) { totals = count; totalBytes = size }
	progress := func(_ string, size int64, _ bool) { processed++; processedBytes += size }
	onError := func(path string, err error) { t.Errorf("%s: %v", path, err) }
	// The marker must remain safe even when the supplied callback is not concurrent.
	marks := map[string]int{}
	mark := func(paths ...string) {
		for _, path := range paths {
			marks[path]++
		}
	}
	roots := []string{folder, folder}
	if err := compressDisabledZstd(t.Context(), roots, 1, setTotals, progress, mark, onError); err != nil {
		t.Fatal(err)
	}
	if totals != 12 || processed != 12 || totalBytes != int64(12*len(want)) || processedBytes != totalBytes {
		t.Fatalf("compression totals=%d processed=%d bytes=%d/%d", totals, processed, processedBytes, totalBytes)
	}
	processed, processedBytes = 0, 0
	if err := restoreAllZstd(t.Context(), roots, setTotals, progress, mark, onError); err != nil {
		t.Fatal(err)
	}
	if totals != 12 || processed != 12 || processedBytes != totalBytes {
		t.Fatalf("restore totals=%d processed=%d bytes=%d/%d", totals, processed, processedBytes, totalBytes)
	}
	for path, info := range infos {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s content: %v", path, err)
		}
		after, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if after.Mode() != info.Mode() || !after.ModTime().Equal(info.ModTime()) {
			t.Fatalf("metadata changed: %s", path)
		}
		if _, err := os.Stat(path + managedZstdExtension); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("archive remains: %v", err)
		}
	}
}

func TestZstdRestoreNestedArchivesPreserveSerialOrder(t *testing.T) {
	folder := t.TempDir()
	path := filepath.Join(folder, "payload")
	want := bytes.Repeat([]byte("nested-archive"), 4096)
	if err := os.WriteFile(path, want, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := streamCompressZstd(t.Context(), path, path+".nzst"); err != nil {
		t.Fatal(err)
	}
	if err := streamCompressZstd(t.Context(), path+".nzst", path+".nzst.nzst"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	files := []compressionFile{{path: path + ".NZST"}, {path: path + ".nzst.nzst"}}
	if got := zstdRestoreWorkerCount(files, 8); got != 1 {
		t.Fatalf("workers = %d", got)
	}
	var order []string
	if err := restoreAllZstd(t.Context(), []string{folder}, func(int, int64) {}, func(path string, _ int64, _ bool) { order = append(order, filepath.Base(path)) }, ignoreCompressionMutations, func(path string, err error) { t.Errorf("%s: %v", path, err) }); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(order) != "[payload.nzst payload.nzst.nzst]" {
		t.Fatalf("order = %v", order)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("restored content: %v", err)
	}
}

func TestZstdCanceledStreamsPreserveInputAndCleanTemp(t *testing.T) {
	for _, restore := range []bool{false, true} {
		t.Run(fmt.Sprint(restore), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "payload")
			want := bytes.Repeat([]byte("cancellation"), 16384)
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
			if restore {
				if err := compressZstdFile(t.Context(), path, ignoreCompressionMutations); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			mark := func(paths ...string) {
				for _, p := range paths {
					if filepath.Ext(p) == compressionTempMarker {
						cancel()
					}
				}
			}
			input, temp := path, path+managedZstdExtension+compressionTempMarker
			var err error
			if restore {
				input, temp = path+managedZstdExtension, path+compressionTempMarker
				err = restoreZstdFile(ctx, input, maxZstdRestoreSize, mark)
			} else {
				err = compressZstdFile(ctx, path, mark)
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v", err)
			}
			if _, err := os.Stat(input); err != nil {
				t.Fatalf("input lost: %v", err)
			}
			if _, err := os.Stat(temp); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("temp remains: %v", err)
			}
		})
	}
}

func awaitZstdSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("worker signal timed out")
	}
}

func awaitZstdResult(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("workers did not finish")
		return nil
	}
}
