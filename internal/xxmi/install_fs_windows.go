//go:build windows

package xxmi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

type installRoot struct {
	path string
	root *os.Root
}

func openInstallRoot(path string) (*installRoot, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	absolute = filepath.Clean(absolute)
	before, err := os.Lstat(absolute)
	if err != nil {
		return nil, err
	}
	if err := validateInstallDirectory(before, absolute); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, err
	}
	after, err := root.Stat(".")
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	if !os.SameFile(before, after) {
		_ = root.Close()
		return nil, fmt.Errorf("directory identity changed while opening %q", absolute)
	}
	return &installRoot{path: absolute, root: root}, nil
}

func ensureInstallRoot(path string) (*installRoot, error) {
	root, err := openInstallRoot(path)
	if err == nil {
		return root, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	absolute = filepath.Clean(absolute)
	parentPath := filepath.Dir(absolute)
	if parentPath == absolute {
		return nil, fmt.Errorf("create installation root %q: %w", absolute, os.ErrNotExist)
	}
	parent, err := ensureInstallRoot(parentPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = parent.Close() }()

	name := filepath.Base(absolute)
	if err := parent.root.Mkdir(name, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	return parent.openChild(name)
}

func (r *installRoot) Close() error {
	if r == nil || r.root == nil {
		return nil
	}
	return r.root.Close()
}

func (r *installRoot) openChild(name string) (*installRoot, error) {
	if err := validateInstallComponent(name); err != nil {
		return nil, err
	}
	before, err := r.root.Lstat(name)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(r.path, name)
	if err := validateInstallDirectory(before, path); err != nil {
		return nil, err
	}
	child, err := r.root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	after, err := child.Stat(".")
	if err != nil {
		_ = child.Close()
		return nil, err
	}
	if !os.SameFile(before, after) {
		_ = child.Close()
		return nil, fmt.Errorf("directory identity changed while opening %q", path)
	}
	return &installRoot{path: path, root: child}, nil
}

func (r *installRoot) mkdirAll(relative string, perm fs.FileMode) error {
	return r.withDirectory(relative, true, perm, func(*os.Root) error { return nil })
}

func (r *installRoot) withDirectory(
	relative string,
	create bool,
	perm fs.FileMode,
	operation func(*os.Root) error,
) error {
	parts, err := splitInstallPath(relative)
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		return operation(r.root)
	}

	current := r
	opened := make([]*installRoot, 0, len(parts))
	defer func() {
		for index := len(opened) - 1; index >= 0; index-- {
			_ = opened[index].Close()
		}
	}()
	for _, part := range parts {
		child, openErr := current.openChild(part)
		if openErr != nil && create && errors.Is(openErr, os.ErrNotExist) {
			if mkdirErr := current.root.Mkdir(part, perm); mkdirErr != nil && !errors.Is(mkdirErr, fs.ErrExist) {
				return mkdirErr
			}
			child, openErr = current.openChild(part)
		}
		if openErr != nil {
			return openErr
		}
		opened = append(opened, child)
		current = child
	}
	return operation(current.root)
}

func (r *installRoot) readFile(relative string) ([]byte, os.FileInfo, error) {
	var (
		data []byte
		info os.FileInfo
	)
	err := r.withParent(relative, false, 0, func(parent *os.Root, name string) error {
		before, err := parent.Lstat(name)
		if err != nil {
			return err
		}
		if err := validateInstallFile(before, filepath.Join(r.path, relative)); err != nil {
			return err
		}
		file, err := parent.Open(name)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		after, err := file.Stat()
		if err != nil {
			return err
		}
		if !os.SameFile(before, after) {
			return fmt.Errorf("file identity changed while opening %q", filepath.Join(r.path, relative))
		}
		data, err = io.ReadAll(file)
		if err == nil {
			info = after
		}
		return err
	})
	return data, info, err
}

func (r *installRoot) writeFileAtomic(
	ctx context.Context,
	relative string,
	input io.Reader,
	perm fs.FileMode,
	expected os.FileInfo,
) error {
	return r.withParent(relative, true, 0o755, func(parent *os.Root, name string) (operationErr error) {
		before, err := parent.Lstat(name)
		switch {
		case err == nil:
			if err := validateInstallFile(before, filepath.Join(r.path, relative)); err != nil {
				return err
			}
			if expected != nil && !os.SameFile(expected, before) {
				return fmt.Errorf("file identity changed before replacing %q", filepath.Join(r.path, relative))
			}
		case errors.Is(err, os.ErrNotExist):
			if expected != nil {
				return fmt.Errorf("file disappeared before replacing %q", filepath.Join(r.path, relative))
			}
		default:
			return err
		}

		temporary, file, err := createInstallTempFile(parent, ".nahida-write-", perm)
		if err != nil {
			return err
		}
		removeTemporary := true
		defer func() {
			if removeTemporary {
				operationErr = errors.Join(operationErr, parent.Remove(temporary))
			}
		}()

		_, copyErr := io.Copy(file, &contextReader{ctx: ctx, reader: input})
		syncErr := file.Sync()
		closeErr := file.Close()
		if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		current, err := parent.Lstat(name)
		switch {
		case err == nil:
			if err := validateInstallFile(current, filepath.Join(r.path, relative)); err != nil {
				return err
			}
			if before == nil || !os.SameFile(before, current) {
				return fmt.Errorf("file identity changed before replacing %q", filepath.Join(r.path, relative))
			}
		case errors.Is(err, os.ErrNotExist):
			if before != nil {
				return fmt.Errorf("file disappeared before replacing %q", filepath.Join(r.path, relative))
			}
		default:
			return err
		}
		if err := parent.Rename(temporary, name); err != nil {
			return err
		}
		removeTemporary = false
		return nil
	})
}

func (r *installRoot) removeAll(relative string) error {
	return r.withParent(relative, false, 0, func(parent *os.Root, name string) error {
		info, err := parent.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if isInstallReparsePoint(info) {
			return fmt.Errorf("refusing to remove reparse point %q", filepath.Join(r.path, relative))
		}
		return parent.RemoveAll(name)
	})
}

func (r *installRoot) withParent(
	relative string,
	create bool,
	perm fs.FileMode,
	operation func(*os.Root, string) error,
) error {
	parts, err := splitInstallPath(relative)
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		return errors.New("installation path must name a child")
	}
	parent := filepath.Join(parts[:len(parts)-1]...)
	if parent == "" {
		parent = "."
	}
	return r.withDirectory(parent, create, perm, func(root *os.Root) error {
		return operation(root, parts[len(parts)-1])
	})
}

