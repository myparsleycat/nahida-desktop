package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/samber/lo"
	"golang.org/x/sync/errgroup"

	"nahida.live/desktop/internal/drive"
)

const (
	// restoreBatch is how many download URLs one request asks for, the
	// server's bound.
	restoreBatch = 100
	// restoreConcurrency is how many files download at once.
	restoreConcurrency = 4
)

// Restore downloads the named folders of a snapshot into destination, one
// sub-folder per target. The destination has to be missing or empty, so a
// restore never overwrites anything.
func (b *Backup) Restore(ctx context.Context, snapshotID string, targetIDs []string, destination string) error {
	if _, err := b.requireClient(); err != nil {
		return err
	}
	destination = filepath.Clean(strings.TrimSpace(destination))
	empty, err := isEmptyOrMissing(destination)
	if err != nil {
		return err
	}
	if !empty {
		return ErrDestination
	}
	_, statErr := os.Stat(destination)
	existed := statErr == nil
	loaded, err := b.manifest(ctx, snapshotID)
	if err != nil {
		return err
	}
	folders := restoreFolders(loaded.Snapshot.Targets, targetIDs)

	b.mu.Lock()
	if b.cancel != nil {
		b.mu.Unlock()
		return ErrRunning
	}
	parent := b.ctx
	if parent == nil {
		parent = context.Background()
	}
	runCtx, cancel := context.WithCancel(parent)
	b.cancel = cancel
	b.runs.Add(1)
	b.mu.Unlock()

	go func() {
		defer b.runs.Done()
		defer func() {
			cancel()
			b.mu.Lock()
			b.cancel = nil
			b.mu.Unlock()
			b.setStatus(Status{State: StateIdle})
		}()
		err := b.restore(runCtx, snapshotID, loaded, folders, destination)
		outcome := OutcomeCompleted
		message := ""
		switch {
		case runCtx.Err() != nil:
			outcome = OutcomeCancelled
		case err != nil:
			outcome = OutcomeFailed
			message = err.Error()
			b.report(err, "restore", "download", map[string]any{"snapshotId": snapshotID, "destination": destination})
		}
		// The destination was empty, so what a stopped restore leaves there is
		// only its own partial output; it goes, and the same folder can be
		// chosen again.
		if outcome != OutcomeCompleted {
			cleanupErr := cleanupRestore(destination, folders, existed)
			b.report(
				cleanupErr,
				"restore",
				"cleanup",
				map[string]any{"snapshotId": snapshotID, "destination": destination},
			)
		}
		if b.opts.EventEmit != nil {
			b.opts.EventEmit("backup:restore", map[string]any{
				"snapshotId": snapshotID, "destination": destination, "outcome": outcome, "error": message,
			})
		}
	}()
	return nil
}

func (b *Backup) restore(
	ctx context.Context,
	snapshotID string,
	loaded manifest,
	folders map[string]string,
	destination string,
) error {
	files := lo.Filter(loaded.Files, func(file ManifestFile, _ int) bool {
		_, ok := folders[file.TargetID]
		return ok
	})
	total := lo.SumBy(files, func(file ManifestFile) int64 { return file.Size })

	var done int
	var bytes int64
	var progressMu sync.Mutex
	throttle := newStatusThrottle(b, Status{State: StateRestoring, Total: len(files), TotalBytes: total})
	for _, batch := range lo.Chunk(files, restoreBatch) {
		var downloads []drive.BackupDownload
		if err := b.opts.Remote.BackupJSON(
			ctx,
			http.MethodPost,
			"/backup/snapshots/"+url.PathEscape(snapshotID)+"/files:download",
			map[string]any{
				"ids": lo.Map(batch, func(file ManifestFile, _ int) string { return file.ID }),
			},
			&downloads,
		); err != nil {
			return err
		}
		byID := lo.KeyBy(downloads, func(download drive.BackupDownload) string { return download.ID })

		// Every file of the batch is resolved before any download starts, so a
		// refusal never leaves downloads running behind the returned error.
		type job struct {
			file     ManifestFile
			download drive.BackupDownload
			target   string
		}
		jobs := make([]job, 0, len(batch))
		for _, file := range batch {
			download, ok := byID[file.ID]
			if !ok {
				return fmt.Errorf("the server did not answer a download for %s", file.Path)
			}
			target := filepath.Join(destination, folders[file.TargetID], filepath.FromSlash(file.Path))
			if !isWithin(target, destination) {
				return fmt.Errorf("refusing to restore %s outside the destination", file.Path)
			}
			jobs = append(jobs, job{file: file, download: download, target: target})
		}

		group, groupCtx := errgroup.WithContext(ctx)
		group.SetLimit(restoreConcurrency)
		for _, item := range jobs {
			file, download, target := item.file, item.download, item.target
			group.Go(func() error {
				if err := b.opts.Remote.DownloadBackupFile(groupCtx, snapshotID, download, target, func(delta int64) {
					progressMu.Lock()
					bytes += delta
					current := bytes
					progressMu.Unlock()
					throttle.update(func(status *Status) { status.Bytes = current })
				}); err != nil {
					return fmt.Errorf("%s: %w", file.Path, err)
				}
				if err := verifySHA256(target, file.SHA256); err != nil {
					return fmt.Errorf("%s: %w", file.Path, err)
				}
				progressMu.Lock()
				done++
				current := done
				progressMu.Unlock()
				throttle.update(func(status *Status) { status.Processed = current })
				return nil
			})
		}
		if err := group.Wait(); err != nil {
			return err
		}
	}
	throttle.flush()
	return nil
}

// restoreFolders names the sub-folder each chosen target restores into. The
// label is made safe for a folder name, and a repeated label gets a number.
func restoreFolders(targets []ManifestTarget, chosen []string) map[string]string {
	folders := make(map[string]string, len(chosen))
	used := map[string]struct{}{}
	for _, target := range targets {
		if !lo.Contains(chosen, target.ID) {
			continue
		}
		base := safeFolderName(target.Label)
		name := base
		for index := 2; ; index++ {
			if _, taken := used[strings.ToLower(name)]; !taken {
				break
			}
			name = fmt.Sprintf("%s (%d)", base, index)
		}
		used[strings.ToLower(name)] = struct{}{}
		folders[target.ID] = name
	}
	return folders
}

func safeFolderName(label string) string {
	name := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '_'
		}
		return r
	}, strings.TrimSpace(label))
	name = strings.TrimRight(name, ". ")
	if name == "" {
		return "backup"
	}
	return name
}

// cleanupRestore removes the sub-folders a stopped restore wrote into, and the
// destination itself when the restore created it.
func cleanupRestore(destination string, folders map[string]string, existed bool) error {
	var errs []error
	for _, name := range folders {
		errs = append(errs, os.RemoveAll(filepath.Join(destination, name)))
	}
	if !existed {
		if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func isEmptyOrMissing(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

func verifySHA256(path, expected string) error {
	handle, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, handle); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != expected {
		return errors.New("RESTORE_HASH_MISMATCH")
	}
	return nil
}
