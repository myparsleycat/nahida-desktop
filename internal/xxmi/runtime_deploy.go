package xxmi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const runtimeManifestName = ".nahida-runtime.json"

var errNoCachedLegacyRuntime = errors.New("no legacy runtime is cached")

type runtimeManifest struct {
	Mode        RuntimeMode       `json:"mode"`
	Source      string            `json:"source"`
	Files       map[string]string `json:"files"`
	UserManaged map[string]string `json:"userManaged,omitempty"`
	DeployedAt  string            `json:"deployedAt"`
}

func (x *XXMI) DeployRuntime(ctx context.Context, key string) ([]string, error) {
	cfg, err := x.GetImporterConfig(ctx, key)
	if err != nil {
		return nil, err
	}
	return x.deployRuntime(ctx, key, cfg)
}

func (x *XXMI) deployRuntime(ctx context.Context, key string, cfg ImporterConfig) ([]string, error) {
	cacheRoot, err := xxmiCacheRoot()
	if err != nil {
		return nil, err
	}
	var sourceFolder, sourceID string
	switch cfg.Mode {
	case RuntimeXXMI:
		version, err := x.resolveLibsVersion(ctx, cfg)
		if err != nil {
			return nil, err
		}
		sourceFolder = filepath.Join(cacheRoot, "packages", "xxmi-libs", version)
		if err := x.EnsureLibsVersion(ctx, version); err != nil {
			if info, statErr := os.Stat(sourceFolder); statErr == nil && info.IsDir() {
				return nil, fmt.Errorf("XXMI_RUNTIME_CORRUPTED: %w", err)
			}
			return nil, err
		}
		sourceID = "xxmi-libs@" + version
	case RuntimeLegacy:
		id := cfg.LegacyRuntime
		parent := filepath.Join(cacheRoot, "packages", "legacy-3dmigoto")
		if id == "" {
			id, err = newestLegacyRuntime(parent)
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, errNoCachedLegacyRuntime) {
				id, err = x.UpdateLegacyRuntime(ctx)
			}
			if err != nil {
				return nil, err
			}
		}
		if len(id) != 12 || strings.Trim(id, "0123456789abcdef") != "" {
			return nil, errors.New("invalid legacy runtime ID")
		}
		sourceID = "legacy@" + id
		sourceFolder = filepath.Join(parent, id)
		data, err := os.ReadFile(filepath.Join(sourceFolder, "source.json"))
		if err != nil {
			return nil, fmt.Errorf("XXMI_RUNTIME_CORRUPTED: %w", err)
		}
		var source LegacyRuntimeSource
		if err := json.Unmarshal(data, &source); err != nil {
			return nil, err
		}
		if err := verifyLegacyRuntimeCache(sourceFolder, source.ZipSHA256); err != nil {
			return nil, fmt.Errorf("XXMI_RUNTIME_CORRUPTED: %w", err)
		}
	default:
		return nil, fmt.Errorf("invalid runtime mode %q", cfg.Mode)
	}
	return deployRuntimeFiles(ctx, key, cfg, sourceFolder, sourceID, cacheRoot)
}

func (x *XXMI) resolveLibsVersion(ctx context.Context, cfg ImporterConfig) (string, error) {
	if version := normalizeVersion(cfg.XXMIVersion.Pinned); version != "" {
		return version, nil
	}
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if client == nil {
		return "", errors.New("XXMI settings store is not configured")
	}
	pkg, err := client.XXMIPackages.Get(ctx, "xxmi-libs")
	if err != nil {
		return "", err
	}
	if pkg == nil || pkg.LatestVersion == nil {
		return "", errors.New("XXMI libraries latest version is unknown")
	}
	return normalizeVersion(*pkg.LatestVersion), nil
}

func newestLegacyRuntime(parent string) (string, error) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", err
	}
	newest := ""
	var newestTime time.Time
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(newestTime) {
			newest, newestTime = entry.Name(), info.ModTime()
		}
	}
	if newest == "" {
		return "", errNoCachedLegacyRuntime
	}
	return newest, nil
}

