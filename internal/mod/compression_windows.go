//go:build windows

package mod

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/db"
)

const (
	wofProviderFile                 = 2
	fileProviderCompressionXpress4K = 0
	fsctlDeleteExternalBacking      = 0x90314
	hresultCompressionNotBeneficial = 0x80070158
	moveFileReplaceExisting         = 0x1
	moveFileWriteThrough            = 0x8
	compressionLedgerPrefix         = "mod_compression:file:"
	minimumXpressBatchSize          = 64
)

var (
	wofutilDLL                 = windows.NewLazySystemDLL("wofutil.dll")
	procWofSetFileDataLocation = wofutilDLL.NewProc("WofSetFileDataLocation")
	procWofIsExternalFile      = wofutilDLL.NewProc("WofIsExternalFile")
	kernel32CompressionDLL     = windows.NewLazySystemDLL("kernel32.dll")
	procGetDiskFreeSpaceW      = kernel32CompressionDLL.NewProc("GetDiskFreeSpaceW")
	procMoveFileExW            = kernel32CompressionDLL.NewProc("MoveFileExW")
	wofSetFileDataLocationCall = nativeWofSetFileDataLocation
	deleteExternalBackingCall  = nativeDeleteExternalBacking
	setCompressionCall         = nativeSetCompression
	wofStateCall               = nativeWofState
	fileAttributesCall         = nativeFileAttributes
	fileIdentityCall           = nativeFileIdentity
	fileIdentityFromHandleCall = nativeFileIdentityFromHandle
	volumeClusterSizeCall      = volumeClusterSize
)

type wofCompressionInfoV1 struct {
	Algorithm uint32
	Flags     uint32
}

type wofLedgerEntry struct {
	FileID    string `json:"fileId"`
	Path      string `json:"path"`
	Provider  uint32 `json:"provider,omitempty"`
	Algorithm uint32 `json:"algorithm"`
	State     string `json:"state"`
}

type wofOwnership uint8

const (
	wofBackingNone wofOwnership = iota
	wofBackingOwned
	wofBackingForeign
)

type xpressCompressionPlan struct {
	files         []compressionFile
	managed       map[string]wofLedgerEntry
	ledgerUpdates []wofLedgerEntry
	external      []string
}

type xpressInspection struct {
	file         *compressionFile
	ledgerUpdate *wofLedgerEntry
	external     string
	err          error
}

type preparedXpressFile struct {
	file   compressionFile
	handle windows.Handle
	entry  wofLedgerEntry
}

type xpressCompressionResult struct {
	file  compressionFile
	entry wofLedgerEntry
	err   error
}

var xpressSkippedExtensions = map[string]struct{}{
	".dl_": {}, ".gif": {}, ".jpg": {}, ".jpeg": {}, ".png": {}, ".wmf": {},
	".mkv": {}, ".mp4": {}, ".wmv": {}, ".avi": {}, ".bik": {}, ".bk2": {},
	".flv": {}, ".ogg": {}, ".mpg": {}, ".m2v": {}, ".m4v": {}, ".vob": {},
	".mp3": {}, ".aac": {}, ".wma": {}, ".flac": {}, ".zip": {}, ".xap": {},
	".rar": {}, ".7z": {}, ".cab": {}, ".lzx": {}, ".docx": {}, ".xlsx": {},
	".pptx": {}, ".vssx": {}, ".vstx": {}, ".onepkg": {}, ".tar": {}, ".gz": {},
	".dmg": {}, ".bz2": {}, ".tgz": {}, ".lz": {}, ".xz": {}, ".txz": {}, ".zst": {},
}

