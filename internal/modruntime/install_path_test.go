package modruntime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInstallRejectsWritesThroughEscapingJunction(t *testing.T) {
	manager, modsRoot := newTestManager(t)
	if _, err := manager.Install(context.Background(), "EFMI", "Probe"); err != nil {
		t.Fatal(err)
	}
	root := installRoot(modsRoot, "Probe")
	shader := filepath.Join(root, "shaders", "a.hlsl")
	if err := os.Remove(shader); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Dir(shader)); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", filepath.Dir(shader), outside).
		CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot create a junction: %v (%s)", err, output)
	}
	if _, err := manager.Install(context.Background(), "EFMI", "Probe"); err == nil {
		t.Fatal("installation accepted an escaping shader path")
	}
	if _, err := os.Stat(filepath.Join(outside, "a.hlsl")); !os.IsNotExist(err) {
		t.Fatalf("installation wrote outside runtime: %v", err)
	}
}
