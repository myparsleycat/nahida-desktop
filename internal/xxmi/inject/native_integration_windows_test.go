//go:build windows && amd64 && integration

package inject

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestNativeInjectionChild(t *testing.T) {
	if os.Getenv("NAHIDA_NATIVE_INJECT_TEST") != "1" {
		return
	}
	fmt.Println("ready")
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatal(err)
	}
}

func TestNativeInjectionLoadsUnicodeDLLAndReportsRemoteError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeInjectionChild$")
	child.Env = append(os.Environ(), "NAHIDA_NATIVE_INJECT_TEST=1")
	input, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = input.Close()
		_ = child.Process.Kill()
		_ = child.Wait()
	}()
	if line, err := bufio.NewReader(output).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("child readiness = %q, %v", line, err)
	}
	systemDir, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatal(err)
	}
	dll, err := os.ReadFile(filepath.Join(systemDir, "version.dll"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "한글 DLL 경로.dll")
	if err := os.WriteFile(path, dll, 0o600); err != nil {
		t.Fatal(err)
	}
	module, err := injectNativeDLL(ctx, child.Process.Pid, path, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if module <= 0xffffffff {
		t.Fatalf("x64 module handle was truncated: %#x", module)
	}
	loaded, err := processHasModule(child.Process.Pid, path)
	if err != nil || !loaded {
		t.Fatalf("DLL absent from child modules: loaded=%t, err=%v", loaded, err)
	}
	broken := filepath.Join(t.TempDir(), "invalid.dll")
	if err := os.WriteFile(broken, []byte("not a PE image"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := injectNativeDLL(
		ctx,
		child.Process.Pid,
		broken,
		5*time.Second,
	); !errors.Is(
		err,
		windows.ERROR_BAD_EXE_FORMAT,
	) {
		t.Fatalf("remote LoadLibraryW error = %v, want ERROR_BAD_EXE_FORMAT", err)
	}
}
