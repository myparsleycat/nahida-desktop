package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const (
	githubLatestReleaseURL = "https://api.github.com/repos/myparsleycat/nahida-desktop/releases/latest"
	userAgent              = "NahidaDesktopLatestInstaller/1.0"
	tempFilePrefix         = "nahida-desktop-latest-installer-"
)

// Missing digests do not block installation; only present SHA-256 digests are enforced.
const allowMissingDigest = true

var (
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
		showError("Nahida Desktop Latest Installer", err.Error())
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
	defer cleanup()

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

	resp, err := http.DefaultClient.Do(req)
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
	req, err := http.NewRequest(http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return "", nil, fmt.Errorf("failed to prepare installer download request: %w", err)
	}

	req.Header.Set("User-Agent", userAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("failed to download installer %q: %w", asset.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("failed to download installer %q: github returned HTTP %d", asset.Name, resp.StatusCode)
	}

	tempDir := os.TempDir()
	tempFile, err := os.CreateTemp(tempDir, tempFilePrefix+"*.exe")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temporary installer file: %w", err)
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

	finalPath := tempPath
	if renamedPath, err := renameTempFile(tempPath, asset.Name); err == nil {
		finalPath = renamedPath
		cleanup = func() {
			_ = os.Remove(finalPath)
		}
	}

	return finalPath, cleanup, nil
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

func renameTempFile(currentPath string, assetName string) (string, error) {
	baseName := filepath.Base(assetName)
	if baseName == "." || baseName == string(filepath.Separator) || baseName == "" {
		return "", errors.New("invalid asset name")
	}

	finalPath := filepath.Join(filepath.Dir(currentPath), baseName)
	if err := os.Rename(currentPath, finalPath); err != nil {
		return "", err
	}

	return finalPath, nil
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
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	messagePtr, _ := syscall.UTF16PtrFromString(message)
	const mbIconError = 0x00000010
	const mbOK = 0x00000000
	messageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(messagePtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		mbOK|mbIconError,
	)
}
