package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	githubLatestReleaseURL = "https://api.github.com/repos/myparsleycat/nahida-desktop/releases/latest"
	userAgent              = "NahidaDesktopLatestInstaller/1.1"
	cacheDirName           = "Nahida Desktop\\InstallerCache"
	dialogTitle            = "Nahida Desktop Latest Installer"
	v3InstallerAssetName   = "nahida-desktop-windows-amd64-installer.exe"
	apiTimeout             = 30 * time.Second
	downloadTimeout        = 15 * time.Minute
)

type releaseResponse struct {
	Name    string         `json:"name"`
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
}

func main() {
	if err := run(); err != nil {
		showError(dialogTitle, err.Error())
		os.Exit(1)
	}
}

func run() error {
	release, err := fetchLatestRelease()
	if err != nil {
		return err
	}

	asset, err := selectInstallerAsset(release.Assets)
	if err != nil {
		return err
	}

	installerPath, err := downloadInstaller(asset)
	if err != nil {
		return err
	}

	if err := exec.Command(installerPath).Start(); err != nil {
		return fmt.Errorf("failed to start installer %q: %w", asset.Name, err)
	}

	return nil
}

func fetchLatestRelease() (*releaseResponse, error) {
	req, err := http.NewRequest(http.MethodGet, githubLatestReleaseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare latest release request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := (&http.Client{Timeout: apiTimeout}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request latest release metadata: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to request latest release metadata: github returned HTTP %d", resp.StatusCode)
	}

	var release releaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to decode latest release metadata: %w", err)
	}

	return &release, nil
}

func selectInstallerAsset(assets []releaseAsset) (*releaseAsset, error) {
	var fallback *releaseAsset
	for i := range assets {
		asset := &assets[i]
		if isV3InstallerAssetName(asset.Name) {
			return asset, nil
		}
		if fallback == nil && isV2SetupInstallerAssetName(asset.Name) {
			fallback = asset
		}
	}
	if fallback != nil {
		return fallback, nil
	}

	return nil, errors.New("failed to locate a Windows installer asset in the latest release")
}

func isV3InstallerAssetName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), v3InstallerAssetName)
}

func isV2SetupInstallerAssetName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if !strings.HasSuffix(name, ".exe") {
		return false
	}

	normalized := strings.NewReplacer("-", " ", "_", " ").Replace(name)
	return strings.HasPrefix(normalized, "nahida desktop setup ")
}

func downloadInstaller(asset *releaseAsset) (string, error) {
	cacheDir, err := resolveCacheDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve installer cache directory: %w", err)
	}

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create installer cache directory: %w", err)
	}

	if err := pruneInstallerCache(cacheDir, asset.Name); err != nil {
		return "", fmt.Errorf("failed to prepare installer cache directory: %w", err)
	}

	finalPath := filepath.Join(cacheDir, filepath.Base(asset.Name))
	if err := verifyExistingInstaller(asset, finalPath); err == nil {
		return finalPath, nil
	}

	req, err := http.NewRequest(http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to prepare installer download request: %w", err)
	}

	req.Header.Set("User-Agent", userAgent)

	resp, err := (&http.Client{Timeout: downloadTimeout}).Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download installer %q: %w", asset.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download installer %q: github returned HTTP %d", asset.Name, resp.StatusCode)
	}

	tempFile, err := os.CreateTemp(cacheDir, "download-*.tmp")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary installer file in cache: %w", err)
	}

	tempPath := tempFile.Name()
	removeTemp := func() {
		_ = os.Remove(tempPath)
	}

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tempFile, hasher), resp.Body); err != nil {
		_ = tempFile.Close()
		removeTemp()
		return "", fmt.Errorf("failed to write installer %q to disk: %w", asset.Name, err)
	}

	if err := tempFile.Close(); err != nil {
		removeTemp()
		return "", fmt.Errorf("failed to finalize installer %q: %w", asset.Name, err)
	}

	if err := verifyDigest(asset.Digest, hasher.Sum(nil)); err != nil {
		removeTemp()
		return "", fmt.Errorf("failed to verify installer %q: %w", asset.Name, err)
	}

	if err := os.Remove(finalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		removeTemp()
		return "", fmt.Errorf("failed to replace cached installer %q: %w", asset.Name, err)
	}

	if err := os.Rename(tempPath, finalPath); err != nil {
		removeTemp()
		return "", fmt.Errorf("failed to finalize cached installer %q: %w", asset.Name, err)
	}

	return finalPath, nil
}

func verifyDigest(digest string, actual []byte) error {
	expected, err := parseSHA256Digest(digest)
	if err != nil {
		return err
	}

	if len(actual) != sha256.Size || subtle.ConstantTimeCompare(expected, actual) != 1 {
		return fmt.Errorf("digest mismatch: expected %s, got %s", hex.EncodeToString(expected), hex.EncodeToString(actual))
	}

	return nil
}

func parseSHA256Digest(digest string) ([]byte, error) {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return nil, errors.New("release asset digest is missing")
	}

	algorithm, value, found := strings.Cut(digest, ":")
	if !found {
		return nil, fmt.Errorf("unsupported digest format %q", digest)
	}

	if strings.ToLower(strings.TrimSpace(algorithm)) != "sha256" {
		return nil, fmt.Errorf("unsupported digest algorithm %q", algorithm)
	}

	decoded, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, fmt.Errorf("invalid SHA-256 digest %q: %w", digest, err)
	}

	if len(decoded) != sha256.Size {
		return nil, errors.New("release asset digest is not a SHA-256 digest")
	}

	return decoded, nil
}

func resolveCacheDir() (string, error) {
	baseDir, err := os.UserCacheDir()
	if err != nil {
		localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
		if localAppData == "" {
			return "", err
		}

		baseDir = localAppData
	}

	return filepath.Join(baseDir, cacheDirName), nil
}

func verifyExistingInstaller(asset *releaseAsset, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return err
	}

	return verifyDigest(asset.Digest, hasher.Sum(nil))
}

func pruneInstallerCache(cacheDir string, keepName string) error {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return err
	}

	keep := filepath.Base(keepName)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := filepath.Base(entry.Name())
		if name == keep {
			continue
		}
		if !isV3InstallerAssetName(name) && !isV2SetupInstallerAssetName(name) {
			continue
		}

		path := filepath.Join(cacheDir, name)
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}

	return nil
}
