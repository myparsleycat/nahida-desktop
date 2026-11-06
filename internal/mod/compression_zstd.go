package mod

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"

	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const (
	managedZstdExtension = ".nzst"
	maxZstdRestoreSize   = int64(64 << 30)
)

// Encoders and decoders allocate window-sized history buffers, megabytes each,
// on first use, so they are kept for the next file instead. Only those that
// finished their stream go back; a failed one is dropped with its state.
var (
	zstdEncoders sync.Pool
	zstdDecoders sync.Pool
)

func compressDisabledZstd(
	ctx context.Context,
	roots []string,
	threshold int64,
	setTotals func(int, int64),
	progress func(string, int64, bool),
	mark compressionMutationMarker,
	declined *compressionDeclinedFiles,
	onError func(string, error),
) error {
	folders, err := disabledModFolders(ctx, roots)
	if err != nil {
		return err
	}
	var files []compressionFile
	for _, folder := range folders {
		found, err := collectZstdFiles(ctx, []string{folder}, func(path string, info fs.FileInfo) bool {
			// Media and archives are compressed already; reading them every pass gains nothing.
			_, compressed := xpressSkippedExtensions[strings.ToLower(filepath.Ext(path))]
			return info.Size() >= threshold && !compressed && !isZstdArchivePath(path) &&
				!isCompressionInProgressPath(path)
		}, mark, onError)
		files = append(files, found...)
		if err != nil {
			return err
		}
	}
	files = uniqueZstdFiles(files)
	declined.retain(folders, files)
	files = slices.DeleteFunc(files, declined.contains)
	setCompressionTotals(files, setTotals)
	mark = serialZstdMarker(mark)
	return runZstdWorkers(ctx, files, runtime.GOMAXPROCS(0), func(file compressionFile) error {
		noGain, err := compressZstdFileIfSmaller(ctx, file.path, mark)
		if noGain {
			declined.add(file)
		}
		return err
	}, progress, onError)
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
	archives, err := collectZstdFiles(ctx, roots, func(path string, _ fs.FileInfo) bool {
		return isManagedZstdPath(path) && include(path)
	}, mark, onError)
	if err != nil {
		return err
	}
	archives = uniqueZstdFiles(archives)
	setCompressionTotals(archives, setTotals)
	mark = serialZstdMarker(mark)
	return runZstdWorkers(
		ctx,
		archives,
		zstdRestoreWorkerCount(archives, runtime.GOMAXPROCS(0)),
		func(file compressionFile) error {
			return restoreZstdFile(ctx, file.path, maxZstdRestoreSize, mark)
		},
		progress,
		onError,
	)
}

func uniqueZstdFiles(files []compressionFile) []compressionFile {
	seen := make(map[string]struct{}, len(files))
	result := files[:0]
	for _, file := range files {
		key := strings.ToLower(filepath.Clean(file.path))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, file)
	}
	return result
}

func zstdRestoreWorkerCount(files []compressionFile, workers int) int {
	inputs := make(map[string]struct{}, len(files))
	for _, file := range files {
		inputs[strings.ToLower(filepath.Clean(file.path))] = struct{}{}
	}
	for path := range inputs {
		// Nested archive names can make one restore overwrite another input.
		if _, overlaps := inputs[strings.TrimSuffix(path, managedZstdExtension)]; overlaps {
			return 1
		}
	}
	return workers
}

func serialZstdMarker(mark compressionMutationMarker) compressionMutationMarker {
	var mu sync.Mutex
	return func(paths ...string) {
		mu.Lock()
		defer mu.Unlock()
		mark(paths...)
	}
}

func runZstdWorkers(
	ctx context.Context,
	files []compressionFile,
	workers int,
	process func(compressionFile) error,
	progress func(string, int64, bool),
	onError func(string, error),
) error {
	if len(files) == 0 || ctx.Err() != nil {
		return ctx.Err()
	}
	workers = min(len(files), max(1, workers))
	type result struct {
		file compressionFile
		err  error
	}
	jobs := make(chan compressionFile)
	results := make(chan result, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for file := range jobs {
				if ctx.Err() != nil {
					return
				}
				results <- result{file: file, err: process(file)}
			}
		})
	}
	go func() {
		defer func() {
			close(jobs)
			wg.Wait()
			close(results)
		}()
		for _, file := range files {
			if ctx.Err() != nil {
				return
			}
			select {
			case jobs <- file:
			case <-ctx.Done():
				return
			}
		}
	}()
	// Drain even after cancellation so no worker outlives the operation lock.
	for result := range results {
		if err := ctx.Err(); err != nil && errors.Is(result.err, err) {
			continue
		}
		progress(result.file.path, result.file.size, false)
		if result.err != nil && onError != nil {
			onError(result.file.path, result.err)
		}
	}
	return ctx.Err()
}

