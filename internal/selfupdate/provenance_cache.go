package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/sigstore/sigstore-go/pkg/tuf"

	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/platform"
)

const (
	tufCacheFile     = "tuf-cache.json"
	tufStagingPrefix = ".tuf-"
)

type tufFiles map[string][]byte

type tufCache struct {
	// Latest retains the TUF rollback state even when a refresh fails before fetching the trust target.
	Latest tufFiles `json:"latest"`
}

func tufMirrorPath() string {
	return tuf.URLToPath(tuf.DefaultMirror)
}

func (s *sigstoreTrust) readTUFCache(ctx context.Context) (tufCache, error) {
	path := filepath.Join(s.cacheDir, tufCacheFile)
	data, err := os.ReadFile(path)
	if err == nil {
		var cache tufCache
		err := json.Unmarshal(data, &cache)
		if err == nil {
			return cache, nil
		}
		warnSelfUpdate(s.log, fmt.Errorf("decode Sigstore trust cache: %w", err), "trust-cache",
			map[string]any{"path": path})
	} else if !errors.Is(err, os.ErrNotExist) {
		return tufCache{}, err
	}
	files, err := readTUFFiles(ctx, s.cacheDir)
	return tufCache{Latest: files}, err
}

func (c tufCache) write(dir string) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return platform.WriteFileAtomic(filepath.Join(dir, tufCacheFile), data, 0o600)
}

func (s *sigstoreTrust) sweepTUFStaging(ctx context.Context) {
	release, err := diskio.AcquireDir(ctx, s.cacheDir)
	if err != nil {
		return
	}
	defer release()

	entries, err := os.ReadDir(s.cacheDir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			warnSelfUpdate(s.log, err, "sweep-trust-staging", map[string]any{"directory": s.cacheDir})
		}
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), tufStagingPrefix) {
			continue
		}
		dir := filepath.Join(s.cacheDir, entry.Name())
		if err := os.RemoveAll(dir); err != nil {
			warnSelfUpdate(s.log, err, "sweep-trust-staging", map[string]any{"directory": dir})
		}
	}
}

func readTUFFiles(ctx context.Context, dir string) (tufFiles, error) {
	release, err := diskio.AcquireDir(ctx, dir)
	if err != nil {
		return nil, err
	}
	defer release()

	files := make(tufFiles)
	source := os.DirFS(dir)
	mirror := tufMirrorPath()
	err = fs.WalkDir(source, mirror, func(name string, entry fs.DirEntry, err error) error {
		if name == mirror && errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		files[name], err = fs.ReadFile(source, name)
		return err
	})
	if err != nil {
		return nil, err
	}
	configPath := mirror + ".json"
	data, err := fs.ReadFile(source, configPath)
	if err == nil {
		files[configPath] = data
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return files, nil
}

// The TUF client can modify its cache even in ForceCache mode. Give it a private directory for each invocation.
func stageTUFFiles(ctx context.Context, parent string, files tufFiles) (dir string, returnErr error) {
	release, err := diskio.AcquireDir(ctx, parent)
	if err != nil {
		return "", err
	}
	defer release()

	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(parent, tufStagingPrefix)
	if err != nil {
		return "", err
	}
	defer func() {
		if returnErr != nil {
			_ = os.RemoveAll(stage)
		}
	}()

	destination, err := os.OpenRoot(stage)
	if err != nil {
		return "", err
	}
	defer func() { _ = destination.Close() }()
	for name, data := range files {
		path, err := filepath.Localize(name)
		if err != nil {
			return "", err
		}
		if err := destination.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", err
		}
		if err := destination.WriteFile(path, data, 0o600); err != nil {
			return "", err
		}
	}
	return stage, nil
}
