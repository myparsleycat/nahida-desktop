//go:build windows

package mod

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/setting"
)

func TestWofCompressUsesXpress4KProviderStructure(t *testing.T) {
	path := writeCompressionTestFile(t, "payload.bin", []byte("payload"))
	previous := wofSetFileDataLocationCall
	t.Cleanup(func() { wofSetFileDataLocationCall = previous })
	called := false
	wofSetFileDataLocationCall = func(_ windows.Handle, provider uint32, info *wofCompressionInfoV1) uintptr {
		called = true
		if provider != wofProviderFile || info.Algorithm != fileProviderCompressionXpress4K || info.Flags != 0 {
			t.Fatalf("provider=%d info=%+v", provider, *info)
		}
		return 0
	}
	if err := wofCompress(path); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("WofSetFileDataLocation was not called")
	}
}

func TestWofCompressionNotBeneficialIsAFileError(t *testing.T) {
	path := writeCompressionTestFile(t, "payload.bin", []byte("payload"))
	previous := wofSetFileDataLocationCall
	t.Cleanup(func() { wofSetFileDataLocationCall = previous })
	wofSetFileDataLocationCall = func(windows.Handle, uint32, *wofCompressionInfoV1) uintptr {
		return hresultCompressionNotBeneficial
	}
	if err := wofCompress(path); !errors.Is(err, errCompressionNotBeneficial) {
		t.Fatalf("err = %v", err)
	}
}

func TestCompactGUIDefaultSkipExtensions(t *testing.T) {
	for _, extension := range []string{".jpg", ".mp4", ".zip", ".zst", ".nzst"} {
		if _, ok := xpressSkippedExtensions[extension]; !ok {
			t.Fatalf("missing CompactGUI skip extension %q", extension)
		}
	}
}

func TestXpressCompressionFilesScansOnceAndCachesClusterSize(t *testing.T) {
	root := t.TempDir()
	large := writeCompressionTestFileAt(t, root, "large.bin", bytes.Repeat([]byte{'a'}, 8192))
	writeCompressionTestFileAt(t, root, "small.bin", bytes.Repeat([]byte{'b'}, 4096))
	writeCompressionTestFileAt(t, root, "skipped.zip", bytes.Repeat([]byte{'c'}, 8192))
	writeCompressionTestFileAt(t, root, "download.dds.ntmp", bytes.Repeat([]byte{'d'}, 8192))

	previous := volumeClusterSizeCall
	t.Cleanup(func() { volumeClusterSizeCall = previous })
	var calls atomic.Int32
	volumeClusterSizeCall = func(string) (uint32, error) {
		calls.Add(1)
		return 4096, nil
	}

	files, err := xpressCompressionFiles(t.Context(), []string{root}, &compressionDeclinedFiles{})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].path != large {
		t.Fatalf("files = %+v", files)
	}
	if calls.Load() != 1 {
		t.Fatalf("cluster size calls = %d, want 1", calls.Load())
	}
}

