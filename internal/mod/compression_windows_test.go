//go:build windows

package mod

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/setting"
)

func TestWofCompressUsesXpress4KProviderStructure(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "wof-")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

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

func TestWofCompressionNotBeneficialIsDistinguished(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "wof-")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	previous := wofSetFileDataLocationCall
	t.Cleanup(func() { wofSetFileDataLocationCall = previous })
	wofSetFileDataLocationCall = func(windows.Handle, uint32, *wofCompressionInfoV1) uintptr {
		return hresultCompressionNotBeneficial
	}
	if err := wofCompress(path); !errors.Is(err, errCompressionNotBeneficial) {
		t.Fatalf("err = %v", err)
	}
}

func TestWofDecompressUsesDeleteExternalBacking(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "wof-")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	previous := deleteExternalBackingCall
	t.Cleanup(func() { deleteExternalBackingCall = previous })
	called := false
	deleteExternalBackingCall = func(windows.Handle) error {
		called = true
		return nil
	}
	if err := wofDecompress(path); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("FSCTL_DELETE_EXTERNAL_BACKING was not called")
	}
}

func TestDecompressExternalFileRemovesWofAndNTFSCompression(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "external-")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	previousState := wofStateCall
	previousAttributes := fileAttributesCall
	previousDelete := deleteExternalBackingCall
	previousSetCompression := setCompressionCall
	t.Cleanup(func() {
		wofStateCall = previousState
		fileAttributesCall = previousAttributes
		deleteExternalBackingCall = previousDelete
		setCompressionCall = previousSetCompression
	})
	wofStateCall = func(string) (bool, uint32, uint32, error) {
		return true, wofProviderFile, fileProviderCompressionXpress4K, nil
	}
	fileAttributesCall = func(string) (uint32, error) {
		return windows.FILE_ATTRIBUTE_COMPRESSED, nil
	}
	deleted := false
	deleteExternalBackingCall = func(windows.Handle) error {
		deleted = true
		return nil
	}
	compressionFormat := uint16(1)
	setCompressionCall = func(_ windows.Handle, format uint16) error {
		compressionFormat = format
		return nil
	}
	var marked []string
	if err := decompressExternalFile(path, func(paths ...string) { marked = append(marked, paths...) }); err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("WOF backing was not removed")
	}
	if compressionFormat != 0 {
		t.Fatalf("compression format = %d, want COMPRESSION_FORMAT_NONE", compressionFormat)
	}
	if len(marked) != 2 || marked[0] != path || marked[1] != path {
		t.Fatalf("marked paths = %v", marked)
	}
}