func restoreZstdFolder(
	ctx context.Context,
	folder string,
	mark compressionMutationMarker,
	onError func(string, error),
) error {
	return restoreZstdFiles(
		ctx,
		[]string{folder},
		func(string) bool { return true },
		func(int, int64) {},
		func(string, int64, bool) {},
		mark,
		onError,
	)
}

func disabledModFolders(ctx context.Context, roots []string) ([]string, error) {
	var folders []string
	var errs []error
	for _, root := range roots {
		if isDisabled(filepath.Base(root)) {
			folders = append(folders, root)
			continue
		}
		release, err := diskio.AcquireDir(ctx, root)
		if err != nil {
			errs = append(errs, err)
			break
		}
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				if !errors.Is(walkErr, fs.ErrNotExist) {
					errs = append(errs, fmt.Errorf("access %q: %w", path, walkErr))
				}
				return nil
			}
			if !entry.IsDir() {
				return nil
			}
			if path != root && platform.IsUnaddressableName(entry.Name()) {
				return filepath.SkipDir
			}
			info, err := entry.Info()
			if err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					errs = append(errs, err)
				}
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
		release()
		if err != nil {
			errs = append(errs, err)
		}
	}
	return folders, errors.Join(errs...)
}

func filesForZstd(folder string, threshold int64) ([]compressionFile, error) {
	return walkCompressionFiles(context.Background(), []string{folder}, func(path string, info fs.FileInfo) bool {
		return info.Size() >= threshold && !isZstdArchivePath(path) && !isLegacyZstdArtifact(path)
	})
}

