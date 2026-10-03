package xxmi

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"nahida.live/desktop/internal/github"
	"nahida.live/desktop/internal/infra"
)

// externalDisabledImportersKey stores the importers the user turned off for this app as a JSON array.
// The launcher's own config is left untouched.
const externalDisabledImportersKey = "xxmi.disabledImporters"

// externalLauncher is a validated snapshot of an external XXMI Launcher installation.
type externalLauncher struct {
	path     string
	config   map[string]any
	parsed   parsedConfig
	disabled map[string]struct{}
}

func (l externalLauncher) configPath() string {
	return filepath.Join(l.path, xxmiConfigName)
}

func (l externalLauncher) importerFolder(key string) string {
	folder := l.parsed.Importers[key].Importer.ImporterFolder
	if !filepath.IsAbs(folder) {
		folder = filepath.Join(l.path, folder)
	}
	return filepath.Clean(folder)
}

// available reports whether the launcher itself has the importer set up.
func (l externalLauncher) available(key string) bool {
	_, configured := l.parsed.Importers[key]
	return configured && strings.TrimSpace(l.parsed.Packages.Packages[key].LatestVersion) != ""
}

func (l externalLauncher) enabled(key string) bool {
	_, disabled := l.disabled[key]
	return !disabled && l.available(key)
}

// importers lists the importers accepted by include, sorted by key.
func (l externalLauncher) importers(include func(key string) bool) []EnabledImporter {
	keys := make([]string, 0, len(l.parsed.Importers))
	for key := range l.parsed.Importers {
		if include(key) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)

	out := make([]EnabledImporter, 0, len(keys))
	for _, key := range keys {
		folder := l.importerFolder(key)
		var installed *string
		if spec, ok := lookupImporterPackage(key); ok {
			installed = readImporterVersion(folder, spec)
		}
		gameFolder := l.parsed.Importers[key].Importer.GameFolder
		if gameFolder != "" && !filepath.IsAbs(gameFolder) {
			gameFolder = filepath.Join(l.path, gameFolder)
		}
		info := l.parsed.Packages.Packages[key]
		out = append(out, EnabledImporter{
			Key: key, Mode: RuntimeXXMI, ImporterFolder: folder, GameFolder: gameFolder,
			InstalledVersion: installed, PackageInfo: info,
			UpdateAvailable: updateAvailable(info.LatestVersion, info.DeployedVersion, info.SkippedVersion),
		})
	}
	return out
}

// loadExternalLauncher returns nil when no external launcher path is saved. An unreadable
// or invalid config is logged and treated as unconfigured so consumers degrade gracefully.
func (x *XXMI) loadExternalLauncher(ctx context.Context) (*externalLauncher, error) {
	path, err := x.externalLauncherPath(ctx)
	if err != nil || path == nil {
		return nil, err
	}
	config, parsed, err := readAndValidateConfig(filepath.Join(*path, xxmiConfigName))
	if err != nil {
		_ = infra.ReportError(x.log, err, "XXMI.loadExternalLauncher", infra.Diagnostic{
			Severity: infra.DiagnosticWarn, Operation: "load-external-launcher", Stage: "read-config",
			Fields: map[string]any{"path": *path},
		})
		return nil, nil
	}
	disabled, err := x.externalDisabledImporters(ctx)
	if err != nil {
		return nil, err
	}
	return &externalLauncher{path: *path, config: config, parsed: parsed, disabled: disabled}, nil
}

func (x *XXMI) externalDisabledImporters(ctx context.Context) (map[string]struct{}, error) {
	client, err := x.settingsClient()
	if err != nil {
		return nil, err
	}
	stored, err := client.Settings.GetValue(ctx, externalDisabledImportersKey)
	if err != nil || stored == nil || *stored == "" {
		return nil, err
	}
	var keys []string
	if err := json.Unmarshal([]byte(*stored), &keys); err != nil {
		return nil, fmt.Errorf("decode disabled XXMI importers: %w", err)
	}
	disabled := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		disabled[key] = struct{}{}
	}
	return disabled, nil
}

