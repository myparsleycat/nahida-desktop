package modelviewer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/infra"
)

var errModelViewerSpoolClosed = errors.New("model viewer spool is closed")

// modelViewerSpool keeps the mesh buffers of one viewer session in a file, so an open viewer does not hold them in
// memory. The viewer reads a buffer again whenever it loads that mesh.
type modelViewerSpool struct {
	path string
	mu   sync.RWMutex
	file *os.File
	size int64
}

func newModelViewerSpool(dir, sessionID string) (*modelViewerSpool, error) {
	path := filepath.Join(dir, sessionID+".bin")
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create model viewer spool: %w", err)
	}
	return &modelViewerSpool{path: path, file: file}, nil
}

// openSpool makes the spool of a viewer session. Without app data, or when the file cannot be made, it returns nil
// and the session keeps its mesh buffers in memory.
func (t *Service) openSpool(sessionID string) *modelViewerSpool {
	if t.appData == nil {
		return nil
	}
	dir, err := t.appData.EnsureDir(appdata.ModelViewerSpoolDir)
	if err == nil {
		var spool *modelViewerSpool
		if spool, err = newModelViewerSpool(dir, sessionID); err == nil {
			return spool
		}
	}
	_ = infra.ReportError(t.log, err, "StaticGlb.loadForViewer", infra.Diagnostic{
		Operation: "open-spool",
		Fields:    map[string]any{"memorySessionId": sessionID, "fallback": "memory"},
	})
	return nil
}

// appendStream stores the length bytes that write produces, so a caller can encode straight into the spool
// instead of building the whole buffer first. Each call reserves its own range and writes it under the read
// lock, so meshes encode in parallel; a range whose write fails or comes up short is never handed out.
func (s *modelViewerSpool) appendStream(ctx context.Context, length int, write func(io.Writer) error) (int64, error) {
	s.mu.Lock()
	if s.file == nil {
		s.mu.Unlock()
		return 0, errModelViewerSpoolClosed
	}
	offset := s.size
	s.size += int64(length)
	s.mu.Unlock()

	release, err := diskio.Acquire(ctx, s.path)
	if err != nil {
		return 0, err
	}
	defer release()

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.file == nil {
		return 0, errModelViewerSpoolClosed
	}
	out := &modelViewerRangeWriter{w: io.NewOffsetWriter(s.file, offset), limit: int64(length)}
	if err := write(out); err != nil {
		return 0, fmt.Errorf("write model viewer spool: %w", err)
	}
	if out.n != out.limit {
		return 0, fmt.Errorf("write model viewer spool: wrote %d bytes, want %d", out.n, length)
	}
	return offset, nil
}

// modelViewerRangeWriter keeps a write inside its reserved range, which the next mesh's range follows directly.
type modelViewerRangeWriter struct {
	w        io.Writer
	n, limit int64
}

func (r *modelViewerRangeWriter) Write(data []byte) (int, error) {
	if r.n+int64(len(data)) > r.limit {
		return 0, fmt.Errorf("write exceeds the reserved %d bytes", r.limit)
	}
	n, err := r.w.Write(data)
	r.n += int64(n)
	return n, err
}

func (s *modelViewerSpool) read(ctx context.Context, offset int64, length int) ([]byte, error) {
	release, err := diskio.Acquire(ctx, s.path)
	if err != nil {
		return nil, err
	}
	defer release()

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.file == nil {
		return nil, errModelViewerSpoolClosed
	}
	data := make([]byte, length)
	if _, err := s.file.ReadAt(data, offset); err != nil {
		return nil, fmt.Errorf("read model viewer spool: %w", err)
	}
	return data, nil
}

// close waits for reads in progress, then deletes the file.
func (s *modelViewerSpool) close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	closeErr := s.file.Close()
	s.file = nil
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.Join(closeErr, fmt.Errorf("remove model viewer spool: %w", err))
	}
	return closeErr
}

// cleanupModelViewerSpools removes spool files a previous run left behind. Startup runs it before any viewer loads.
func cleanupModelViewerSpools(ctx context.Context, dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read model viewer spool directory: %w", err)
	}
	var cleanupErrs []error
	for _, entry := range entries {
		if ctx.Err() != nil {
			return errors.Join(ctx.Err(), errors.Join(cleanupErrs...))
		}
		path := filepath.Join(dir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("remove stale model viewer spool %q: %w", path, err))
		}
	}
	return errors.Join(cleanupErrs...)
}
