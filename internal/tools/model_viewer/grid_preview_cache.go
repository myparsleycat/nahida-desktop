package modelviewer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const (
	gridPreviewCacheDir       = "cache/mod-grid-preview-v2"
	gridPreviewLegacyCacheDir = "cache/mod-grid-preview-v1"
	gridPreviewUploadID       = "grid-preview-image"
	gridPreviewImageMaxBytes  = 2 * 1024 * 1024
	gridPreviewImageSize      = 512
)

type GridPreviewCache struct {
	Fingerprint string `json:"fingerprint"`
	// URL of the cached image, empty when the mod has no current render.
	URL string `json:"url"`
}

func (t *Service) GetModGridPreviewCache(
	ctx context.Context, modPath, variant string,
) (result GridPreviewCache, err error) {
	defer func() { err = t.reportGridPreviewCacheError(err, "read", modPath) }()
	result.Fingerprint, err = gridPreviewFingerprint(ctx, modPath)
	if err != nil || t.data == nil {
		return result, err
	}

	t.gridPreviewMu.Lock()
	defer t.gridPreviewMu.Unlock()
	t.gridPreviewLegacyCleanup.Do(t.removeLegacyGridPreviewCache)
	path, err := t.gridPreviewCachePath(modPath, variant, result.Fingerprint)
	if err != nil {
		return result, err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("read grid preview cache: %w", err)
	}
	defer func() { _ = file.Close() }()

	// A damaged cache is disposable; the next render replaces it. Images are
	// fully decoded before they are written, so the header is enough here.
	if validateGridPreviewImageConfig(file) == nil {
		result.URL = t.protocol.LocalFileURL(path, false)
	}
	return result, nil
}

// PrepareModGridPreviewCacheUpload opens the slot the renderer uploads a
// rendered preview into, inside the model session that produced it.
func (t *Service) PrepareModGridPreviewCacheUpload(sessionID string, byteLength int64) (string, error) {
	if byteLength <= 0 || byteLength > gridPreviewImageMaxBytes {
		return "", errors.New("invalid grid preview image size")
	}
	return t.protocol.CreateMemoryUpload(sessionID, gridPreviewUploadID, byteLength)
}

func (t *Service) SaveModGridPreviewCache(
	ctx context.Context, modPath, variant, fingerprint, sessionID string,
) (err error) {
	defer func() { err = t.reportGridPreviewCacheError(err, "write", modPath) }()
	if t.data == nil || fingerprint == "" {
		return nil
	}
	image, err := t.protocol.TakeMemoryUpload(sessionID, gridPreviewUploadID)
	if err != nil {
		return fmt.Errorf("take grid preview upload: %w", err)
	}
	if err := validateGridPreviewImage(image); err != nil {
		return err
	}
	current, err := gridPreviewFingerprint(ctx, modPath)
	if err != nil {
		return err
	}
	// Never associate an image with files edited while the model was loading/rendering.
	if current != fingerprint {
		return nil
	}

	t.gridPreviewMu.Lock()
	defer t.gridPreviewMu.Unlock()
	path, err := t.gridPreviewCachePath(modPath, variant, fingerprint)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create grid preview cache: %w", err)
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, image, 0o600); err != nil {
		return fmt.Errorf("write grid preview cache: %w", err)
	}
	if err := platform.ReplaceAtomic(temporary, path); err != nil {
		return fmt.Errorf("replace grid preview cache: %w", err)
	}

	// Renders of earlier fingerprints can never be served again.
	stale, err := filepath.Glob(gridPreviewCachePattern(path))
	if err != nil {
		return fmt.Errorf("list stale grid preview cache: %w", err)
	}
	for _, other := range stale {
		if other != path {
			_ = os.Remove(other)
		}
	}
	return trimGridPreviewCache(filepath.Dir(path))
}

// The name carries the fingerprint, so a lookup is a single open and a stale
// render is never read.
func (t *Service) gridPreviewCachePath(modPath, variant, fingerprint string) (string, error) {
	abs, err := filepath.Abs(modPath)
	if err != nil {
		return "", fmt.Errorf("resolve grid preview path: %w", err)
	}
	key := sha256.Sum256([]byte(strings.ToLower(abs) + "\x00" + variant))
	revision := sha256.Sum256([]byte(fingerprint))
	return t.data.Resolve(filepath.Join(gridPreviewCacheDir, fmt.Sprintf("%x-%x.png", key, revision[:8])))
}

