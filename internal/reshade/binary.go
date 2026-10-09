package reshade

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/infra"
)

const (
	manifestName   = "manifest.json"
	maxSetupSize   = 64 << 20
	maxModuleSize  = 32 << 20
	setupURLFormat = "https://reshade.me/downloads/ReShade_Setup_%s_Addon.exe"
)

type manifest struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

// installedVersions lists the cached versions whose module still matches its manifest, newest first.
func (r *ReShade) installedVersions() ([]string, error) {
	root, err := r.cacheRoot()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	versions := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !supportedVersion(entry.Name()) {
			continue
		}
		if _, err := verifyCache(filepath.Join(root, entry.Name()), entry.Name()); err == nil {
			versions = append(versions, entry.Name())
		}
	}
	sortVersions(versions)
	return versions, nil
}

// ensureVersion returns the cache folder of version, downloading the official setup when it is missing.
func (r *ReShade) ensureVersion(ctx context.Context, version string) (string, error) {
	if !supportedVersion(version) {
		return "", errors.New("RESHADE_VERSION_UNSUPPORTED")
	}
	root, err := r.cacheRoot()
	if err != nil {
		return "", err
	}
	destination := filepath.Join(root, version)
	r.binaryMu.Lock()
	_, cacheErr := verifyCache(destination, version)
	r.binaryMu.Unlock()
	if cacheErr == nil {
		return destination, nil
	}
	err = r.runTransfer(ctx, "ReShade "+version, destination, fmt.Sprintf(setupURLFormat, version),
		func(ctx context.Context, progress func(int64, int64)) error {
			_, err := r.downloadVersion(ctx, version, progress)
			return err
		})
	return destination, err
}

func (r *ReShade) downloadVersion(ctx context.Context, version string, progress func(int64, int64)) (string, error) {
	r.binaryMu.Lock()
	defer r.binaryMu.Unlock()

	root, err := r.cacheRoot()
	if err != nil {
		return "", err
	}
	destination := filepath.Join(root, version)
	if _, err := verifyCache(destination, version); err == nil {
		return destination, nil
	}
	if r.download == nil {
		return "", errors.New("download client is not configured")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	staging, err := os.MkdirTemp(root, version+".tmp-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(staging) }()

	setup := filepath.Join(staging, "setup.exe")
	setupURL := fmt.Sprintf(setupURLFormat, version)
	var downloaded, total int64
	err = r.download.File(ctx, infra.DownloadRequest{
		URL: setupURL, Destination: setup, MaxSize: maxSetupSize,
		OnResponse: func(length int64) {
			downloaded, total = 0, length
			progress(downloaded, total)
			r.emitProgress("binary", version, "download", downloaded, total)
		},
		Progress: func(bytes int64) {
			downloaded += bytes
			progress(downloaded, total)
			r.emitProgress("binary", version, "download", downloaded, total)
		},
	})
	r.emitProgress("binary", version, "done", downloaded, total)
	if err != nil {
		return "", infra.AnnotateError(fmt.Errorf("download ReShade %s: %w", version, err), infra.Diagnostic{
			Stage: "download", Fields: map[string]any{"url": infra.SanitizeLogURL(setupURL)},
		})
	}

	module := filepath.Join(staging, moduleName)
	if err := extractModule(setup, module); err != nil {
		return "", fmt.Errorf("extract ReShade %s: %w", version, err)
	}
	if err := os.Remove(setup); err != nil {
		return "", err
	}
	if err := r.checkModule(module, version); err != nil {
		return "", fmt.Errorf("verify ReShade %s: %w", version, err)
	}
	hash, err := fileSHA256(module)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(manifest{Version: version, SHA256: hash})
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(staging, manifestName), data, 0o600); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// Only a cache that failed verification is left here to replace.
	if err := os.RemoveAll(destination); err != nil {
		return "", err
	}
	if err := os.Rename(staging, destination); err != nil {
		return "", err
	}
	return destination, nil
}

// extractModule copies the 64-bit module out of the archive appended to the setup executable. The
// entry is matched by its exact name, so no archive path reaches the filesystem.
func extractModule(setup, destination string) error {
	reader, err := zip.OpenReader(setup)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()
	for _, entry := range reader.File {
		if entry.Name != moduleName {
			continue
		}
		if entry.UncompressedSize64 > maxModuleSize {
			return errors.New("module exceeds size limit")
		}
		input, err := entry.Open()
		if err != nil {
			return err
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			_ = input.Close()
			return err
		}
		written, copyErr := io.Copy(output, io.LimitReader(input, maxModuleSize+1))
		err = errors.Join(copyErr, input.Close(), output.Close())
		if err == nil && written > maxModuleSize {
			err = errors.New("module exceeds size limit")
		}
		return err
	}
	return fmt.Errorf("setup does not contain %s", moduleName)
}

// checkModule rejects a download that is not the 64-bit ReShade module of the requested version. The
// add-on build is unsigned, so this guards against a wrong or damaged file, not a forged one.
func (r *ReShade) checkModule(path, version string) error {
	file, err := pe.Open(path)
	if err != nil {
		return err
	}
	machine, characteristics := file.Machine, file.Characteristics
	if err := file.Close(); err != nil {
		return err
	}
	if machine != pe.IMAGE_FILE_MACHINE_AMD64 || characteristics&pe.IMAGE_FILE_DLL == 0 {
		return errors.New("module is not a 64-bit DLL")
	}
	found, err := r.fileVersion(path)
	if err != nil {
		return fmt.Errorf("read module version: %w", err)
	}
	if found != version {
		return fmt.Errorf("module version %s does not match", found)
	}
	return nil
}

func moduleFileVersion(path string) (string, error) {
	size, err := windows.GetFileVersionInfoSize(path, nil)
	if err != nil {
		return "", err
	}
	if size == 0 {
		return "", errors.New("module has no version resource")
	}
	block := make([]byte, size)
	if err := windows.GetFileVersionInfo(path, 0, size, unsafe.Pointer(&block[0])); err != nil {
		return "", err
	}
	var info *windows.VS_FIXEDFILEINFO
	var length uint32
	if err := windows.VerQueryValue(unsafe.Pointer(&block[0]), `\`, unsafe.Pointer(&info), &length); err != nil {
		return "", err
	}
	if info == nil || length < uint32(unsafe.Sizeof(*info)) {
		return "", errors.New("module has no fixed version")
	}
	return fmt.Sprintf(
		"%d.%d.%d", info.FileVersionMS>>16, info.FileVersionMS&0xffff, info.FileVersionLS>>16,
	), nil
}

// verifyCache returns the manifest of a cached version whose module still has the recorded hash.
func verifyCache(folder, version string) (manifest, error) {
	data, err := os.ReadFile(filepath.Join(folder, manifestName))
	if err != nil {
		return manifest{}, err
	}
	var cached manifest
	if err := json.Unmarshal(data, &cached); err != nil {
		return manifest{}, fmt.Errorf("decode ReShade manifest: %w", err)
	}
	if cached.Version != version {
		return manifest{}, fmt.Errorf("ReShade manifest version mismatch: %q", cached.Version)
	}
	hash, err := fileSHA256(filepath.Join(folder, moduleName))
	if err != nil {
		return manifest{}, err
	}
	if !strings.EqualFold(hash, cached.SHA256) {
		return manifest{}, errors.New("ReShade module hash mismatch")
	}
	return cached, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
