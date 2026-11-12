package drive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const uploadHashBufferSize = 256 * 1024

var uploadHashBuffers = sync.Pool{New: func() any {
	buffer := make([]byte, uploadHashBufferSize)
	return &buffer
}}

type uploadHashEntry struct {
	SHA256 string
	Path   string
	Info   os.FileInfo
}

type uploadHashOptions struct {
	cache map[string]uploadHashEntry
	// verifySource rejects a source whose size differs from the prepared size
	// or that changes while it is read.
	verifySource bool
	onFile       func(UploadFile, uploadHashEntry)
	onBytes      func(int64)
	onInvalid    func(UploadFile)
}

func hashUploadFiles(
	ctx context.Context,
	files []UploadFile,
	concurrency int,
	opts uploadHashOptions,
) ([]uploadHashEntry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(files) == 0 {
		return []uploadHashEntry{}, nil
	}
	concurrency = min(max(concurrency, 1), len(files))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	out := make([]uploadHashEntry, len(files))
	jobs := make(chan int)
	var workers sync.WaitGroup
	var callbackMu sync.Mutex
	var errorOnce sync.Once
	var firstErr error
	setError := func(err error) {
		errorOnce.Do(func() {
			firstErr = err
			cancel()
		})
	}
	callbacks := opts
	callbacks.onBytes = func(n int64) {
		if opts.onBytes != nil {
			callbackMu.Lock()
			opts.onBytes(n)
			callbackMu.Unlock()
		}
	}
	callbacks.onInvalid = func(file UploadFile) {
		if opts.onInvalid != nil {
			callbackMu.Lock()
			opts.onInvalid(file)
			callbackMu.Unlock()
		}
	}
	workers.Add(concurrency)
	for range concurrency {
		go func() {
			defer workers.Done()
			var buffer *[]byte
			defer func() {
				if buffer != nil {
					uploadHashBuffers.Put(buffer)
				}
			}()
			for index := range jobs {
				entry, err := hashUploadSource(ctx, files[index], &buffer, callbacks)
				if err != nil {
					if !infra.IsCancellationError(err) {
						callbacks.onInvalid(files[index])
					}
					setError(&uploadSourceError{File: files[index], Err: err})
					return
				}
				out[index] = entry
				if opts.onFile != nil {
					callbackMu.Lock()
					opts.onFile(files[index], entry)
					callbackMu.Unlock()
				}
			}
		}()
	}
sendLoop:
	for index := range files {
		select {
		case <-ctx.Done():
			break sendLoop
		case jobs <- index:
		}
	}
	close(jobs)
	workers.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func hashUploadSource(
	ctx context.Context,
	source UploadFile,
	buffer **[]byte,
	opts uploadHashOptions,
) (uploadHashEntry, error) {
	if err := ctx.Err(); err != nil {
		return uploadHashEntry{}, err
	}
	path := filepath.FromSlash(source.FullPath)
	release, err := diskio.Acquire(ctx, path)
	if err != nil {
		return uploadHashEntry{}, err
	}
	defer release()
	file, err := os.Open(path)
	if err != nil {
		return uploadHashEntry{}, fmt.Errorf("%w: %w", ErrBackupSourceRead, err)
	}
	defer func() { _ = file.Close() }()
	before, err := file.Stat()
	if err != nil {
		return uploadHashEntry{}, err
	}
	if opts.verifySource && before.Size() != source.Size {
		return uploadHashEntry{}, uploadSourceChanged(source, "hash-size", nil, before)
	}
	if cached, ok := opts.cache[source.FID]; ok {
		if cached.SHA256 != "" && platform.SamePathFold(cached.Path, source.FullPath) &&
			sameUploadSource(cached.Info, before) {
			if opts.onBytes != nil {
				opts.onBytes(before.Size())
			}
			return cached, nil
		}
		if opts.onInvalid != nil {
			opts.onInvalid(source)
		}
	}
	if *buffer == nil {
		*buffer, _ = uploadHashBuffers.Get().(*[]byte)
	}
	hasher := sha256.New()
	n, err := io.CopyBuffer(hasher, contextReader{ctx: ctx, reader: file, onBytes: opts.onBytes}, **buffer)
	if err != nil {
		return uploadHashEntry{}, err
	}
	after, err := file.Stat()
	if err != nil {
		return uploadHashEntry{}, err
	}
	if opts.verifySource && (n != before.Size() || !sameUploadSource(before, after)) {
		return uploadHashEntry{}, uploadSourceChanged(source, "hash-read", before, after)
	}
	return uploadHashEntry{SHA256: hex.EncodeToString(hasher.Sum(nil)), Path: source.FullPath, Info: after}, nil
}

func sameUploadSource(expected, observed os.FileInfo) bool {
	return expected != nil && observed != nil && expected.Size() == observed.Size() &&
		expected.ModTime().Equal(observed.ModTime()) && os.SameFile(expected, observed)
}

func uploadSourceChanged(source UploadFile, stage string, expected, observed os.FileInfo) error {
	fields := map[string]any{"name": source.Name, "inputPath": source.FullPath, "expectedSize": source.Size}
	if expected != nil {
		fields["expectedSize"] = expected.Size()
		fields["expectedModified"] = expected.ModTime()
	}
	if observed != nil {
		fields["observedSize"] = observed.Size()
		fields["observedModified"] = observed.ModTime()
	}
	err := &UploadV2Error{Code: "upload_source_changed", Message: "source file changed after it was selected"}
	return infra.AnnotateError(err, infra.Diagnostic{Stage: stage, Fields: fields})
}

type contextReader struct {
	ctx     context.Context
	reader  io.Reader
	onBytes func(int64)
}

func (r contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(buffer)
	if n > 0 && r.onBytes != nil {
		r.onBytes(int64(n))
	}
	return n, err
}
