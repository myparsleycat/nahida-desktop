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
	for _, name := range []string{"game.exe", "d3d11.dll", "3dmloader.dll"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	digest := sha256.Sum256([]byte("3dmloader.dll"))
	spec := LaunchSpec{
		Mode: ModeXXMI, ProcessName: "game.exe", StartExe: filepath.Join(root, "game.exe"),
		WorkDir: root, StartMethod: "Native", InjectMode: "Hook", TimeoutSeconds: 30,
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
}
