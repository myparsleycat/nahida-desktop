package reshade

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

func commitEffectPackage(
	ctx context.Context,
	layout layout,
	pkg EffectPackage,
	staging string,
	files []string,
	records map[string]effectRecord,
) (retainBackup bool, returnErr error) {
	release, err := diskio.AcquireDir(ctx, layout.effects())
	if err != nil {
		return false, err
	}
	defer release()

	type original struct{ target, backup string }
	var originals []original
	seen := map[string]bool{}
	snapshot := func(file string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		target := filepath.Join(layout.effects(), file)
		if platform.SamePathFold(target, layout.effects()) || !platform.SameOrChildPath(layout.effects(), target) {
			return fmt.Errorf("effect path escapes shared folders: %s", file)
		}
		key := strings.ToLower(filepath.Clean(target))
		if seen[key] {
			return nil
		}
		info, err := os.Stat(target)
		backup := ""
		if err == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("effect destination is not a regular file: %s", target)
			}
			backup = filepath.Join(staging, "backup", file)
			if err := copyFile(target, backup); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		originals = append(originals, original{target, backup})
		seen[key] = true
		return nil
	}
	defer func() {
		if returnErr == nil {
			return
		}
		// Rollback must finish even when pause or cancellation interrupted the install.
		var rollbackErr error
		for _, original := range slices.Backward(originals) {
			if original.backup != "" {
				rollbackErr = errors.Join(rollbackErr, copyFile(original.backup, original.target))
			} else if err := os.Remove(original.target); err != nil && !errors.Is(err, os.ErrNotExist) {
				rollbackErr = errors.Join(rollbackErr, err)
			}
		}
		retainBackup = rollbackErr != nil
		returnErr = infra.AnnotateError(errors.Join(returnErr, rollbackErr), infra.Diagnostic{
			Stage: "rollback", Fields: map[string]any{
				"rollbackFailed": retainBackup, "backupPath": filepath.Join(staging, "backup"),
			},
		})
	}()

	previous := records[pkg.ID].Files
	for _, file := range files {
		if err := snapshot(file); err != nil {
			return false, err
		}
		if err := copyFile(
			filepath.Join(staging, "prepared", file),
			filepath.Join(layout.effects(), file),
		); err != nil {
			return false, err
		}
	}
	retained := map[string]bool{}
	for id, record := range records {
		if id == pkg.ID {
			continue
		}
		for _, file := range record.Files {
			retained[strings.ToLower(filepath.Clean(file))] = true
		}
	}
	for _, file := range files {
		retained[strings.ToLower(filepath.Clean(file))] = true
	}
	for _, file := range previous {
		if retained[strings.ToLower(filepath.Clean(file))] {
			continue
		}
		if err := snapshot(file); err != nil {
			return false, err
		}
		if err := os.Remove(filepath.Join(layout.effects(), file)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	records[pkg.ID] = effectRecord{Name: pkg.Name, InstalledAt: time.Now().UTC().Format(time.RFC3339), Files: files}
	return false, writeEffectRecords(layout.record(), records)
}
