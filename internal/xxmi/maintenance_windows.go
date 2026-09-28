//go:build windows

package xxmi

import (
	"context"
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
	if cfg.Mode == RuntimeXXMI {
		version, err := x.resolveLibsVersion(ctx, cfg)
		if err != nil {
			return nil, err
		}
		warnings, err = x.repairLibsCache(ctx, version)
		if err != nil {
			return nil, err
		}
	}
	cfg.Migoto.UnsafeMode = false
	deployedWarnings, err := x.deployRuntime(ctx, key, cfg)
	return append(warnings, deployedWarnings...), err
}

func (x *XXMI) repairLibsCache(ctx context.Context, version string) ([]string, error) {
	if version == "" || strings.ContainsAny(version, `\/:*?"<>|`) {
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
