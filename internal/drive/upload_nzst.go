package drive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"

	"nahida.live/desktop/internal/infra"
)

const (
	uploadNZSTExtension = ".nzst"
	uploadNZSTMaxSize   = int64(64 << 30)
)

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
	source, err := os.Open(filepath.FromSlash(path))
	if err != nil {
		return 0, err
	}
	defer func() { _ = source.Close() }()
	info, err := source.Stat()
	if err != nil {
		return 0, err
	}
	if info.Size() == 0 {
		return 0, io.ErrUnexpectedEOF
	}
	decoder, err := zstd.NewReader(source, zstd.WithDecoderConcurrency(1))
	if err != nil {
		return 0, err
	}
	defer decoder.Close()
	return io.Copy(output, io.LimitReader(&uploadNZSTReader{ctx: ctx, reader: decoder}, limit+1))
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
func restoreUploadSources(ctx context.Context, files []UploadFile, tempDir *string) ([]UploadFile, bool, error) {
	restored := make([]UploadFile, len(files))
	copy(restored, files)
	hasNZST := false
	for index, file := range files {
		if !isUploadNZST(file.FullPath) {
			continue
		}
		hasNZST = true
		if err := ctx.Err(); err != nil {
			return nil, hasNZST, err
		}
		if *tempDir == "" {
			directory, err := os.MkdirTemp("", "nahida-drive-upload-*")
			if err != nil {
				return nil, hasNZST, &uploadSourceError{File: file, Err: err}
			}
			*tempDir = directory
		}
		temp, err := os.CreateTemp(*tempDir, "source-*")
		if err != nil {
			return nil, hasNZST, &uploadSourceError{File: file, TempPath: *tempDir, Err: err}
		}
		size, copyErr := copyUploadNZST(ctx, file.FullPath, temp, min(file.Size, uploadNZSTMaxSize))
		closeErr := temp.Close()
		if copyErr == nil && size != file.Size {
			copyErr = fmt.Errorf("restored size changed: got %d, expected %d", size, file.Size)
		}
		if err := errors.Join(copyErr, closeErr); err != nil {
			return nil, hasNZST, &uploadSourceError{File: file, TempPath: temp.Name(), Err: err}
		}
		restored[index].FullPath = filepath.ToSlash(temp.Name())
	}
	return restored, hasNZST, nil
}
