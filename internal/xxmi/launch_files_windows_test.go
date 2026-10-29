package xxmi

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/xxmi/inject"
)

func TestProtectLaunchFilesRetainsLocksAndRejectsUnsignedInjector(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "loader.exe")
	if err := os.WriteFile(path, []byte("loader"), 0o600); err != nil {
		t.Fatal(err)
	}
	input, err := verifiedLaunchFile(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	spec := inject.LaunchSpec{LegacyLoader: input}
	closeFiles, err := protectLaunchFiles(t.Context(), &spec)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFiles()
	if err := os.WriteFile(path, []byte("changed"), 0o600); err == nil {
		t.Fatal("app released its launch file before the helper finished")
	}
	closeFiles()
	data, err := json.Marshal(xxmiLibraryManifest{Signatures: map[string]string{"3dmloader.dll": "invalid"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Manifest.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	spec = inject.LaunchSpec{LoaderDLL: input}
	if _, err := protectLaunchFiles(t.Context(), &spec); err == nil {
		t.Fatal("injector without an authentic signature was accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("signature failure leaked a lock: %v", err)
	}
}

func TestProtectLaunchFilesCancellationReleasesFiles(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "loader.exe")
	if err := os.WriteFile(path, []byte("loader"), 0o600); err != nil {
		t.Fatal(err)
	}
	input, err := verifiedLaunchFile(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = protectLaunchFiles(ctx, &inject.LaunchSpec{LegacyLoader: input})
	if !errors.Is(err, context.Canceled) || strings.HasPrefix(err.Error(), "XXMI_RUNTIME_CORRUPTED:") {
		t.Fatalf("cancelled preparation was not preserved as cancellation: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("cancelled preparation leaked a lock: %v", err)
	}
}