func planXpress4K(ctx context.Context, roots []string, client *db.Client) (xpressCompressionPlan, error) {
	managed, legacyUpdates, err := loadManagedWofIDs(ctx, client)
	if err != nil {
		return xpressCompressionPlan{}, err
	}
	files, scanErr := walkCompressionFiles(roots, func(string, fs.FileInfo) bool { return true })
	plan := xpressCompressionPlan{managed: managed}
	updates := make(map[string]wofLedgerEntry, len(legacyUpdates))
	for _, entry := range legacyUpdates {
		updates[entry.FileID] = entry
	}

	clusters := make(map[string]uint32)
	badVolumes := make(map[string]struct{})
	var errs []error
	if scanErr != nil {
		errs = append(errs, scanErr)
	}
	for _, file := range files {
		volume := strings.ToLower(filepath.VolumeName(file.path))
		if _, ok := clusters[volume]; ok {
			continue
		}
		if _, ok := badVolumes[volume]; ok {
			continue
		}
		cluster, clusterErr := volumeClusterSizeCall(file.path)
		if clusterErr != nil {
			badVolumes[volume] = struct{}{}
			errs = append(errs, fmt.Errorf("cluster size for volume %q: %w", filepath.VolumeName(file.path), clusterErr))
			continue
		}
		clusters[volume] = cluster
	}

	inspections, inspectErr := inspectXpressFiles(ctx, files, managed, clusters)
	if inspectErr != nil {
		errs = append(errs, inspectErr)
	}
	for _, inspection := range inspections {
		if inspection.err != nil {
			errs = append(errs, inspection.err)
		}
		if inspection.file != nil {
			plan.files = append(plan.files, *inspection.file)
		}
		if inspection.ledgerUpdate != nil {
			updates[inspection.ledgerUpdate.FileID] = *inspection.ledgerUpdate
		}
		if inspection.external != "" {
			plan.external = append(plan.external, inspection.external)
		}
	}
	for _, entry := range updates {
		plan.ledgerUpdates = append(plan.ledgerUpdates, entry)
	}
	return plan, errors.Join(errs...)
}

