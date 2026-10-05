//go:build windows

package elevated

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func stagedFile(t *testing.T, content string) (path, digest string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "staged.bin")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(content))
	return path, hex.EncodeToString(sum[:])
}

func TestApplyFileOpsCopiesAndRemoves(t *testing.T) {
	t.Parallel()

	source, digest := stagedFile(t, "new settings")
	root := t.TempDir()
	created := filepath.Join(root, "Persistent", "LocalStorage", "GENERAL_DATA.bin")
	replaced := filepath.Join(root, "Engine.ini")
	stale := filepath.Join(root, "LocalStorage.db-journal")
	for _, path := range []string{replaced, stale} {
		if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	err := applyFileOps(t.Context(), []FileOp{
		{Kind: FileOpCopy, Source: source, Target: created, SHA256: digest},
		{Kind: FileOpCopy, Source: source, Target: replaced, SHA256: strings.ToUpper(digest)},
		{Kind: FileOpRemove, Target: stale},
		{Kind: FileOpRemove, Target: filepath.Join(root, "already-gone.db")},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{created, replaced} {
		if data, err := os.ReadFile(path); err != nil || string(data) != "new settings" {
			t.Fatalf("%s = %q, err = %v", path, data, err)
		}
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("removed file remains: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}

func TestApplyFileOpsReplacesReadOnlyTarget(t *testing.T) {
	t.Parallel()

	source, digest := stagedFile(t, "new library")
	target := filepath.Join(t.TempDir(), "d3d11.dll")
	if err := os.WriteFile(target, []byte("old library"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(target, 0o600) })

	mismatched := FileOp{Kind: FileOpCopy, Source: source, Target: target, SHA256: strings.Repeat("0", 64)}
	if err := applyFileOps(t.Context(), []FileOp{mismatched}); err == nil {
		t.Fatal("copy with a mismatched digest was applied")
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm()&0o200 != 0 {
		t.Fatalf("refused copy made the target writable: mode = %v, err = %v", info.Mode(), err)
	}

	err := applyFileOps(t.Context(), []FileOp{{Kind: FileOpCopy, Source: source, Target: target, SHA256: digest}})
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "new library" {
		t.Fatalf("target = %q, err = %v", data, err)
	}
}

func TestApplyFileOpsKeepsReadOnlyTargetItCannotReplace(t *testing.T) {
	t.Parallel()

	source, digest := stagedFile(t, "new library")
	target := filepath.Join(t.TempDir(), "d3d11.dll")
	if err := os.WriteFile(target, []byte("old library"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(target, 0o600) })

	// A handle without delete sharing, as a process that loaded the library holds one, refuses
	// the replacement after the attribute was cleared for it.
	name, err := windows.UTF16PtrFromString(target)
	if err != nil {
		t.Fatal(err)
	}
	inUse, err := windows.CreateFile(
		name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL, 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = windows.CloseHandle(inUse) })

	err = applyFileOps(t.Context(), []FileOp{{Kind: FileOpCopy, Source: source, Target: target, SHA256: digest}})
	if err == nil {
		t.Fatal("a target in use was replaced")
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o200 != 0 {
		t.Fatalf("failed replacement left the target writable: mode = %v", info.Mode())
	}
	if data, readErr := os.ReadFile(target); readErr != nil || string(data) != "old library" {
		t.Fatalf("target = %q, err = %v", data, readErr)
	}
}

func TestApplyFileOpsRejectsUnsafeRequests(t *testing.T) {
	source, digest := stagedFile(t, "new settings")
	root := t.TempDir()
	existing := filepath.Join(root, "existing.ini")
	if err := os.WriteFile(existing, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "folder")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	valid := FileOp{Kind: FileOpCopy, Source: source, Target: existing, SHA256: digest}

	for _, test := range []struct {
		name string
		op   FileOp
	}{
		{name: "digest mismatch", op: FileOp{
			Kind: FileOpCopy, Source: source, Target: existing, SHA256: strings.Repeat("0", 64),
		}},
		{name: "missing digest", op: FileOp{Kind: FileOpCopy, Source: source, Target: existing}},
		{name: "relative target", op: FileOp{Kind: FileOpCopy, Source: source, Target: "existing.ini", SHA256: digest}},
		{name: "network target", op: FileOp{
			Kind: FileOpCopy, Source: source, Target: `\\server\share\existing.ini`, SHA256: digest,
		}},
		{name: "device target", op: FileOp{Kind: FileOpRemove, Target: `\\?\` + existing}},
		{name: "unclean target", op: FileOp{Kind: FileOpRemove, Target: filepath.Join(root, "folder") + `\..\existing.ini`}},
		{name: "relative source", op: FileOp{Kind: FileOpCopy, Source: "staged.bin", Target: existing, SHA256: digest}},
		{name: "copy over directory", op: FileOp{Kind: FileOpCopy, Source: source, Target: directory, SHA256: digest}},
		{name: "remove directory", op: FileOp{Kind: FileOpRemove, Target: directory}},
		{name: "remove with source", op: FileOp{Kind: FileOpRemove, Source: source, Target: existing}},
		{name: "unknown kind", op: FileOp{Kind: "rename", Source: source, Target: existing}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := applyFileOps(t.Context(), []FileOp{test.op}); err == nil {
				t.Fatal("unsafe request was applied")
			}
			if data, err := os.ReadFile(existing); err != nil || string(data) != "old" {
				t.Fatalf("existing file = %q, err = %v", data, err)
			}
			if _, err := os.Stat(directory); err != nil {
				t.Fatalf("directory was removed: %v", err)
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 2 {
				t.Fatalf("temporary files left behind: %v", entries)
			}
		})
	}

	t.Run("invalid operation stops the batch before it starts", func(t *testing.T) {
		err := applyFileOps(t.Context(), []FileOp{valid, {Kind: FileOpRemove, Target: "relative.db"}})
		if err == nil {
			t.Fatal("batch with an invalid operation was applied")
		}
		if data, err := os.ReadFile(existing); err != nil || string(data) != "old" {
			t.Fatalf("existing file = %q, err = %v", data, err)
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := applyFileOps(ctx, []FileOp{valid}); err == nil {
			t.Fatal("cancelled batch was applied")
		}
		if data, err := os.ReadFile(existing); err != nil || string(data) != "old" {
			t.Fatalf("existing file = %q, err = %v", data, err)
		}
	})
}

func TestClientAppliesFilesThroughHelper(t *testing.T) {
	t.Parallel()

	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
	})
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- serveHelperConnection(t.Context(), serverConn, "secret", serverOperations{
			applyFiles: applyFileOps,
		})
	}()
	client := NewClient()
	client.conn, client.secret = clientConn, "secret"
	if _, err := client.callLocked(t.Context(), operationHello, nil); err != nil {
		t.Fatal(err)
	}

	source, digest := stagedFile(t, "new settings")
	target := filepath.Join(t.TempDir(), "Config", "UserEngine.ini")
	if err := client.ApplyFiles(t.Context(), []FileOp{
		{Kind: FileOpCopy, Source: source, Target: target, SHA256: digest},
	}); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "new settings" {
		t.Fatalf("target = %q, err = %v", data, err)
	}

	err := client.ApplyFiles(t.Context(), []FileOp{{Kind: FileOpRemove, Target: "relative.db"}})
	if err == nil || !strings.Contains(err.Error(), "relative.db") {
		t.Fatalf("rejected request error = %v, want the target named", err)
	}

	// A request too large for one message must not cost the helper connection.
	oversized := make([]FileOp, 0, 1024)
	for range cap(oversized) {
		oversized = append(oversized, FileOp{Kind: FileOpRemove, Target: target + strings.Repeat("x", 100)})
	}
	if err := client.ApplyFiles(t.Context(), oversized); err == nil {
		t.Fatal("oversized request was sent")
	}
	if !client.Connected() {
		t.Fatal("oversized request closed the helper connection")
	}
	if err := client.ApplyFiles(t.Context(), []FileOp{{Kind: FileOpRemove, Target: target}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("removed target remains: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}
