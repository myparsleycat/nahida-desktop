package modelviewer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"nahida.live/desktop/internal/infra"
)

const gridPreviewCacheDir = "cache/mod-grid-preview-v1"

type GridPreviewCache struct {
	Fingerprint string `json:"fingerprint"`
	Image       string `json:"image"`
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
	path, err := t.gridPreviewCachePath(modPath, variant)
	if err != nil {
		return result, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("read grid preview cache: %w", err)
	}
	var cached GridPreviewCache
	// A damaged cache is disposable; the next render replaces it.
	if json.Unmarshal(raw, &cached) == nil && cached.Fingerprint == result.Fingerprint {
		if validateGridPreviewImage(cached.Image) == nil {
			result.Image = cached.Image
		}
	}
	return result, nil
}

func (t *Service) SaveModGridPreviewCache(
	ctx context.Context, modPath, variant, fingerprint, image string,
) (err error) {
	defer func() { err = t.reportGridPreviewCacheError(err, "write", modPath) }()
	if t.data == nil || fingerprint == "" {
		return nil
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
	path, err := t.gridPreviewCachePath(modPath, variant)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create grid preview cache: %w", err)
	}
	raw, err := json.Marshal(GridPreviewCache{Fingerprint: fingerprint, Image: image})
	if err != nil {
		return fmt.Errorf("encode grid preview cache: %w", err)
	}
	// Serialize readers and writers, and rename only after the complete file is closed.
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, raw, 0o600); err != nil {
		return fmt.Errorf("write grid preview cache: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("replace grid preview cache: %w", err)
	}
	return trimGridPreviewCache(filepath.Dir(path))
}

func (t *Service) gridPreviewCachePath(modPath, variant string) (string, error) {
	abs, err := filepath.Abs(modPath)
	if err != nil {
		return "", fmt.Errorf("resolve grid preview path: %w", err)
	}
	key := sha256.Sum256([]byte(strings.ToLower(abs) + "\x00" + variant))
	return t.data.Resolve(filepath.Join(gridPreviewCacheDir, fmt.Sprintf("%x.json", key)))
}

func gridPreviewFingerprint(ctx context.Context, modPath string) (string, error) {
	info, err := os.Stat(modPath)
	if err != nil {
		return "", fmt.Errorf("stat grid preview mod: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("grid preview mod path must be a directory")
	}
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

func validateGridPreviewImage(image string) error {
	const prefix = "data:image/png;base64,"
	if len(image) > 2*1024*1024 || !strings.HasPrefix(image, prefix) {
		return errors.New("invalid grid preview image")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(image, prefix))
	if err != nil {
		return fmt.Errorf("decode grid preview image: %w", err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("read grid preview PNG: %w", err)
	}
	if config.Width != 512 || config.Height != 512 {
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
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
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