func inspectXpressFiles(
	ctx context.Context,
	files []compressionFile,
	managed map[string]wofLedgerEntry,
	clusters map[string]uint32,
) ([]xpressInspection, error) {
	inspections := make([]xpressInspection, len(files))
	if len(files) == 0 {
		return inspections, ctx.Err()
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	workers := min(xpressWorkerCount(runtime.GOMAXPROCS(0)), len(files))
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				if err := ctx.Err(); err != nil {
					inspections[index].err = err
					continue
				}
				inspections[index] = inspectXpressFile(files[index], managed, clusters)
			}
		}()
	}
	for index := range files {
		select {
		case jobs <- index:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return inspections, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	return inspections, nil
}

func inspectXpressFile(
	file compressionFile,
	managed map[string]wofLedgerEntry,
	clusters map[string]uint32,
) xpressInspection {
	attributes, err := fileAttributesCall(file.path)
	if err != nil {
		return xpressInspection{err: fmt.Errorf("attributes %q: %w", file.path, err)}
	}
	external, provider, algorithm, err := wofStateCall(file.path)
	if err != nil {
		return xpressInspection{err: fmt.Errorf("WOF state %q: %w", file.path, err)}
	}
	if external || attributes&windows.FILE_ATTRIBUTE_COMPRESSED != 0 {
		id, identityErr := fileIdentityCall(file.path)
		if identityErr != nil {
			return xpressInspection{err: fmt.Errorf("file identity %q: %w", file.path, identityErr)}
		}
		entry, ok := managed[id]
		managedBacking := ok && external && attributes&windows.FILE_ATTRIBUTE_COMPRESSED == 0 &&
			provider == entry.Provider && algorithm == entry.Algorithm
		if !managedBacking {
			return xpressInspection{external: file.path}
		}
		if !samePath(entry.Path, file.path) {
			entry.Path = file.path
			return xpressInspection{ledgerUpdate: &entry}
		}
		return xpressInspection{}
	}
	if _, skipped := xpressSkippedExtensions[strings.ToLower(filepath.Ext(file.path))]; skipped {
		return xpressInspection{}
	}
	cluster, ok := clusters[strings.ToLower(filepath.VolumeName(file.path))]
	if !ok || file.size <= int64(cluster) {
		return xpressInspection{}
	}
	return xpressInspection{file: &file}
}

func applyXpress4K(
	ctx context.Context,
	plan xpressCompressionPlan,
	client *db.Client,
	setTotals func(int, int64),
	progress func(string, int64, bool),
	mark compressionMutationMarker,
) error {
	var totalBytes int64
	for _, file := range plan.files {
		totalBytes += file.size
	}
	setTotals(len(plan.files), totalBytes)
	if err := applyWofLedgerBatch(ctx, client, plan.ledgerUpdates, nil); err != nil {
		return err
	}
	for _, entry := range plan.ledgerUpdates {
		plan.managed[entry.FileID] = entry
	}
	workers := xpressWorkerCount(runtime.GOMAXPROCS(0))
	batchSize := max(minimumXpressBatchSize, workers*4)
	seenIDs := make(map[string]struct{}, len(plan.files))
	var errs []error
	for start := 0; start < len(plan.files); start += batchSize {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		end := min(start+batchSize, len(plan.files))
		batchErr := applyXpressBatch(ctx, plan.files[start:end], plan.managed, seenIDs, client, workers, progress, mark)
		if batchErr != nil {
			errs = append(errs, batchErr)
		}
		if errors.Is(batchErr, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return errors.Join(errs...)
		}
		if errors.Is(batchErr, errUnmanagedWofBacking) || errors.Is(batchErr, errWofLedgerCommit) {
			return errors.Join(errs...)
		}
	}
	return errors.Join(errs...)
}

var (
	errUnmanagedWofBacking = errors.New("unmanaged WOF backing")
	errWofLedgerCommit     = errors.New("WOF ledger commit failed")
)

func applyXpressBatch(
	ctx context.Context,
	files []compressionFile,
	managed map[string]wofLedgerEntry,
	seenIDs map[string]struct{},
	client *db.Client,
	workers int,
	progress func(string, int64, bool),
	mark compressionMutationMarker,
) error {
	prepared := make([]preparedXpressFile, 0, len(files))
	revalidatedUpdates := make([]wofLedgerEntry, 0)
	revalidatedFiles := make([]compressionFile, 0)
	var errs []error
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			closePreparedXpressFiles(prepared)
			return errors.Join(append(errs, err)...)
		}
		handle, err := openPreparedXpressFile(file.path)
		if err != nil {
			progress(file.path, file.size, true)
			errs = append(errs, fmt.Errorf("open XPRESS4K file %q: %w", file.path, err))
			continue
		}
		attributes, err := fileAttributesCall(file.path)
		if err != nil {
			_ = windows.CloseHandle(handle)
			progress(file.path, file.size, true)
			errs = append(errs, fmt.Errorf("attributes %q: %w", file.path, err))
			continue
		}
		if attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			_ = windows.CloseHandle(handle)
			progress(file.path, file.size, true)
			errs = append(errs, fmt.Errorf("XPRESS4K target became a reparse point: %s", file.path))
			continue
		}
		external, provider, algorithm, err := wofStateCall(file.path)
		if err != nil {
			_ = windows.CloseHandle(handle)
			progress(file.path, file.size, true)
			errs = append(errs, fmt.Errorf("WOF state %q: %w", file.path, err))
			continue
		}
		id, err := fileIdentityFromHandleCall(handle)
		if err != nil {
			_ = windows.CloseHandle(handle)
			progress(file.path, file.size, true)
			errs = append(errs, fmt.Errorf("file identity %q: %w", file.path, err))
			continue
		}
		if external || attributes&windows.FILE_ATTRIBUTE_COMPRESSED != 0 {
			_ = windows.CloseHandle(handle)
			entry, ok := managed[id]
			managedBacking := ok && external && attributes&windows.FILE_ATTRIBUTE_COMPRESSED == 0 &&
				provider == entry.Provider && algorithm == entry.Algorithm
			if managedBacking {
				if !samePath(entry.Path, file.path) {
					entry.Path = file.path
					revalidatedUpdates = append(revalidatedUpdates, entry)
					revalidatedFiles = append(revalidatedFiles, file)
				} else {
					progress(file.path, file.size, false)
				}
				continue
			}
			closePreparedXpressFiles(prepared)
			progress(file.path, file.size, true)
			return errors.Join(append(errs, fmt.Errorf("%w detected: %s", errUnmanagedWofBacking, file.path))...)
		}
		if _, duplicate := seenIDs[id]; duplicate {
			_ = windows.CloseHandle(handle)
			progress(file.path, file.size, false)
			continue
		}
		seenIDs[id] = struct{}{}
		prepared = append(prepared, preparedXpressFile{
			file: file, handle: handle,
			entry: wofLedgerEntry{FileID: id, Path: file.path, Provider: wofProviderFile,
				Algorithm: fileProviderCompressionXpress4K, State: "compressing"},
		})
	}
	if len(prepared) == 0 && len(revalidatedUpdates) == 0 {
		return errors.Join(errs...)
	}
	preEntries := make([]wofLedgerEntry, 0, len(prepared)+len(revalidatedUpdates))
	preEntries = append(preEntries, revalidatedUpdates...)
	for index := range prepared {
		preEntries = append(preEntries, prepared[index].entry)
	}
	if err := applyWofLedgerBatch(ctx, client, preEntries, nil); err != nil {
		closePreparedXpressFiles(prepared)
		for _, file := range revalidatedFiles {
			progress(file.path, file.size, true)
		}
		for _, file := range prepared {
			progress(file.file.path, file.file.size, true)
		}
		return errors.Join(append(errs, fmt.Errorf("%w: %w", errWofLedgerCommit, err))...)
	}
	for _, file := range prepared {
		managed[file.entry.FileID] = file.entry
	}
	for _, entry := range revalidatedUpdates {
		managed[entry.FileID] = entry
	}
	for _, file := range revalidatedFiles {
		progress(file.path, file.size, false)
	}
	if len(prepared) == 0 {
		return errors.Join(errs...)
	}
	results := compressPreparedXpressFiles(ctx, prepared, workers, mark)
	completed := make([]wofLedgerEntry, 0, len(results))
	deletes := make([]string, 0, len(results))
	for _, result := range results {
		switch {
		case result.err == nil:
			result.entry.State = "compressed"
			completed = append(completed, result.entry)
		case errors.Is(result.err, errCompressionNotBeneficial):
			deletes = append(deletes, compressionLedgerPrefix+result.entry.FileID)
		default:
			errs = append(errs, fmt.Errorf("compress XPRESS4K %q: %w", result.file.path, result.err))
		}
	}
	postErr := applyWofLedgerBatch(ctx, client, completed, deletes)
	if postErr != nil {
		errs = append(errs, fmt.Errorf("%w: %w", errWofLedgerCommit, postErr))
	} else {
		for _, entry := range completed {
			managed[entry.FileID] = entry
		}
		for _, key := range deletes {
			delete(managed, strings.TrimPrefix(key, compressionLedgerPrefix))
		}
	}
	for _, result := range results {
		failed := postErr != nil || result.err != nil && !errors.Is(result.err, errCompressionNotBeneficial)
		progress(result.file.path, result.file.size, failed)
	}
	return errors.Join(errs...)
}