// SetExternalImporterEnabled hides an external launcher importer from this app or shows it again.
func (x *XXMI) SetExternalImporterEnabled(ctx context.Context, key string, enabled bool) error {
	spec, ok := lookupImporterPackage(key)
	if !ok {
		return fmt.Errorf("unknown importer %q", key)
	}
	client, err := x.settingsClient()
	if err != nil {
		return err
	}
	if !x.acquireImporter(spec.key) {
		return errors.New("XXMI_BUSY")
	}
	defer x.releaseImporter(spec.key)

	// Notify consumers after disabledMu is released so they can read the saved selection.
	changed := false
	defer func() {
		if !changed {
			return
		}
		x.mu.RLock()
		notify := x.externalImportersChanged
		x.mu.RUnlock()
		if notify != nil {
			notify(ctx)
		}
	}()

	x.disabledMu.Lock()
	defer x.disabledMu.Unlock()
	disabled, err := x.externalDisabledImporters(ctx)
	if err != nil {
		return err
	}
	if _, isDisabled := disabled[spec.key]; isDisabled != enabled {
		return nil
	}
	keys := make([]string, 0, len(disabled)+1)
	for stored := range disabled {
		if stored != spec.key {
			keys = append(keys, stored)
		}
	}
	if !enabled {
		keys = append(keys, spec.key)
	}
	slices.Sort(keys)

	value, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	encoded := string(value)
	if err := client.Settings.Upsert(ctx, externalDisabledImportersKey, &encoded); err != nil {
		return err
	}
	changed = true
	if x.log != nil {
		x.log.Info(
			fmt.Sprintf("Set external XXMI importer %s enabled=%t", spec.key, enabled),
			"XXMI.setExternalImporterEnabled",
		)
	}
	return nil
}

func (x *XXMI) requireExternalLauncher(ctx context.Context) (*externalLauncher, error) {
	launcher, err := x.loadExternalLauncher(ctx)
	if err != nil {
		return nil, err
	}
	if launcher == nil {
		return nil, errors.New("XXMI is not configured")
	}
	return launcher, nil
}

func (x *XXMI) SaveXXMIPath(ctx context.Context, inputPath string) error {
	absolute, err := filepath.Abs(strings.TrimSpace(inputPath))
	if err != nil {
		return err
	}
	if _, _, err := readAndValidateConfig(filepath.Join(absolute, xxmiConfigName)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return infra.WithCause(errors.New("XXMI Launcher Config.json not found"), err)
		}
		return fmt.Errorf("XXMI Launcher Config.json is invalid: %w", err)
	}
	client, err := x.settingsClient()
	if err != nil {
		return err
	}
	if err := client.Settings.Upsert(ctx, xxmiPathKey, &absolute); err != nil {
		return err
	}
	if x.eventEmit != nil {
		x.eventEmit("renderer:reload")
	}
	return nil
}

func (x *XXMI) FindXXMIPath(ctx context.Context) (*string, error) {
	return x.findExternalLauncherPath(ctx)
}

func (x *XXMI) externalEnabledImporters(ctx context.Context) ([]EnabledImporter, error) {
	launcher, err := x.loadExternalLauncher(ctx)
	if err != nil || launcher == nil {
		return []EnabledImporter{}, err
	}
	return launcher.importers(launcher.enabled), nil
}

func (x *XXMI) externalHuntingRuntime(ctx context.Context, importerKey string) (HuntingRuntime, error) {
	launcher, err := x.requireExternalLauncher(ctx)
	if err != nil {
		return HuntingRuntime{}, err
	}
	for key, importer := range launcher.parsed.Importers {
		if !strings.EqualFold(key, strings.TrimSpace(importerKey)) {
			continue
		}
		if !launcher.enabled(key) {
			return HuntingRuntime{}, fmt.Errorf("importer %q is not enabled", key)
		}
		folder := launcher.importerFolder(key)
		return HuntingRuntime{
			ImporterKey: key, ImporterFolder: folder, INIPath: filepath.Join(folder, "d3dx.ini"),
			GameEXENames: importer.Importer.processNames(),
		}, nil
	}
	return HuntingRuntime{}, fmt.Errorf("unknown importer %q", importerKey)
}

