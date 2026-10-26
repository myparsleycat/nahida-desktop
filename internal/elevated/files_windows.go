//go:build windows

package elevated

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/platform"
)

const (
	fileOpsTimeout = 2 * time.Minute
	// maxFilePayloadSize leaves room in a protocol message for the envelope around the operations.
	maxFilePayloadSize = maxMessageSize - 1024
)

// FileHandler serves OperationFiles. A reset-access hands its target to the verified parent user.
func FileHandler() Handler {
	return Handler{
		Timeout: fileOpsTimeout,
		Run: func(ctx context.Context, peer Peer, payload json.RawMessage) (json.RawMessage, error) {
			var ops []FileOp
			if err := json.Unmarshal(payload, &ops); err != nil {
				return nil, errors.New("invalid file request")
			}
			return nil, applyFileOps(ctx, ops, peer.UserSID)
		},
	}
}

// NewCopyOp describes replacing target with source, bound to the bytes source holds now.
func NewCopyOp(ctx context.Context, source, target string) (FileOp, error) {
	release, err := diskio.Acquire(ctx, source)
	if err != nil {
		return FileOp{}, err
	}
	defer release()

	file, err := os.Open(source)
	if err != nil {
		return FileOp{}, err
	}
	defer func() { _ = file.Close() }()

	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return FileOp{}, err
	}
	return FileOp{
		Kind: FileOpCopy, Source: source, Target: target, SHA256: hex.EncodeToString(digest.Sum(nil)),
	}, nil
}

// ApplyFiles has the helper run ops in order with administrator rights. They are not a
// transaction: a failure leaves the earlier operations applied.
func (c *Client) ApplyFiles(ctx context.Context, ops []FileOp) error {
	if len(ops) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	payload, err := json.Marshal(ops)
	if err != nil {
		return err
	}

	// An oversized request is refused here because a failed pipe write tears the helper down.
	if len(payload) > maxFilePayloadSize {
		return fmt.Errorf("elevated file request is too large: %d operations", len(ops))
	}
	callCtx, cancel := context.WithTimeout(ctx, fileOpsTimeout+10*time.Second)
	defer cancel()
	_, err = c.Call(callCtx, OperationFiles, payload)
	return err
}

// applyFileOps runs ops in order with the helper's administrator rights. Every operation is
// validated before the first one runs, but they are not a transaction: a failure leaves the
// earlier operations applied. A reset-access hands its target to owner, the account of the
// verified parent process.
func applyFileOps(ctx context.Context, ops []FileOp, owner *windows.SID) error {
	for _, op := range ops {
		if err := validateFileOp(op); err != nil {
			return fmt.Errorf("%s %q: %w", op.Kind, op.Target, err)
		}
	}
	for _, op := range ops {
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		switch op.Kind {
		case FileOpCopy:
			err = copyVerifiedFile(ctx, op)
		case FileOpRemove:
			err = removeFile(ctx, op)
		case FileOpResetAccess:
			err = resetFileAccess(op.Target, owner)
		}
		if err != nil {
			return fmt.Errorf("%s %q: %w", op.Kind, op.Target, err)
		}
	}
	return nil
}

func validateFileOp(op FileOp) error {
	if !isLocalDrivePath(op.Target) {
		return errors.New("target must be an absolute path on a local drive")
	}
	switch op.Kind {
	case FileOpCopy:
		if !isLocalDrivePath(op.Source) {
			return errors.New("source must be an absolute path on a local drive")
		}
		if digest, err := hex.DecodeString(op.SHA256); err != nil || len(digest) != sha256.Size {
			return errors.New("copy requires a SHA-256 digest")
		}
	case FileOpRemove:
		if op.Source != "" || op.SHA256 != "" {
			return errors.New("remove takes only a target")
		}
	case FileOpResetAccess:
		if op.Source != "" || op.SHA256 != "" {
			return errors.New("reset-access takes only a target")
		}
		if filepath.Dir(op.Target) == op.Target {
			return errors.New("target cannot be a volume root")
		}
	default:
		return errors.New("operation is not allowed")
	}
	if system, err := windows.GetSystemWindowsDirectory(); err != nil || platform.SameOrChildPath(system, op.Target) {
		return errors.New("target cannot be in the Windows folder")
	}
	return nil
}

