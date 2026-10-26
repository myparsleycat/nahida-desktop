//go:build windows

package elevated

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/platform"
)

const (
	hardenChildEnv    = "NAHIDA_HARDEN_CHILD"
	hardenProbeDLL    = "dwmapi.dll"
	hardenLoadedLabel = "hardened load: "
)

// TestHardenHelperProcessChild runs in a copy of the test binary that has a DLL planted beside it.
// Hardening changes process-wide loader state, so it never runs in the parent test process.
func TestHardenHelperProcessChild(t *testing.T) {
	if os.Getenv(hardenChildEnv) != "1" {
		return
	}
	name, err := windows.UTF16PtrFromString(hardenProbeDLL)
	if err != nil {
		t.Fatal(err)
	}
	var module windows.Handle
	if windows.GetModuleHandleEx(0, name, &module) == nil {
		t.Fatalf("%s was loaded before hardening, so the planted copy cannot be checked", hardenProbeDLL)
	}

	if err := HardenHelperProcess(); err != nil {
		t.Fatal(err)
	}
	module, err = windows.LoadLibrary(hardenProbeDLL)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]uint16, windows.MAX_LONG_PATH)
	length, err := windows.GetModuleFileName(module, &buffer[0], uint32(len(buffer)))
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(hardenLoadedLabel + windows.UTF16ToString(buffer[:length]))
}

func TestHardenHelperProcessIgnoresDLLsBesideTheExecutable(t *testing.T) {
	system, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	probe := filepath.Join(dir, "harden-probe.exe")
	copyTestFile(t, executable, probe)
	planted := filepath.Join(dir, hardenProbeDLL)
	copyTestFile(t, filepath.Join(system, hardenProbeDLL), planted)

	command := exec.CommandContext(t.Context(), probe, "-test.run=^TestHardenHelperProcessChild$")
	command.Env = append(os.Environ(), hardenChildEnv+"=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("hardened child failed: %v\n%s", err, output)
	}
	_, loaded, ok := strings.Cut(string(output), hardenLoadedLabel)
	if !ok {
		t.Fatalf("hardened child did not report a load:\n%s", output)
	}
	loaded, _, _ = strings.Cut(loaded, "\n")
	loaded = strings.TrimSpace(loaded)
	if !platform.SameOrChildPath(system, loaded) {
		t.Fatalf("%s loaded from %q, want %q (planted copy at %q)", hardenProbeDLL, loaded, system, planted)
	}
}

func copyTestFile(t *testing.T, source, target string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o700); err != nil {
		t.Fatal(err)
	}
}