func (x *XXMI) externalGameExecutable(ctx context.Context, importer string) (string, error) {
	launcher, err := x.requireExternalLauncher(ctx)
	if err != nil {
		return "", err
	}
	config, ok := launcher.parsed.Importers[importer]
	if !ok {
		return "", fmt.Errorf("importer %s not found", importer)
	}
	executable := config.Importer.gameExecutable()
	if executable == "" {
		executable = config.Importer.processName()
	}
	return executable, nil
}

func (x *XXMI) startExternalGame(ctx context.Context, importer string) error {
	importer = strings.TrimSpace(importer)
	if importer == "" {
		return errors.New("importer is required")
	}
	launcher, err := x.requireExternalLauncher(ctx)
	if err != nil {
		return err
	}
	config, ok := launcher.parsed.Importers[importer]
	if !ok {
		return fmt.Errorf("importer %s not found", importer)
	}
	if _, disabled := launcher.disabled[importer]; disabled {
		return fmt.Errorf("importer %s is disabled", importer)
	}
	processName := config.Importer.processName()
	if processName == "" {
		return fmt.Errorf("game process is not configured for importer %s", importer)
	}
	gameExecutable := config.Importer.gameExecutable()
	if gameExecutable == "" {
		gameExecutable = processName
	}
	if err := x.rejectLaunchBlockers(ctx, importer, gameExecutable, true); err != nil {
		return err
	}

	executable := filepath.Join(launcher.path, "Resources", "Bin", launcherImageName)
	if info, err := os.Stat(executable); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("XXMI Launcher not found at %s", executable)
	}
	if x.log != nil {
		x.log.Info("Starting game "+importer+" via XXMI Launcher", "XXMI.startGame")
	}
	if err := startLauncher(executable, importer); err != nil {
		return err
	}

	timeoutSeconds := launcher.parsed.Launcher.StartTimeout
	if timeoutSeconds <= 0 {
		timeoutSeconds = 60
	}
	pid, err := waitForVisibleProcess(ctx, processName, time.Duration(timeoutSeconds*float64(time.Second)))
	if err != nil {
		return err
	}
	if x.log != nil {
		x.log.Info(fmt.Sprintf("Detected %s (PID: %d)", processName, pid), "XXMI.startGame")
	}
	timer := time.NewTimer(time.Second)
	select {
	case <-ctx.Done():
		timer.Stop()
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func waitForVisibleProcess(ctx context.Context, processName string, timeout time.Duration) (int, error) {
	return waitForVisibleProcessWith(
		ctx,
		processName,
		timeout,
		100*time.Millisecond,
		findProcessPID,
		processHasVisibleWindow,
	)
}

func waitForVisibleProcessWith(
	ctx context.Context,
	processName string,
	timeout, pollInterval time.Duration,
	find func(context.Context, string) (int, error),
	hasVisibleWindow func(int) bool,
) (int, error) {
	deadline := time.Now().Add(timeout)
	for {
		pid, err := find(ctx, processName)
		if err != nil {
			return 0, err
		}
		if pid > 0 && hasVisibleWindow(pid) {
			return pid, nil
		}
		if timeout > 0 && !time.Now().Before(deadline) {
			return 0, fmt.Errorf("failed to detect game process %s after starting launcher", processName)
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return 0, ctx.Err()
		case <-timer.C:
		}
	}
}

// processNames lists the image names the game can run under, in the launcher's own order:
// an explicit override, then the process executables, then the game executables.
func (i externalImporter) processNames() []string {
	if name := strings.TrimSpace(i.GameProcessEXE); i.GameProcessEXEEnabled && name != "" {
		return []string{name}
	}
	if len(i.ProcessEXENames) > 0 {
		return slices.Clone(i.ProcessEXENames)
	}
	return slices.Clone(i.GameEXENames)
}

// launchesFromGameFolder reports whether the launcher starts the executable in game_folder itself.
// Platform, custom, and manual launches do not need a valid game folder.
func (i externalImporter) launchesFromGameFolder() bool {
	return i.GameLaunch == "" || i.GameLaunch == "DIRECT"
}

// gameExecutable returns the configured game executable, or "" when the folder cannot provide one.
func (i externalImporter) gameExecutable() string {
	if !i.launchesFromGameFolder() {
		return ""
	}
	return configuredGameExecutable(i.GameFolder, i.GameEXENames)
}

// processName returns the image name the launcher waits for after starting the game.
func (i externalImporter) processName() string {
	overridden := i.GameProcessEXEEnabled && strings.TrimSpace(i.GameProcessEXE) != ""
	if !overridden && len(i.ProcessEXENames) == 0 {
		// The launcher runs whichever configured executable exists, such as YuanShen.exe.
		if executable := i.gameExecutable(); executable != "" {
			return filepath.Base(executable)
		}
	}
	names := i.processNames()
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

// EnsureLauncherClosed closes the external launcher before its DLLs are replaced.
//
//wails:ignore
func (x *XXMI) EnsureLauncherClosed(ctx context.Context) error {
	return ensureLauncherClosed(ctx, x.findProcess)
}

type InstallDLLVersionInput struct {
	Version string `json:"version"`
}

// InstallDLLVersion replaces the XXMI libraries package of the external launcher.
func (x *XXMI) InstallDLLVersion(ctx context.Context, input InstallDLLVersionInput) (returnErr error) {
	x.packageMu.Lock()
	defer x.packageMu.Unlock()

	stage := "validate-input"
	fields := map[string]any{"version": installDiagnosticValue(input.Version)}
	defer func() {
		if returnErr != nil {
			returnErr = infra.ReportError(x.log, returnErr, "XXMI.installDllVersion", infra.Diagnostic{
				Operation: "install-dll-version", Stage: stage, Fields: fields,
			})
		}
	}()
	version := strings.TrimSpace(input.Version)
	if version == "" || strings.ContainsAny(version, "\r\n\x00") {
		return errors.New("invalid version")
	}
	fileVersion := normalizeVersion(version)
	if fileVersion == "" {
		return errors.New("invalid version")
	}

	stage = "load-config"
	launcher, err := x.requireExternalLauncher(ctx)
	if err != nil {
		return err
	}
	fields["xxmi_path"] = launcher.path
	if x.archive == nil || !x.github.Configured() {
		return errors.New("XXMI install services are not configured")
	}

	stage = "close-launcher"
	if err := ensureLauncherClosed(ctx, x.findProcess); err != nil {
		return err
	}

	stage = "download-package"
	workDir, err := os.MkdirTemp("", "nahida-xxmi-dll-")
	if err != nil {
		return err
	}
	defer func() { x.reportCleanup(os.RemoveAll(workDir), "InstallDLLVersion") }()
	zipPath := filepath.Join(workDir, "package.zip")
	if err := x.github.DownloadFile(ctx, github.FileRequest{
		Repo:        libsRepo,
		URL:         github.ReleaseFileURL(libsRepo, version, "XXMI-PACKAGE-"+version+".zip"),
		Destination: zipPath,
	}); err != nil {
		return fmt.Errorf("failed to download XXMI package: %w", err)
	}

	stage = "extract-package"
	extractedPath, err := x.archive.Extract(
		ctx,
		zipPath,
		filepath.Join(workDir, "extracted"),
		infra.ExtractOptions{},
		nil,
	)
	if err != nil {
		return fmt.Errorf("extract XXMI package: %w", err)
	}
	stagingDir := filepath.Join(workDir, "staging")
	if err := copyTreeContext(ctx, extractedPath, stagingDir); err != nil {
		return fmt.Errorf("stage XXMI package: %w", err)
	}

	stage = "validate-manifest"
	manifest, err := x.github.FetchFile(
		ctx, libsRepo, github.ReleaseFileURL(libsRepo, version, "Manifest.json"), 1024*1024,
	)
	if err != nil {
		return fmt.Errorf("failed to download XXMI manifest: %w", err)
	}
	var manifestData struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(manifest, &manifestData); err != nil {
		return fmt.Errorf("decode XXMI manifest: %w", err)
	}
	if normalizeVersion(manifestData.Version) != fileVersion {
		return fmt.Errorf("manifest version mismatch: expected %s, got %s", version, manifestData.Version)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, "Manifest.json"), manifest, 0o644); err != nil {
		return err
	}

	// The launcher would immediately replace a manually selected version on its next update check.
	stage = "disable-auto-update"
	launcherSection, ok := launcher.config["Launcher"].(map[string]any)
	if !ok {
		return errors.New("XXMI Launcher config is missing Launcher section")
	}
	launcherSection["auto_update"] = false
	if err := writeXXMIConfig(launcher.configPath(), launcher.config); err != nil {
		return err
	}

	stage = "install-package"
	destination := filepath.Join(launcher.path, "Resources", "Packages", "XXMI")
	fields["destination"] = destination
	if err := copyTreeContext(ctx, stagingDir, destination); err != nil {
		return fmt.Errorf("install XXMI package: %w", err)
	}
	if x.log != nil {
		x.log.Info("Installed XXMI DLL version "+version+" to "+destination, "XXMI.installDllVersion")
	}
	return nil
}

func (x *XXMI) installExternalImporterPackage(
	ctx context.Context,
	spec importerPackageSpec,
	input InstallImporterPackageInput,
) (returnErr error) {
	stage := "load-config"
	rollbackState := "not-started"
	cleanupState := "not-started"
	fields := map[string]any{"importer": spec.key, "version": installDiagnosticValue(input.Version)}
	defer func() {
		if returnErr == nil {
			return
		}
		fields["rollback"] = rollbackState
		fields["cleanup"] = cleanupState
		returnErr = infra.ReportError(x.log, returnErr, "XXMI.installExternalImporterPackage", infra.Diagnostic{
			Operation: "install-importer-package", Stage: stage, Fields: fields,
		})
	}()
	version := strings.TrimSpace(input.Version)
	fileVersion := normalizeVersion(version)
	if fileVersion == "" {
		return errors.New("invalid version")
	}
	launcher, err := x.requireExternalLauncher(ctx)
	if err != nil {
		return err
	}
	importer, exists := launcher.parsed.Importers[spec.key]
	if !exists {
		return fmt.Errorf("importer %s not found", spec.key)
	}
	importerFolder := launcher.importerFolder(spec.key)
	configPath := launcher.configPath()
	fields["xxmi_path"] = launcher.path
	fields["importer_path"] = importerFolder
	fields["config_path"] = configPath
	if x.archive == nil || !x.github.Configured() {
		return errors.New("XXMI install services are not configured")
	}
	if importerFolder == filepath.Clean(launcher.path) {
		return errors.New("importer folder is not configured")
	}

	stage = "close-launcher"
	launcherExecutable := filepath.Join(launcher.path, "Resources", "Bin", launcherImageName)
	if err := ensureLauncherClosedAt(ctx, launcherExecutable); err != nil {
		return err
	}

	stage = "create-work-directory"
	workDir, err := os.MkdirTemp("", "nahida-xxmi-importer-")
	if err != nil {
		return err
	}
	fields["work_path"] = workDir
	cleanupState = "pending"
	defer func() {
		cleanupErr := os.RemoveAll(workDir)
		if cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
			cleanupState = "failed"
		} else {
			cleanupState = "complete"
		}
		x.reportCleanup(cleanupErr, "InstallImporterPackage")
	}()

	stage = "download-package"
	zipPath := filepath.Join(workDir, "package.zip")
	packageURL := github.ReleaseFileURL(spec.repo, version, fmt.Sprintf(spec.assetFormat, fileVersion))
	fields["package_url"] = infra.SanitizeLogURL(packageURL)
	if err := x.downloadPackageFile(
		ctx,
		"importer:"+spec.key, fileVersion,
		github.FileRequest{Repo: spec.repo, URL: packageURL, Destination: zipPath},
	); err != nil {
		return fmt.Errorf("failed to download %s package: %w", spec.key, err)
	}

	stage = "extract-package"
	extractedPath, err := x.archive.Extract(
		ctx,
		zipPath,
		filepath.Join(workDir, "extracted"),
		infra.ExtractOptions{},
		nil,
	)
	if err != nil {
		return fmt.Errorf("extract %s package: %w", spec.key, err)
	}
	stagingDir := filepath.Join(workDir, "staging")
	if err := copyTreeContext(ctx, extractedPath, stagingDir); err != nil {
		return fmt.Errorf("stage %s package: %w", spec.key, err)
	}
	stagedVersion := readImporterVersion(stagingDir, spec)
	if stagedVersion == nil || normalizeVersion(*stagedVersion) != fileVersion {
		return fmt.Errorf("package version mismatch: expected %s", version)
	}

	stage = "recover-installation"
	transaction, err := beginImporterInstallTransaction(ctx, importerFolder, configPath, spec.key)
	if err != nil {
		return err
	}
	defer func() { x.reportCleanup(transaction.Close(), "InstallImporterPackage") }()
	config, _, err := parseAndValidateConfig(transaction.configRaw)
	if err != nil {
		return err
	}
	if err := setImporterDeployedVersion(config, spec.key, fileVersion); err != nil {
		return err
	}
	configJSON, err := marshalXXMIConfig(config)
	if err != nil {
		return err
	}

	stage = "prepare-transaction"
	prepared := false
	committed := false
	defer func() {
		if !prepared || committed {
			return
		}
		rollbackState = "rolling-back"
		if rollbackErr := transaction.rollback(); rollbackErr != nil {
			rollbackState = "rollback-failed"
			returnErr = infra.WithCause(returnErr, rollbackErr)
			return
		}
		rollbackState = "rolled-back"
	}()
	stageRoot, err := transaction.prepare(ctx)
	if err != nil {
		prepared = transaction.state != ""
		return err
	}
	prepared = true

	var iniBackup []byte
	if !importer.Importer.OverwriteINI {
		iniBackup, _, err = stageRoot.readFile("d3dx.ini")
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			_ = stageRoot.Close()
			return err
		}
	}

	stage = "pre-install"
	if _, err := executeXcmdDeletesRoot(ctx, stagingDir, stageRoot, "PreInstall"); err != nil {
		_ = stageRoot.Close()
		return fmt.Errorf("pre-install %s package: %w", spec.key, err)
	}
	stage = "copy-package"
	if err := copyTreeFilterToRoot(ctx, stagingDir, stageRoot, shouldSkipImporterMods); err != nil {
		_ = stageRoot.Close()
		return fmt.Errorf("install %s package: %w", spec.key, err)
	}
	stage = "post-install"
	if _, err := executeXcmdDeletesFromRoot(ctx, stageRoot, stageRoot, "PostInstall"); err != nil {
		_ = stageRoot.Close()
		return fmt.Errorf("post-install %s package: %w", spec.key, err)
	}
	if iniBackup != nil {
		if err := stageRoot.writeFileAtomic(ctx, "d3dx.ini", bytes.NewReader(iniBackup), 0o644, nil); err != nil {
			_ = stageRoot.Close()
			return err
		}
	}
	if err := stageRoot.Close(); err != nil {
		return err
	}

	stage = "commit"
	rollbackState = "backup-retained"
	if _, _, err := transaction.commit(ctx, configJSON); err != nil {
		return err
	}
	committed = true
	rollbackState = "committed"
	x.reportCleanup(transaction.finish(), "InstallImporterPackage")
	if x.log != nil {
		x.log.Info(
			"Installed "+spec.key+" package version "+version+" to "+importerFolder,
			"XXMI.installExternalImporterPackage",
		)
	}
	return nil
}

