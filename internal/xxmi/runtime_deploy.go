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

	"golang.org/x/mod/semver"

	"nahida.live/desktop/internal/platform"
)

const runtimeManifestName = ".nahida-runtime.json"

var errNoCachedLegacyRuntime = errors.New("no legacy runtime is cached")

type runtimeManifest struct {
	Mode        RuntimeMode       `json:"mode"`
	Source      string            `json:"source"`
	Files       map[string]string `json:"files"`
	UserManaged map[string]string `json:"userManaged,omitempty"`
	// Custom is the cached custom d3d11.dll this deployment wrote. The file is listed in UserManaged.
	Custom     string `json:"custom,omitempty"`
	DeployedAt string `json:"deployedAt"`
}

// deployedCustomDLL reports whether hash still belongs to the custom DLL the manifest recorded, as opposed
// to a file the user changed afterwards. A custom DLL ID is the start of its hash.
func (m runtimeManifest) deployedCustomDLL(name, hash string) bool {
	return name == customDLLName && m.Custom != "" && strings.HasPrefix(hash, m.Custom)
}

// preservesUserFile reports whether unsafe mode keeps the file in the importer folder instead of writing the
// desired one. A custom DLL is written once per selection, so later changes to it are kept like any other
// third-party file, and clearing the selection only replaces the file while it is still that custom DLL.
func preservesUserFile(
	cfg ImporterConfig,
	previous runtimeManifest,
	custom *customRuntimeDLL,
	name, currentHash string,
) bool {
	if !cfg.Migoto.UnsafeMode || previous.Mode != "" && previous.Mode != cfg.Mode {
		return false
	}
	if previous.UserManaged[name] == "" && currentHash == previous.Files[name] {
		return false
	}
	if name != customDLLName {
		return true
	}
	if custom != nil {
		return previous.Custom == custom.id
	}
	return !previous.deployedCustomDLL(name, currentHash)
}

func (x *XXMI) DeployRuntime(ctx context.Context, key string) ([]string, error) {
	cfg, err := x.GetImporterConfig(ctx, key)
	if err != nil {
		return nil, err
	}
	return x.deployRuntime(ctx, key, cfg, false)
}

// deployRuntime copies the selected runtime into the importer folder.
// repair backs up a corrupt manifest and rebuilds it from the verified cache.
func (x *XXMI) deployRuntime(ctx context.Context, key string, cfg ImporterConfig, repair bool) ([]string, error) {
	cacheRoot, err := xxmiCacheRoot()
	if err != nil {
		return nil, err
	}
	var sourceFolder, sourceID string
	var custom *customRuntimeDLL
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

		id, err := x.customDLLID(ctx, cfg)
		if err != nil {
			return nil, err
		}
		if id != "" {
			_, data, err := readCustomDLL(cacheRoot, id)
			if err != nil {
				return nil, fmt.Errorf("XXMI_CUSTOM_DLL_MISSING: %w", err)
			}
			custom = &customRuntimeDLL{id: id, data: data}
		}
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
	return deployCustomRuntimeFiles(ctx, key, cfg, sourceFolder, sourceID, cacheRoot, repair, x.findProcess, custom)
}

func (x *XXMI) resolveLibsVersion(ctx context.Context, cfg ImporterConfig) (string, error) {
	pin, _, err := x.libsPin(ctx, cfg)
	if err != nil || pin != "" {
		return pin, err
	}
	deployed, _ := deployedLibsVersion(cfg.ImporterFolder)
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
	if pkg == nil || pkg.LatestVersion == nil || strings.TrimSpace(*pkg.LatestVersion) == "" {
		releases, err := x.ListReleases(ctx, "xxmi-libs")
		if err == nil && len(releases) > 0 {
			return selectLibsVersion(releases[0].Version, "", deployed, verifiedCachedLibsVersion), nil
		}
		if deployed != "" {
			return deployed, nil
		}
		if cached := newestCachedPackageVersion("xxmi-libs"); cached != "" {
			return cached, nil
		}
		if err != nil {
			return "", fmt.Errorf("resolve latest XXMI libraries: %w", err)
		}
		return "", errors.New("XXMI libraries latest version is unknown")
	}
	skipped := ""
	if pkg.SkippedVersion != nil {
		skipped = normalizeVersion(*pkg.SkippedVersion)
	}
	return selectLibsVersion(normalizeVersion(*pkg.LatestVersion), skipped, deployed, verifiedCachedLibsVersion), nil
}

