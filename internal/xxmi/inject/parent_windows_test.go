//go:build windows

package inject

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestParentProcessChild(t *testing.T) {
	if os.Getenv("NAHIDA_PARENT_PROCESS_TEST") != "1" {
		return
	}
	time.Sleep(30 * time.Second)
}

func TestStartGameProcessUnderParentExe(t *testing.T) {
	system, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatal(err)
	}
	image, err := os.ReadFile(filepath.Join(system, "whoami.exe"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	launcher, alias := filepath.Join(root, "GRYPHLINK"), filepath.Join(root, "Launcher alias")
	if err := os.Mkdir(launcher, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(launcher, "Games.exe"), image, 0o700); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", alias, launcher).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}

	// An instance already running from the same file is replaced, even when it is named through a junction.
	earlier := &parentProcess{path: filepath.Join(launcher, "Games.exe")}
	if _, err := earlier.start(); err != nil {
		t.Fatal(err)
	}
	defer earlier.stop(context.Background(), false)
	parent := &parentProcess{path: filepath.Join(alias, "Games.exe")}
	if _, err := parent.start(); err != nil {
		t.Fatal(err)
	}
	defer parent.stop(context.Background(), false)
	if event, err := windows.WaitForSingleObject(earlier.process, 0); err != nil || event != windows.WAIT_OBJECT_0 {
		t.Fatalf("earlier parent still running: %d, %v", event, err)
	}

	t.Setenv("NAHIDA_PARENT_PROCESS_TEST", "1")
	spec := LaunchSpec{
		StartMethod: "Native", Priority: "Normal", StartExe: os.Args[0], WorkDir: t.TempDir(),
		StartArgs: []string{"-test.run=^TestParentProcessChild$"}, parent: parent,
	}
	if err := startGameProcess(spec); err != nil {
		t.Fatal(err)
	}
	parentPID, err := windows.GetProcessId(parent.process)
	if err != nil {
		t.Fatal(err)
	}
	child := childOf(t, parentPID)
	defer func() {
		if process, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, child); err == nil {
			_ = windows.TerminateProcess(process, 1)
			_ = windows.CloseHandle(process)
		}
	}()

	process := parent.process
	duplicate := windows.Handle(0)
	if err := windows.DuplicateHandle(windows.CurrentProcess(), process, windows.CurrentProcess(), &duplicate,
		windows.SYNCHRONIZE, false, 0); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = windows.CloseHandle(duplicate) }()
	parent.stop(context.Background(), false)
	if event, err := windows.WaitForSingleObject(duplicate, 5000); err != nil || event != windows.WAIT_OBJECT_0 {
		t.Fatalf("parent still running after stop: %d, %v", event, err)
	}
}

func childOf(t *testing.T, parent uint32) uint32 {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
		if err != nil {
			t.Fatal(err)
		}
		entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
		for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
			if entry.ParentProcessID == parent {
				_ = windows.CloseHandle(snapshot)
				return entry.ProcessID
			}
		}
		_ = windows.CloseHandle(snapshot)
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("no process was started under parent %d", parent)
	return 0
}
