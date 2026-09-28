//go:build windows

package inject

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestLegacyLoaderReportsExitCodeBeforeModuleReady(t *testing.T) {
	systemDir, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(filepath.Join(systemDir, "cmd.exe"), "/C", "exit 7")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Wait() }()

	ready, err := waitLegacyLoaderReady(
		context.Background(), command.Process.Pid, filepath.Join(t.TempDir(), "d3d11.dll"), time.Second,
	)
	if ready || err == nil || !strings.Contains(err.Error(), "XXMI_LEGACY_LOADER_EXITED: exit code 7") {
		t.Fatalf("ready = %v, err = %v", ready, err)
	}
}

func TestLegacyLoaderExitTimeoutUsesLoaderDelay(t *testing.T) {
	module := filepath.Join(t.TempDir(), "d3d11.dll")
	ini := filepath.Join(filepath.Dir(module), "d3dx.ini")
	if err := os.WriteFile(ini, []byte("[Other]\ndelay=100\n[Loader]\n;delay=90\ndelay = 20\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := legacyLoaderExitTimeout(module); got != 25*time.Second {
		t.Fatalf("timeout = %s, want 25s", got)
	}
}
