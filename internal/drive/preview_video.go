package drive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const (
	previewVideoMaxSize   = 64 << 20
	previewVideoCacheSize = 256 << 20
	previewVideoSlots     = 2
	previewVideoTempExt   = ".ntmp"
)

var previewVideoExt = regexp.MustCompile(`^\.[a-z0-9]{1,5}$`)

// PreviewVideo returns a local copy of a drive item's preview video, downloading it into the app data cache
// first. Media Foundation would fetch a URL with its own HTTP stack, outside the app's proxy settings, so the
// player only ever gets the cached file. The cache keeps the most recently used files up to
// previewVideoCacheSize.
func (d *Drive) PreviewVideo(ctx context.Context, rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("invalid preview video URL %q", infra.SanitizeLogURL(rawURL))
	}
	if d.appData == nil || d.download == nil {
		return "", errors.New("preview video cache is not configured")
	}
	dir, err := d.appData.EnsureDir(appdata.DrivePreviewDir)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(rawURL))
	ext := strings.ToLower(path.Ext(u.Path))
	if !previewVideoExt.MatchString(ext) {
		ext = ".mp4"
	}
	file := filepath.Join(dir, hex.EncodeToString(sum[:16])+ext)

	result := d.previewFlight.DoChan(file, func() (any, error) {
		return nil, d.fetchPreviewVideo(ctx, rawURL, dir, file)
	})
	select {
	case r := <-result:
		if r.Err != nil {
			return "", r.Err
		}
		return file, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (d *Drive) fetchPreviewVideo(ctx context.Context, rawURL, dir, file string) error {
	if info, err := os.Stat(file); err == nil && info.Mode().IsRegular() {
		now := d.now()
		_ = os.Chtimes(file, now, now)
		return nil
	}

	select {
	case d.previewSlots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-d.previewSlots }()

	err := d.download.File(ctx, infra.DownloadRequest{URL: rawURL, Destination: file, MaxSize: previewVideoMaxSize})
	if err != nil {
		_ = os.Remove(file + previewVideoTempExt)
		return infra.AnnotateError(err, infra.Diagnostic{
			Operation: "drive.preview-video",
			Stage:     "download",
			Fields:    map[string]any{"url": infra.SanitizeLogURL(rawURL), "path": file},
		})
	}
	prunePreviewVideos(dir, file, previewVideoCacheSize)
	return nil
}

// prunePreviewVideos removes the least recently used files in dir until the rest fit in limit, keeping keep.
// A file the player still has open cannot be removed and stays until a later prune.
func prunePreviewVideos(dir, keep string, limit int64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type cached struct {
		path string
		size int64
		used time.Time
	}
	var files []cached
	var total int64
	for _, entry := range entries {
		if !entry.Type().IsRegular() || strings.HasSuffix(entry.Name(), previewVideoTempExt) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, cached{filepath.Join(dir, entry.Name()), info.Size(), info.ModTime()})
		total += info.Size()
	}
	slices.SortFunc(files, func(a, b cached) int { return a.used.Compare(b.used) })
	for _, f := range files {
		if total <= limit {
			return
		}
		if platform.SamePathFold(f.path, keep) {
			continue
		}
		if err := os.Remove(f.path); err == nil || errors.Is(err, fs.ErrNotExist) {
			total -= f.size
		}
	}
}