func compressPreparedXpressFiles(
	ctx context.Context,
	prepared []preparedXpressFile,
	workers int,
	mark compressionMutationMarker,
) []xpressCompressionResult {
	results := make([]xpressCompressionResult, len(prepared))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(workers, len(prepared)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				file := prepared[index]
				err := ctx.Err()
				if err == nil {
					mark(file.file.path)
					err = wofCompressHandle(file.handle)
				}
				_ = windows.CloseHandle(file.handle)
				results[index] = xpressCompressionResult{file: file.file, entry: file.entry, err: err}
			}
		}()
	}
	for index := range prepared {
		jobs <- index
	}
	close(jobs)
	wg.Wait()
	return results
}

func closePreparedXpressFiles(files []preparedXpressFile) {
	for _, file := range files {
		_ = windows.CloseHandle(file.handle)
	}
}

func xpressWorkerCount(gomaxprocs int) int {
	return max(1, gomaxprocs)
}

func cleanupMissingWofLedgers(ctx context.Context, scopes []string, client *db.Client) error {
	managed, err := managedWofIDs(ctx, client)
	if err != nil {
		return err
	}
	var errs []error
	var deleteKeys []string
	for id, entry := range managed {
		contained := false
		for _, scope := range scopes {
			if pathContains(scope, entry.Path) {
				contained = true
				break
			}
		}
		if !contained {
			continue
		}
		if _, err := os.Stat(entry.Path); errors.Is(err, os.ErrNotExist) {
			deleteKeys = append(deleteKeys, compressionLedgerPrefix+id)
		} else if err != nil {
			errs = append(errs, err)
		}
	}
	if err := client.AppState.ApplyBatch(ctx, nil, deleteKeys); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func restoreManagedWOF(
	ctx context.Context,
	roots []string,
	client *db.Client,
	setTotals func(int, int64),
	progress func(string, int64, bool),
	mark compressionMutationMarker,
) error {
	managed, err := managedWofIDs(ctx, client)
	if err != nil {
		return err
	}
	if len(managed) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	files, scanErr := walkCompressionFiles(roots, func(path string, _ fs.FileInfo) bool { return true })
	var errs []error
	if scanErr != nil {
		errs = append(errs, scanErr)
	}
	var managedFiles int
	var totalBytes int64
	for _, file := range files {
		id, identityErr := fileIdentityCall(file.path)
		if identityErr == nil {
			if _, ok := managed[id]; ok {
				managedFiles++
				totalBytes += file.size
			}
		}
	}
	setTotals(managedFiles, totalBytes)
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		id, err := fileIdentityCall(file.path)
		if err != nil {
			continue
		}
		entry, ok := managed[id]
		if !ok {
			continue
		}
		seen[id] = struct{}{}
		ownership, stateErr := inspectWofOwnership(file.path, entry)
		if stateErr == nil && ownership == wofBackingOwned {
			mark(file.path)
			stateErr = wofDecompress(file.path)
		}
		if stateErr == nil {
			stateErr = client.AppState.Delete(ctx, compressionLedgerPrefix+id)
		}
		progress(file.path, file.size, stateErr != nil)
		if stateErr != nil {
			errs = append(errs, fmt.Errorf("restore XPRESS4K %q: %w", file.path, stateErr))
		}
	}
	for id, entry := range managed {
		if _, ok := seen[id]; ok {
			continue
		}
		info, err := os.Stat(entry.Path)
		if errors.Is(err, os.ErrNotExist) {
			// The file may have been deleted while compressed. A missing file has no backing to restore.
			if deleteErr := client.AppState.Delete(ctx, compressionLedgerPrefix+id); deleteErr != nil {
				errs = append(errs, deleteErr)
			}
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("stat ledger path %q: %w", entry.Path, err))
			continue
		}
		currentID, err := fileIdentityCall(entry.Path)
		if err != nil || currentID != id {
			errs = append(errs, fmt.Errorf("WOF ledger identity changed for %q", entry.Path))
			continue
		}
		ownership, err := inspectWofOwnership(entry.Path, entry)
		if err == nil && ownership == wofBackingOwned {
			mark(entry.Path)
			err = wofDecompress(entry.Path)
		}
		if err == nil {
			err = client.AppState.Delete(ctx, compressionLedgerPrefix+id)
		}
		progress(entry.Path, info.Size(), err != nil)
		if err != nil {
			errs = append(errs, fmt.Errorf("restore XPRESS4K ledger path %q: %w", entry.Path, err))
		}
	}
	return errors.Join(errs...)
}