func TestXpressCompressionFilesSkipUnaddressableNames(t *testing.T) {
	root := t.TempDir()
	kept := writeCompressionTestFileAt(t, root, "kept.bin", bytes.Repeat([]byte{'a'}, 8192))
	extended := `\\?\` + root
	if err := os.Mkdir(extended+`\Loading...`, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{`Loading...\inner.bin`, `trailing.`, `space `} {
		if err := os.WriteFile(extended+`\`+name, bytes.Repeat([]byte{'b'}, 8192), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = os.RemoveAll(extended) })
	previous := volumeClusterSizeCall
	t.Cleanup(func() { volumeClusterSizeCall = previous })
	volumeClusterSizeCall = func(string) (uint32, error) { return 4096, nil }

	files, err := xpressCompressionFiles(t.Context(), []string{root}, &compressionDeclinedFiles{})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].path != kept {
		t.Fatalf("files = %+v", files)
	}
}

func TestApplyXpress4KSkipsWofBackedFilesBeforeTotals(t *testing.T) {
	root := t.TempDir()
	backed := writeCompressionTestFileAt(t, root, "backed.dds", bytes.Repeat([]byte{'x'}, 8192))
	plain := writeCompressionTestFileAt(t, root, "plain.dds", bytes.Repeat([]byte{'y'}, 8192))
	stubXpressCalls(t, func(path string) bool { return path == backed })
	var calls atomic.Int32
	wofSetFileDataLocationCall = func(windows.Handle, uint32, *wofCompressionInfoV1) uintptr {
		calls.Add(1)
		return 0
	}
	var total atomic.Int32
	var mu sync.Mutex
	var marked []string

	err := applyXpress4K(t.Context(), []string{root}, func(files int, _ int64) {
		total.Store(int32(files))
	}, func(string, int64, bool) {}, &compressionFileOwnership{}, &compressionDeclinedFiles{}, func(paths ...string) {
		mu.Lock()
		marked = append(marked, paths...)
		mu.Unlock()
	}, func(_ string, err error) { t.Error(err) })
	if err != nil {
		t.Fatal(err)
	}
	if total.Load() != 1 || calls.Load() != 1 {
		t.Fatalf("total=%d calls=%d, want 1 each", total.Load(), calls.Load())
	}
	if !slices.Equal(marked, []string{plain, plain}) {
		t.Fatalf("marked = %v, want the compressed file before and after WOF", marked)
	}
}

func TestApplyXpress4KRemembersNotBeneficialFilesUntilTheyChange(t *testing.T) {
	root := t.TempDir()
	path := writeCompressionTestFileAt(t, root, "README.txt", bytes.Repeat([]byte{'x'}, 8192))
	stubXpressCalls(t, func(string) bool { return false })
	var calls atomic.Int32
	wofSetFileDataLocationCall = func(windows.Handle, uint32, *wofCompressionInfoV1) uintptr {
		calls.Add(1)
		return hresultCompressionNotBeneficial
	}
	declined := compressionDeclinedFiles{}
	run := func() {
		t.Helper()
		if err := applyXpress4K(
			t.Context(), []string{root}, func(int, int64) {}, func(string, int64, bool) {},
			&compressionFileOwnership{}, &declined, ignoreCompressionMutations,
			func(_ string, err error) { t.Errorf("file error: %v", err) },
		); err != nil {
			t.Fatal(err)
		}
	}

	run()
	run()
	if calls.Load() != 1 {
		t.Fatalf("WOF calls = %d, want 1 while the file is unchanged", calls.Load())
	}

	modified := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
	run()
	if calls.Load() != 2 {
		t.Fatalf("WOF calls = %d, want a retry after the file changed", calls.Load())
	}
}

func TestApplyXpress4KSkipsFilesOpenForWriting(t *testing.T) {
	root := t.TempDir()
	path := writeCompressionTestFileAt(t, root, "BODY.dds", bytes.Repeat([]byte{'x'}, 8192))
	writer, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	stubXpressCalls(t, func(string) bool { return false })
	wofSetFileDataLocationCall = func(windows.Handle, uint32, *wofCompressionInfoV1) uintptr {
		t.Error("a file open for writing must not be compressed")
		return 0
	}

	if err := applyXpress4K(
		t.Context(), []string{root}, func(int, int64) {}, func(string, int64, bool) {},
		&compressionFileOwnership{}, &compressionDeclinedFiles{}, ignoreCompressionMutations,
		func(_ string, err error) { t.Errorf("file error: %v", err) },
	); err != nil {
		t.Fatal(err)
	}
}

// A texture whose WOF call outlives the self-change window used to raise a
// watcher event that started another pass over the same folder, forever.
func TestXpressSlowCompressionDoesNotRetriggerItself(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	importer := filepath.Join(base, "Importer")
	path := writeCompressionTestFileAt(
		t, filepath.Join(importer, "Mods", "Character", "Mod"), "BODY.dds", bytes.Repeat([]byte("texture"), 256*1024),
	)
	previous := wofSetFileDataLocationCall
	t.Cleanup(func() { wofSetFileDataLocationCall = previous })
	var calls atomic.Int32
	wofSetFileDataLocationCall = func(handle windows.Handle, provider uint32, info *wofCompressionInfoV1) uintptr {
		calls.Add(1)
		time.Sleep(compressionSelfChangeTTL + 500*time.Millisecond)
		return nativeWofSetFileDataLocation(handle, provider, info)
	}
	settings, err := setting.Open(ctx, filepath.Join(base, "compression.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.SetCompressionConfig(ctx, "xpress4k", 1); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetCompressionEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	m := NewWithOptions(Options{
		Settings: settings,
		XXMI:     compressionImporterSource{{Key: "A", ImporterFolder: importer}},
	})
	m.UseClient(settings.Client())
	t.Cleanup(func() {
		_ = m.ServiceShutdown()
		_ = wofDecompress(path)
		_ = settings.Close()
	})

	if err := m.StartCompression(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.WaitCompressionPass(ctx); err != nil {
		t.Fatal(err)
	}
	if external, _, _, err := nativeWofState(path); err != nil || !external {
		t.Skipf("WOF compression is unavailable on this test volume: external=%v err=%v", external, err)
	}
	time.Sleep(compressionWatchDebounce + time.Second)
	waitForCompression(t, m.compression)

	if calls.Load() != 1 {
		t.Fatalf("WOF calls = %d, want 1", calls.Load())
	}
}

// stubXpressCalls fakes the cluster size and WOF state; the caller replaces
// wofSetFileDataLocationCall, which is restored with the others.
func stubXpressCalls(t *testing.T, backed func(string) bool) {
	t.Helper()
	previousCluster := volumeClusterSizeCall
	previousWof := wofSetFileDataLocationCall
	previousState := wofStateCall
	t.Cleanup(func() {
		volumeClusterSizeCall = previousCluster
		wofSetFileDataLocationCall = previousWof
		wofStateCall = previousState
	})
	volumeClusterSizeCall = func(string) (uint32, error) { return 4096, nil }
	wofStateCall = func(path string) (bool, uint32, uint32, error) {
		return backed(path), wofProviderFile, fileProviderCompressionXpress4K, nil
	}
}

func TestApplyXpress4KCallsEveryEligibleFileOnceAndContinuesAfterFailure(t *testing.T) {
	root := t.TempDir()
	const count = 72
	for index := range count {
		writeCompressionTestFileAt(t, root, filepath.Join("nested", fileName(index)), bytes.Repeat([]byte{'x'}, 8192))
	}
	previousCluster := volumeClusterSizeCall
	previousWof := wofSetFileDataLocationCall
	previousState := wofStateCall
	t.Cleanup(func() {
		volumeClusterSizeCall = previousCluster
		wofSetFileDataLocationCall = previousWof
		wofStateCall = previousState
	})
	volumeClusterSizeCall = func(string) (uint32, error) { return 4096, nil }
	wofStateCall = func(string) (bool, uint32, uint32, error) { return false, 0, 0, nil }
	var calls atomic.Int32
	wofSetFileDataLocationCall = func(windows.Handle, uint32, *wofCompressionInfoV1) uintptr {
		switch calls.Add(1) {
		case 1:
			return hresultCompressionNotBeneficial
		case 2:
			return 0x80070005
		}
		return 0
	}
	var total, processed atomic.Int32
	var fileErrors atomic.Int32
	ownership := compressionFileOwnership{}
	declined := compressionDeclinedFiles{}
	err := applyXpress4K(context.Background(), []string{root}, func(files int, _ int64) {
		total.Store(int32(files))
	}, func(string, int64, bool) {
		processed.Add(1)
	}, &ownership, &declined, ignoreCompressionMutations, func(string, error) {
		fileErrors.Add(1)
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != count || processed.Load() != count || total.Load() != count {
		t.Fatalf("calls=%d processed=%d total=%d", calls.Load(), processed.Load(), total.Load())
	}
	if fileErrors.Load() != 1 || len(declined.files) != 1 {
		t.Fatalf("file errors = %d declined = %d, want 1 each", fileErrors.Load(), len(declined.files))
	}
}

func TestWofOwnershipPersistsAcrossReconciliation(t *testing.T) {
	root := t.TempDir()
	writeCompressionTestFileAt(t, root, "payload.bin", bytes.Repeat([]byte{'x'}, 8192))
	previousCluster := volumeClusterSizeCall
	previousWof := wofSetFileDataLocationCall
	previousState := wofStateCall
	previousDelete := deleteExternalBackingCall
	t.Cleanup(func() {
		volumeClusterSizeCall = previousCluster
		wofSetFileDataLocationCall = previousWof
		wofStateCall = previousState
		deleteExternalBackingCall = previousDelete
	})
	volumeClusterSizeCall = func(string) (uint32, error) { return 4096, nil }
	var external atomic.Bool
	wofStateCall = func(string) (bool, uint32, uint32, error) {
		return external.Load(), wofProviderFile, fileProviderCompressionXpress4K, nil
	}
	wofSetFileDataLocationCall = func(windows.Handle, uint32, *wofCompressionInfoV1) uintptr {
		external.Store(true)
		return 0
	}
	ownership := compressionFileOwnership{}
	for range 2 {
		if err := applyXpress4K(
			context.Background(),
			[]string{root},
			func(int, int64) {},
			func(string, int64, bool) {},
			&ownership,
			&compressionDeclinedFiles{},
			ignoreCompressionMutations,
			func(_ string, err error) { t.Error(err) },
		); err != nil {
			t.Fatal(err)
		}
	}
	var deleted atomic.Int32
	deleteExternalBackingCall = func(windows.Handle) error {
		deleted.Add(1)
		return nil
	}
	if err := restoreWOF(
		context.Background(), []string{root}, func(int, int64) {}, func(string, int64, bool) {},
		&ownership, ignoreCompressionMutations, func(_ string, err error) { t.Error(err) },
	); err != nil {
		t.Fatal(err)
	}
	if deleted.Load() != 1 {
		t.Fatalf("deleted backing count = %d, want 1", deleted.Load())
	}
}

func TestXpressWorkersHaveNoSixtyFourFileBatchBarrier(t *testing.T) {
	workerCount := min(xpressWorkerCount(runtime.GOMAXPROCS(0)), 80)
	if workerCount < 2 {
		t.Skip("parallel worker test requires at least two workers")
	}
	files := make([]compressionFile, 80)
	for index := range files {
		files[index] = compressionFile{path: fileName(index), size: 1}
	}
	release := make(chan struct{})
	started := make(chan struct{}, workerCount)
	var peak, active, began atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- runXpressWorkers(context.Background(), files, func(string, int64, bool) {}, func(string, error) {}, func(compressionFile) error {
			current := active.Add(1)
			for {
				old := peak.Load()
				if current <= old || peak.CompareAndSwap(old, current) {
					break
				}
			}
			if began.Add(1) <= int32(workerCount) {
				started <- struct{}{}
			}
			<-release
			active.Add(-1)
			return nil
		})
	}()
	for range workerCount {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("workers did not fill the continuous pool")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if peak.Load() != int32(workerCount) {
		t.Fatalf("peak workers = %d, want %d", peak.Load(), workerCount)
	}
}

func TestRestoreWOFInspectsEveryRegularFileAndDeletesOnlyOwnedBacking(t *testing.T) {
	root := t.TempDir()
	paths := map[string]string{}
	for _, name := range []string{"plain.bin", "xpress.bin", "wim.bin", "lzx.bin"} {
		paths[name] = writeCompressionTestFileAt(t, root, name, []byte("payload"))
	}
	previousState := wofStateCall
	previousDelete := deleteExternalBackingCall
	t.Cleanup(func() {
		wofStateCall = previousState
		deleteExternalBackingCall = previousDelete
	})
	var inspected atomic.Int32
	wofStateCall = func(path string) (bool, uint32, uint32, error) {
		inspected.Add(1)
		switch filepath.Base(path) {
		case "xpress.bin":
			return true, wofProviderFile, fileProviderCompressionXpress4K, nil
		case "lzx.bin":
			return true, wofProviderFile, 3, nil
		case "wim.bin":
			return true, 1, 0, nil
		default:
			return false, 0, 0, nil
		}
	}
	var deleted atomic.Int32
	deleteExternalBackingCall = func(windows.Handle) error {
		deleted.Add(1)
		return nil
	}
	var total, processed atomic.Int32
	ownership := compressionFileOwnership{}
	addTestWofOwnership(t, &ownership, paths["xpress.bin"])
	if err := restoreWOF(context.Background(), []string{root}, func(files int, _ int64) {
		total.Store(int32(files))
	}, func(string, int64, bool) {
		processed.Add(1)
	}, &ownership, ignoreCompressionMutations, func(string, error) {}); err != nil {
		t.Fatal(err)
	}
	if inspected.Load() != 4 || total.Load() != 1 || processed.Load() != 1 || deleted.Load() != 1 {
		t.Fatalf(
			"inspected=%d total=%d processed=%d deleted=%d",
			inspected.Load(),
			total.Load(),
			processed.Load(),
			deleted.Load(),
		)
	}
}

func TestRestoreWOFContinuesAfterInspectionAndDeleteErrors(t *testing.T) {
	root := t.TempDir()
	writeCompressionTestFileAt(t, root, "inspect-error.bin", []byte("payload"))
	deletePath := writeCompressionTestFileAt(t, root, "delete-error.bin", []byte("payload"))
	successPath := writeCompressionTestFileAt(t, root, "success.bin", []byte("payload"))
	previousState := wofStateCall
	previousDelete := deleteExternalBackingCall
	t.Cleanup(func() {
		wofStateCall = previousState
		deleteExternalBackingCall = previousDelete
	})
	wofStateCall = func(path string) (bool, uint32, uint32, error) {
		if filepath.Base(path) == "inspect-error.bin" {
			return false, 0, 0, errors.New("inspect")
		}
		return true, wofProviderFile, 0, nil
	}
	var deleted atomic.Int32
	deleteExternalBackingCall = func(windows.Handle) error {
		if deleted.Add(1) == 1 {
			return errors.New("delete")
		}
		return nil
	}
	var processed, fileErrors atomic.Int32
	ownership := compressionFileOwnership{}
	addTestWofOwnership(t, &ownership, deletePath, successPath)
	deleteID := testFileIdentity(t, deletePath)
	successID := testFileIdentity(t, successPath)
	if err := restoreWOF(context.Background(), []string{root}, func(int, int64) {}, func(string, int64, bool) {
		processed.Add(1)
	}, &ownership, ignoreCompressionMutations, func(string, error) {
		fileErrors.Add(1)
	}); err != nil {
		t.Fatal(err)
	}
	if processed.Load() != 2 || fileErrors.Load() != 2 || deleted.Load() != 2 {
		t.Fatalf("processed=%d errors=%d delete attempts=%d", processed.Load(), fileErrors.Load(), deleted.Load())
	}
	if !ownership.contains(deleteID) {
		t.Fatal("delete-error identity was removed")
	}
	if ownership.contains(successID) {
		t.Fatal("success identity remains")
	}
}

func TestRestoreWOFRejectsChangedIdentity(t *testing.T) {
	root := t.TempDir()
	keptPath := writeCompressionTestFileAt(t, root, "kept.bin", []byte("payload"))
	replacedPath := writeCompressionTestFileAt(t, root, "replaced.bin", []byte("payload"))
	previousState := wofStateCall
	previousDelete := deleteExternalBackingCall
	previousIdentity := fileIdentityCall
	t.Cleanup(func() {
		wofStateCall = previousState
		deleteExternalBackingCall = previousDelete
		fileIdentityCall = previousIdentity
	})
	wofStateCall = func(string) (bool, uint32, uint32, error) {
		return true, wofProviderFile, 0, nil
	}
	ownership := compressionFileOwnership{}
	addTestWofOwnership(t, &ownership, keptPath, replacedPath)
	keptID := testFileIdentity(t, keptPath)
	replacedID := testFileIdentity(t, replacedPath)
	var replacedIdentifies atomic.Int32
	fileIdentityCall = func(handle windows.Handle) (string, error) {
		id, err := previousIdentity(handle)
		if err != nil {
			return "", err
		}
		if id == replacedID && replacedIdentifies.Add(1) > 1 {
			return id + "-replaced", nil
		}
		return id, nil
	}
	var deleted atomic.Int32
	deleteExternalBackingCall = func(windows.Handle) error {
		deleted.Add(1)
		return nil
	}
	var marked []string
	var processed, fileErrors atomic.Int32
	if err := restoreWOF(context.Background(), []string{root}, func(int, int64) {}, func(string, int64, bool) {
		processed.Add(1)
	}, &ownership, func(paths ...string) {
		marked = append(marked, paths...)
	}, func(string, error) {
		fileErrors.Add(1)
	}); err != nil {
		t.Fatal(err)
	}
	if processed.Load() != 2 || fileErrors.Load() != 1 || deleted.Load() != 1 {
		t.Fatalf("processed=%d errors=%d deleted=%d", processed.Load(), fileErrors.Load(), deleted.Load())
	}
	if !ownership.contains(replacedID) {
		t.Fatal("replaced identity was removed")
	}
	if ownership.contains(keptID) {
		t.Fatal("unchanged identity remains")
	}
	for _, path := range marked {
		if filepath.Base(path) == filepath.Base(replacedPath) {
			t.Fatal("replaced file was marked")
		}
	}
}

func TestRestoreWOFRestoresWorkInPathOrder(t *testing.T) {
	root := t.TempDir()
	names := []string{"c-restore.bin", "a-restore.bin", "b-restore.bin"}
	var paths []string
	for _, name := range names {
		paths = append(paths, writeCompressionTestFileAt(t, root, name, []byte("payload")))
	}
	previousState := wofStateCall
	previousDelete := deleteExternalBackingCall
	t.Cleanup(func() {
		wofStateCall = previousState
		deleteExternalBackingCall = previousDelete
	})
	wofStateCall = func(string) (bool, uint32, uint32, error) {
		return true, wofProviderFile, fileProviderCompressionXpress4K, nil
	}
	deleteExternalBackingCall = func(windows.Handle) error { return nil }
	ownership := compressionFileOwnership{}
	addTestWofOwnership(t, &ownership, paths...)
	var mu sync.Mutex
	var order []string
	var total atomic.Int32
	if err := restoreWOF(context.Background(), []string{root}, func(files int, _ int64) {
		total.Store(int32(files))
	}, func(path string, _ int64, _ bool) {
		mu.Lock()
		order = append(order, filepath.Base(path))
		mu.Unlock()
	}, &ownership, ignoreCompressionMutations, func(string, error) {
		t.Error("restore should not report file errors")
	}); err != nil {
		t.Fatal(err)
	}
	if total.Load() != int32(len(paths)) {
		t.Fatalf("total=%d", total.Load())
	}
	want := []string{"a-restore.bin", "b-restore.bin", "c-restore.bin"}
	if !slices.Equal(order, want) {
		t.Fatalf("order=%v want %v", order, want)
	}
}

func TestRestoreWOFSkipsScanWhenOwnershipIsEmpty(t *testing.T) {
	root := t.TempDir()
	writeCompressionTestFileAt(t, root, "plain.bin", []byte("payload"))
	previousState := wofStateCall
	t.Cleanup(func() { wofStateCall = previousState })
	var inspected atomic.Int32
	wofStateCall = func(string) (bool, uint32, uint32, error) {
		inspected.Add(1)
		return true, wofProviderFile, fileProviderCompressionXpress4K, nil
	}
	var total atomic.Int32
	if err := restoreWOF(context.Background(), []string{root}, func(files int, _ int64) {
		total.Store(int32(files))
	}, func(string, int64, bool) {
		t.Fatal("empty ownership should not report restore progress")
	}, &compressionFileOwnership{}, ignoreCompressionMutations, func(string, error) {
		t.Fatal("empty ownership should not inspect files")
	}); err != nil {
		t.Fatal(err)
	}
	if inspected.Load() != 0 || total.Load() != 0 {
		t.Fatalf("inspected=%d total=%d", inspected.Load(), total.Load())
	}
}

func TestWofCompressionSucceedsWithReadOnlyHandleWhenSupported(t *testing.T) {
	path := writeCompressionTestFile(t, "real-wof.bin", bytes.Repeat([]byte("read-only-wof"), 64*1024))
	if err := wofCompress(path); err != nil {
		if errors.Is(err, windows.ERROR_INVALID_FUNCTION) || errors.Is(err, windows.ERROR_NOT_SUPPORTED) {
			t.Skipf("WOF compression is unavailable on this test volume: %v", err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := wofDecompress(path); err != nil {
			t.Errorf("decompress WOF test file: %v", err)
		}
		external, _, _, err := nativeWofState(path)
		if err != nil {
			t.Errorf("inspect WOF state after cleanup: %v", err)
			return
		}
		if external {
			t.Error("WOF test file remains externally backed after cleanup")
		}
	})
	external, provider, algorithm, err := nativeWofState(path)
	if err != nil {
		t.Fatal(err)
	}
	if !external || provider != wofProviderFile || algorithm != fileProviderCompressionXpress4K {
		t.Fatalf("external=%v provider=%d algorithm=%d", external, provider, algorithm)
	}
}

func addTestWofOwnership(t *testing.T, ownership *compressionFileOwnership, paths ...string) {
	t.Helper()
	for _, path := range paths {
		ownership.add(testFileIdentity(t, path))
	}
}

func testFileIdentity(t *testing.T, path string) string {
	t.Helper()
	handle, err := openXpressFile(path)
	if err != nil {
		t.Fatal(err)
	}
	id, identityErr := fileIdentityCall(handle)
	closeErr := windows.CloseHandle(handle)
	if identityErr != nil {
		t.Fatal(identityErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	return id
}

func writeCompressionTestFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	return writeCompressionTestFileAt(t, t.TempDir(), name, data)
}

func writeCompressionTestFileAt(t *testing.T, root, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func fileName(index int) string {
	const digits = "0123456789"
	return "payload-" + string(digits[index/10]) + string(digits[index%10]) + ".bin"
}
