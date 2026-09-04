package mod

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

const (
	managedZstdExtension = ".nzst"
	maxZstdRestoreSize   = int64(64 << 30)
)

func compressDisabledZstd(
	ctx context.Context,
	roots []string,
	threshold int64,
	setTotals func(int, int64),
	progress func(string, int64, bool),
	mark compressionMutationMarker,
	onError func(string, error),
) error {
	folders, err := disabledModFolders(roots)
	if err != nil {
		return err
	}
	var files []compressionFile
	for _, folder := range folders {
		found, err := collectZstdFiles([]string{folder}, func(path string, info fs.FileInfo) bool {
			return info.Size() >= threshold && !isZstdArchivePath(path)
		}, mark, onError)
		files = append(files, found...)
		if err != nil {
			return err
		}
	}
	setCompressionTotals(files, setTotals)
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := compressZstdFile(ctx, file.path, mark)
		progress(file.path, file.size, false)
		if err != nil {
			onError(file.path, err)
		}
	}
	return nil
}

func restoreAllZstd(
	ctx context.Context,
	roots []string,
	setTotals func(int, int64),
	progress func(string, int64, bool),
	mark compressionMutationMarker,
	onError func(string, error),
) error {
	return restoreZstdFiles(ctx, roots, func(string) bool { return true }, setTotals, progress, mark, onError)
}

func restoreEnabledZstd(
	ctx context.Context,
	roots []string,
	setTotals func(int, int64),
	progress func(string, int64, bool),
	mark compressionMutationMarker,
	onError func(string, error),
) error {
	return restoreZstdFiles(ctx, roots, func(path string) bool {
		return !zstdPathIsDisabled(path, roots)
	}, setTotals, progress, mark, onError)
}

func restoreZstdFiles(
	ctx context.Context,
	roots []string,
	include func(string) bool,
	setTotals func(int, int64),
	progress func(string, int64, bool),
	mark compressionMutationMarker,
	onError func(string, error),
) error {
	archives, err := collectZstdFiles(roots, func(path string, _ fs.FileInfo) bool {
		return isManagedZstdPath(path) && include(path)
	}, mark, onError)
	if err != nil {
		return err
	}
	setCompressionTotals(archives, setTotals)
	for _, archive := range archives {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := restoreZstdFile(ctx, archive.path, mark)
		progress(archive.path, archive.size, false)
		if errors.Is(err, context.Canceled) {
			return err
		}
		if err != nil {
			onError(archive.path, err)
		}
	}
	return nil
}

func restoreZstdFolder(
	ctx context.Context,
	folder string,
	mark compressionMutationMarker,
	onError func(string, error),
) error {
	return restoreZstdFiles(ctx, []string{folder}, func(string) bool { return true }, func(int, int64) {}, func(string, int64, bool) {}, mark, onError)
}

func disabledModFolders(roots []string) ([]string, error) {
	var folders []string
	var errs []error
	for _, root := range roots {
		if isDisabled(filepath.Base(root)) {
			folders = append(folders, root)
			continue
		}
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				errs = append(errs, fmt.Errorf("access %q: %w", path, walkErr))
				return nil
			}
			if !entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				errs = append(errs, err)
				return filepath.SkipDir
			}
			if info.Mode()&os.ModeSymlink != 0 || isReparsePoint(info) {
				return filepath.SkipDir
			}
			if path != root && isDisabled(entry.Name()) {
				folders = append(folders, path)
				return filepath.SkipDir
			}
			return nil
		})
		if err != nil {
			errs = append(errs, err)
		}
	}
	return folders, errors.Join(errs...)
}

func filesForZstd(folder string, threshold int64) ([]compressionFile, error) {
	return walkCompressionFiles([]string{folder}, func(path string, info fs.FileInfo) bool {
		return info.Size() >= threshold && !isZstdArchivePath(path) && !isLegacyZstdArtifact(path)
	})
}

func collectZstdFiles(
	roots []string,
	accept func(string, fs.FileInfo) bool,
	mark compressionMutationMarker,
	onError func(string, error),
) ([]compressionFile, error) {
	return walkCompressionFiles(roots, func(path string, info fs.FileInfo) bool {
		if !isLegacyZstdArtifact(path) {
			return accept(path, info)
		}
		mark(path)
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			onError(path, err)
		}
		return false
	})
}

func isLegacyZstdArtifact(path string) bool {
	name := filepath.Base(path)
	return name == legacyCompressionManifestName || strings.Contains(name, compressionTempMarker)
}

