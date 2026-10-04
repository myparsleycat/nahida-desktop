package drive

import (
	"errors"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"strings"

	"github.com/fxamacker/cbor/v2"
	"github.com/klauspost/compress/zstd"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/transfer"
)

const (
	downloadSpoolExt       = ".spool"
	largeDownloadThreshold = 50 * 1024 * 1024
)

// spooledFile is a download file together with its position in the spool,
// which is what completion is tracked by.
type spooledFile struct {
	index int
	file  transfer.DownloadFile
}

// downloadPlan is what a download keeps between runs. The file list lives in
// the spool, so memory stays flat however many files a folder holds; only the
// directory tree and the few large files stay in memory.
type downloadPlan struct {
	root transfer.Root
	dirs []transfer.Directory
	// rootFiles names the files that land directly in the target directory.
	rootFiles  []string
	totalBytes int64
	fileCount  int
	large      []spooledFile
	spoolPath  string
}

func (p *downloadPlan) singleFile() bool {
	return len(p.dirs) == 0 && p.fileCount == 1
}

func (p *downloadPlan) remove() error {
	if err := os.Remove(p.spoolPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// files yields the spooled files in the order they were written.
func (p *downloadPlan) files() iter.Seq2[spooledFile, error] {
	return func(yield func(spooledFile, error) bool) {
		file, err := os.Open(p.spoolPath)
		if err != nil {
			yield(spooledFile{}, fmt.Errorf("open download spool: %w", err))
			return
		}
		defer func() { _ = file.Close() }()
		reader, err := zstd.NewReader(file, zstd.WithDecoderConcurrency(1))
		if err != nil {
			yield(spooledFile{}, fmt.Errorf("open download spool: %w", err))
			return
		}
		defer reader.Close()

		decoder := cbor.NewDecoder(reader)
		for index := range p.fileCount {
			var item transfer.DownloadFile
			if err := decoder.Decode(&item); err != nil {
				yield(spooledFile{}, fmt.Errorf("read download spool entry %d: %w", index, err))
				return
			}
			if !yield(spooledFile{index: index, file: item}, nil) {
				return
			}
		}
	}
}

// scheduled yields every file with the large ones spread evenly among the
// small ones, so a run of large files does not occupy every worker at once.
func (p *downloadPlan) scheduled() iter.Seq2[spooledFile, error] {
	small := p.fileCount - len(p.large)
	if len(p.large) == 0 || small == 0 {
		return p.files()
	}
	return func(yield func(spooledFile, error) bool) {
		interval := max(1, small/len(p.large))
		large := p.large
		emitted := 0
		for item, err := range p.files() {
			if err != nil {
				yield(spooledFile{}, err)
				return
			}
			if item.file.Size >= largeDownloadThreshold {
				continue
			}
			if !yield(item, nil) {
				return
			}
			emitted++
			if emitted%interval == 0 && len(large) > 0 {
				if !yield(large[0], nil) {
					return
				}
				large = large[1:]
			}
		}
		for _, item := range large {
			if !yield(item, nil) {
				return
			}
		}
	}
}

type spoolWriter struct {
	plan       *downloadPlan
	file       *os.File
	compressor *zstd.Encoder
	encoder    *cbor.Encoder
}

func newSpoolWriter(path string) (*spoolWriter, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create download spool: %w", err)
	}
	compressor, err := zstd.NewWriter(
		file,
		zstd.WithEncoderLevel(zstd.SpeedFastest),
		zstd.WithEncoderConcurrency(1),
	)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("create download spool: %w", err)
	}
	return &spoolWriter{
		plan:       &downloadPlan{spoolPath: path},
		file:       file,
		compressor: compressor,
		encoder:    cbor.NewEncoder(compressor),
	}, nil
}

func (w *spoolWriter) add(file transfer.DownloadFile) error {
	if err := w.encoder.Encode(file); err != nil {
		return fmt.Errorf("write download spool: %w", err)
	}
	if file.Size >= largeDownloadThreshold {
		w.plan.large = append(w.plan.large, spooledFile{index: w.plan.fileCount, file: file})
	}
	w.plan.fileCount++
	return nil
}

func (w *spoolWriter) finish() (*downloadPlan, error) {
	if err := errors.Join(w.compressor.Close(), w.file.Close()); err != nil {
		_ = os.Remove(w.plan.spoolPath)
		return nil, fmt.Errorf("write download spool: %w", err)
	}
	return w.plan, nil
}

func (w *spoolWriter) abort() {
	_ = w.compressor.Close()
	_ = w.file.Close()
	_ = os.Remove(w.plan.spoolPath)
}

func (d *Drive) downloadSpoolPath(pid string) (string, error) {
	if d.appData == nil {
		return "", errors.New("download spool directory is not configured")
	}
	dir, err := d.appData.EnsureDir(appdata.DownloadSpoolDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, pid+downloadSpoolExt), nil
}

// sweepDownloadSpools removes spools whose transfer no longer exists. A spool
// outlives its run so a paused or failed download can resume, which leaves
// nothing to delete it when the user clears the transfer instead.
func (d *Drive) sweepDownloadSpools() {
	if d.appData == nil || d.transfer == nil {
		return
	}
	dir, err := d.appData.Resolve(appdata.DownloadSpoolDir)
	if err != nil {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	live := make(map[string]struct{})
	for _, snapshot := range d.transfer.List() {
		live[snapshot.PID] = struct{}{}
	}
	for _, entry := range entries {
		pid, ok := strings.CutSuffix(entry.Name(), downloadSpoolExt)
		if !ok {
			continue
		}
		if _, exists := live[pid]; exists {
			continue
		}
		d.reportCleanup(os.Remove(filepath.Join(dir, entry.Name())), "sweepDownloadSpools")
	}
}
