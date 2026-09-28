package xxmi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/xxmi/inject"
)

func (x *XXMI) startBuiltinGame(ctx context.Context, key string, cfg ImporterConfig) (returnErr error) {
	x.mu.Lock()
	if x.busy {
		x.mu.Unlock()
		return errors.New("XXMI_BUSY")
	}
	x.busy = true
	x.mu.Unlock()
	defer func() {
		x.mu.Lock()
		x.busy = false
		x.mu.Unlock()
	}()

	stage := "validate"
	defer func() {
		if returnErr != nil && x.log != nil {
			_ = infra.ReportError(x.log, returnErr, "XXMI.StartGame", infra.Diagnostic{
				Operation: "launch-game", Stage: stage,
				Fields: map[string]any{"importer": key, "mode": cfg.Mode, "importerFolder": cfg.ImporterFolder,
					"gameFolder": cfg.GameFolder},
			})
		}
	}()
	progress := func(next string) {
		stage = next
		if x.eventEmit != nil {
			x.eventEmit("xxmi:launch-progress", map[string]any{"importer": key, "stage": stage})
		}
	}
	if !cfg.Enabled {
		return errors.New("XXMI_NOT_CONFIGURED")
	}
	if _, err := os.Stat(filepath.Join(cfg.ImporterFolder, "d3dx.ini")); err != nil {
		return fmt.Errorf("XXMI_IMPORTER_NOT_INSTALLED: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(cfg.ImporterFolder, "Mods"), 0o700); err != nil {
		return err
	}
	packageSpec, ok := lookupImporterPackage(key)
	if !ok {
		return fmt.Errorf("unknown importer %q", key)
	}
	gameExe := configuredGameExecutable(cfg.GameFolder, packageSpec.gameExeNames)
	if gameExe == "" || !filepath.IsAbs(gameExe) {
		return errors.New("XXMI_GAME_FOLDER_NOT_CONFIGURED")
	}
	if info, err := os.Stat(gameExe); err != nil || !info.Mode().IsRegular() {
		return errors.New("XXMI_GAME_FOLDER_NOT_CONFIGURED")
	}
	processName := filepath.Base(gameExe)
	if len(packageSpec.processNames) > 0 {
		processName = packageSpec.processNames[0]
	}

	progress("launch-guard")
	if err := x.rejectLaunchBlockers(ctx, key, gameExe); err != nil {
		return err
	}
	progress("ensure-runtime")
	warnings, err := x.DeployRuntime(ctx, key)
	if err != nil {
		return err
	}
	_ = warnings
	if x.elevated == nil {
		return errors.New("XXMI_ELEVATION_DENIED: elevated helper is unavailable")
	}
	progress("update-ini")
	if err := x.updateLaunchINI(ctx, cfg, processName); err != nil {
		return err
	}
	progress("elevate")
	release, err := x.elevated.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("XXMI_ELEVATION_DENIED: %w", err)
	}
	defer release()
	launchSpec, err := x.builtinLaunchSpec(cfg, gameExe, processName)
	if err != nil {
		return err
	}
	progress("inject-launch")
	result, err := x.elevated.LaunchXXMI(ctx, launchSpec)
	if err != nil {
		return err
	}
	progress("finish")
	cfg.LaunchCount++
	if err := x.SaveImporterConfig(ctx, key, cfg); err != nil {
		return err
	}
	if x.log != nil {
		x.log.Info(fmt.Sprintf("Started %s (PID: %d, verified: %t)", key, result.PID, result.InjectionVerified),
			"XXMI.StartGame")
	}
	return nil
}

func (x *XXMI) updateLaunchINI(ctx context.Context, cfg ImporterConfig, processName string) error {
	root, err := openInstallRoot(cfg.ImporterFolder)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	data, info, err := root.readFile("d3dx.ini")
	if err != nil {
		return err
	}
	doc := parseINI(data)
	doc.SetOption("Loader", "target", processName, true)
	doc.SetOption("Loader", "module", "d3d11.dll", true)
	doc.SetOption("Loader", "launch", "", true)
	if cfg.Mode == RuntimeXXMI {
		doc.SetOption("Loader", "loader", x.elevated.HelperImageName(), true)
	}
	if !doc.Changed() {
		return nil
	}
	return root.writeFileAtomic(ctx, "d3dx.ini", bytes.NewReader(doc.Bytes()), 0o600, info)
}

func (x *XXMI) builtinLaunchSpec(cfg ImporterConfig, gameExe, processName string) (inject.LaunchSpec, error) {
	data, err := os.ReadFile(filepath.Join(cfg.ImporterFolder, runtimeManifestName))
	if err != nil {
		return inject.LaunchSpec{}, err
	}
	var deployed runtimeManifest
	if err := json.Unmarshal(data, &deployed); err != nil {
		return inject.LaunchSpec{}, err
	}
	spec := inject.LaunchSpec{
		Mode: inject.RuntimeMode(cfg.Mode), ProcessName: processName, StartExe: gameExe,
		WorkDir: filepath.Dir(gameExe), StartMethod: cfg.ProcessStartMethod, Priority: cfg.ProcessPriority,
		InjectMode: "Hook", UseHook: true, TimeoutSeconds: cfg.ProcessTimeout,
		ModuleDLL: filepath.Join(cfg.ImporterFolder, "d3d11.dll"),
	}
	if cfg.UseLaunchOptions {
		spec.StartArgs, err = splitLaunchOptions(cfg.LaunchOptions)
		if err != nil {
			return inject.LaunchSpec{}, fmt.Errorf("parse launch options: %w", err)
		}
	}
	if strings.EqualFold(processName, "Client-Win64-Shipping.exe") {
		spec.StartArgs = append([]string{"-dx11"}, spec.StartArgs...)
	}
	if strings.EqualFold(processName, "Endfield.exe") {
		spec.StartArgs = append([]string{"-force-d3d11"}, spec.StartArgs...)
	}
	if cfg.CustomLaunch.Enabled {
		spec.CustomLaunchCmd = cfg.CustomLaunch.Command
		spec.InjectMode = cfg.CustomLaunch.InjectMode
	}
	if cfg.ExtraLibraries.Enabled {
		spec.ExtraDLLs = append([]string(nil), cfg.ExtraLibraries.Paths...)
	}
	if cfg.Mode == RuntimeXXMI {
		version := strings.TrimPrefix(deployed.Source, "xxmi-libs@")
		if version == deployed.Source || version == "" {
			return inject.LaunchSpec{}, errors.New("XXMI_RUNTIME_CORRUPTED: invalid library source")
		}
		cacheRoot, err := xxmiCacheRoot()
		if err != nil {
			return inject.LaunchSpec{}, err
		}
		spec.LoaderDLL, err = verifiedLaunchFile(
			filepath.Join(cacheRoot, "packages", "xxmi-libs", version, "3dmloader.dll"),
		)
		if err != nil {
			return inject.LaunchSpec{}, err
		}
	} else {
		spec.LegacyLoader, err = verifiedLaunchFile(filepath.Join(cfg.ImporterFolder, "3DMigoto Loader.exe"))
		if err != nil {
			return inject.LaunchSpec{}, err
		}
	}
	return spec, nil
}

func verifiedLaunchFile(path string) (inject.VerifiedFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return inject.VerifiedFile{}, err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return inject.VerifiedFile{}, err
	}
	return inject.VerifiedFile{Path: path, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}
