package xxmi

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"golang.org/x/mod/semver"
)

type CachedLibs struct {
	Version    string `json:"version"`
	Referenced bool   `json:"referenced"`
	// InUse marks the version selected for a launch; Referenced also protects older deployed versions.
	InUse bool `json:"inUse"`
	// InUseVersions names the releases launched from this version. A provider release deploys over the signed
	// libraries it was built on, so it is listed here under its own version, as in "1.2.2-nhd.1".
	InUseVersions []string `json:"inUseVersions"`
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
			result = append(result, CachedLibs{
				Version:       entry.Name(),
				Referenced:    referenced[entry.Name()].Referenced,
				InUse:         referenced[entry.Name()].InUse,
				InUseVersions: referenced[entry.Name()].InUseVersions,
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		order := semver.Compare("v"+result[i].Version, "v"+result[j].Version)
		if order != 0 {
			return order > 0
		}
		return result[i].Version > result[j].Version
	})
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
		if !entry.IsDir() || referenced[entry.Name()].Referenced || strings.Contains(entry.Name(), ".tmp-") {
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

func (x *XXMI) libsCacheReferences(ctx context.Context) (string, map[string]CachedLibs, error) {
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
	latest, skipped := "", ""
	if pkg != nil {
		if pkg.LatestVersion != nil {
			latest = normalizeVersion(*pkg.LatestVersion)
		}
		if pkg.SkippedVersion != nil {
			skipped = normalizeVersion(*pkg.SkippedVersion)
		}
	}
	referenced := map[string]CachedLibs{}
	for _, row := range rows {
		var cfg ImporterConfig
		if err := json.Unmarshal([]byte(row.Config), &cfg); err != nil {
			return "", nil, err
		}
		pin, _, err := x.libsPin(ctx, cfg)
		if err != nil {
			return "", nil, err
		}
		deployedVersion := ""
		data, err := os.ReadFile(filepath.Join(cfg.ImporterFolder, runtimeManifestName))
		if err == nil {
			var deployed runtimeManifest
			if json.Unmarshal(data, &deployed) == nil && strings.HasPrefix(deployed.Source, "xxmi-libs@") {
				deployedVersion = strings.TrimPrefix(deployed.Source, "xxmi-libs@")
				reference := referenced[deployedVersion]
				reference.Referenced = true
				referenced[deployedVersion] = reference
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", nil, err
		}

		selected, release := signedLibsVersion(pin), ""

		// A provider d3d11.dll deploys over the signed libraries its release was built on, whichever
		// signed release is the latest.
		provider, err := x.deployedLibsProvider(ctx, cfg)
		if err != nil {
			return "", nil, err
		}
		if provider.overlayPackage != "" {
			version, err := x.resolveProviderDLLVersion(ctx, provider, pin, cfg.ImporterFolder)
			if err == nil {
				selected, release = signedLibsVersion(version), version
			}
		}
		if selected == "" && cfg.Mode == RuntimeXXMI {
			selected = cachedLibsVersion(latest, skipped, deployedVersion, verifiedCachedLibsVersion)
		} else if selected == "" && legacyUsesXXMIInjector(cfg) {
			selected = newestCachedPackageVersion("xxmi-libs")
		}
		if selected != "" {
			reference := referenced[selected]
			reference.Version, reference.Referenced, reference.InUse = selected, true, true
			if release = cmp.Or(release, selected); !slices.Contains(reference.InUseVersions, release) {
				reference.InUseVersions = append(reference.InUseVersions, release)
			}
			referenced[selected] = reference
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
