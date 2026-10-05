//go:build windows

package elevated

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/platform"
)

const (
	fileOpsTimeout = 2 * time.Minute
	// maxFilePayloadSize leaves room in a protocol message for the envelope around the operations.
	maxFilePayloadSize = maxMessageSize - 1024
)

// applyFileOps runs ops in order with the helper's administrator rights. Every operation is
// validated before the first one runs, but they are not a transaction: a failure leaves the
// earlier operations applied.
func applyFileOps(ctx context.Context, ops []FileOp) error {
	for _, op := range ops {
		if err := validateFileOp(op); err != nil {
			return fmt.Errorf("%s %q: %w", op.Kind, op.Target, err)
		}
	}
	for _, op := range ops {
		if err := ctx.Err(); err != nil {
			return err
		}
		apply := removeFile
		if op.Kind == FileOpCopy {
			apply = copyVerifiedFile
		}
		if err := apply(ctx, op); err != nil {
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
	default:
		return errors.New("operation is not allowed")
	}
	return nil
}

// isLocalDrivePath rejects UNC and device paths and drive letters mapped to a share: a share is
// resolved with the helper's credentials instead of the user's, a mapping the user made is
// missing from the elevated logon session, and device paths bypass path normalization.
func isLocalDrivePath(path string) bool {
	volume := filepath.VolumeName(path)
	if len(volume) != 2 || volume[1] != ':' || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	root, err := windows.UTF16PtrFromString(volume + `\`)
	return err == nil && windows.GetDriveType(root) != windows.DRIVE_REMOTE
}

func copyVerifiedFile(ctx context.Context, op FileOp) (returnErr error) {
	directory := filepath.Dir(op.Target)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	readOnly := false
	switch info, err := os.Lstat(op.Target); {
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

	temporary, err := os.CreateTemp(directory, ".nahida-write-")
	if err != nil {
		return err
	}
	replaced := false
	defer func() {
		if !replaced {
			returnErr = errors.Join(returnErr, os.Remove(temporary.Name()))
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
		if err := os.Chmod(op.Target, 0o600); err != nil {
			return err
		}
	}
	if err := platform.ReplaceAtomic(temporary.Name(), op.Target); err != nil {
		if readOnly {
			err = errors.Join(err, os.Chmod(op.Target, 0o400))
		}
		return err
	}
	replaced = true
	return nil
}

func removeFile(_ context.Context, op FileOp) error {
	info, err := os.Lstat(op.Target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("target is not a regular file")
	}
	return os.Remove(op.Target)
}

type readerFunc func([]byte) (int, error)

func (read readerFunc) Read(buffer []byte) (int, error) { return read(buffer) }