// setImporterDeployedVersion records a manually installed package and turns off launcher
// auto updates so the launcher does not replace the selected version.
func setImporterDeployedVersion(config map[string]any, key, version string) error {
	launcher, ok := config["Launcher"].(map[string]any)
	if !ok {
		return errors.New("XXMI Launcher config is missing Launcher section")
	}
	launcher["auto_update"] = false
	packagesSection, ok := config["Packages"].(map[string]any)
	if !ok {
		return errors.New("XXMI Launcher config is missing Packages section")
	}
	packages, ok := packagesSection["packages"].(map[string]any)
	if !ok {
		return errors.New("XXMI Launcher config is missing Packages.packages")
	}
	entry, _ := packages[key].(map[string]any)
	if entry == nil {
		packages[key] = map[string]any{
			"latest_version": "", "skipped_version": "", "deployed_version": version,
			"update_check_time": 0, "latest_release_notes": "", "deployed_release_notes": "",
		}
		return nil
	}
	entry["deployed_version"] = version
	return nil
}

func writeXXMIConfig(path string, config map[string]any) error {
	configJSON, err := marshalXXMIConfig(config)
	if err != nil {
		return err
	}
	root, err := openInstallRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	_, info, err := root.readFile(filepath.Base(path))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return root.writeFileAtomic(
		context.Background(), filepath.Base(path), bytes.NewReader(configJSON), 0o644, info,
	)
}

