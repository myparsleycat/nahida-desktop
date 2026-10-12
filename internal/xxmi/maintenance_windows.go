//go:build windows

package xxmi

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"nahida.live/desktop/internal/infra"
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

		// The deployment below still writes the verified cached release when a fresh one cannot be fetched.
		source, err := x.redownloadProviderRelease(ctx, cfg)
		if infra.IsCancellationError(err) {
			return nil, err
		}
		if err != nil {
			_ = infra.ReportError(x.log, err, "XXMI.RepairRuntime", infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: "repair-runtime", Stage: "redownload-provider",
				Fields: map[string]any{"importer": key, "provider": source},
			})
			warnings = append(warnings, "Could not download "+cmp.Or(source, "the libraries provider release")+
				" again: "+err.Error())
		}
	case RuntimeLegacy:
		cfg, warnings, err = x.repairLegacyCache(ctx, cfg)
		if err != nil {
			return nil, err
		}
	}
	cfg.Migoto.UnsafeMode = false
	deployedWarnings, err := x.deployRuntime(ctx, key, cfg, true)
	return append(warnings, deployedWarnings...), err
}

// RepairImporterPackage installs the importer's package again over the installed one, at the version that is
// installed, so that package files that were changed or removed come back. Mods are left alone, and the
// d3dx.ini is treated as any other installation of the package treats it.
func (x *XXMI) RepairImporterPackage(ctx context.Context, key string) (returnErr error) {
	stage, version := "validate", ""
	defer func() {
		returnErr = infra.ReportError(x.log, returnErr, "XXMI.RepairImporterPackage", infra.Diagnostic{
			Operation: "repair-importer-package", Stage: stage,
			Fields: map[string]any{"importer": installDiagnosticValue(key), "version": version},
		})
	}()
	spec, ok := lookupImporterPackage(key)
	if !ok {
		return errors.New("unknown XXMI importer")
	}
	for _, name := range append(append([]string{}, spec.gameExeNames...), spec.processNames...) {
		pid, err := x.findProcess(ctx, name)
		if err != nil {
			return err
		}
		if pid != 0 {
			return errors.New("XXMI_GAME_RUNNING")
		}
	}

	stage = "resolve-version"
	cfg, err := x.GetImporterConfig(ctx, spec.key)
	if err != nil {
		return err
	}
	if installed := readImporterVersion(cfg.ImporterFolder, spec); installed != nil {
		version = *installed
	} else if version = normalizeVersion(cfg.PackageVersion.Pinned); version == "" {
		// The file that names the installed version is part of what is being repaired.
		releases, err := x.ListReleases(ctx, "importer:"+spec.key)
		if err != nil {
			return err
		}
		if len(releases) == 0 {
			return fmt.Errorf("importer %s has no releases", spec.key)
		}
		version = releases[0].Version
	}

	// A release installed without a signature was accepted by the user when it was installed, and the record
	// of that outlives the file that names the installed version. A record that cannot be read accepts
	// nothing, and a signed release is installed again without one.
	record, err := readPackageVerification(cfg.ImporterFolder)
	allowUnsigned := err == nil && record != nil && record.Method != "ecdsa" &&
		normalizeVersion(record.Version) == normalizeVersion(version)

	stage = "install-package"
	return x.InstallImporterPackage(ctx, InstallImporterPackageInput{
		Importer: spec.key, Version: version, AllowUnsigned: allowUnsigned,
	})
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