func isManagedZstdPath(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), managedZstdExtension)
}

func isZstdArchivePath(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".zst") || strings.HasSuffix(lower, managedZstdExtension)
}

func zstdPathIsDisabled(path string, roots []string) bool {
	for _, root := range roots {
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		if isDisabled(filepath.Base(root)) {
			return true
		}
		directory := filepath.Dir(relative)
		if directory == "." {
			continue
		}
		for _, component := range strings.Split(directory, string(filepath.Separator)) {
			if isDisabled(component) {
				return true
			}
		}
	}
	return false
}

func compressZstdFile(ctx context.Context, sourcePath string, mark compressionMutationMarker) error {
	info, err := os.Stat(sourcePath)
	if err != nil {
		return err
	}
	targetPath := sourcePath + managedZstdExtension
	tempPath := targetPath + compressionTempMarker
	if targetInfo, err := os.Lstat(targetPath); err == nil {
		if !targetInfo.Mode().IsRegular() {
			return fmt.Errorf("zstd destination is not a regular file: %s", targetPath)
		}
		mark(targetPath)
		if err := os.Remove(targetPath); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	mark(tempPath)
	if err := os.Remove(tempPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := streamCompressZstd(ctx, sourcePath, tempPath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("compress %q: %w", sourcePath, err)
	}
	tempInfo, err := os.Stat(tempPath)
	if err != nil {
		return err
	}
	if tempInfo.Size() >= info.Size() {
		return os.Remove(tempPath)
	}
	if err := os.Chmod(tempPath, info.Mode()); err != nil {
		return err
	}
	if err := os.Chtimes(tempPath, info.ModTime(), info.ModTime()); err != nil {
		return err
	}
	mark(sourcePath, targetPath)
	if err := os.Rename(tempPath, targetPath); err != nil {
		return err
	}
	return os.Remove(sourcePath)
}

func restoreZstdFile(ctx context.Context, targetPath string, mark compressionMutationMarker) error {
	if !isManagedZstdPath(targetPath) {
		return fmt.Errorf("not a managed zstd path: %s", targetPath)
	}
	targetInfo, err := os.Stat(targetPath)
	if err != nil {
		return err
	}
	sourcePath := targetPath[:len(targetPath)-len(managedZstdExtension)]
	tempPath := sourcePath + compressionTempMarker
	if sourceInfo, err := os.Lstat(sourcePath); err == nil {
		if !sourceInfo.Mode().IsRegular() {
			return fmt.Errorf("zstd source is not a regular file: %s", sourcePath)
		}
		mark(sourcePath, targetPath, tempPath)
		if err := os.Remove(tempPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return os.Remove(targetPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	mark(tempPath)
	if err := os.Remove(tempPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := streamRestoreZstd(ctx, targetPath, tempPath); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Chmod(tempPath, targetInfo.Mode()); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Chtimes(tempPath, targetInfo.ModTime(), targetInfo.ModTime()); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	mark(sourcePath, targetPath)
	if err := os.Rename(tempPath, sourcePath); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return os.Remove(targetPath)
}

func streamCompressZstd(ctx context.Context, sourcePath, tempPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	temp, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	remove := true
	defer func() {
		_ = temp.Close()
		if remove {
			_ = os.Remove(tempPath)
		}
	}()
	encoder, err := zstd.NewWriter(temp, zstd.WithEncoderLevel(zstd.SpeedDefault), zstd.WithEncoderConcurrency(1), zstd.WithEncoderCRC(true))
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(encoder, &contextReader{ctx: ctx, reader: source})
	closeErr := encoder.Close()
	if copyErr != nil || closeErr != nil {
		return errors.Join(copyErr, closeErr)
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	remove = false
	return nil
}

func streamRestoreZstd(ctx context.Context, sourcePath, tempPath string) error {
	return streamRestoreZstdWithLimit(ctx, sourcePath, tempPath, maxZstdRestoreSize)
}

func streamRestoreZstdWithLimit(ctx context.Context, sourcePath, tempPath string, limit int64) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	decoder, err := zstd.NewReader(source, zstd.WithDecoderConcurrency(1))
	if err != nil {
		return err
	}
	defer decoder.Close()
	temp, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	decoded := io.LimitReader(&contextReader{ctx: ctx, reader: decoder}, limit+1)
	written, copyErr := io.Copy(temp, decoded)
	if copyErr != nil {
		_ = temp.Close()
		return copyErr
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if written > limit {
		return fmt.Errorf("zstd data exceeds restore limit: %s", sourcePath)
	}
	return nil
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