func marshalXXMIConfig(config map[string]any) ([]byte, error) {
	configJSON, err := json.MarshalIndent(config, "", "    ")
	if err != nil {
		return nil, err
	}
	return append(configJSON, '\n'), nil
}

func (x *XXMI) externalDeployedLibsVersion(ctx context.Context) (string, bool) {
	launcher, err := x.loadExternalLauncher(ctx)
	if err != nil || launcher == nil {
		return "", false
	}
	if version := dllVersion(&launcher.path); version != nil {
		return *version, true
	}
	version := launcher.parsed.Packages.Packages["XXMI"].DeployedVersion
	return version, version != ""
}

// enableExternalUnsafeMode lets the external launcher load a user-built d3d11.dll. The launcher
// only honors unsafe mode with a signature made by its own per-install private key.
func (x *XXMI) enableExternalUnsafeMode(ctx context.Context, importer string) error {
	launcher, err := x.requireExternalLauncher(ctx)
	if err != nil {
		return err
	}
	importers, _ := launcher.config["Importers"].(map[string]any)
	section, _ := importers[importer].(map[string]any)
	migoto, _ := section["Migoto"].(map[string]any)
	unsafeMode, exists := migoto["unsafe_mode"].(bool)
	if !exists || unsafeMode {
		return nil
	}
	signature, err := unsafeModeSignature(launcher.path)
	if err != nil {
		return fmt.Errorf("sign XXMI unsafe mode: %w", err)
	}
	migoto["unsafe_mode"] = true
	migoto["unsafe_mode_signature"] = signature
	return writeXXMIConfig(launcher.configPath(), launcher.config)
}

func unsafeModeSignature(xxmiPath string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(xxmiPath, "Resources", "Security", "private_key.der"))
	if err != nil {
		return "", err
	}
	der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return "", err
	}
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return "", err
	}
	current, err := user.Current()
	if err != nil {
		return "", err
	}
	username := current.Username
	if index := strings.LastIndexAny(username, `\/`); index >= 0 {
		username = username[index+1:]
	}
	digest := sha256.Sum256([]byte(username))

	var signature []byte
	switch key := parsed.(type) {
	case *rsa.PrivateKey:
		signature, err = rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	case *ecdsa.PrivateKey:
		signature, err = ecdsa.SignASN1(rand.Reader, key, digest[:])
	default:
		return "", fmt.Errorf("unsupported XXMI private key type %T", parsed)
	}
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}
