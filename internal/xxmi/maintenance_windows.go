//go:build windows

package xxmi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	return x.DeployRuntime(ctx, key)
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
