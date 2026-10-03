//go:build windows

package xxmi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

const userDataProgressInterval = 100 * time.Millisecond

// move carries from over to to. A rename is instant but cannot leave its volume, so user data on another volume is
// copied instead; its source stays in place for manual cleanup, and rollback only removes the copy.
func (m *importerFolderMigration) move(ctx context.Context, key, name, from, to string) error {
	err := m.rename(from, to)
	if err == nil {
		m.moves = append(m.moves, importerFolderMove{from: from, to: to})
		return nil
	}
	if !errors.Is(err, windows.ERROR_NOT_SAME_DEVICE) {
		return fmt.Errorf("move %q to %q: %w", from, to, err)
	}

	total, err := userDataSize(ctx, from)
	if err != nil {
		return fmt.Errorf("measure %q: %w", from, err)
	}
	var free uint64
	parent, err := windows.UTF16PtrFromString(filepath.Dir(to))
	if err != nil {
		return err
	}
	if err := windows.GetDiskFreeSpaceEx(parent, &free, nil, nil); err != nil {
		return fmt.Errorf("read free space of %q: %w", filepath.Dir(to), err)
	}
	if uint64(total) > free {
		return fmt.Errorf("%w: %s %q needs %d bytes, %d free", errImportNoSpace, key, to, total, free)
	}

	var copied int64
	var reported time.Time
	report := func(n int64) {
		copied += n
		if m.progress == nil || copied < total && time.Since(reported) < userDataProgressInterval {
			return
		}
		reported = time.Now()
		m.progress(key, name, copied, total)
	}

	// Recorded before copying so rollback also removes a partial copy.
	m.moves = append(m.moves, importerFolderMove{from: from, to: to, copied: true})
	if err := copyUserData(ctx, from, to, report); err != nil {
		return fmt.Errorf("copy %q to %q: %w", from, to, err)
	}
	return nil
}

func userDataSize(ctx context.Context, path string) (int64, error) {
	var total int64
	err := filepath.WalkDir(path, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if isInstallReparsePoint(info) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// copyUserData copies the file or folder tree at from to to. Links are recreated with their original target
// instead of being followed, as a rename would have carried them over; anything else that is not a regular file
// fails the copy rather than silently omitting user data.
func copyUserData(ctx context.Context, from, to string, report func(int64)) error {
	buffer := make([]byte, 1<<20)
	return filepath.WalkDir(from, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}

		switch {
		case isInstallReparsePoint(info):
			linkTo, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("unsupported reparse point %q: %w", path, err)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				err = os.Symlink(linkTo, target)
			} else {
				err = createJunction(target, linkTo)
			}
			if err != nil || !entry.IsDir() {
				return err
			}
			return fs.SkipDir
		case info.IsDir():
			return os.Mkdir(target, 0o755)
		case !info.Mode().IsRegular():
			return fmt.Errorf("unsupported file type: %q", path)
		}
		return copyUserDataFile(ctx, path, target, info, buffer, report)
	})
}

func copyUserDataFile(
	ctx context.Context, from, to string, info fs.FileInfo, buffer []byte, report func(int64),
) (returnErr error) {
	input, err := os.Open(from)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, input.Close()) }()

	// A displaced file is replaced in place, so the target may already exist.
	output, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, err = io.CopyBuffer(output, &userDataReader{ctx: ctx, reader: input, report: report}, buffer)
	if err := errors.Join(err, output.Close()); err != nil {
		return err
	}
	return os.Chtimes(to, info.ModTime(), info.ModTime())
}

type userDataReader struct {
	ctx    context.Context
	reader io.Reader
	report func(int64)
}

func (r *userDataReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(buffer)
	r.report(int64(n))
	return n, err
}
