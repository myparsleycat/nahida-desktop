package mod

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

const maxZstdRestoreSize int64 = 64 << 30

type zstdManifest struct {
	Version int                           `json:"version"`
	Entries map[string]*zstdManifestEntry `json:"entries"`
}

type zstdManifestEntry struct {
	OriginalPath string `json:"originalPath"`
	ZstdPath     string `json:"zstdPath"`
	OriginalSize int64  `json:"originalSize"`
	SHA256       string `json:"sha256,omitempty"`
	ModTime      int64  `json:"modTime"`
	FileMode     uint32 `json:"fileMode"`
	State        string `json:"state"`
}

func compressDisabledZstd(
	ctx context.Context,
	roots []string,
	threshold int64,
	setTotals func(int, int64),
	progress func(string, int64, bool),
	mark compressionMutationMarker,
) error {
	folders, discoverErr := disabledModFolders(roots)
	var files []compressionFile
	var errs []error
	if discoverErr != nil {
		errs = append(errs, discoverErr)
	}
	for _, folder := range folders {
		handled := map[string]struct{}{}
		manifest, err := readZstdManifest(folder)
		if err == nil {
			for key, entry := range manifest.Entries {
				path := filepath.Join(folder, entry.OriginalPath)
				handled[strings.ToLower(filepath.Clean(path))] = struct{}{}
				setTotals(1, entry.OriginalSize)
				recoveryErr := recoverZstdEntry(ctx, folder, manifest, key, entry, mark)
				progress(path, entry.OriginalSize, recoveryErr != nil)
				if recoveryErr != nil {
					errs = append(errs, recoveryErr)
				}
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
		found, err := filesForZstd(folder, threshold)
		for _, file := range found {
			if _, ok := handled[strings.ToLower(filepath.Clean(file.path))]; !ok {
				files = append(files, file)
			}
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	var totalBytes int64
	for _, file := range files {
		totalBytes += file.size
	}
	setTotals(len(files), totalBytes)
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		folder := owningDisabledFolder(file.path, folders)
		err := compressZstdFile(ctx, folder, file.path, mark)
		progress(file.path, file.size, err != nil)
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func restoreAllZstd(ctx context.Context, roots []string, setTotals func(int, int64), progress func(string, int64, bool), mark compressionMutationMarker) error {
	return restoreZstdManifests(ctx, roots, func(string) bool { return true }, setTotals, progress, mark)
}

func restoreEnabledZstd(ctx context.Context, roots []string, setTotals func(int, int64), progress func(string, int64, bool), mark compressionMutationMarker) error {
	return restoreZstdManifests(ctx, roots, func(folder string) bool {
		return !isDisabled(filepath.Base(folder))
	}, setTotals, progress, mark)
}

func restoreZstdManifests(
	ctx context.Context,
	roots []string,
	include func(string) bool,
	setTotals func(int, int64),
	progress func(string, int64, bool),
	mark compressionMutationMarker,
) error {
	var manifests []string
	var errs []error
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				errs = append(errs, fmt.Errorf("access %q: %w", path, walkErr))
				return nil
			}
			if entry.IsDir() {
				info, err := entry.Info()
				if err != nil {
					errs = append(errs, err)
					return filepath.SkipDir
				}
				if info.Mode()&os.ModeSymlink != 0 || isReparsePoint(info) {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Name() == compressionManifestName && include(filepath.Dir(path)) {
				manifests = append(manifests, path)
			}
			return nil
		})
		if err != nil {
			errs = append(errs, err)
		}
	}
	for _, path := range manifests {
		manifest, err := readZstdManifest(filepath.Dir(path))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		var totalBytes int64
		for _, entry := range manifest.Entries {
			totalBytes += entry.OriginalSize
		}
		setTotals(len(manifest.Entries), totalBytes)
		for key, entry := range manifest.Entries {
			if err := ctx.Err(); err != nil {
				return errors.Join(append(errs, err)...)
			}
			err := restoreZstdEntry(ctx, filepath.Dir(path), manifest, key, entry, mark)
			progress(filepath.Join(filepath.Dir(path), entry.OriginalPath), entry.OriginalSize, err != nil)
			if err != nil {
				errs = append(errs, err)
			}
		}
		if len(manifest.Entries) == 0 {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func restoreZstdFolder(ctx context.Context, folder string, mark compressionMutationMarker) error {
	manifest, err := readZstdManifest(folder)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for key, entry := range manifest.Entries {
		if err := restoreZstdEntry(ctx, folder, manifest, key, entry, mark); err != nil {
			errs = append(errs, err)
		}
	}
	if len(manifest.Entries) == 0 {
		_ = os.Remove(filepath.Join(folder, compressionManifestName))
	}
	return errors.Join(errs...)
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
		name := filepath.Base(path)
		return info.Size() >= threshold && name != compressionManifestName &&
			!strings.Contains(name, compressionTempMarker) && !strings.HasSuffix(strings.ToLower(name), ".zst")
	})
}

func owningDisabledFolder(path string, folders []string) string {
	for _, folder := range folders {
		rel, err := filepath.Rel(folder, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return folder
		}
	}
	return filepath.Dir(path)
}

func compressZstdFile(ctx context.Context, folder, sourcePath string, mark compressionMutationMarker) error {
	manifest, err := readZstdManifest(folder)
	if errors.Is(err, os.ErrNotExist) {
		manifest = &zstdManifest{Version: 1, Entries: map[string]*zstdManifestEntry{}}
	} else if err != nil {
		return err
	}
	rel, err := filepath.Rel(folder, sourcePath)
	if err != nil {
		return err
	}
	if _, err := safeZstdRelativePath(folder, rel); err != nil {
		return fmt.Errorf("unsafe zstd source path %q", rel)
	}
	if existing := manifest.Entries[rel]; existing != nil {
		return recoverZstdEntry(ctx, folder, manifest, rel, existing, mark)
	}
	targetRel := rel + ".zst"
	targetPath := filepath.Join(folder, targetRel)
	if _, err := os.Lstat(targetPath); err == nil {
		return fmt.Errorf("zstd destination already exists: %s", targetPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return err
	}
	entry := &zstdManifestEntry{
		OriginalPath: rel, ZstdPath: targetRel, OriginalSize: info.Size(),
		ModTime: info.ModTime().UnixNano(), FileMode: uint32(info.Mode()), State: "compressing",
	}
	manifest.Entries[rel] = entry
	if err := writeZstdManifest(folder, manifest); err != nil {
		delete(manifest.Entries, rel)
		return err
	}
	tempPath := targetPath + compressionTempMarker
	_ = os.Remove(tempPath)
	hash, err := streamCompressZstd(ctx, sourcePath, tempPath)
	if err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("compress %q: %w", sourcePath, err)
	}
	entry.SHA256 = hash
	if err := validateZstd(ctx, tempPath, entry); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	tempInfo, err := os.Stat(tempPath)
	if err != nil {
		return err
	}
	if tempInfo.Size() >= entry.OriginalSize {
		_ = os.Remove(tempPath)
		delete(manifest.Entries, rel)
		return writeZstdManifest(folder, manifest)
	}
	if err := writeZstdManifest(folder, manifest); err != nil {
		return err
	}
	mark(sourcePath, targetPath)
	if err := os.Rename(tempPath, targetPath); err != nil {
		return err
	}
	if err := os.Chmod(targetPath, fs.FileMode(entry.FileMode)); err != nil {
		return err
	}
	if err := os.Chtimes(targetPath, time.Unix(0, entry.ModTime), time.Unix(0, entry.ModTime)); err != nil {
		return err
	}
	if err := os.Remove(sourcePath); err != nil {
		return err
	}
	entry.State = "compressed"
	return writeZstdManifest(folder, manifest)
}

func recoverZstdEntry(ctx context.Context, folder string, manifest *zstdManifest, key string, entry *zstdManifestEntry, mark compressionMutationMarker) error {
	sourcePath, targetPath, err := resolveZstdEntryPaths(folder, key, entry)
	if err != nil {
		return err
	}
	_, sourceErr := os.Stat(sourcePath)
	_, targetErr := os.Stat(targetPath)
	if sourceErr == nil && targetErr == nil && entry.SHA256 != "" {
		if err := validateZstd(ctx, targetPath, entry); err != nil {
			return err
		}
		hash, err := hashFile(ctx, sourcePath)
		if err != nil || hash != entry.SHA256 {
			return fmt.Errorf("zstd source verification failed: %s", sourcePath)
		}
		mark(sourcePath, targetPath)
		if err := os.Remove(sourcePath); err != nil {
			return err
		}
		entry.State = "compressed"
		return writeZstdManifest(folder, manifest)
	}
	if sourceErr == nil && errors.Is(targetErr, os.ErrNotExist) {
		delete(manifest.Entries, key)
		if err := writeZstdManifest(folder, manifest); err != nil {
			return err
		}
		return compressZstdFile(ctx, folder, sourcePath, mark)
	}
	if errors.Is(sourceErr, os.ErrNotExist) && targetErr == nil {
		if entry.SHA256 == "" {
			return fmt.Errorf("zstd entry has no checksum: %s", targetPath)
		}
		if err := validateZstd(ctx, targetPath, entry); err != nil {
			return err
		}
		entry.State = "compressed"
		return writeZstdManifest(folder, manifest)
	}
	return fmt.Errorf("zstd entry cannot be recovered safely: %s", sourcePath)
}

func restoreZstdEntry(ctx context.Context, folder string, manifest *zstdManifest, key string, entry *zstdManifestEntry, mark compressionMutationMarker) error {
	sourcePath, targetPath, err := resolveZstdEntryPaths(folder, key, entry)
	if err != nil {
		return err
	}
	sourceInfo, sourceErr := os.Stat(sourcePath)
	_, targetErr := os.Stat(targetPath)
	if sourceErr == nil && errors.Is(targetErr, os.ErrNotExist) {
		if entry.SHA256 == "" {
			if entry.State != "compressing" {
				return fmt.Errorf("zstd entry has no checksum: %s", sourcePath)
			}
		} else {
			hash, hashErr := hashFile(ctx, sourcePath)
			if hashErr != nil || sourceInfo.Size() != entry.OriginalSize || hash != entry.SHA256 {
				return fmt.Errorf("existing original does not match manifest: %s", sourcePath)
			}
		}
		if err := removeKnownZstdTemp(targetPath + compressionTempMarker); err != nil {
			return err
		}
		delete(manifest.Entries, key)
		return writeZstdManifest(folder, manifest)
	}
	if sourceErr == nil {
		if targetErr != nil {
			return targetErr
		}
		hash, hashErr := hashFile(ctx, sourcePath)
		if hashErr != nil || entry.SHA256 == "" || sourceInfo.Size() != entry.OriginalSize || hash != entry.SHA256 {
			return fmt.Errorf("existing original does not match manifest: %s", sourcePath)
		}
		if err := validateZstd(ctx, targetPath, entry); err != nil {
			return err
		}
		mark(sourcePath, targetPath)
		if err := os.Remove(targetPath); err != nil {
			return err
		}
		delete(manifest.Entries, key)
		return writeZstdManifest(folder, manifest)
	}
	if sourceErr != nil && !errors.Is(sourceErr, os.ErrNotExist) {
		return sourceErr
	}
	if targetErr != nil && !errors.Is(targetErr, os.ErrNotExist) {
		return targetErr
	}
	if entry.SHA256 == "" {
		return fmt.Errorf("zstd entry has no checksum: %s", targetPath)
	}
	tempPath := sourcePath + compressionTempMarker
	_ = os.Remove(tempPath)
	if err := streamRestoreZstd(ctx, targetPath, tempPath, entry); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Chmod(tempPath, fs.FileMode(entry.FileMode)); err != nil {
		return err
	}
	if err := os.Chtimes(tempPath, time.Unix(0, entry.ModTime), time.Unix(0, entry.ModTime)); err != nil {
		return err
	}
	mark(sourcePath, targetPath)
	if err := os.Rename(tempPath, sourcePath); err != nil {
		return err
	}
	if err := os.Remove(targetPath); err != nil {
		return err
	}
	delete(manifest.Entries, key)
	return writeZstdManifest(folder, manifest)
}

func streamCompressZstd(ctx context.Context, sourcePath, tempPath string) (string, error) {
	source, err := os.Open(sourcePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = source.Close() }()
	temp, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
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
		return "", err
	}
	hasher := sha256.New()
	_, copyErr := io.Copy(encoder, io.TeeReader(&contextReader{ctx: ctx, reader: source}, hasher))
	closeErr := encoder.Close()
	if copyErr != nil || closeErr != nil {
		return "", errors.Join(copyErr, closeErr)
	}
	if err := temp.Sync(); err != nil {
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	remove = false
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func streamRestoreZstd(ctx context.Context, sourcePath, tempPath string, entry *zstdManifestEntry) error {
	if entry == nil || entry.OriginalSize <= 0 {
		return fmt.Errorf("invalid zstd original size for %s", sourcePath)
	}
	if entry.OriginalSize > maxZstdRestoreSize {
		return fmt.Errorf("zstd original size exceeds restore limit for %s", sourcePath)
	}
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
	hasher := sha256.New()
	decoded := io.LimitReader(&contextReader{ctx: ctx, reader: decoder}, maxZstdRestoreSize+1)
	written, copyErr := io.Copy(io.MultiWriter(temp, hasher), decoded)
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
	if written != entry.OriginalSize || hex.EncodeToString(hasher.Sum(nil)) != entry.SHA256 {
		return fmt.Errorf("restored zstd data failed verification: %s", sourcePath)
	}
	return nil
}

func validateZstd(ctx context.Context, path string, entry *zstdManifestEntry) error {
	temp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+compressionTempMarker+".verify-")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	_ = temp.Close()
	_ = os.Remove(tempPath)
	defer func() { _ = os.Remove(tempPath) }()
	return streamRestoreZstd(ctx, path, tempPath, entry)
}

func hashFile(ctx context.Context, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, &contextReader{ctx: ctx, reader: file}); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func readZstdManifest(folder string) (*zstdManifest, error) {
	raw, err := os.ReadFile(filepath.Join(folder, compressionManifestName))
	if err != nil {
		return nil, err
	}
	var manifest zstdManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("decode zstd manifest in %q: %w", folder, err)
	}
	if manifest.Version != 1 || manifest.Entries == nil {
		return nil, fmt.Errorf("unsupported zstd manifest in %q", folder)
	}
	return &manifest, nil
}

func writeZstdManifest(folder string, manifest *zstdManifest) error {
	path := filepath.Join(folder, compressionManifestName)
	if len(manifest.Entries) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(folder, compressionManifestName+compressionTempMarker)
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	remove := true
	defer func() {
		_ = temp.Close()
		if remove {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := temp.Write(raw); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := replaceFile(tempPath, path); err != nil {
		return err
	}
	remove = false
	return hideFile(path)
}

func resolveZstdEntryPaths(folder, key string, entry *zstdManifestEntry) (string, string, error) {
	if entry == nil || key != entry.OriginalPath {
		return "", "", fmt.Errorf("invalid zstd manifest key %q", key)
	}
	original, err := safeZstdRelativePath(folder, entry.OriginalPath)
	if err != nil {
		return "", "", err
	}
	target, err := safeZstdRelativePath(folder, entry.ZstdPath)
	if err != nil {
		return "", "", err
	}
	if entry.ZstdPath != entry.OriginalPath+".zst" {
		return "", "", fmt.Errorf("invalid zstd destination for %q", entry.OriginalPath)
	}
	return original, target, nil
}

func safeZstdRelativePath(folder, relative string) (string, error) {
	if relative == "" || relative == "." || filepath.IsAbs(relative) || filepath.VolumeName(relative) != "" {
		return "", fmt.Errorf("unsafe zstd manifest path %q", relative)
	}
	cleanRelative := filepath.Clean(relative)
	if cleanRelative != relative || cleanRelative == ".." || strings.HasPrefix(cleanRelative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe zstd manifest path %q", relative)
	}
	root, err := filepath.Abs(folder)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.Abs(filepath.Join(root, cleanRelative))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("zstd manifest path escapes folder %q", relative)
	}
	if err := rejectReparseComponents(root, resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

func rejectReparseComponents(root, path string) error {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || isReparsePoint(rootInfo) {
		return fmt.Errorf("zstd manifest root is a reparse point %q", root)
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || isReparsePoint(info) {
			return fmt.Errorf("zstd manifest path crosses reparse point %q", current)
		}
	}
	return nil
}

func removeKnownZstdTemp(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
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