func deployRuntimeFiles(
	ctx context.Context,
	key string,
	cfg ImporterConfig,
	sourceFolder, sourceID, cacheRoot string,
) ([]string, error) {
	root, err := openInstallRoot(cfg.ImporterFolder)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	if _, _, err := root.readFile("d3dx.ini"); err != nil {
		return nil, fmt.Errorf("XXMI_IMPORTER_NOT_INSTALLED: %w", err)
	}
	previous := runtimeManifest{Files: map[string]string{}}
	if data, _, err := root.readFile(runtimeManifestName); err == nil {
		if err := json.Unmarshal(data, &previous); err != nil {
			return nil, fmt.Errorf("decode runtime manifest: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	desired := map[string][]byte{}
	if cfg.Mode == RuntimeXXMI {
		for _, name := range []string{"d3d11.dll", "d3dcompiler_47.dll"} {
			data, err := os.ReadFile(filepath.Join(sourceFolder, name))
			if err != nil {
				return nil, err
			}
			desired[name] = data
		}
	} else {
		data, err := os.ReadFile(filepath.Join(sourceFolder, "source.json"))
		if err != nil {
			return nil, err
		}
		var source LegacyRuntimeSource
		if err := json.Unmarshal(data, &source); err != nil {
			return nil, err
		}
		for name := range source.Files {
			if filepath.Base(name) != name {
				return nil, fmt.Errorf("invalid runtime filename %q", name)
			}
			data, err := os.ReadFile(filepath.Join(sourceFolder, name))
			if err != nil {
				return nil, err
			}
			desired[name] = data
		}
	}
	warnings := []string{}
	manifest := runtimeManifest{
		Mode:        cfg.Mode,
		Source:      sourceID,
		Files:       map[string]string{},
		UserManaged: map[string]string{},
		DeployedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	backupFolder := filepath.Join(cacheRoot, "backups", key+" "+time.Now().Format("2006-01-02 15-04-05"))
	backup := func(name string, data []byte) error {
		if err := os.MkdirAll(backupFolder, 0o700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(backupFolder, name), data, 0o600)
	}
	for name, previousHash := range previous.Files {
		if _, keep := desired[name]; keep {
			continue
		}
		current, _, err := root.readFile(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if hashBytes(current) != previousHash {
			if err := backup(name, current); err != nil {
				return nil, err
			}
			warnings = append(warnings, "Backed up modified runtime file "+name)
		}
		if err := root.removeAll(name); err != nil {
			return nil, err
		}
	}
	if cfg.Mode == RuntimeXXMI {
		if current, _, err := root.readFile("nvapi64.dll"); err == nil {
			if err := backup("nvapi64.dll", current); err != nil {
				return nil, err
			}
			if err := root.removeAll("nvapi64.dll"); err != nil {
				return nil, err
			}
			warnings = append(warnings, "Backed up deprecated nvapi64.dll")
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	for name, desiredBytes := range desired {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		wantedHash := hashBytes(desiredBytes)
		current, info, err := root.readFile(name)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err == nil && hashBytes(current) == wantedHash {
			manifest.Files[name] = wantedHash
			continue
		}
		if err == nil && cfg.Migoto.UnsafeMode &&
			(previous.UserManaged[name] != "" || hashBytes(current) != previous.Files[name]) {
			warnings = append(warnings, "Preserved third-party runtime file "+name)
			manifest.UserManaged[name] = hashBytes(current)
			continue
		}
		if err == nil && hashBytes(current) != previous.Files[name] {
			if err := backup(name, current); err != nil {
				return nil, err
			}
			warnings = append(warnings, "Backed up modified runtime file "+name)
		}
		if err := root.writeFileAtomic(ctx, name, bytes.NewReader(desiredBytes), 0o600, info); err != nil {
			return nil, err
		}
		manifest.Files[name] = wantedHash
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	_, info, err := root.readFile(runtimeManifestName)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := root.writeFileAtomic(ctx, runtimeManifestName, bytes.NewReader(data), 0o600, info); err != nil {
		return nil, err
	}
	return warnings, nil
}

func hashBytes(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