func selectLibsVersion(latest, skipped, deployed string, cacheVerified func(string) bool) string {
	if deployed == "" || latest == deployed {
		return latest
	}
	if latest == "" || latest == skipped {
		return deployed
	}
	if semver.IsValid("v"+latest) && semver.IsValid("v"+deployed) &&
		semver.Compare("v"+latest, "v"+deployed) < 0 {
		return deployed
	}
	if !cacheVerified(latest) {
		return deployed
	}
	return latest
}

func verifiedCachedLibsVersion(version string) bool {
	root, err := xxmiCacheRoot()
	return err == nil && verifyXXMILibsCache(filepath.Join(root, "packages", "xxmi-libs", version), version) == nil
}

func newestLegacyRuntime(parent string) (string, error) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", err
	}
	newest := ""
	var newestTime time.Time
	for _, entry := range entries {
		if !entry.IsDir() || strings.Contains(entry.Name(), ".tmp-") || strings.Contains(entry.Name(), ".corrupt-") {
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
	repair bool,
	findProcess func(context.Context, string) (int, error),
) ([]string, error) {
	return deployCustomRuntimeFiles(ctx, key, cfg, sourceFolder, sourceID, cacheRoot, repair, findProcess, nil)
}

// deployCustomRuntimeFiles deploys the runtime with custom in place of the signed d3d11.dll when it is set.
func deployCustomRuntimeFiles(
	ctx context.Context,
	key string,
	cfg ImporterConfig,
	sourceFolder, sourceID, cacheRoot string,
	repair bool,
	findProcess func(context.Context, string) (int, error),
	custom *customRuntimeDLL,
) ([]string, error) {
	root, err := openInstallRoot(cfg.ImporterFolder)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	if _, err := os.Stat(filepath.Join(cfg.ImporterFolder, "d3dx.ini")); err != nil {
		return nil, fmt.Errorf("XXMI_IMPORTER_NOT_INSTALLED: %w", err)
	}
	previous := runtimeManifest{Files: map[string]string{}}
	manifestPresent := false
	var corruptManifest []byte
	if data, _, err := root.readFile(runtimeManifestName); err == nil {
		if err := json.Unmarshal(data, &previous); err != nil {
			if !repair {
				return nil, fmt.Errorf("XXMI_RUNTIME_CORRUPTED: decode runtime manifest: %w", err)
			}
			// Keep the bytes. The verified cache supplies the replacement manifest
			// after this copy is stored with the other runtime backups.
			corruptManifest = data
			previous = runtimeManifest{Files: map[string]string{}}
		} else {
			manifestPresent = true
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
		if custom != nil {
			desired[customDLLName] = custom.data
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
	if !manifestPresent && cfg.Mode == RuntimeXXMI {
		if err := bootstrapExternalRuntime(root, &previous, cfg.DeployedSignatures, spectrumPublicKey); err != nil {
			return nil, err
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
	if custom != nil {
		manifest.Custom = custom.id
	}
	changing, err := runtimeFilesNeedDeployment(root, previous, desired, cfg, custom)
	if err != nil {
		return nil, err
	}
	if changing {
		spec, ok := lookupImporterPackage(key)
		if !ok {
			return nil, fmt.Errorf("unknown importer %q", key)
		}
		if err := waitForGameProcesses(ctx, append(append([]string{}, spec.gameExeNames...), spec.processNames...),
			5*time.Second, findProcess); err != nil {
			return nil, err
		}
	}
	backupFolder := ""
	backup := func(name string, data []byte) error {
		if backupFolder == "" {
			backupRoot := filepath.Join(cacheRoot, "backups")
			if err := os.MkdirAll(backupRoot, 0o700); err != nil {
				return err
			}
			folder, err := os.MkdirTemp(backupRoot, key+" "+time.Now().Format("2006-01-02 15-04-05")+"-")
			if err != nil {
				return err
			}
			backupFolder = folder
		}
		return os.WriteFile(filepath.Join(backupFolder, name), data, 0o600)
	}
	if corruptManifest != nil {
		if err := backup(runtimeManifestName, corruptManifest); err != nil {
			return nil, fmt.Errorf("back up corrupt runtime manifest: %w", err)
		}
		warnings = append(warnings, "Backed up corrupt runtime manifest")
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
			return nil, runtimeFileError(err, filepath.Join(cfg.ImporterFolder, name))
		}
	}
	for name := range previous.UserManaged {
		if _, tracked := previous.Files[name]; tracked {
			continue
		}
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
		if err := backup(name, current); err != nil {
			return nil, err
		}
		if err := root.removeAll(name); err != nil {
			return nil, runtimeFileError(err, filepath.Join(cfg.ImporterFolder, name))
		}
		warnings = append(warnings, "Backed up modified runtime file "+name)
	}
	if cfg.Mode == RuntimeXXMI {
		if current, _, err := root.readFile("nvapi64.dll"); err == nil {
			if err := backup("nvapi64.dll", current); err != nil {
				return nil, err
			}
			if err := root.removeAll("nvapi64.dll"); err != nil {
				return nil, runtimeFileError(err, filepath.Join(cfg.ImporterFolder, "nvapi64.dll"))
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
		// A custom DLL is user-managed even though this deployment wrote it: it is not a signed runtime file.
		deployed := manifest.Files
		if custom != nil && name == customDLLName {
			deployed = manifest.UserManaged
		}
		if err == nil && hashBytes(current) == wantedHash {
			deployed[name] = wantedHash
			continue
		}
		if err == nil && preservesUserFile(cfg, previous, custom, name, hashBytes(current)) {
			warnings = append(warnings, "Preserved third-party runtime file "+name)
			manifest.UserManaged[name] = hashBytes(current)
			continue
		}
		// Custom DLLs are user-managed; their cached copies may be pruned after a selection change.
		if err == nil && hashBytes(current) != previous.Files[name] {
			if err := backup(name, current); err != nil {
				return nil, err
			}
			warnings = append(warnings, "Backed up modified runtime file "+name)
		}
		if err := root.writeFileAtomic(ctx, name, bytes.NewReader(desiredBytes), 0o600, info); err != nil {
			return nil, runtimeFileError(err, filepath.Join(cfg.ImporterFolder, name))
		}
		deployed[name] = wantedHash
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
		return nil, runtimeFileError(err, filepath.Join(cfg.ImporterFolder, runtimeManifestName))
	}
	return warnings, nil
}

func runtimeFileError(err error, path string) error {
	fs := platform.NewFS()
	lock := fs.IsLockedPathError(err, path)
	if !lock.IsLocked {
		return err
	}
	if len(lock.Processes) > 0 {
		return fmt.Errorf("XXMI_RUNTIME_LOCKED: %s is held by %s: %w", path, fs.FormatProcessList(lock.Processes), err)
	}
	return fmt.Errorf("XXMI_RUNTIME_LOCKED: %s: %w", path, err)
}

func bootstrapExternalRuntime(
	root *installRoot,
	manifest *runtimeManifest,
	signatures map[string]string,
	publicKey string,
) error {
	for _, name := range []string{"d3d11.dll", "d3dcompiler_47.dll"} {
		signature := signatures[name]
		if signature == "" {
			continue
		}
		current, _, err := root.readFile(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if verifyPackageSignature(publicKey, signature, current) == nil {
			manifest.Files[name] = hashBytes(current)
		}
	}
	return nil
}

func runtimeFilesNeedDeployment(
	root *installRoot,
	previous runtimeManifest,
	desired map[string][]byte,
	cfg ImporterConfig,
	custom *customRuntimeDLL,
) (bool, error) {
	for name := range previous.Files {
		if _, keep := desired[name]; keep {
			continue
		}
		if _, _, err := root.readFile(name); err == nil {
			return true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	for name := range previous.UserManaged {
		if _, keep := desired[name]; keep {
			continue
		}
		if _, _, err := root.readFile(name); err == nil {
			return true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	if cfg.Mode == RuntimeXXMI {
		if _, _, err := root.readFile("nvapi64.dll"); err == nil {
			return true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	for name, wanted := range desired {
		current, _, err := root.readFile(name)
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		if hashBytes(current) == hashBytes(wanted) ||
			preservesUserFile(cfg, previous, custom, name, hashBytes(current)) {
			continue
		}
		return true, nil
	}
	return false, nil
}

func waitForGameProcesses(
	ctx context.Context,
	names []string,
	deadline time.Duration,
	probe func(context.Context, string) (int, error),
) error {
	limit := time.NewTimer(deadline)
	defer limit.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		running := false
		for _, name := range names {
			pid, err := probe(ctx, name)
			if err != nil {
				return err
			}
			if pid != 0 {
				running = true
				break
			}
		}
		if !running {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-limit.C:
			return errors.New("XXMI_GAME_RUNNING")
		case <-ticker.C:
		}
	}
}

func validateDeployedRuntime(folder string, mode RuntimeMode) error {
	root, err := openInstallRoot(folder)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	data, _, err := root.readFile(runtimeManifestName)
	if err != nil {
		return err
	}
	if len(data) > 1<<20 {
		return errors.New("runtime manifest exceeds size limit")
	}
	var manifest runtimeManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	if manifest.Mode != mode || manifest.Source == "" || len(manifest.Files)+len(manifest.UserManaged) == 0 {
		return errors.New("runtime manifest does not match the selected mode")
	}
	for name, expected := range manifest.Files {
		if filepath.Base(name) != name || expected == "" {
			return fmt.Errorf("invalid runtime manifest entry %q", name)
		}
		data, _, err := root.readFile(name)
		if err != nil {
			return fmt.Errorf("read deployed %s: %w", name, err)
		}
		if hashBytes(data) != expected {
			return fmt.Errorf("deployed %s hash mismatch", name)
		}
	}
	return nil
}

func validateXXMIRuntimeFiles(importerFolder, cacheFolder string, unsafeMode bool) error {
	if unsafeMode {
		return nil
	}
	root, err := openInstallRoot(importerFolder)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	for _, name := range []string{"d3d11.dll", "d3dcompiler_47.dll"} {
		deployed, _, err := root.readFile(name)
		if err != nil {
			return fmt.Errorf("read deployed %s: %w", name, err)
		}
		signed, err := os.ReadFile(filepath.Join(cacheFolder, name))
		if err != nil {
			return fmt.Errorf("read signed %s: %w", name, err)
		}
		if hashBytes(deployed) != hashBytes(signed) {
			return fmt.Errorf("deployed %s differs from signed XXMI libraries", name)
		}
	}
	return nil
}

func validateLegacyRuntimeFiles(importerFolder, cacheFolder string) error {
	data, err := os.ReadFile(filepath.Join(cacheFolder, "source.json"))
	if err != nil {
		return err
	}
	var source LegacyRuntimeSource
	if err := json.Unmarshal(data, &source); err != nil {
		return err
	}
	if err := verifyLegacyRuntimeCache(cacheFolder, source.ZipSHA256); err != nil {
		return err
	}
	root, err := openInstallRoot(importerFolder)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	for name, expected := range source.Files {
		deployed, _, err := root.readFile(name)
		if err != nil {
			return fmt.Errorf("read deployed %s: %w", name, err)
		}
		if hashBytes(deployed) != expected {
			return fmt.Errorf("deployed %s differs from legacy runtime source", name)
		}
	}
	return nil
}

func hashBytes(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