func (r *installRoot) childInfo(name string) (os.FileInfo, error) {
	if err := validateInstallComponent(name); err != nil {
		return nil, err
	}
	info, err := r.root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if isInstallReparsePoint(info) {
		return nil, fmt.Errorf("reparse point is not allowed at %q", filepath.Join(r.path, name))
	}
	return info, nil
}

func (r *installRoot) renameChild(oldName, newName string) error {
	if _, err := r.childInfo(oldName); err != nil {
		return err
	}
	if err := validateInstallComponent(newName); err != nil {
		return err
	}
	if _, err := r.childInfo(newName); err == nil {
		return fmt.Errorf("rename destination already exists: %q", filepath.Join(r.path, newName))
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return r.root.Rename(oldName, newName)
}

func createInstallTempFile(root *os.Root, prefix string, perm fs.FileMode) (string, *os.File, error) {
	for range 32 {
		random := make([]byte, 16)
		if _, err := rand.Read(random); err != nil {
			return "", nil, err
		}
		name := prefix + hex.EncodeToString(random)
		file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return name, file, err
	}
	return "", nil, errors.New("create unique installation temporary file")
}

func splitInstallPath(relative string) ([]string, error) {
	if relative == "" || relative == "." {
		return nil, nil
	}
	if filepath.IsAbs(relative) || !filepath.IsLocal(relative) {
		return nil, fmt.Errorf("installation path must be local: %q", relative)
	}
	cleaned := filepath.Clean(relative)
	parts := strings.FieldsFunc(cleaned, func(character rune) bool {
		return character == '/' || character == '\\'
	})
	for _, part := range parts {
		if err := validateInstallComponent(part); err != nil {
			return nil, err
		}
	}
	return parts, nil
}

func validateInstallComponent(name string) error {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return fmt.Errorf("invalid installation path component %q", name)
	}
	return nil
}

func validateInstallDirectory(info os.FileInfo, path string) error {
	if isInstallReparsePoint(info) {
		return fmt.Errorf("reparse point is not allowed at %q", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("installation path is not a directory: %q", path)
	}
	return nil
}

func validateInstallFile(info os.FileInfo, path string) error {
	if isInstallReparsePoint(info) {
		return fmt.Errorf("reparse point is not allowed at %q", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("installation path is not a regular file: %q", path)
	}
	return nil
}

func isInstallReparsePoint(info os.FileInfo) bool {
	if info == nil || info.Mode()&os.ModeSymlink != 0 {
		return info != nil
	}
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && data.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}
