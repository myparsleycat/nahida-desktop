package xxmi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"nahida.live/desktop/internal/github"
	"nahida.live/desktop/internal/infra"
)

type ImportVersionMode string

const (
	ImportVersionLatest ImportVersionMode = "latest"
	ImportVersionPinned ImportVersionMode = "pinned"
)

// ImportPackageVersion captures the source and release versions displayed before an import.
type ImportPackageVersion struct {
	Package          string `json:"package"`
	InstalledVersion string `json:"installedVersion"`
	LatestVersion    string `json:"latestVersion"`
	LatestNotes      string `json:"latestNotes"`
	UpdateAvailable  bool   `json:"updateAvailable"`
	CheckFailed      bool   `json:"checkFailed"`
}

// PreviewExternalLauncherImport reads source versions without changing files or settings.
// Failed release checks leave the installed versions available for a pinned import.
func (x *XXMI) PreviewExternalLauncherImport(
	ctx context.Context,
	path string,
) (versions []ImportPackageVersion, returnErr error) {
	ctx = infra.WithGitHubOperation(ctx, "xxmi-preview-import")
	path = strings.TrimSpace(path)
	defer func() {
		if returnErr != nil {
			returnErr = infra.ReportError(x.log, returnErr, "XXMI.previewExternalLauncherImport", infra.Diagnostic{
				Operation: "preview-external-launcher-import", Stage: "read-source",
				Fields: map[string]any{"external_path": path},
			})
		}
	}()
	if err := validateLocalFolder("external launcher", path, true); err != nil {
		return nil, err
	}
	config, parsed, err := readAndValidateConfig(filepath.Join(path, xxmiConfigName))
	if err != nil {
		return nil, err
	}
	versions = []ImportPackageVersion{}
	repositories := map[string]github.Repo{"xxmi-libs": libsRepo}
	for _, key := range []string{"GIMI", "SRMI", "ZZMI", "WWMI", "HIMI", "EFMI"} {
		folder := parsed.Importers[key].Importer.ImporterFolder
		if !filepath.IsAbs(folder) {
			folder = filepath.Join(path, folder)
		}
		pkg := parsed.Packages.Packages[key]
		if pkg.LatestVersion == "" && !fileExists(filepath.Join(folder, "d3dx.ini")) {
			continue
		}
		spec, _ := lookupImporterPackage(key)
		installed := readImporterVersion(folder, spec)
		if installed == nil && pkg.LatestVersion != "" {
			version := normalizeVersion(pkg.LatestVersion)
			installed = &version
		}
		if installed == nil {
			return nil, fmt.Errorf("%w: %s in %q", errImportVersionUnknown, key, folder)
		}
		id := "importer:" + key
		versions = append(versions, ImportPackageVersion{Package: id, InstalledVersion: *installed})
		repositories[id] = spec.repo
	}
	if len(versions) == 0 {
		return versions, nil
	}
	libs := ImportPackageVersion{Package: "xxmi-libs"}
	if installed := dllVersion(&path); installed != nil {
		libs.InstalledVersion = normalizeVersion(*installed)
	}
	versions = append(versions, libs)
	launcher, _ := config["Launcher"].(map[string]any)
	includePrereleases, _ := launcher["pre_release"].(bool)
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for i := range versions {
		version := &versions[i]
		releases, err := x.github.CachedReleases(checkCtx, repositories[version.Package], false)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		for _, release := range releases {
			candidate := normalizeVersion(release.TagName)
			if release.Draft || release.Prerelease && !includePrereleases || !semver.IsValid("v"+candidate) {
				continue
			}
			if version.LatestVersion == "" || semver.Compare("v"+candidate, "v"+version.LatestVersion) > 0 {
				version.LatestVersion = candidate
				version.LatestNotes = releaseNotes(release.Body)
			}
		}
		if err != nil || version.LatestVersion == "" {
			version.CheckFailed = true
			if err == nil {
				err = errors.New("no compatible release found")
			}
			_ = infra.ReportError(x.log, err, "XXMI.previewExternalLauncherImport", infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: "preview-external-launcher-import", Stage: "check-releases",
				Fields: map[string]any{
					"external_path":     path,
					"package":           version.Package,
					"installed_version": version.InstalledVersion,
					"repository":        repositories[version.Package].String(),
				},
			})
			continue
		}
		version.UpdateAvailable = version.InstalledVersion == "" ||
			semver.Compare("v"+version.LatestVersion, "v"+version.InstalledVersion) > 0
	}
	return versions, nil
}

func selectImportVersion(input ImportExternalLauncherInput, pkg, installed string) (string, error) {
	installed = normalizeVersion(installed)
	var snapshot *ImportPackageVersion
	for i := range input.Versions {
		if input.Versions[i].Package != pkg {
			continue
		}
		if snapshot != nil {
			return "", fmt.Errorf("duplicate import package %q", pkg)
		}
		snapshot = &input.Versions[i]
	}
	if snapshot != nil && normalizeVersion(snapshot.InstalledVersion) != installed {
		return "", fmt.Errorf("XXMI_IMPORT_SOURCE_CHANGED: %s", pkg)
	}
	if input.VersionMode != ImportVersionLatest {
		if snapshot != nil && installed == "" {
			return "", fmt.Errorf("%w: %s", errImportVersionUnknown, pkg)
		}
		return installed, nil
	}
	if snapshot == nil || snapshot.CheckFailed || !semver.IsValid("v"+normalizeVersion(snapshot.LatestVersion)) {
		return "", fmt.Errorf("XXMI_IMPORT_PREVIEW_REQUIRED: %s", pkg)
	}
	latest := normalizeVersion(snapshot.LatestVersion)
	if installed != "" && semver.Compare("v"+installed, "v"+latest) >= 0 {
		return installed, nil
	}
	return latest, nil
}

func (x *XXMI) cacheImportLibraries(ctx context.Context, path, installed, selected string) error {
	if installed != "" && selected == installed {
		if err := importExternalLibs(path, installed); err != nil {
			return err
		}
	}
	return x.ensureLibsVersionLocked(ctx, selected)
}

// snapshotImportRuntime preserves a reused folder's runtime when a later importer fails to install.
func snapshotImportRuntime(folder string) (func() error, error) {
	root, err := openInstallRoot(folder)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	files := map[string][]byte{runtimeManifestName: nil, "d3d11.dll": nil, "d3dcompiler_47.dll": nil}
	if data, _, err := root.readFile(runtimeManifestName); err == nil {
		var manifest runtimeManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return nil, err
		}
		for name := range manifest.Files {
			if filepath.Base(name) != name {
				return nil, fmt.Errorf("invalid runtime filename %q", name)
			}
			files[name] = nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for name := range files {
		data, _, err := root.readFile(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		files[name] = append([]byte{}, data...)
	}
	return func() error {
		root, err := openInstallRoot(folder)
		if err != nil {
			return err
		}
		defer func() { _ = root.Close() }()
		var errs []error
		for name, data := range files {
			if data == nil {
				err = root.root.Remove(name)
			} else {
				err = root.writeFileAtomic(context.Background(), name, bytes.NewReader(data), 0o600, nil)
			}
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, fmt.Errorf("restore runtime %s in %q: %w", name, folder, err))
			}
		}
		return errors.Join(errs...)
	}, nil
}
