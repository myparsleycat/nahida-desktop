//go:build windows

package xxmi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (x *XXMI) RepairRuntime(ctx context.Context, key string) ([]string, error) {
	spec, ok := lookupImporterPackage(key)
	if !ok {
		return nil, errors.New("unknown XXMI importer")
	}
	for _, name := range append(append([]string{}, spec.gameExeNames...), spec.processNames...) {
		pid, err := findProcessPID(ctx, name)
		if err != nil {
			return nil, err
		}
		if pid != 0 {
			return nil, errors.New("XXMI_GAME_RUNNING")
		}
	}
	cfg, err := x.GetImporterConfig(ctx, key)
	if err != nil {
		return nil, err
	}
	if err := ValidateImporterSettings(key, cfg); err != nil {
		return nil, err
	}
	warnings := []string{}
	switch cfg.Mode {
	case RuntimeXXMI:
		version, err := x.resolveLibsVersion(ctx, cfg)
		if err != nil {
			return nil, err
		}
		warnings, err = x.repairLibsCache(ctx, version)
		if err != nil {
			return nil, err
		}
	case RuntimeLegacy:
		cfg, warnings, err = x.repairLegacyCache(ctx, cfg)
		if err != nil {
			return nil, err
		}
	}
	cfg.Migoto.UnsafeMode = false
	deployedWarnings, err := x.deployRuntime(ctx, key, cfg)
	return append(warnings, deployedWarnings...), err
}

func (x *XXMI) repairLegacyCache(ctx context.Context, cfg ImporterConfig) (ImporterConfig, []string, error) {
	cacheRoot, err := xxmiCacheRoot()
	if err != nil {
		return cfg, nil, err
	}
	parent := filepath.Join(cacheRoot, "packages", "legacy-3dmigoto")
	id := cfg.LegacyRuntime
	if id == "" {
		id, err = newestLegacyRuntime(parent)
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, errNoCachedLegacyRuntime) {
			id, err = x.UpdateLegacyRuntime(ctx)
		}
		if err != nil {
			return cfg, nil, err
		}
	}
	if len(id) != 12 || strings.Trim(id, "0123456789abcdef") != "" {
		return cfg, nil, errors.New("invalid legacy runtime ID")
	}
	folder := filepath.Join(parent, id)
	data, err := os.ReadFile(filepath.Join(folder, "source.json"))
	if err != nil {
		return cfg, nil, fmt.Errorf("re-import the legacy runtime ZIP to repair its cache: %w", err)
	}
	var source LegacyRuntimeSource
	if err := json.Unmarshal(data, &source); err != nil {
		return cfg, nil, fmt.Errorf("re-import the legacy runtime ZIP to repair its cache: %w", err)
	}
	if err := verifyLegacyRuntimeCache(folder, source.ZipSHA256); err == nil {
		return cfg, []string{}, nil
	}
	if source.URL != legacyRuntimeURL {
		return cfg, nil, errors.New("re-import the local legacy runtime ZIP to repair its cache")
	}
	backupRoot := filepath.Join(cacheRoot, "backups")
	if err := os.MkdirAll(backupRoot, 0o700); err != nil {
		return cfg, nil, err
	}
	backup := filepath.Join(backupRoot, fmt.Sprintf("legacy-%s-corrupt-%d", id, time.Now().UnixNano()))
	if err := os.Rename(folder, backup); err != nil {
		return cfg, nil, fmt.Errorf("back up corrupt legacy runtime: %w", err)
	}
	newID, err := x.UpdateLegacyRuntime(ctx)
	if err != nil {
		return cfg, nil, errors.Join(err, os.Rename(backup, folder))
	}
	if cfg.LegacyRuntime != "" && newID != id {
		return cfg, nil, errors.Join(
			errors.New("pinned legacy runtime changed upstream; select the new runtime or re-import the original ZIP"),
			os.Rename(backup, folder),
		)
	}
	cfg.LegacyRuntime = newID
	return cfg, []string{"Backed up corrupt legacy runtime to " + backup}, nil
}

func (x *XXMI) repairLibsCache(ctx context.Context, version string) ([]string, error) {
	if version == "" || version == "." || version == ".." || strings.ContainsAny(version, `\/:*?"<>|`) {
		return nil, errors.New("invalid XXMI libraries version")
	}
	cacheRoot, err := xxmiCacheRoot()
	if err != nil {
		return nil, err
	}
	folder := filepath.Join(cacheRoot, "packages", "xxmi-libs", version)
	if err := verifyXXMILibsCache(folder, version); err == nil {
		return []string{}, nil
	}
	backup := ""
	if _, err := os.Stat(folder); err == nil {
		backupRoot := filepath.Join(cacheRoot, "backups")
		if err := os.MkdirAll(backupRoot, 0o700); err != nil {
			return nil, err
		}
		backup = filepath.Join(backupRoot, fmt.Sprintf("xxmi-libs-%s-corrupt-%d", version, time.Now().UnixNano()))
		if err := os.Rename(folder, backup); err != nil {
			return nil, fmt.Errorf("back up corrupt XXMI libraries: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := x.EnsureLibsVersion(ctx, version); err != nil {
		if backup != "" {
			return nil, errors.Join(err, os.Rename(backup, folder))
		}
		return nil, err
	}
	if backup != "" {
		return []string{"Backed up corrupt XXMI libraries to " + backup}, nil
	}
	return []string{}, nil
}

func (x *XXMI) OpenImporterFolder(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg, err := x.GetImporterConfig(ctx, key)
	if err != nil {
		return err
	}
	if err := validateLocalFolder("importer folder", cfg.ImporterFolder, true); err != nil {
		return err
	}
	info, err := os.Stat(cfg.ImporterFolder)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("importer folder %q is not a directory", cfg.ImporterFolder)
	}
	command := exec.Command("explorer.exe", filepath.Clean(cfg.ImporterFolder))
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}
