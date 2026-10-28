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
	gameLoadedLabel   = "game load: "
)

// TestHardenHelperProcessChild runs in a copy of the test binary that has a DLL planted beside it.
// Hardening changes process-wide loader state, so it never runs in the parent test process. The hardened
// copy starts one more copy without hardening, as the helper starts a game.
func TestHardenHelperProcessChild(t *testing.T) {
	mode := os.Getenv(hardenChildEnv)
	if mode != "helper" && mode != "game" {
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

	if mode == "game" {
		fmt.Println(gameLoadedLabel + loadProbeDLL(t))
		return
	}

	if err := HardenHelperProcess(); err != nil {
		t.Fatal(err)
	}
	fmt.Println(hardenLoadedLabel + loadProbeDLL(t))

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), executable, "-test.run=^TestHardenHelperProcessChild$")
	command.Env = append(os.Environ(), hardenChildEnv+"=game")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("game child failed: %v\n%s", err, output)
	}
	fmt.Print(string(output))
}

func loadProbeDLL(t *testing.T) string {
	t.Helper()
	module, err := windows.LoadLibrary(hardenProbeDLL)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]uint16, windows.MAX_LONG_PATH)
	length, err := windows.GetModuleFileName(module, &buffer[0], uint32(len(buffer)))
	if err != nil {
		t.Fatal(err)
	}
	return windows.UTF16ToString(buffer[:length])
}

// The helper must ignore a DLL planted beside it, while a game it starts must still load the proxy DLLs that
// mods such as OptiScaler place beside the game.
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
	command.Env = append(os.Environ(), hardenChildEnv+"=helper")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("hardened child failed: %v\n%s", err, output)
	}
	loaded := reportedLoad(t, output, hardenLoadedLabel)
	if !platform.SameOrChildPath(system, loaded) {
		t.Fatalf("%s loaded from %q, want %q (planted copy at %q)", hardenProbeDLL, loaded, system, planted)
	}

	// The test folder may be reached through a short name, so the planted copy is compared by its final path.
	gameLoaded, err := platform.FinalPath(reportedLoad(t, output, gameLoadedLabel))
	if err != nil {
		t.Fatal(err)
	}
	plantedPath, err := platform.FinalPath(planted)
	if err != nil {
		t.Fatal(err)
	}
	if !platform.SamePathFold(gameLoaded, plantedPath) {
		t.Fatalf("child of the hardened process loaded %s from %q, want the planted copy %q",
			hardenProbeDLL, gameLoaded, plantedPath)
	}
}

func reportedLoad(t *testing.T, output []byte, label string) string {
	t.Helper()
	_, loaded, ok := strings.Cut(string(output), label)
	if !ok {
		t.Fatalf("no %q line in child output:\n%s", label, output)
	}
	loaded, _, _ = strings.Cut(loaded, "\n")
	return strings.TrimSpace(loaded)
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
