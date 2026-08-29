package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallDirFromExe(t *testing.T) {
	t.Parallel()

	got := installDirFromExe(filepath.Join("C:", "Program Files", "MyCompany", "MyApp", "MyApp.exe"))
	want := filepath.Clean(filepath.Join("C:", "Program Files", "MyCompany", "MyApp"))
	if got != want {
		t.Fatalf("installDirFromExe = %q, want %q", got, want)
	}
}

func TestInstallDirMatchesExecutable(t *testing.T) {
	t.Parallel()

	got, err := InstallDir()
	if err != nil {
		t.Fatalf("InstallDir: %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	want := filepath.Dir(resolved)
	if got != want {
		t.Fatalf("InstallDir = %q, want %q", got, want)
	}
	if info, err := os.Stat(got); err != nil || !info.IsDir() {
		t.Fatalf("InstallDir is not a directory: %q %v", got, err)
	}
}