// isLocalDrivePath rejects UNC and device paths and drive letters mapped to a share: a share is
// resolved with the helper's credentials instead of the user's, and device paths bypass path
// normalization. Drive letters live in each logon session, so a mapped or subst drive of the user
// does not resolve in the elevated session, where the same letter may even name another drive.
func isLocalDrivePath(path string) bool {
	volume := filepath.VolumeName(path)
	if len(volume) != 2 || volume[1] != ':' || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	root, err := windows.UTF16PtrFromString(volume + `\`)
	if err != nil {
		return false
	}
	switch windows.GetDriveType(root) {
	case windows.DRIVE_REMOTE, windows.DRIVE_NO_ROOT_DIR, windows.DRIVE_UNKNOWN:
		return false
	}
	return true
}

var errWindowsFolder = errors.New("target cannot be in the Windows folder")

// openTargetDir opens the folder target is in and checks where it physically is, since a junction in the
// requested path can lead anywhere. Every later step goes through the returned root, so a link swapped into
// the path afterwards cannot move the operation. With create, missing folders are made below the deepest
// existing one; without it, a missing folder reports os.ErrNotExist.
func openTargetDir(target string, create bool) (*os.Root, error) {
	ancestor := filepath.Dir(target)
	var missing []string
	for {
		info, err := os.Stat(ancestor)
		if err == nil {
			if !info.IsDir() {
				return nil, fmt.Errorf("%q is not a folder", ancestor)
			}
			break
		}
		parent := filepath.Dir(ancestor)
		if !errors.Is(err, os.ErrNotExist) || parent == ancestor {
			return nil, err
		}
		missing = append([]string{filepath.Base(ancestor)}, missing...)
		ancestor = parent
	}

	root, err := os.OpenRoot(ancestor)
	if err != nil {
		return nil, err
	}
	if err := checkPhysicalLocation(root); err != nil {
		_ = root.Close()
		return nil, err
	}
	if len(missing) == 0 {
		return root, nil
	}
	defer func() { _ = root.Close() }()
	if !create {
		return nil, os.ErrNotExist
	}

	relative := filepath.Join(missing...)
	if err := root.MkdirAll(relative, 0o755); err != nil {
		return nil, err
	}
	dir, err := root.OpenRoot(relative)
	if err != nil {
		return nil, err
	}
	if err := checkPhysicalLocation(dir); err != nil {
		_ = dir.Close()
		return nil, err
	}
	return dir, nil
}

func checkPhysicalLocation(dir *os.Root) error {
	self, err := dir.Open(".")
	if err != nil {
		return err
	}
	defer func() { _ = self.Close() }()
	final, err := platform.FinalPathOfHandle(windows.Handle(self.Fd()))
	if err != nil {
		return err
	}
	return checkOutsideWindowsFolder(final)
}

// checkOutsideWindowsFolder rejects a physical path, one with every link resolved, in the Windows folder.
func checkOutsideWindowsFolder(path string) error {
	system, err := windows.GetSystemWindowsDirectory()
	if err != nil {
		return err
	}
	physical, err := platform.FinalPath(system)
	if err != nil {
		return err
	}
	if platform.SameOrChildPath(physical, path) {
		return errWindowsFolder
	}
	return nil
}

func copyVerifiedFile(ctx context.Context, op FileOp) (returnErr error) {
	dir, err := openTargetDir(op.Target, true)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	name := filepath.Base(op.Target)

	readOnly := false
	switch info, err := dir.Lstat(name); {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return err
	case !info.Mode().IsRegular():
		return errors.New("target is not a regular file")
	default:
		readOnly = info.Mode().Perm()&0o200 == 0
	}
	source, err := os.Open(op.Source)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()

	temporaryName := ".nahida-write-" + rand.Text()
	temporary, err := dir.OpenFile(temporaryName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	replaced := false
	defer func() {
		if !replaced {
			returnErr = errors.Join(returnErr, dir.Remove(temporaryName))
		}
	}()
	digest := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(temporary, digest), readerFunc(func(buffer []byte) (int, error) {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		return source.Read(buffer)
	}))
	if err := errors.Join(copyErr, temporary.Sync(), temporary.Close()); err != nil {
		return err
	}

	// The digest covers the bytes that landed in the destination directory, so a staged file that
	// changed after the application hashed it is never published.
	if !strings.EqualFold(hex.EncodeToString(digest.Sum(nil)), op.SHA256) {
		return errors.New("staged file does not match its digest")
	}

	// Windows refuses to replace a read-only file, and administrator rights do not override that.
	if readOnly {
		if err := dir.Chmod(name, 0o600); err != nil {
			return err
		}
	}
	if err := platform.ReplaceAtomicAt(dir, temporaryName, name); err != nil {
		if readOnly {
			err = errors.Join(err, dir.Chmod(name, 0o400))
		}
		return err
	}
	replaced = true

	// The published file is the temporary one, which never carried the attribute.
	if readOnly {
		if err := dir.Chmod(name, 0o400); err != nil {
			return fmt.Errorf("restore read-only attribute: %w", err)
		}
	}
	return nil
}

func removeFile(_ context.Context, op FileOp) error {
	dir, err := openTargetDir(op.Target, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	name := filepath.Base(op.Target)

	info, err := dir.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("target is not a regular file")
	}
	return dir.Remove(name)
}

func resetFileAccess(target string, owner *windows.SID) error {
	if owner == nil {
		return errors.New("the account to give the file to is unknown")
	}
	name, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}

	// Backup and restore rights let the helper open and rewrite an entry whose permissions shut out even
	// administrators, which is the case this operation exists for.
	privileges := []string{"SeBackupPrivilege", "SeTakeOwnershipPrivilege", "SeRestorePrivilege"}
	return withPrivileges(privileges, func() error {
		// MAXIMUM_ALLOWED prevents SetSecurityInfo from propagating the DACL into unchecked descendants.
		// Require the security rights explicitly so an incomplete handle fails before changing ownership.
		handle, err := windows.CreateFile(
			name, windows.MAXIMUM_ALLOWED|windows.READ_CONTROL|windows.WRITE_DAC|windows.WRITE_OWNER,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING,
			windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0,
		)
		if err != nil {
			return err
		}
		defer func() { _ = windows.CloseHandle(handle) }()
		var info windows.ByHandleFileInformation
		if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
			return err
		}
		if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return errors.New("target is a link")
		}

		// Hard links share one security descriptor, so resetting it would also change a file outside the
		// requested folder.
		if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 && info.NumberOfLinks > 1 {
			return errors.New("target has other hard links")
		}
		final, err := platform.FinalPathOfHandle(handle)
		if err != nil {
			return err
		}
		if !platform.SamePathFold(final, target) {
			return errors.New("target is reached through a link")
		}
		if err := checkOutsideWindowsFolder(final); err != nil {
			return err
		}
		return resetHandleAccess(handle, owner)
	})
}

// The handle keeps both security changes on the checked entry even if its path changes. Setting the owner
// first makes inherited CREATOR OWNER entries name the account that requested the restore.
func resetHandleAccess(handle windows.Handle, owner *windows.SID) error {
	const owned = windows.OWNER_SECURITY_INFORMATION
	const inherited = windows.DACL_SECURITY_INFORMATION | windows.UNPROTECTED_DACL_SECURITY_INFORMATION

	// "D:" is an empty DACL, which the unprotected flag fills with the inherited entries. A nil DACL would
	// grant everyone full access instead.
	descriptor, err := windows.SecurityDescriptorFromString("D:")
	if err != nil {
		return err
	}
	empty, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	current, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, owned)
	if err != nil {
		return err
	}
	if currentOwner, _, err := current.Owner(); err != nil || !currentOwner.Equals(owner) {
		if err := windows.SetSecurityInfo(
			handle,
			windows.SE_FILE_OBJECT,
			owned,
			owner,
			nil,
			nil,
			nil,
		); err != nil {
			return fmt.Errorf("set owner: %w", err)
		}
	}
	return windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, inherited, nil, nil, empty, nil)
}

// withPrivileges enables the named privileges of the process token while run executes and then puts back
// the ones it changed. A privilege the token lacks stays off, and run meets the ordinary access checks.
func withPrivileges(names []string, run func() error) error {
	var token windows.Token
	if err := windows.OpenProcessToken(
		windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token,
	); err != nil {
		return err
	}
	defer func() { _ = token.Close() }()

	var restore []windows.Tokenprivileges
	defer func() {
		for _, previous := range restore {
			_ = windows.AdjustTokenPrivileges(token, false, &previous, 0, nil, nil)
		}
	}()
	for _, name := range names {
		privilege, err := windows.UTF16PtrFromString(name)
		if err != nil {
			return err
		}
		var luid windows.LUID
		if err := windows.LookupPrivilegeValue(nil, privilege, &luid); err != nil {
			return err
		}
		enable := windows.Tokenprivileges{
			PrivilegeCount: 1,
			Privileges:     [1]windows.LUIDAndAttributes{{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}},
		}
		var previous windows.Tokenprivileges
		var size uint32
		if err := windows.AdjustTokenPrivileges(
			token, false, &enable, uint32(unsafe.Sizeof(previous)), &previous, &size,
		); err != nil {
			return err
		}
		if previous.PrivilegeCount > 0 {
			restore = append(restore, previous)
		}
	}
	return run()
}

type readerFunc func([]byte) (int, error)

func (read readerFunc) Read(buffer []byte) (int, error) { return read(buffer) }
