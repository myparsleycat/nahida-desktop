package touchprofile

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"nahida.live/desktop/internal/infra"
)

// contractError preserves user-facing Electron error text, including its
// original capitalisation and punctuation.
type contractError string

func (e contractError) Error() string { return string(e) }

func newTouchID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func samePathFold(left, right string) bool {
	leftAbs, _ := filepath.Abs(left)
	rightAbs, _ := filepath.Abs(right)
	return strings.EqualFold(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
}

func sameOrChildPath(root, target string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(relative)
}

func writeTouchFileAtomic(target string, data []byte, mode os.FileMode) (returnErr error) {
	temp, err := os.CreateTemp(filepath.Dir(target), ".touch-profile-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() {
		cleanupErr := os.Remove(tempPath)
		if !errors.Is(cleanupErr, os.ErrNotExist) {
			returnErr = infra.WithCause(returnErr, infra.AnnotateError(cleanupErr, infra.Diagnostic{Stage: "cleanup"}))
		}
	}()
	if err := temp.Chmod(mode); err != nil {
		return infra.WithCause(err, temp.Close())
	}
	if _, err := temp.Write(data); err != nil {
		return infra.WithCause(err, temp.Close())
	}
	if err := temp.Sync(); err != nil {
		return infra.WithCause(err, temp.Close())
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return replaceAtomic(tempPath, target)
}

func copyTouchTree(ctx context.Context, source, target string) error {
	if err := os.Mkdir(target, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if samePathFold(path, source) {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if !sameOrChildPath(target, destination) {
			return errors.New("copy target escaped touch output directory")
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic links are not supported in touch mods: %s", path)
		}
		if entry.IsDir() {
			return os.Mkdir(destination, 0o755)
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return infra.WithCause(fmt.Errorf("unsupported mod entry: %s", path), err)
		}
		return copyTouchFile(path, destination, info.Mode().Perm())
	})
}

func copyTouchFile(source, target string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	return errors.Join(copyErr, closeErr)
}