func inspectWofOwnership(path string, entry wofLedgerEntry) (wofOwnership, error) {
	attributes, err := fileAttributesCall(path)
	if err != nil {
		return wofBackingNone, err
	}
	external, provider, algorithm, err := wofStateCall(path)
	if err != nil {
		return wofBackingNone, err
	}
	if attributes&windows.FILE_ATTRIBUTE_COMPRESSED != 0 {
		return wofBackingForeign, nil
	}
	if !external {
		return wofBackingNone, nil
	}
	if provider == entry.Provider && algorithm == entry.Algorithm {
		return wofBackingOwned, nil
	}
	return wofBackingForeign, nil
}

func unmanagedCompressionFiles(ctx context.Context, roots []string, client *db.Client) ([]string, error) {
	managed, err := managedWofIDs(ctx, client)
	if err != nil {
		return nil, err
	}
	var external []string
	files, scanErr := walkCompressionFiles(roots, func(path string, _ fs.FileInfo) bool { return true })
	var errs []error
	if scanErr != nil {
		errs = append(errs, scanErr)
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return external, errors.Join(append(errs, err)...)
		}
		attributes, err := fileAttributesCall(file.path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		wof, provider, algorithm, err := wofStateCall(file.path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !wof && attributes&windows.FILE_ATTRIBUTE_COMPRESSED == 0 {
			continue
		}
		id, err := fileIdentityCall(file.path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		entry, ok := managed[id]
		managedBacking := ok && wof && attributes&windows.FILE_ATTRIBUTE_COMPRESSED == 0 &&
			provider == entry.Provider && algorithm == entry.Algorithm
		if !managedBacking {
			external = append(external, file.path)
		}
	}
	return external, errors.Join(errs...)
}

func decompressExternalFiles(
	ctx context.Context,
	paths []string,
	setTotals func(int, int64),
	progress func(string, int64, bool),
	mark compressionMutationMarker,
	onError func(string, error),
) error {
	files := make([]compressionFile, 0, len(paths))
	var errs []error
	var totalBytes int64
	for _, path := range paths {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			onError(path, err)
			errs = append(errs, fmt.Errorf("stat external compression %q: %w", path, err))
			continue
		}
		files = append(files, compressionFile{path: path, size: info.Size()})
		totalBytes += info.Size()
	}
	setTotals(len(files), totalBytes)
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		err := decompressExternalFile(file.path, mark)
		progress(file.path, file.size, err != nil)
		if err != nil {
			onError(file.path, err)
			errs = append(errs, fmt.Errorf("decompress external compression %q: %w", file.path, err))
		}
	}
	return errors.Join(errs...)
}