func gridPreviewCachePattern(path string) string {
	name := filepath.Base(path)
	return filepath.Join(filepath.Dir(path), name[:sha256.Size*2]+"-*.png")
}

func (t *Service) removeLegacyGridPreviewCache() {
	if legacy, err := t.data.Resolve(gridPreviewLegacyCacheDir); err == nil {
		_ = os.RemoveAll(legacy)
	}
}

// gridPreviewFingerprint identifies the mod's render sources by relative path, size, and
// modification time. File contents are deliberately not read: the fingerprint is recomputed on
// every cache lookup, and hashing textures made a cache hit as slow as reading the whole mod.
func gridPreviewFingerprint(ctx context.Context, modPath string) (string, error) {
	info, err := os.Stat(modPath)
	if err != nil {
		return "", fmt.Errorf("stat grid preview mod: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("grid preview mod path must be a directory")
	}

	release, err := diskio.AcquireDir(ctx, modPath)
	if err != nil {
		return "", fmt.Errorf("wait for grid preview mod disk: %w", err)
	}
	defer release()

	hash := sha256.New()
	err = filepath.WalkDir(modPath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || !isGridPreviewModFile(entry.Name()) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported grid preview source: %s", path)
		}
		relative, err := filepath.Rel(modPath, path)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(
			hash,
			"%s\x00%d\x00%d\n",
			filepath.ToSlash(relative),
			info.Size(),
			info.ModTime().UnixNano(),
		)
		return err
	})
	if err != nil {
		return "", fmt.Errorf("scan grid preview mod files: %w", err)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// isGridPreviewModFile reports whether a mod file can affect the rendered grid preview.
// Textures are included because the preview shades meshes with their material textures; editing
// only a texture, without touching the INI or mesh files, must still invalidate the cached image.
func isGridPreviewModFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".ib", ".vb", ".buf", ".ini", ".fmt", ".hlsl", ".hlsli", ".cso", ".bin",
		".dds", ".png", ".jpg", ".jpeg":
		return true
	default:
		return false
	}
}

func validateGridPreviewImage(image []byte) error {
	if len(image) > gridPreviewImageMaxBytes {
		return errors.New("invalid grid preview image")
	}
	if err := validateGridPreviewImageConfig(bytes.NewReader(image)); err != nil {
		return err
	}
	if _, err := png.Decode(bytes.NewReader(image)); err != nil {
		return fmt.Errorf("decode grid preview PNG: %w", err)
	}
	return nil
}

func validateGridPreviewImageConfig(image io.Reader) error {
	config, err := png.DecodeConfig(image)
	if err != nil {
		return fmt.Errorf("read grid preview PNG: %w", err)
	}
	if config.Width != gridPreviewImageSize || config.Height != gridPreviewImageSize {
		return errors.New("grid preview image must be 512 by 512")
	}
	return nil
}

func trimGridPreviewCache(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("list grid preview cache: %w", err)
	}
	files := make([]fs.FileInfo, 0, len(entries))
	var total int64
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".png" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files = append(files, info)
		total += info.Size()
	}
	slices.SortFunc(files, func(a, b fs.FileInfo) int { return a.ModTime().Compare(b.ModTime()) })
	for i, file := range files {
		if len(files)-i <= 512 && total <= 256*1024*1024 {
			break
		}
		if err := os.Remove(filepath.Join(dir, file.Name())); err != nil {
			return fmt.Errorf("evict grid preview cache: %w", err)
		}
		total -= file.Size()
	}
	return nil
}

func (t *Service) reportGridPreviewCacheError(err error, stage, modPath string) error {
	if err != nil && t.log != nil {
		return infra.ReportError(t.log, err, "Tools.ModGridPreviewCache", infra.Diagnostic{
			Operation: "mod-grid-preview-cache",
			Stage:     stage,
			Fields:    map[string]any{"modPath": modPath},
		})
	}
	return err
}
