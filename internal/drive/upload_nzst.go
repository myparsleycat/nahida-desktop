package drive

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
	"sync"

	"github.com/klauspost/compress/zstd"

	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/infra"
)

const (
	uploadNZSTExtension = ".nzst"
	uploadNZSTMaxSize   = int64(64 << 30)
)

// A decoder allocates a window-sized history buffer, megabytes, on first use,
// so it is kept for the next file. Only one that read its stream to the end
// goes back.
var uploadNZSTDecoders sync.Pool

func isUploadNZST(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), uploadNZSTExtension)
}

func uploadNZSTLimit(name string, allowed map[string]int64, limit int64) int64 {
	if extensionLimit := allowed[strings.ToLower(filepath.Ext(name))]; extensionLimit > 0 {
		limit = min(limit, extensionLimit)
	}
	return min(limit, uploadNZSTMaxSize)
}

// Return at most limit+1 bytes, allowing callers to distinguish an oversized
// file from corrupt data without trusting optional frame size metadata.
func copyUploadNZST(ctx context.Context, path string, output io.Writer, limit int64) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	release, err := diskio.Acquire(ctx, filepath.FromSlash(path))
	if err != nil {
		return 0, err
	}
	defer release()
	source, err := os.Open(filepath.FromSlash(path))
	if err != nil {
		return 0, err
	}
	defer func() { _ = source.Close() }()
	return decodeUploadNZST(ctx, source, output, limit)
}

func decodeUploadNZST(ctx context.Context, source *os.File, output io.Writer, limit int64) (int64, error) {
	info, err := source.Stat()
	if err != nil {
		return 0, err
	}
	if info.Size() == 0 {
		return 0, io.ErrUnexpectedEOF
	}
	decoder, ok := uploadNZSTDecoders.Get().(*zstd.Decoder)
	if !ok {
		decoder, err = zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))
		if err != nil {
			return 0, err
		}
	}
	reusable := false
	defer func() {
		if !reusable {
			decoder.Close()
		}
	}()
	if err := decoder.Reset(source); err != nil {
		return 0, err
	}
	written, err := io.Copy(output, io.LimitReader(&uploadNZSTReader{ctx: ctx, reader: decoder}, limit+1))
	if err == nil && written <= limit {
		_ = decoder.Reset(nil)
		reusable = true
		uploadNZSTDecoders.Put(decoder)
	}
	return written, err
}

type uploadNZSTReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *uploadNZSTReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

type uploadSourceError struct {
	File     UploadFile
	TempPath string
	Err      error
}

func (e *uploadSourceError) Error() string {
	return fmt.Sprintf("prepare upload source %q (%s): %v", e.File.Name, e.File.FullPath, e.Err)
}

func (e *uploadSourceError) Unwrap() error { return e.Err }

func (d *Drive) reportUploadSourceFailure(destinationID, stage string, err error) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	fields := map[string]any{"destinationId": destinationID}
	var sourceErr *uploadSourceError
	if errors.As(err, &sourceErr) {
		fields["name"] = sourceErr.File.Name
		fields["inputPath"] = sourceErr.File.FullPath
		fields["tempPath"] = sourceErr.TempPath
		fields["cleanupRegistered"] = sourceErr.TempPath != ""
	}
	return infra.ReportError(d.log, err, "Drive", infra.Diagnostic{Operation: "upload", Stage: stage, Fields: fields})
}

// The caller owns cleanup even on failure. Restart data must retain source
// paths; only this execution's copy may point at temporary decoded files.
func restoreUploadSources(
	ctx context.Context,
	files []UploadFile,
	tempDir *string,
	onProgress func(int64, bool),
) ([]UploadFile, map[string]uploadHashEntry, error) {
	restored := append([]UploadFile{}, files...)
	hashes := make(map[string]uploadHashEntry)
	for index, file := range files {
		if !isUploadNZST(file.FullPath) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if *tempDir == "" {
			directory, err := os.MkdirTemp("", "nahida-drive-upload-*")
			if err != nil {
				return nil, nil, &uploadSourceError{File: file, Err: err}
			}
			*tempDir = directory
		}
		temp, err := os.CreateTemp(*tempDir, "source-*")
		if err != nil {
			return nil, nil, &uploadSourceError{File: file, TempPath: *tempDir, Err: err}
		}
		entry, restoreErr := restoreUploadFile(ctx, file, temp, onProgress)
		closeErr := temp.Close()
		if err := errors.Join(restoreErr, closeErr); err != nil {
			return nil, nil, &uploadSourceError{File: file, TempPath: temp.Name(), Err: err}
		}
		restored[index].FullPath = filepath.ToSlash(temp.Name())
		hashes[file.FID] = entry
		if onProgress != nil {
			onProgress(0, true)
		}
	}
	return restored, hashes, nil
}

func restoreUploadFile(
	ctx context.Context,
	file UploadFile,
	temp *os.File,
	onProgress func(int64, bool),
) (uploadHashEntry, error) {
	release, err := diskio.Acquire(ctx, filepath.FromSlash(file.FullPath), temp.Name())
	if err != nil {
		return uploadHashEntry{}, err
	}
	defer release()
	source, err := os.Open(filepath.FromSlash(file.FullPath))
	if err != nil {
		return uploadHashEntry{}, fmt.Errorf("%w: %w", ErrBackupSourceRead, err)
	}
	defer func() { _ = source.Close() }()
	before, err := source.Stat()
	if err != nil {
		return uploadHashEntry{}, err
	}
	hasher := sha256.New()
	writer := uploadRestoreWriter{writer: io.MultiWriter(temp, hasher), onProgress: onProgress}
	size, copyErr := decodeUploadNZST(ctx, source, writer, min(file.Size, uploadNZSTMaxSize))
	if copyErr != nil {
		return uploadHashEntry{}, copyErr
	}
	after, err := source.Stat()
	if err != nil {
		return uploadHashEntry{}, err
	}
	if !sameUploadSource(before, after) {
		return uploadHashEntry{}, uploadSourceChanged(file, "restore-source", before, after)
	}
	if size != file.Size {
		return uploadHashEntry{}, infra.AnnotateError(uploadSourceChanged(file, "restore-size", nil, nil),
			infra.Diagnostic{Fields: map[string]any{"observedSize": size}})
	}
	info, err := temp.Stat()
	if err != nil {
		return uploadHashEntry{}, err
	}
	return uploadHashEntry{SHA256: hex.EncodeToString(hasher.Sum(nil)), Path: temp.Name(), Info: info}, nil
}

type uploadRestoreWriter struct {
	writer     io.Writer
	onProgress func(int64, bool)
}

func (w uploadRestoreWriter) Write(data []byte) (int, error) {
	n, err := w.writer.Write(data)
	if n > 0 && w.onProgress != nil {
		w.onProgress(int64(n), false)
	}
	return n, err
}
