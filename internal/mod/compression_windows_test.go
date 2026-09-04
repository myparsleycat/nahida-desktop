//go:build windows

package mod

import (
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
