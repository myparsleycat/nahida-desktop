//go:build windows

package mod

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

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
		Algorithm: fileProviderCompressionXpress4K, State: "compressed",
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
