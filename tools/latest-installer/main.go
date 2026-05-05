package main

import (
	"crypto/sha256"
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
	"syscall"
	"time"
	"unsafe"
)

const (
	githubLatestReleaseURL = "https://api.github.com/repos/myparsleycat/nahida-desktop/releases/latest"
	userAgent              = "NahidaDesktopLatestInstaller/1.1"
	cacheDirName           = "Nahida Desktop\\InstallerCache"
	dialogTitle            = "Nahida Desktop Latest Installer"
)

// Release assets must include a SHA-256 digest before installation can proceed.
const allowMissingDigest = false

var (
	httpClient       = &http.Client{Timeout: 30 * time.Second}
	user32           = syscall.NewLazyDLL("user32.dll")
	messageBoxW      = user32.NewProc("MessageBoxW")
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	getConsoleWindow = kernel32.NewProc("GetConsoleWindow")
	showWindow       = user32.NewProc("ShowWindow")
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
	hideConsoleWindow()

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

	installerPath, cleanup, err := downloadInstaller(asset)
	if err != nil {
		return err
	}

	if err := exec.Command(installerPath).Start(); err != nil {
		cleanup()
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

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request latest release metadata: %w", err)
	}
	defer resp.Body.Close()

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
	for _, asset := range assets {
		if !isSetupInstallerAssetName(asset.Name) {
			continue
		}

		return &asset, nil
	}

	return nil, errors.New("failed to locate a Windows installer asset in the latest release")
}

func isSetupInstallerAssetName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if !strings.HasSuffix(name, ".exe") {
		return false
	}

	normalized := strings.NewReplacer("-", " ", "_", " ").Replace(name)
	return strings.HasPrefix(normalized, "nahida desktop setup ")
}

func downloadInstaller(asset *releaseAsset) (string, func(), error) {
	cacheDir, err := resolveCacheDir()
	if err != nil {
		return "", nil, fmt.Errorf("failed to resolve installer cache directory: %w", err)
	}

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", nil, fmt.Errorf("failed to create installer cache directory: %w", err)
	}

	if err := pruneInstallerCache(cacheDir, asset.Name); err != nil {
		return "", nil, fmt.Errorf("failed to prepare installer cache directory: %w", err)
	}

	finalPath := filepath.Join(cacheDir, filepath.Base(asset.Name))
	if err := verifyExistingInstaller(asset, finalPath); err == nil {
		return finalPath, func() {}, nil
	}

	req, err := http.NewRequest(http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return "", nil, fmt.Errorf("failed to prepare installer download request: %w", err)
	}

	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("failed to download installer %q: %w", asset.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("failed to download installer %q: github returned HTTP %d", asset.Name, resp.StatusCode)
	}

	tempFile, err := os.CreateTemp(cacheDir, "download-*.tmp")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temporary installer file in cache: %w", err)
	}

	tempPath := tempFile.Name()
	cleanup := func() {
		_ = os.Remove(tempPath)
	}

	hasher := sha256.New()
	writer := io.MultiWriter(tempFile, hasher)
	if _, err := io.Copy(writer, resp.Body); err != nil {
		_ = tempFile.Close()
		cleanup()
		return "", nil, fmt.Errorf("failed to write installer %q to disk: %w", asset.Name, err)
	}

	if err := tempFile.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to finalize installer %q: %w", asset.Name, err)
	}

	if err := verifyDigest(asset.Digest, hasher.Sum(nil)); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to verify installer %q: %w", asset.Name, err)
	}

	if err := os.Remove(finalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		cleanup()
		return "", nil, fmt.Errorf("failed to replace cached installer %q: %w", asset.Name, err)
	}

	if err := os.Rename(tempPath, finalPath); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to finalize cached installer %q: %w", asset.Name, err)
	}

	return finalPath, func() {}, nil
}

func verifyDigest(digest string, actual []byte) error {
	expected, ok, err := parseSHA256Digest(digest)
	if err != nil {
		return err
	}

	if !ok {
		if allowMissingDigest {
			return nil
		}

		return errors.New("release asset digest is missing")
	}

	if len(expected) != sha256.Size {
		return errors.New("release asset digest is not a SHA-256 digest")
	}

	if !equalBytes(expected, actual) {
		return fmt.Errorf("digest mismatch: expected %s, got %s", hex.EncodeToString(expected), hex.EncodeToString(actual))
	}

	return nil
}

func parseSHA256Digest(digest string) ([]byte, bool, error) {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return nil, false, nil
	}

	algorithm, value, found := strings.Cut(digest, ":")
	if !found {
		return nil, false, fmt.Errorf("unsupported digest format %q", digest)
	}

	if strings.ToLower(strings.TrimSpace(algorithm)) != "sha256" {
		return nil, false, fmt.Errorf("unsupported digest algorithm %q", algorithm)
	}

	decoded, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, false, fmt.Errorf("invalid SHA-256 digest %q: %w", digest, err)
	}

	return decoded, true, nil
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
	defer file.Close()

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

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if filepath.Base(entry.Name()) == filepath.Base(keepName) {
			continue
		}

		if !isSetupInstallerAssetName(entry.Name()) {
			continue
		}

		path := filepath.Join(cacheDir, entry.Name())
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}

	return nil
}

func equalBytes(a []byte, b []byte) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func hideConsoleWindow() {
	window, _, _ := getConsoleWindow.Call()
	if window == 0 {
		return
	}

	const swHide = 0
	showWindow.Call(window, swHide)
}

func showError(title string, message string) {
	const mbIconError = 0x00000010
	const mbOK = 0x00000000
	showMessageBox(title, message, mbOK|mbIconError)
}

func showMessageBox(title string, message string, style uintptr) uintptr {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	messagePtr, _ := syscall.UTF16PtrFromString(message)
	result, _, _ := messageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(messagePtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		style,
	)

	return result
}
