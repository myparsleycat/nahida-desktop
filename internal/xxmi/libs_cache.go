package xxmi

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nahida.live/desktop/internal/github"
	"nahida.live/desktop/internal/infra"
)

var xxmiLibraryFiles = []string{"3dmloader.dll", "d3d11.dll", "d3dcompiler_47.dll"}

type xxmiLibraryManifest struct {
	Version    string            `json:"version"`
	Signatures map[string]string `json:"signatures"`
}

func (x *XXMI) EnsureLibsVersion(ctx context.Context, version string) error {
	x.packageMu.Lock()
	defer x.packageMu.Unlock()
	return x.ensureLibsVersionLocked(ctx, version)
}

// ensureLibsVersionLocked shares the package lock with importer installation.
func (x *XXMI) ensureLibsVersionLocked(ctx context.Context, version string) error {
	ctx = infra.WithGitHubOperation(ctx, "xxmi-install-libraries")
	version = signedLibsVersion(version)
	if version == "" || version == "." || version == ".." || strings.ContainsAny(version, `\/:*?"<>|`) {
		return errors.New("invalid XXMI libraries version")
	}
	root, err := xxmiCacheRoot()
	if err != nil {
		return err
	}
	parent := filepath.Join(root, "packages", "xxmi-libs")
	destination := filepath.Join(parent, version)
	if info, err := os.Lstat(destination); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("XXMI libraries cache is not a regular directory")
		}
		if verifyXXMILibsCache(destination, version) == nil {
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	releases, err := x.github.AllReleases(ctx, libsRepo)
	if err != nil {
		return err
	}
	var release *github.Release
	for i := range releases {
		if normalizeVersion(releases[i].TagName) == version && !releases[i].Draft {
			release = &releases[i]
			break
		}
	}
	if release == nil {
		return fmt.Errorf("XXMI libraries release %s not found", version)
	}
	signature := releaseSignature(release.Body)
	if signature == "" {
		return fmt.Errorf("XXMI libraries release %s is unsigned", version)
	}
	assetName := "XXMI-PACKAGE-v" + version + ".zip"
	zipURL := releaseAssetURL(*release, assetName)
	if zipURL == "" {
		return fmt.Errorf("XXMI libraries asset %s not found", assetName)
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, version+".tmp-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	zipPath := filepath.Join(staging, "package.zip")
	if err := x.downloadPackageFile(
		ctx,
		"xxmi-libs", version,
		github.FileRequest{Repo: libsRepo, URL: zipURL, Destination: zipPath},
	); err != nil {
		return fmt.Errorf("download XXMI libraries %s: %w", version, err)
	}
	zipInfo, err := os.Stat(zipPath)
	if err != nil {
		return err
	}
	if !zipInfo.Mode().IsRegular() || zipInfo.Size() > 256<<20 {
		return errors.New("XXMI libraries archive is not a regular file or exceeds size limit")
	}
	zipBytes, err := os.ReadFile(zipPath)
	if err != nil {
		return err
	}
	if err := verifyPackageSignature(spectrumPublicKey, signature, zipBytes); err != nil {
		return fmt.Errorf("XXMI libraries %s: %w", version, err)
	}
	manifestURL := releaseAssetURL(*release, "Manifest.json")
	var manifestBytes []byte
	if manifestURL != "" {
		manifestBytes, err = x.github.FetchFile(ctx, libsRepo, manifestURL, 1<<20)
		if err != nil {
			return err
		}
	}
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()
	for _, entry := range reader.File {
		name := filepath.Base(entry.Name)
		if name != entry.Name {
			return fmt.Errorf("unexpected XXMI libraries archive path %q", entry.Name)
		}
		if name == "Manifest.json" && manifestBytes == nil {
			manifestBytes, err = readZipEntry(entry, 1<<20)
			if err != nil {
				return err
			}
		}
		if !containsString(xxmiLibraryFiles, name) {
			continue
		}
		data, err := readZipEntry(entry, 64<<20)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(staging, name), data, 0o600); err != nil {
			return err
		}
	}
	if len(manifestBytes) == 0 {
		return errors.New("XXMI libraries Manifest.json is missing")
	}
	if err := os.WriteFile(filepath.Join(staging, "Manifest.json"), manifestBytes, 0o600); err != nil {
		return err
	}
	if err := reader.Close(); err != nil {
		return err
	}
	if err := os.Remove(zipPath); err != nil {
		return err
	}
	if err := verifyXXMILibsCache(staging, version); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return replaceCorruptLibsCache(root, staging, destination, version)
}

func replaceCorruptLibsCache(root, staging, destination, version string) error {
	backup := ""
	if info, err := os.Lstat(destination); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("XXMI libraries cache is not a regular directory")
		}
		if verifyXXMILibsCache(destination, version) == nil {
			return nil
		}
		backupRoot := filepath.Join(root, "backups")
		if err := os.MkdirAll(backupRoot, 0o700); err != nil {
			return err
		}
		backup = filepath.Join(backupRoot, fmt.Sprintf("xxmi-libs-%s-corrupt-%d", version, time.Now().UnixNano()))
		if err := os.Rename(destination, backup); err != nil {
			return fmt.Errorf("back up corrupt XXMI libraries cache: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(staging, destination); err != nil {
		if backup != "" {
			return errors.Join(err, os.Rename(backup, destination))
		}
		return err
	}
	return nil
}

func verifyXXMILibsCache(folder, version string) error {
	data, err := os.ReadFile(filepath.Join(folder, "Manifest.json"))
	if err != nil {
		return err
	}
	var manifest xxmiLibraryManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("decode XXMI library manifest: %w", err)
	}
	if normalizeVersion(manifest.Version) != version {
		return fmt.Errorf("XXMI library manifest version mismatch: %q", manifest.Version)
	}
	for _, name := range xxmiLibraryFiles {
		data, err := os.ReadFile(filepath.Join(folder, name))
		if err != nil {
			return err
		}
		if err := verifyPackageSignature(spectrumPublicKey, manifest.Signatures[name], data); err != nil {
			return fmt.Errorf("XXMI library %s: %w", name, err)
		}
	}
	return nil
}

func releaseAssetURL(release github.Release, name string) string {
	for _, asset := range release.Assets {
		if asset.Name == name {
			return asset.BrowserDownloadURL
		}
	}
	return ""
}

func readZipEntry(entry *zip.File, maxBytes int64) ([]byte, error) {
	if entry.UncompressedSize64 > uint64(maxBytes) || !entry.Mode().IsRegular() {
		return nil, fmt.Errorf("invalid archive entry %q", entry.Name)
	}
	reader, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("archive entry %q exceeds size limit", entry.Name)
	}
	return data, nil
}

func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
