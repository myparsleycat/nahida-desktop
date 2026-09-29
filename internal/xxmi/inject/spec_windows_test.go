//go:build windows

package inject

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateLaunchSpecRejectsUnsafePathsAndHashMismatch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, name := range []string{"game.exe", "d3d11.dll", "3dmloader.dll", "3DMigoto Loader.exe", "extra.dll"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	digest := sha256.Sum256([]byte("3dmloader.dll"))
	spec := LaunchSpec{
		Mode: ModeXXMI, ProcessName: "game.exe", StartExe: filepath.Join(root, "game.exe"),
		WorkDir: root, StartMethod: "Native", Priority: "Normal", InjectMode: "Hook", TimeoutSeconds: 30,
		ModuleDLL: filepath.Join(root, "d3d11.dll"),
		LoaderDLL: VerifiedFile{Path: filepath.Join(root, "3dmloader.dll"), SHA256: hex.EncodeToString(digest[:])},
	}
	if err := ValidateLaunchSpec(spec); err != nil {
		t.Fatal(err)
	}
	badHash := spec
	badHash.LoaderDLL.SHA256 = hex.EncodeToString(make([]byte, sha256.Size))
	if err := ValidateLaunchSpec(badHash); err == nil {
		t.Fatal("wrong loader hash accepted")
	}
	for _, path := range []string{`relative\d3d11.dll`, `\\server\share\d3d11.dll`} {
		badPath := spec
		badPath.ModuleDLL = path
		if err := ValidateLaunchSpec(badPath); err == nil {
			t.Fatalf("unsafe path %q accepted", path)
		}
	}
	legacy := spec
	legacy.Mode = ModeLegacy
	legacy.LegacyLoader = VerifiedFile{
		Path:   filepath.Join(root, "3DMigoto Loader.exe"),
		SHA256: fileHash(t, filepath.Join(root, "3DMigoto Loader.exe")),
	}
	legacy.ExtraDLLs = []string{filepath.Join(root, "extra.dll")}
	legacy.LoaderDLL = VerifiedFile{}
	if err := ValidateLaunchSpec(legacy); err == nil {
		t.Fatal("legacy extra DLLs were accepted without a verified injector")
	}
	legacy.LoaderDLL = spec.LoaderDLL
	if err := ValidateLaunchSpec(legacy); err != nil {
		t.Fatalf("verified legacy extra DLL injector was rejected: %v", err)
	}
	bypass := spec
	bypass.InjectMode = "Bypass"
	bypass.LoaderDLL = VerifiedFile{}
	if err := ValidateLaunchSpec(bypass); err != nil {
		t.Fatalf("XXMI bypass without an injector was rejected: %v", err)
	}
	bypass.Mode = ModeLegacy
	bypass.LegacyLoader = VerifiedFile{}
	if err := ValidateLaunchSpec(bypass); err != nil {
		t.Fatalf("legacy bypass without a loader was rejected: %v", err)
	}
	bypass.ExtraDLLs = []string{filepath.Join(root, "extra.dll")}
	if err := ValidateLaunchSpec(bypass); err == nil {
		t.Fatal("bypass extra DLL was accepted without a verified injector")
	}
	bypass.LoaderDLL = spec.LoaderDLL
	if err := ValidateLaunchSpec(bypass); err != nil {
		t.Fatalf("bypass extra DLL with verified injector was rejected: %v", err)
	}
}

func fileHash(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