func TestExternalDecompressionRetriesRemainingFilesAndStaysDisabled(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	importer := filepath.Join(base, "Importer")
	root := filepath.Join(importer, "Mods")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(root, "first.bin"), filepath.Join(root, "second.bin")}
	for _, path := range paths {
		if err := os.WriteFile(path, make([]byte, 8192), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	settings, err := setting.Open(ctx, filepath.Join(base, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	m := NewWithOptions(Options{
		Settings: settings,
		XXMI:     compressionImporterSource{{Key: "A", ImporterFolder: importer}},
	})
	m.UseClient(settings.Client())
	t.Cleanup(func() {
		_ = m.ServiceShutdown()
		_ = settings.Close()
	})

	compressed := map[string]bool{paths[0]: true, paths[1]: true}
	previousState := wofStateCall
	previousAttributes := fileAttributesCall
	previousIdentity := fileIdentityCall
	previousSetCompression := setCompressionCall
	t.Cleanup(func() {
		wofStateCall = previousState
		fileAttributesCall = previousAttributes
		fileIdentityCall = previousIdentity
		setCompressionCall = previousSetCompression
	})
	wofStateCall = func(string) (bool, uint32, uint32, error) { return false, 0, 0, nil }
	fileAttributesCall = func(path string) (uint32, error) {
		if compressed[path] {
			return windows.FILE_ATTRIBUTE_COMPRESSED, nil
		}
		return 0, nil
	}
	fileIdentityCall = func(path string) (string, error) { return path, nil }
	call := 0
	setCompressionCall = func(windows.Handle, uint16) error {
		path := paths[call]
		call++
		if path == paths[1] {
			return errors.New("disk full")
		}
		compressed[path] = false
		return nil
	}

	if err := m.StartCompression(ctx); err != nil {
		t.Fatal(err)
	}
	waitForCompression(t, m.compression)
	state, err := m.GetCompressionState(ctx)
	if err != nil || state.Status != "blocked" || state.ExternalFiles != 2 || !state.CanDecompressExternal {
		t.Fatalf("blocked state = %+v, err=%v", state, err)
	}
	queued, err := m.DecompressExternalCompression(ctx)
	if err != nil || queued.Status != "decompressing" || queued.CanDecompressExternal {
		t.Fatalf("queued state = %+v, err=%v", queued, err)
	}
	waitForCompression(t, m.compression)
	state, err = m.GetCompressionState(ctx)
	if err != nil || state.Status != "blocked" || state.ExternalFiles != 1 || state.FailedFiles != 1 ||
		state.Error != "EXTERNAL_DECOMPRESSION_FAILED" || !state.CanDecompressExternal || state.Enabled {
		t.Fatalf("partial state = %+v, err=%v", state, err)
	}

	setCompressionCall = func(windows.Handle, uint16) error {
		compressed[paths[1]] = false
		return nil
	}
	if _, err := m.DecompressExternalCompression(ctx); err != nil {
		t.Fatal(err)
	}
	waitForCompression(t, m.compression)
	state, err = m.GetCompressionState(ctx)
	if err != nil || state.Status != "idle" || state.ExternalFiles != 0 || state.Error != "" || state.Enabled ||
		state.TargetEnabled != nil || state.CanDecompressExternal {
		t.Fatalf("completed state = %+v, err=%v", state, err)
	}
	if _, err := m.DecompressExternalCompression(ctx); err == nil || err.Error() != "EXTERNAL_DECOMPRESSION_LOCKED" {
		t.Fatalf("idle decompression error = %v", err)
	}
}

func TestCompactGUIDefaultSkipExtensions(t *testing.T) {
	for _, extension := range []string{".png", ".mp4", ".zip", ".docx", ".xz"} {
		if _, ok := xpressSkippedExtensions[extension]; !ok {
			t.Fatalf("missing CompactGUI default skip extension %q", extension)
		}
	}
}

func TestPlanXpress4KScansOnceAndCachesClusterSize(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	settings, err := setting.Open(ctx, filepath.Join(base, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = settings.Close() })
	root := filepath.Join(base, "mods")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, size := range map[string]int{
		"payload.bin": 8192, "image.png": 8192, "tiny.bin": 1024,
		"managed.bin": 8192, "foreign.bin": 8192,
	} {
		if err := os.WriteFile(filepath.Join(root, name), make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	managedPath := filepath.Join(root, "managed.bin")
	if err := saveWofLedger(ctx, settings.Client(), wofLedgerEntry{
		FileID: "managed", Path: filepath.Join(root, "old-managed.bin"), Provider: wofProviderFile,
		Algorithm: fileProviderCompressionXpress4K, State: "compressed",
	}); err != nil {
		t.Fatal(err)
	}

	previousCluster := volumeClusterSizeCall
	previousAttributes := fileAttributesCall
	previousState := wofStateCall
	previousIdentity := fileIdentityCall
	t.Cleanup(func() {
		volumeClusterSizeCall = previousCluster
		fileAttributesCall = previousAttributes
		wofStateCall = previousState
		fileIdentityCall = previousIdentity
	})
	var clusterCalls atomic.Int32
	volumeClusterSizeCall = func(string) (uint32, error) {
		clusterCalls.Add(1)
		return 4096, nil
	}
	fileAttributesCall = func(string) (uint32, error) { return 0, nil }
	wofStateCall = func(path string) (bool, uint32, uint32, error) {
		switch filepath.Base(path) {
		case "managed.bin":
			return true, wofProviderFile, fileProviderCompressionXpress4K, nil
		case "foreign.bin":
			return true, wofProviderFile, 1, nil
		default:
			return false, 0, 0, nil
		}
	}
	fileIdentityCall = func(path string) (string, error) {
		return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), nil
	}

	plan, err := planXpress4K(ctx, []string{root}, settings.Client())
	if err != nil {
		t.Fatal(err)
	}
	if clusterCalls.Load() != 1 {
		t.Fatalf("cluster calls = %d, want 1", clusterCalls.Load())
	}
	if len(plan.files) != 1 || filepath.Base(plan.files[0].path) != "payload.bin" {
		t.Fatalf("compression files = %+v", plan.files)
	}
	if len(plan.external) != 1 || filepath.Base(plan.external[0]) != "foreign.bin" {
		t.Fatalf("external files = %v", plan.external)
	}
	if len(plan.ledgerUpdates) != 1 || plan.ledgerUpdates[0].Path != managedPath {
		t.Fatalf("ledger updates = %+v", plan.ledgerUpdates)
	}
}

func TestApplyXpressBatchCommitsLedgerBeforeConcurrentCompression(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	settings, err := setting.Open(ctx, filepath.Join(base, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = settings.Close() })
	files := make([]compressionFile, 2)
	for index := range files {
		path := filepath.Join(base, fmt.Sprintf("payload-%d.bin", index))
		if err := os.WriteFile(path, make([]byte, 8192), 0o644); err != nil {
			t.Fatal(err)
		}
		files[index] = compressionFile{path: path, size: 8192}
	}

	previousAttributes := fileAttributesCall
	previousState := wofStateCall
	previousIdentity := fileIdentityFromHandleCall
	previousCompress := wofSetFileDataLocationCall
	t.Cleanup(func() {
		fileAttributesCall = previousAttributes
		wofStateCall = previousState
		fileIdentityFromHandleCall = previousIdentity
		wofSetFileDataLocationCall = previousCompress
	})
	fileAttributesCall = func(string) (uint32, error) { return 0, nil }
	wofStateCall = func(string) (bool, uint32, uint32, error) { return false, 0, 0, nil }
	var identityMu sync.Mutex
	handleIDs := map[windows.Handle]string{}
	identityIndex := 0
	fileIdentityFromHandleCall = func(handle windows.Handle) (string, error) {
		identityMu.Lock()
		defer identityMu.Unlock()
		id := fmt.Sprintf("id-%d", identityIndex)
		identityIndex++
		handleIDs[handle] = id
		return id, nil
	}
	started := make(chan struct{}, len(files))
	release := make(chan struct{})
	var active atomic.Int32
	var peak atomic.Int32
	var compressionErr error
	var compressionErrMu sync.Mutex
	wofSetFileDataLocationCall = func(handle windows.Handle, _ uint32, _ *wofCompressionInfoV1) uintptr {
		rows, err := settings.Client().AppState.ListByPrefix(ctx, compressionLedgerPrefix)
		if err != nil {
			compressionErrMu.Lock()
			compressionErr = fmt.Errorf("read pre-compression ledger: %w", err)
			compressionErrMu.Unlock()
		} else if len(rows) != len(files) {
			compressionErrMu.Lock()
			compressionErr = fmt.Errorf("pre-compression ledger rows = %d, want %d", len(rows), len(files))
			compressionErrMu.Unlock()
		}
		identityMu.Lock()
		_, hasIdentity := handleIDs[handle]
		identityMu.Unlock()
		if !hasIdentity {
			compressionErrMu.Lock()
			compressionErr = errors.New("compression did not reuse identity handle")
			compressionErrMu.Unlock()
		}
		current := active.Add(1)
		for {
			observed := peak.Load()
			if current <= observed || peak.CompareAndSwap(observed, current) {
				break
			}
		}
		started <- struct{}{}
		<-release
		active.Add(-1)
		return 0
	}

	done := make(chan error, 1)
	go func() {
		done <- applyXpressBatch(ctx, files, map[string]wofLedgerEntry{}, map[string]struct{}{}, settings.Client(), 2,
			func(string, int64, bool) {}, ignoreCompressionMutations)
	}()
	for range files {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("compression workers did not start concurrently")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if peak.Load() != 2 {
		t.Fatalf("peak concurrency = %d, want 2", peak.Load())
	}
	compressionErrMu.Lock()
	defer compressionErrMu.Unlock()
	if compressionErr != nil {
		t.Fatal(compressionErr)
	}
	rows, err := settings.Client().AppState.ListByPrefix(ctx, compressionLedgerPrefix)
	if err != nil || len(rows) != len(files) {
		t.Fatalf("completed ledgers = %+v, err = %v", rows, err)
	}
	for _, row := range rows {
		if !strings.Contains(row.Value, `"state":"compressed"`) {
			t.Fatalf("ledger was not completed: %s", row.Value)
		}
	}
}

func TestApplyXpressBatchLocksPathBeforeRevalidation(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	settings, err := setting.Open(ctx, filepath.Join(base, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = settings.Close() })
	path := filepath.Join(base, "payload.bin")
	if err := os.WriteFile(path, make([]byte, 8192), 0o644); err != nil {
		t.Fatal(err)
	}

	previousAttributes := fileAttributesCall
	previousState := wofStateCall
	previousIdentity := fileIdentityFromHandleCall
	previousCompress := wofSetFileDataLocationCall
	t.Cleanup(func() {
		fileAttributesCall = previousAttributes
		wofStateCall = previousState
		fileIdentityFromHandleCall = previousIdentity
		wofSetFileDataLocationCall = previousCompress
	})
	fileAttributesCall = func(string) (uint32, error) { return 0, nil }
	var removeErr error
	wofStateCall = func(path string) (bool, uint32, uint32, error) {
		removeErr = os.Remove(path)
		return false, 0, 0, nil
	}
	fileIdentityFromHandleCall = func(windows.Handle) (string, error) { return "id", nil }
	wofSetFileDataLocationCall = func(windows.Handle, uint32, *wofCompressionInfoV1) uintptr {
		return hresultCompressionNotBeneficial
	}

	if err := applyXpressBatch(ctx, []compressionFile{{path: path, size: 8192}}, map[string]wofLedgerEntry{},
		map[string]struct{}{}, settings.Client(), 1, func(string, int64, bool) {}, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	if removeErr == nil {
		t.Fatal("XPRESS4K target path was replaceable during WOF revalidation")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("locked target was removed: %v", err)
	}
}

func TestApplyXpressBatchKeepsRecoverableResults(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	settings, err := setting.Open(ctx, filepath.Join(base, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = settings.Close() })
	files := make([]compressionFile, 2)
	for index := range files {
		path := filepath.Join(base, fmt.Sprintf("payload-%d.bin", index))
		if err := os.WriteFile(path, make([]byte, 8192), 0o644); err != nil {
			t.Fatal(err)
		}
		files[index] = compressionFile{path: path, size: 8192}
	}

	previousAttributes := fileAttributesCall
	previousState := wofStateCall
	previousIdentity := fileIdentityFromHandleCall
	previousCompress := wofSetFileDataLocationCall
	t.Cleanup(func() {
		fileAttributesCall = previousAttributes
		wofStateCall = previousState
		fileIdentityFromHandleCall = previousIdentity
		wofSetFileDataLocationCall = previousCompress
	})
	fileAttributesCall = func(string) (uint32, error) { return 0, nil }
	wofStateCall = func(string) (bool, uint32, uint32, error) { return false, 0, 0, nil }
	var mu sync.Mutex
	handleIDs := map[windows.Handle]string{}
	nextID := 0
	fileIdentityFromHandleCall = func(handle windows.Handle) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		id := fmt.Sprintf("id-%d", nextID)
		nextID++
		handleIDs[handle] = id
		return id, nil
	}
	wofSetFileDataLocationCall = func(handle windows.Handle, _ uint32, _ *wofCompressionInfoV1) uintptr {
		mu.Lock()
		id := handleIDs[handle]
		mu.Unlock()
		if id == "id-0" {
			return hresultCompressionNotBeneficial
		}
		return 0x80070005
	}
	if err := applyXpressBatch(ctx, files, map[string]wofLedgerEntry{}, map[string]struct{}{}, settings.Client(), 2,
		func(string, int64, bool) {}, ignoreCompressionMutations); err == nil {
		t.Fatal("expected compression failure")
	}
	if value, err := settings.Client().AppState.GetValue(ctx, compressionLedgerPrefix+"id-0"); err != nil || value != nil {
		t.Fatalf("not-beneficial ledger = %v, err = %v", value, err)
	}
	value, err := settings.Client().AppState.GetValue(ctx, compressionLedgerPrefix+"id-1")
	if err != nil || value == nil || !strings.Contains(*value, `"state":"compressing"`) {
		t.Fatalf("failed ledger = %v, err = %v", value, err)
	}
}

func TestApplyXpressBatchDoesNotCompressBeforeLedgerCommit(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	settings, err := setting.Open(ctx, filepath.Join(base, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = settings.Close() })
	path := filepath.Join(base, "payload.bin")
	if err := os.WriteFile(path, make([]byte, 8192), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.Client().SQL().ExecContext(ctx, `CREATE TRIGGER fail_wof_ledger
BEFORE INSERT ON app_state WHEN NEW.key = 'mod_compression:file:id'
BEGIN SELECT RAISE(ABORT, 'ledger failure'); END`); err != nil {
		t.Fatal(err)
	}

	previousAttributes := fileAttributesCall
	previousState := wofStateCall
	previousIdentity := fileIdentityFromHandleCall
	previousCompress := wofSetFileDataLocationCall
	t.Cleanup(func() {
		fileAttributesCall = previousAttributes
		wofStateCall = previousState
		fileIdentityFromHandleCall = previousIdentity
		wofSetFileDataLocationCall = previousCompress
	})
	fileAttributesCall = func(string) (uint32, error) { return 0, nil }
	wofStateCall = func(string) (bool, uint32, uint32, error) { return false, 0, 0, nil }
	fileIdentityFromHandleCall = func(windows.Handle) (string, error) { return "id", nil }
	var calls atomic.Int32
	wofSetFileDataLocationCall = func(windows.Handle, uint32, *wofCompressionInfoV1) uintptr {
		calls.Add(1)
		return 0
	}
	err = applyXpressBatch(ctx, []compressionFile{{path: path, size: 8192}}, map[string]wofLedgerEntry{},
		map[string]struct{}{}, settings.Client(), 1, func(string, int64, bool) {}, ignoreCompressionMutations)
	if !errors.Is(err, errWofLedgerCommit) {
		t.Fatalf("error = %v, want ledger commit failure", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("WOF calls = %d, want 0", calls.Load())
	}
}

func TestApplyXpressBatchPreservesPreLedgerWhenCompletionCommitFails(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	settings, err := setting.Open(ctx, filepath.Join(base, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = settings.Close() })
	path := filepath.Join(base, "payload.bin")
	if err := os.WriteFile(path, make([]byte, 8192), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.Client().SQL().ExecContext(ctx, `CREATE TRIGGER fail_wof_completion
BEFORE UPDATE ON app_state WHEN NEW.value LIKE '%"state":"compressed"%'
BEGIN SELECT RAISE(ABORT, 'completion failure'); END`); err != nil {
		t.Fatal(err)
	}

	previousAttributes := fileAttributesCall
	previousState := wofStateCall
	previousIdentity := fileIdentityFromHandleCall
	previousCompress := wofSetFileDataLocationCall
	t.Cleanup(func() {
		fileAttributesCall = previousAttributes
		wofStateCall = previousState
		fileIdentityFromHandleCall = previousIdentity
		wofSetFileDataLocationCall = previousCompress
	})
	fileAttributesCall = func(string) (uint32, error) { return 0, nil }
	wofStateCall = func(string) (bool, uint32, uint32, error) { return false, 0, 0, nil }
	fileIdentityFromHandleCall = func(windows.Handle) (string, error) { return "id", nil }
	wofSetFileDataLocationCall = func(windows.Handle, uint32, *wofCompressionInfoV1) uintptr { return 0 }
	failed := false
	err = applyXpressBatch(ctx, []compressionFile{{path: path, size: 8192}}, map[string]wofLedgerEntry{},
		map[string]struct{}{}, settings.Client(), 1, func(_ string, _ int64, value bool) { failed = value }, ignoreCompressionMutations)
	if !errors.Is(err, errWofLedgerCommit) {
		t.Fatalf("error = %v, want ledger commit failure", err)
	}
	if !failed {
		t.Fatal("completion commit failure was not reported as a failed file")
	}
	value, err := settings.Client().AppState.GetValue(ctx, compressionLedgerPrefix+"id")
	if err != nil || value == nil || !strings.Contains(*value, `"state":"compressing"`) {
		t.Fatalf("recoverable ledger = %v, err = %v", value, err)
	}
}

func TestApplyXpressBatchAbortsIfBackingBecomesForeign(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	settings, err := setting.Open(ctx, filepath.Join(base, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = settings.Close() })
	path := filepath.Join(base, "payload.bin")
	if err := os.WriteFile(path, make([]byte, 8192), 0o644); err != nil {
		t.Fatal(err)
	}

	previousAttributes := fileAttributesCall
	previousState := wofStateCall
	previousIdentity := fileIdentityFromHandleCall
	previousCompress := wofSetFileDataLocationCall
	t.Cleanup(func() {
		fileAttributesCall = previousAttributes
		wofStateCall = previousState
		fileIdentityFromHandleCall = previousIdentity
		wofSetFileDataLocationCall = previousCompress
	})
	fileAttributesCall = func(string) (uint32, error) { return 0, nil }
	wofStateCall = func(string) (bool, uint32, uint32, error) {
		return true, wofProviderFile, 1, nil
	}
	fileIdentityFromHandleCall = func(windows.Handle) (string, error) { return "foreign", nil }
	var calls atomic.Int32
	wofSetFileDataLocationCall = func(windows.Handle, uint32, *wofCompressionInfoV1) uintptr {
		calls.Add(1)
		return 0
	}
	err = applyXpressBatch(ctx, []compressionFile{{path: path, size: 8192}}, map[string]wofLedgerEntry{},
		map[string]struct{}{}, settings.Client(), 1, func(string, int64, bool) {}, ignoreCompressionMutations)
	if !errors.Is(err, errUnmanagedWofBacking) {
		t.Fatalf("error = %v, want unmanaged WOF backing", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("WOF calls = %d, want 0", calls.Load())
	}
	rows, err := settings.Client().AppState.ListByPrefix(ctx, compressionLedgerPrefix)
	if err != nil || len(rows) != 0 {
		t.Fatalf("foreign backing ledgers = %+v, err = %v", rows, err)
	}
}

func TestApplyXpressBatchLeavesPreLedgerOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	base := t.TempDir()
	settings, err := setting.Open(ctx, filepath.Join(base, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = settings.Close() })
	path := filepath.Join(base, "payload.bin")
	if err := os.WriteFile(path, make([]byte, 8192), 0o644); err != nil {
		t.Fatal(err)
	}

	previousAttributes := fileAttributesCall
	previousState := wofStateCall
	previousIdentity := fileIdentityFromHandleCall
	previousCompress := wofSetFileDataLocationCall
	t.Cleanup(func() {
		fileAttributesCall = previousAttributes
		wofStateCall = previousState
		fileIdentityFromHandleCall = previousIdentity
		wofSetFileDataLocationCall = previousCompress
	})
	fileAttributesCall = func(string) (uint32, error) { return 0, nil }
	wofStateCall = func(string) (bool, uint32, uint32, error) { return false, 0, 0, nil }
	fileIdentityFromHandleCall = func(windows.Handle) (string, error) { return "id", nil }
	wofSetFileDataLocationCall = func(windows.Handle, uint32, *wofCompressionInfoV1) uintptr {
		cancel()
		return 0
	}
	err = applyXpressBatch(ctx, []compressionFile{{path: path, size: 8192}}, map[string]wofLedgerEntry{},
		map[string]struct{}{}, settings.Client(), 1, func(string, int64, bool) {}, ignoreCompressionMutations)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, errWofLedgerCommit) {
		t.Fatalf("error = %v, want cancellation and ledger commit failure", err)
	}
	value, err := settings.Client().AppState.GetValue(context.Background(), compressionLedgerPrefix+"id")
	if err != nil || value == nil || !strings.Contains(*value, `"state":"compressing"`) {
		t.Fatalf("canceled ledger = %v, err = %v", value, err)
	}
}

func TestXpressWorkerCountUsesAllAvailableProcessors(t *testing.T) {
	for input, want := range map[int]int{-1: 1, 0: 1, 1: 1, 16: 16} {
		if got := xpressWorkerCount(input); got != want {
			t.Fatalf("xpressWorkerCount(%d) = %d, want %d", input, got, want)
		}
	}
}

func TestApplyXpressBatchRealWofWithLockedRevalidation(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	settings, err := setting.Open(ctx, filepath.Join(base, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = settings.Close() })
	path := filepath.Join(base, "payload.bin")
	if err := os.WriteFile(path, bytes.Repeat([]byte("xpress4k"), 512*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wofCompress(path); err != nil {
		t.Skipf("WOF compression is unavailable on the test volume: %v", err)
	}
	if err := wofDecompress(path); err != nil {
		t.Fatal(err)
	}

	if err := applyXpressBatch(ctx, []compressionFile{{path: path, size: 4 * 1024 * 1024}},
		map[string]wofLedgerEntry{}, map[string]struct{}{}, settings.Client(), 1,
		func(string, int64, bool) {}, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = wofDecompress(path) })
	external, provider, algorithm, err := nativeWofState(path)
	if err != nil || !external || provider != wofProviderFile || algorithm != fileProviderCompressionXpress4K {
		t.Fatalf("WOF state = external:%v provider:%d algorithm:%d err:%v", external, provider, algorithm, err)
	}
	id, err := nativeFileIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	value, err := settings.Client().AppState.GetValue(ctx, compressionLedgerPrefix+id)
	if err != nil || value == nil || !strings.Contains(*value, `"state":"compressed"`) {
		t.Fatalf("completed ledger = %v, err=%v", value, err)
	}
}

func TestRestoreManagedWofLeavesForeignBackingUntouched(t *testing.T) {
	ctx := context.Background()
	settings, err := setting.Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = settings.Close() })
	root := t.TempDir()
	path := filepath.Join(root, "payload.bin")
	if err := os.WriteFile(path, make([]byte, 8192), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := wofLedgerEntry{
		FileID: "file-id", Path: path, Provider: wofProviderFile,
		Algorithm: fileProviderCompressionXpress4K, State: "compressed",
	}
	if err := saveWofLedger(ctx, settings.Client(), entry); err != nil {
		t.Fatal(err)
	}

	previousState, previousIdentity, previousDelete := wofStateCall, fileIdentityCall, deleteExternalBackingCall
	t.Cleanup(func() {
		wofStateCall, fileIdentityCall, deleteExternalBackingCall = previousState, previousIdentity, previousDelete
	})
	wofStateCall = func(string) (bool, uint32, uint32, error) {
		return true, wofProviderFile, 1, nil
	}
	fileIdentityCall = func(string) (string, error) { return entry.FileID, nil }
	deleteCalled := false
	deleteExternalBackingCall = func(windows.Handle) error {
		deleteCalled = true
		return nil
	}

	if err := restoreManagedWOF(ctx, []string{root}, settings.Client(), func(int, int64) {}, func(string, int64, bool) {}, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	if deleteCalled {
		t.Fatal("foreign WOF backing was deleted")
	}
	ledger, err := settings.Client().AppState.GetValue(ctx, compressionLedgerPrefix+entry.FileID)
	if err != nil || ledger != nil {
		t.Fatalf("stale ledger = %v, err=%v", ledger, err)
	}
	external, err := unmanagedCompressionFiles(ctx, []string{root}, settings.Client())
	if err != nil || len(external) != 1 || external[0] != path {
		t.Fatalf("external = %v, err=%v", external, err)
	}
}

func TestRestoreManagedWofDeletesMatchingBacking(t *testing.T) {
	ctx := context.Background()
	settings, err := setting.Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = settings.Close() })
	root := t.TempDir()
	path := filepath.Join(root, "payload.bin")
	if err := os.WriteFile(path, make([]byte, 8192), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := wofLedgerEntry{
		FileID: "file-id", Path: path, Provider: wofProviderFile,
		Algorithm: fileProviderCompressionXpress4K, State: "compressed",
	}
	if err := saveWofLedger(ctx, settings.Client(), entry); err != nil {
		t.Fatal(err)
	}

	previousState, previousIdentity, previousDelete := wofStateCall, fileIdentityCall, deleteExternalBackingCall
	t.Cleanup(func() {
		wofStateCall, fileIdentityCall, deleteExternalBackingCall = previousState, previousIdentity, previousDelete
	})
	wofStateCall = func(string) (bool, uint32, uint32, error) {
		return true, entry.Provider, entry.Algorithm, nil
	}
	fileIdentityCall = func(string) (string, error) { return entry.FileID, nil }
	deleteCalled := false
	deleteExternalBackingCall = func(windows.Handle) error {
		deleteCalled = true
		return nil
	}

	if err := restoreManagedWOF(ctx, []string{root}, settings.Client(), func(int, int64) {}, func(string, int64, bool) {}, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	if !deleteCalled {
		t.Fatal("managed WOF backing was not deleted")
	}
}

func TestRestoreManagedWofRealXpressBackingWithReadOnlyAccess(t *testing.T) {
	ctx := context.Background()
	settings, err := setting.Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = settings.Close() })
	root := t.TempDir()
	path := filepath.Join(root, "payload.bin")
	want := bytes.Repeat([]byte("xpress4k"), 512*1024)
	if err := os.WriteFile(path, want, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wofCompress(path); err != nil {
		t.Skipf("WOF compression is unavailable on the test volume: %v", err)
	}
	t.Cleanup(func() { _ = wofDecompress(path) })
	external, provider, algorithm, err := nativeWofState(path)
	if err != nil || !external || provider != wofProviderFile || algorithm != fileProviderCompressionXpress4K {
		t.Fatalf("WOF state = external:%v provider:%d algorithm:%d err:%v", external, provider, algorithm, err)
	}
	id, err := nativeFileIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveWofLedger(ctx, settings.Client(), wofLedgerEntry{
		FileID: id, Path: path, Provider: provider, Algorithm: algorithm, State: "compressed",
	}); err != nil {
		t.Fatal(err)
	}

	handle, err := openDecompressionFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	if err := restoreManagedWOF(ctx, []string{root}, settings.Client(), func(int, int64) {}, func(string, int64, bool) {}, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	external, _, _, err = nativeWofState(path)
	if err != nil || external {
		t.Fatalf("WOF backing remains: external=%v err=%v", external, err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("restored data mismatch: size=%d err=%v", len(got), err)
	}
	ledger, err := settings.Client().AppState.GetValue(ctx, compressionLedgerPrefix+id)
	if err != nil || ledger != nil {
		t.Fatalf("restored ledger = %v, err=%v", ledger, err)
	}
}

func TestRestoreManagedWofClearsLedgerAfterAutomaticRemoval(t *testing.T) {
	ctx := context.Background()
	settings, err := setting.Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = settings.Close() })
	root := t.TempDir()
	path := filepath.Join(root, "payload.bin")
	if err := os.WriteFile(path, make([]byte, 8192), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := wofLedgerEntry{
		FileID: "file-id", Path: path, Provider: wofProviderFile,
		Algorithm: fileProviderCompressionXpress4K, State: "compressing",
	}
	if err := saveWofLedger(ctx, settings.Client(), entry); err != nil {
		t.Fatal(err)
	}

	previousState, previousIdentity, previousDelete := wofStateCall, fileIdentityCall, deleteExternalBackingCall
	t.Cleanup(func() {
		wofStateCall, fileIdentityCall, deleteExternalBackingCall = previousState, previousIdentity, previousDelete
	})
	wofStateCall = func(string) (bool, uint32, uint32, error) { return false, 0, 0, nil }
	fileIdentityCall = func(string) (string, error) { return entry.FileID, nil }
	deleteCalled := false
	deleteExternalBackingCall = func(windows.Handle) error {
		deleteCalled = true
		return nil
	}

	if err := restoreManagedWOF(ctx, []string{root}, settings.Client(), func(int, int64) {}, func(string, int64, bool) {}, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	if deleteCalled {
		t.Fatal("automatic WOF removal invoked explicit decompression")
	}
	ledger, err := settings.Client().AppState.GetValue(ctx, compressionLedgerPrefix+entry.FileID)
	if err != nil || ledger != nil {
		t.Fatalf("automatic removal ledger = %v, err=%v", ledger, err)
	}
}