func decompressExternalFile(path string, mark compressionMutationMarker) error {
	attributes, err := fileAttributesCall(path)
	if err != nil {
		return err
	}
	external, _, _, err := wofStateCall(path)
	if err != nil {
		return err
	}
	var errs []error
	if external {
		mark(path)
		if err := wofDecompress(path); err != nil {
			errs = append(errs, fmt.Errorf("remove WOF backing: %w", err))
		}
	}
	if attributes&windows.FILE_ATTRIBUTE_COMPRESSED != 0 {
		mark(path)
		if err := ntfsDecompress(path); err != nil {
			errs = append(errs, fmt.Errorf("remove NTFS compression: %w", err))
		}
	}
	return errors.Join(errs...)
}

func managedWofIDs(ctx context.Context, client *db.Client) (map[string]wofLedgerEntry, error) {
	managed, updates, err := loadManagedWofIDs(ctx, client)
	if err != nil {
		return nil, err
	}
	if err := applyWofLedgerBatch(ctx, client, updates, nil); err != nil {
		return nil, err
	}
	return managed, nil
}

func loadManagedWofIDs(ctx context.Context, client *db.Client) (map[string]wofLedgerEntry, []wofLedgerEntry, error) {
	rows, err := client.AppState.ListByPrefix(ctx, compressionLedgerPrefix)
	if err != nil {
		return nil, nil, err
	}
	out := make(map[string]wofLedgerEntry, len(rows))
	var updates []wofLedgerEntry
	for _, row := range rows {
		var entry wofLedgerEntry
		if err := json.Unmarshal([]byte(row.Value), &entry); err != nil {
			return nil, nil, fmt.Errorf("decode WOF ledger %q: %w", row.Key, err)
		}
		if entry.Provider == 0 {
			entry.Provider = wofProviderFile
			updates = append(updates, entry)
		}
		out[entry.FileID] = entry
	}
	return out, updates, nil
}

func saveWofLedger(ctx context.Context, client *db.Client, entry wofLedgerEntry) error {
	return applyWofLedgerBatch(ctx, client, []wofLedgerEntry{entry}, nil)
}

func applyWofLedgerBatch(ctx context.Context, client *db.Client, entries []wofLedgerEntry, deleteKeys []string) error {
	updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
	rows := make([]db.AppStateRow, 0, len(entries))
	for _, entry := range entries {
		raw, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		rows = append(rows, db.AppStateRow{
			Key: compressionLedgerPrefix + entry.FileID, Value: string(raw), UpdatedAt: updatedAt,
		})
	}
	return client.AppState.ApplyBatch(ctx, rows, deleteKeys)
}

func nativeWofState(path string) (bool, uint32, uint32, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false, 0, 0, err
	}
	var external int32
	var provider uint32
	info := wofCompressionInfoV1{}
	infoSize := uint32(unsafe.Sizeof(info))
	hr, _, _ := procWofIsExternalFile.Call(
		uintptr(unsafe.Pointer(pathPtr)), uintptr(unsafe.Pointer(&external)), uintptr(unsafe.Pointer(&provider)),
		uintptr(unsafe.Pointer(&info)), uintptr(unsafe.Pointer(&infoSize)),
	)
	if int32(hr) < 0 {
		return false, 0, 0, hresultError(hr)
	}
	return external != 0, provider, info.Algorithm, nil
}

var errCompressionNotBeneficial = errors.New("compression not beneficial")

func wofCompress(path string) error {
	handle, err := openCompressionFile(path)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	return wofCompressHandle(handle)
}

func wofCompressHandle(handle windows.Handle) error {
	info := wofCompressionInfoV1{Algorithm: fileProviderCompressionXpress4K}
	hr := wofSetFileDataLocationCall(handle, wofProviderFile, &info)
	if uint32(hr) == hresultCompressionNotBeneficial {
		return errCompressionNotBeneficial
	}
	if int32(hr) < 0 {
		return hresultError(hr)
	}
	return nil
}

