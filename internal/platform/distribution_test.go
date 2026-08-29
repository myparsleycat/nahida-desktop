package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNSISInstalledFromDirRequiresUninstaller(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if nsisInstalledFromDir(dir) {
		t.Fatal("empty install dir must not look installed")
	}
	if nsisInstalledFromDir("") {
		t.Fatal("empty path must not look installed")
	}

	uninstaller := filepath.Join(dir, nsisUninstallerName)
	if err := os.WriteFile(uninstaller, []byte("nsis"), 0o600); err != nil {
		t.Fatalf("write uninstaller: %v", err)
	}
	if !nsisInstalledFromDir(dir) {
		t.Fatal("NSIS uninstaller must mark the directory as installed")
	}

	if err := os.Remove(uninstaller); err != nil {
		t.Fatalf("remove uninstaller: %v", err)
	}
	if err := os.Mkdir(uninstaller, 0o700); err != nil {
		t.Fatalf("mkdir uninstaller: %v", err)
	}
	if nsisInstalledFromDir(dir) {
		t.Fatal("uninstaller directory must not look installed")
	}
}

func TestSupportsAutostartRequiresPackagedNSISInstall(t *testing.T) {
	t.Setenv("NAHIDA_DEV", "1")
	if SupportsAutostart() {
		t.Fatal("unpackaged builds must not support autostart")
	}

	t.Setenv("NAHIDA_DEV", "")
	if SupportsAutostart() != nsisInstalled() {
		t.Fatalf("packaged autostart support = %v, want nsisInstalled %v", SupportsAutostart(), nsisInstalled())
	}
}
