package xxmi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type CachedLibs struct {
	Version    string `json:"version"`
	Referenced bool   `json:"referenced"`
}

type LegacyRuntimeInfo struct {
	ID     string              `json:"id"`
	Source LegacyRuntimeSource `json:"source"`
}

func (x *XXMI) ListCachedLibs(ctx context.Context) ([]CachedLibs, error) {
	parent, referenced, err := x.libsCacheReferences(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(parent)
	if errors.Is(err, os.ErrNotExist) {
		return []CachedLibs{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]CachedLibs, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() && !strings.Contains(entry.Name(), ".tmp-") {
			result = append(result, CachedLibs{Version: entry.Name(), Referenced: referenced[entry.Name()]})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version > result[j].Version })
	return result, nil
}

func (x *XXMI) PruneLibsCache(ctx context.Context) ([]string, error) {
	x.packageMu.Lock()
	defer x.packageMu.Unlock()

	parent, referenced, err := x.libsCacheReferences(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(parent)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	removed := []string{}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		if !entry.IsDir() || referenced[entry.Name()] || strings.Contains(entry.Name(), ".tmp-") {
			continue
		}
		target := filepath.Join(parent, entry.Name())
		relative, err := filepath.Rel(parent, target)
		if err != nil || relative != entry.Name() || filepath.IsAbs(relative) {
			return removed, fmt.Errorf("invalid cache entry %q", entry.Name())
		}
		info, err := os.Lstat(target)
		if err != nil {
			return removed, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return removed, fmt.Errorf("invalid cache directory %q", target)
		}
		if err := os.RemoveAll(target); err != nil {
			return removed, err
		}
		removed = append(removed, entry.Name())
	}
	return removed, nil
}

func (x *XXMI) libsCacheReferences(ctx context.Context) (string, map[string]bool, error) {
	cacheRoot, err := xxmiCacheRoot()
	if err != nil {
		return "", nil, err
	}
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if client == nil {
		return "", nil, errors.New("XXMI settings store is not configured")
	}
	rows, err := client.XXMIImporters.List(ctx)
	if err != nil {
		return "", nil, err
	}
	pkg, err := client.XXMIPackages.Get(ctx, "xxmi-libs")
	if err != nil {
		return "", nil, err
	}
	referenced := map[string]bool{}
	for _, row := range rows {
		var cfg ImporterConfig
		if err := json.Unmarshal([]byte(row.Config), &cfg); err != nil {
			return "", nil, err
		}
		if cfg.Mode == RuntimeXXMI {
			if cfg.XXMIVersion.Pinned != "" {
				referenced[normalizeVersion(cfg.XXMIVersion.Pinned)] = true
			} else if pkg != nil && pkg.LatestVersion != nil {
				referenced[normalizeVersion(*pkg.LatestVersion)] = true
			}
		} else if cfg.Mode == RuntimeLegacy && cfg.ExtraLibraries.Enabled && len(cfg.ExtraLibraries.Paths) > 0 {
			version := normalizeVersion(cfg.XXMIVersion.Pinned)
			if version == "" {
				version = newestCachedPackageVersion("xxmi-libs")
			}
			if version != "" {
				referenced[version] = true
			}
		}
		data, err := os.ReadFile(filepath.Join(cfg.ImporterFolder, runtimeManifestName))
		if err == nil {
			var deployed runtimeManifest
			if json.Unmarshal(data, &deployed) == nil && strings.HasPrefix(deployed.Source, "xxmi-libs@") {
				referenced[strings.TrimPrefix(deployed.Source, "xxmi-libs@")] = true
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", nil, err
		}
	}
	return filepath.Join(cacheRoot, "packages", "xxmi-libs"), referenced, nil
}

func (x *XXMI) GetLegacyRuntimes(ctx context.Context) ([]LegacyRuntimeInfo, error) {
	root, err := xxmiCacheRoot()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(root, "packages", "legacy-3dmigoto"))
	if errors.Is(err, os.ErrNotExist) {
		return []LegacyRuntimeInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]LegacyRuntimeInfo, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() || len(entry.Name()) != 12 || strings.Trim(entry.Name(), "0123456789abcdef") != "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, "packages", "legacy-3dmigoto", entry.Name(), "source.json"))
		if err != nil {
			return nil, err
		}
		var source LegacyRuntimeSource
		if err := json.Unmarshal(data, &source); err != nil {
			return nil, err
		}
		result = append(result, LegacyRuntimeInfo{ID: entry.Name(), Source: source})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Source.FetchedAt > result[j].Source.FetchedAt })
	return result, nil
}
