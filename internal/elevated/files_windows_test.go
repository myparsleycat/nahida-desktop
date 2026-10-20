//go:build windows

package elevated

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/platform"
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
	}, nil)
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
	if err := applyFileOps(t.Context(), []FileOp{mismatched}, nil); err == nil {
		t.Fatal("copy with a mismatched digest was applied")
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm()&0o200 != 0 {
		t.Fatalf("refused copy made the target writable: mode = %v, err = %v", info.Mode(), err)
	}

	err := applyFileOps(t.Context(), []FileOp{{Kind: FileOpCopy, Source: source, Target: target, SHA256: digest}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "new library" {
		t.Fatalf("target = %q, err = %v", data, err)
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm()&0o200 != 0 {
		t.Fatalf("replaced target lost its read-only attribute: mode = %v, err = %v", info.Mode(), err)
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

	err = applyFileOps(t.Context(), []FileOp{{Kind: FileOpCopy, Source: source, Target: target, SHA256: digest}}, nil)
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
			if err := applyFileOps(t.Context(), []FileOp{test.op}, nil); err == nil {
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
		err := applyFileOps(t.Context(), []FileOp{valid, {Kind: FileOpRemove, Target: "relative.db"}}, nil)
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
		if err := applyFileOps(ctx, []FileOp{valid}, nil); err == nil {
			t.Fatal("cancelled batch was applied")
		}
		if data, err := os.ReadFile(existing); err != nil || string(data) != "old" {
			t.Fatalf("existing file = %q, err = %v", data, err)
		}
	})
}

func TestApplyFileOpsResetsAccessToInheritedPermissions(t *testing.T) {
	root, err := platform.FinalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(root, "SlotFix.ini")
	if err := os.WriteFile(locked, []byte("[Constants]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(
		"icacls", locked, "/deny", "*"+user.User.Sid.String()+":(RD)",
	).CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot deny reading a file: %v (%s)", err, output)
	}
	t.Cleanup(func() { _ = exec.Command("icacls", locked, "/reset").Run() })
	if _, err := os.ReadFile(locked); err == nil {
		t.Skip("filesystem does not enforce the read denial")
	}

	if err := applyFileOps(
		t.Context(),
		[]FileOp{{Kind: FileOpResetAccess, Target: locked}},
		user.User.Sid,
	); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(locked); err != nil || string(data) != "[Constants]\n" {
		t.Fatalf("reset file = %q, err = %v", data, err)
	}
}

func TestApplyFileOpsRejectsUnsafeAccessResets(t *testing.T) {
	root, err := platform.FinalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(root, "folder")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "inside.ini"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.ini")
	if err := os.WriteFile(outside, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	hardLink := filepath.Join(folder, "hard-link.ini")
	if err := os.Link(outside, hardLink); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, folder).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v (%s)", err, output)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		op    FileOp
		owner *windows.SID
	}{
		{name: "junction", op: FileOp{Kind: FileOpResetAccess, Target: link}, owner: user.User.Sid},
		{name: "below a junction", op: FileOp{Kind: FileOpResetAccess, Target: filepath.Join(link, "inside.ini")}, owner: user.User.Sid},
		{name: "with source", op: FileOp{Kind: FileOpResetAccess, Source: folder, Target: folder}, owner: user.User.Sid},
		{name: "hard link", op: FileOp{Kind: FileOpResetAccess, Target: hardLink}, owner: user.User.Sid},
		{name: "unknown owner", op: FileOp{Kind: FileOpResetAccess, Target: folder}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := applyFileOps(t.Context(), []FileOp{test.op}, test.owner); err == nil {
				t.Fatal("unsafe access reset was applied")
			}
		})
	}

	system, err := windows.GetSystemWindowsDirectory()
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{filepath.Join(system, "win.ini"), filepath.VolumeName(root) + `\`} {
		if err := validateFileOp(FileOp{Kind: FileOpResetAccess, Target: target}); err == nil {
			t.Fatalf("unsafe target was accepted: %q", target)
		}
	}
}

func TestResetFolderAccessLeavesDescendantHardLinkPermissions(t *testing.T) {
	root, err := platform.FinalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(root, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil,
	); err != nil {
		t.Fatal(err)
	}

	parent := filepath.Join(root, "parent")
	folder := filepath.Join(parent, "folder")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	descriptor, err = windows.SecurityDescriptorFromString(
		"D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")(A;OICI;FR;;;WD)",
	)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err = descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(parent, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.ini")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, filepath.Join(folder, "linked.ini")); err != nil {
		t.Fatal(err)
	}
	before, err := windows.GetNamedSecurityInfo(outside, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if err := resetFileAccess(folder, user.User.Sid); err != nil {
		t.Fatal(err)
	}
	after, err := windows.GetNamedSecurityInfo(outside, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if before.String() != after.String() {
		t.Fatalf("folder reset changed an outside file's DACL: before %s, after %s", before.String(), after.String())
	}
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
		serverDone <- serveHelperConnection(t.Context(), serverConn, "secret", Peer{}, map[string]Handler{
			OperationFiles: FileHandler(),
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