func wofDecompress(path string) error {
	handle, err := openDecompressionFile(path)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	return deleteExternalBackingCall(handle)
}

func nativeWofSetFileDataLocation(handle windows.Handle, provider uint32, info *wofCompressionInfoV1) uintptr {
	hr, _, _ := procWofSetFileDataLocation.Call(
		uintptr(handle), uintptr(provider), uintptr(unsafe.Pointer(info)), unsafe.Sizeof(*info),
	)
	return hr
}

func nativeDeleteExternalBacking(handle windows.Handle) error {
	var returned uint32
	return windows.DeviceIoControl(handle, fsctlDeleteExternalBacking, nil, 0, nil, 0, &returned, nil)
}

func ntfsDecompress(path string) error {
	handle, err := openCompressionFile(path)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	return setCompressionCall(handle, 0)
}

func nativeSetCompression(handle windows.Handle, format uint16) error {
	var returned uint32
	return windows.DeviceIoControl(
		handle,
		windows.FSCTL_SET_COMPRESSION,
		(*byte)(unsafe.Pointer(&format)),
		uint32(unsafe.Sizeof(format)),
		nil,
		0,
		&returned,
		nil,
	)
}

func nativeFileAttributes(path string) (uint32, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.GetFileAttributes(pathPtr)
}

func openCompressionFile(path string) (windows.Handle, error) {
	return openCompressionFileWithAccess(path, windows.GENERIC_READ|windows.GENERIC_WRITE)
}

func openPreparedXpressFile(path string) (windows.Handle, error) {
	return openCompressionFileWithOptions(
		path,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_SEQUENTIAL_SCAN|windows.FILE_FLAG_OPEN_REPARSE_POINT,
	)
}

func openDecompressionFile(path string) (windows.Handle, error) {
	return openCompressionFileWithAccess(path, windows.GENERIC_READ)
}

func openCompressionFileWithAccess(path string, access uint32) (windows.Handle, error) {
	return openCompressionFileWithOptions(
		path,
		access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_DELETE,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_SEQUENTIAL_SCAN,
	)
}

func openCompressionFileWithOptions(path string, access, share, flags uint32) (windows.Handle, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	return windows.CreateFile(pathPtr, access, share, nil, windows.OPEN_EXISTING, flags, 0)
}

func nativeFileIdentity(path string) (string, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	handle, err := windows.CreateFile(pathPtr, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return "", err
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	return nativeFileIdentityFromHandle(handle)
}

func nativeFileIdentityFromHandle(handle windows.Handle) (string, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return "", err
	}
	return fmt.Sprintf("%08x-%08x%08x", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow), nil
}

func volumeClusterSize(path string) (uint32, error) {
	root := filepath.VolumeName(path) + `\`
	rootPtr, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return 0, err
	}
	var sectorsPerCluster, bytesPerSector uint32
	r1, _, callErr := procGetDiskFreeSpaceW.Call(
		uintptr(unsafe.Pointer(rootPtr)), uintptr(unsafe.Pointer(&sectorsPerCluster)),
		uintptr(unsafe.Pointer(&bytesPerSector)), 0, 0,
	)
	if r1 == 0 {
		return 0, callErr
	}
	return sectorsPerCluster * bytesPerSector, nil
}

func hresultError(value uintptr) error {
	code := uint32(value)
	if code&0xffff0000 == 0x80070000 {
		return syscall.Errno(code & 0xffff)
	}
	return fmt.Errorf("HRESULT 0x%08x", code)
}

func isReparsePoint(info fs.FileInfo) bool {
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && data.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func replaceFile(source, destination string) error {
	sourcePtr, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destinationPtr, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	r1, _, callErr := procMoveFileExW.Call(
		uintptr(unsafe.Pointer(sourcePtr)), uintptr(unsafe.Pointer(destinationPtr)),
		moveFileReplaceExisting|moveFileWriteThrough,
	)
	if r1 == 0 {
		return callErr
	}
	return nil
}

func hideFile(path string) error {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attributes, err := windows.GetFileAttributes(pathPtr)
	if err != nil {
		return err
	}
	return windows.SetFileAttributes(pathPtr, attributes|windows.FILE_ATTRIBUTE_HIDDEN)
}