func collectZstdFiles(
	ctx context.Context,
	roots []string,
	accept func(string, fs.FileInfo) bool,
	mark compressionMutationMarker,
	onError func(string, error),
) ([]compressionFile, error) {
	return walkCompressionFiles(ctx, roots, func(path string, info fs.FileInfo) bool {
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

// hasZstdArchives reports whether a managed archive lies anywhere below folder. An unreadable entry counts as
// one, so the caller restores rather than skipping archives it could not see.
func hasZstdArchives(folder string) bool {
	defer holdScanDisk(folder)()
	found := false
	_ = filepath.WalkDir(folder, func(path string, entry fs.DirEntry, err error) error {
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			found = true
			return fs.SkipAll
		}
		if err == nil && entry.Type().IsRegular() &&
			strings.HasSuffix(strings.ToLower(entry.Name()), managedZstdExtension) {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
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
	_, err := compressZstdFileIfSmaller(ctx, sourcePath, mark)
	return err
}

// compressZstdFileIfSmaller replaces sourcePath with its archive. It reports noGain, leaving the source as it
// is, when the archive would not be smaller.
func compressZstdFileIfSmaller(
	ctx context.Context,
	sourcePath string,
	mark compressionMutationMarker,
) (noGain bool, returnErr error) {
	release, err := diskio.Acquire(ctx, sourcePath)
	if err != nil {
		return false, err
	}
	defer release()

	info, err := os.Stat(sourcePath)
	if err != nil {
		return false, err
	}
	targetPath := sourcePath + managedZstdExtension
	tempPath := targetPath + compressionTempMarker
	if targetInfo, err := os.Lstat(targetPath); err == nil {
		if !targetInfo.Mode().IsRegular() {
			return false, fmt.Errorf("zstd destination is not a regular file: %s", targetPath)
		}
		mark(targetPath)
		if err := os.Remove(targetPath); err != nil {
			return false, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	mark(tempPath)
	if err := os.Remove(tempPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	err = streamCompressZstdWithLimit(ctx, sourcePath, tempPath, info.Size())
	if errors.Is(err, errZstdNoGain) {
		return true, nil
	}
	if err != nil {
		return false, infra.WithCause(fmt.Errorf("compress %q: %w", sourcePath, err), os.Remove(tempPath))
	}
	tempInfo, err := os.Stat(tempPath)
	if err != nil {
		return false, err
	}
	if tempInfo.Size() >= info.Size() {
		return true, os.Remove(tempPath)
	}
	if err := os.Chmod(tempPath, info.Mode()); err != nil {
		return false, err
	}
	if err := os.Chtimes(tempPath, info.ModTime(), info.ModTime()); err != nil {
		return false, err
	}
	// The source stays writable while it is compressed. A write in that window would leave the archive
	// with old content and then delete the new file, so the source is kept and retried on a later pass.
	if current, err := os.Stat(sourcePath); err != nil || current.Size() != info.Size() ||
		!current.ModTime().Equal(info.ModTime()) {
		removeErr := os.Remove(tempPath)
		if err != nil {
			return false, infra.WithCause(err, removeErr)
		}
		return false, removeErr
	}
	mark(sourcePath, targetPath)
	if err := os.Rename(tempPath, targetPath); err != nil {
		return false, err
	}
	return false, os.Remove(sourcePath)
}

func restoreZstdFile(ctx context.Context, targetPath string, limit int64, mark compressionMutationMarker) error {
	if !isManagedZstdPath(targetPath) {
		return fmt.Errorf("not a managed zstd path: %s", targetPath)
	}
	release, err := diskio.Acquire(ctx, targetPath)
	if err != nil {
		return err
	}
	defer release()

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
	if err := streamRestoreZstdWithLimit(ctx, targetPath, tempPath, limit); err != nil {
		return infra.WithCause(err, os.Remove(tempPath))
	}
	if err := os.Chmod(tempPath, targetInfo.Mode()); err != nil {
		return infra.WithCause(err, os.Remove(tempPath))
	}
	if err := os.Chtimes(tempPath, targetInfo.ModTime(), targetInfo.ModTime()); err != nil {
		return infra.WithCause(err, os.Remove(tempPath))
	}
	mark(sourcePath, targetPath)
	if err := platform.MoveNoReplace(tempPath, sourcePath); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return infra.WithCause(err, os.Remove(tempPath))
		}
		// A file written to the source path during the restore is newer than the archive, which is
		// handled like a source that was already present.
		if err := os.Remove(tempPath); err != nil {
			return err
		}
	}
	return os.Remove(targetPath)
}

// errZstdNoGain stops a compression whose output has reached the size of its source.
var errZstdNoGain = errors.New("zstd output is not smaller than its source")

type zstdLimitWriter struct {
	writer  io.Writer
	limit   int64
	written int64
}

func (w *zstdLimitWriter) Write(data []byte) (int, error) {
	if w.written+int64(len(data)) >= w.limit {
		return 0, errZstdNoGain
	}
	written, err := w.writer.Write(data)
	w.written += int64(written)
	return written, err
}

func streamCompressZstd(ctx context.Context, sourcePath, tempPath string) error {
	return streamCompressZstdWithLimit(ctx, sourcePath, tempPath, 0)
}

// streamCompressZstdWithLimit fails with errZstdNoGain as soon as the output reaches limit bytes, so an
// incompressible file is not read to its end. A limit of 0 or less disables the check.
func streamCompressZstdWithLimit(ctx context.Context, sourcePath, tempPath string, limit int64) (returnErr error) {
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
			returnErr = infra.WithCause(returnErr, os.Remove(tempPath))
		}
	}()
	encoder, ok := zstdEncoders.Get().(*zstd.Encoder)
	if !ok {
		encoder, err = zstd.NewWriter(
			nil,
			zstd.WithEncoderLevel(zstd.SpeedDefault),
			zstd.WithEncoderConcurrency(1),
			zstd.WithEncoderCRC(true),
		)
		if err != nil {
			return err
		}
	}
	var output io.Writer = temp
	if limit > 0 {
		output = &zstdLimitWriter{writer: temp, limit: limit}
	}
	encoder.Reset(output)
	_, copyErr := io.Copy(encoder, &contextReader{ctx: ctx, reader: source})
	closeErr := encoder.Close()
	if copyErr != nil || closeErr != nil {
		return errors.Join(copyErr, closeErr)
	}
	encoder.Reset(nil)
	zstdEncoders.Put(encoder)

	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	remove = false
	return nil
}

func streamRestoreZstdWithLimit(ctx context.Context, sourcePath, tempPath string, limit int64) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	decoder, ok := zstdDecoders.Get().(*zstd.Decoder)
	if !ok {
		decoder, err = zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))
		if err != nil {
			return err
		}
	}
	if err := decoder.Reset(source); err != nil {
		return err
	}
	temp, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	decoded := io.LimitReader(&contextReader{ctx: ctx, reader: decoder}, limit+1)
	written, copyErr := io.Copy(temp, decoded)
	if copyErr != nil {
		return infra.WithCause(copyErr, temp.Close())
	}
	if err := temp.Sync(); err != nil {
		return infra.WithCause(err, temp.Close())
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if written > limit {
		return fmt.Errorf("zstd data exceeds restore limit: %s", sourcePath)
	}
	_ = decoder.Reset(nil)
	zstdDecoders.Put(decoder)
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
