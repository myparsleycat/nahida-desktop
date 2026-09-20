//go:build windows

package elevated

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nahida.live/desktop/internal/appdata"
)

const (
	// helperExecutableName is the artifact the build task produces and embeds.
	helperExecutableName = "nahida-elevated-helper.exe"
	helperDirName        = "elevated"
	// Extracted helpers are content-addressed, so a version change installs a
	// new file instead of replacing an image a previous helper may still hold
	// open.
	helperFilePrefix = "nahida-elevated-helper-"
	helperFileSuffix = ".exe"
)

// errHelperNotBundled is returned when the running build did not embed a helper
// binary, which is expected for development and test builds.
var errHelperNotBundled = errors.New("elevated helper is not bundled in this build")

// helperBundle carries the prebuilt helper. The build task writes the binary
// into bundle/ before compiling the application, so installed, updated, and
// portable runs all ship the same helper instead of relying on the installer.
//
//go:embed bundle
var helperBundle embed.FS

// defaultHelperDir resolves the per-user directory the embedded helper is
// extracted into. It lives under the shared app data root so every distribution
// channel uses the same path, including portable runs without an installer.
func defaultHelperDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, appdata.RootDirName, helperDirName), nil
}

// bundledHelper returns the embedded helper bytes.
func bundledHelper() ([]byte, error) {
	data, err := helperBundle.ReadFile("bundle/" + helperExecutableName)
	if err != nil {
		return nil, errHelperNotBundled
	}
	if len(data) == 0 {
		return nil, errHelperNotBundled
	}
	return data, nil
}

// helperDigest returns the SHA-256 of data.
func helperDigest(data []byte) [sha256.Size]byte {
	return sha256.Sum256(data)
}

// helperFileName is the content-addressed name of the extracted helper.
func helperFileName(data []byte) string {
	digest := helperDigest(data)
	return helperFilePrefix + hex.EncodeToString(digest[:8]) + helperFileSuffix
}

// writeHelper installs data under dir with a content-addressed name and returns
// its path. Stale versions are removed best effort. Reparse points in the helper
// directory chain are rejected so a same-user process cannot redirect the
// elevated launch to a different binary.
func writeHelper(dir string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", errHelperNotBundled
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create elevated helper directory %q: %w", dir, err)
	}
	if err := rejectHelperReparsePoints(dir); err != nil {
		return "", err
	}

	target := filepath.Join(dir, helperFileName(data))
	if !helperFileMatches(target, data) {
		if err := installHelper(target, dir, data); err != nil {
			return "", err
		}
	}
	removeStaleHelpers(dir, target)
	return target, nil
}

// helperFileMatches reports whether path already holds exactly data.
func helperFileMatches(path string, data []byte) bool {
	current, err := os.ReadFile(path)
	return err == nil && helperDigest(current) == helperDigest(data)
}

// installHelper writes data to target through a temporary file in dir so a
// failed write never leaves a partial executable behind.
func installHelper(target, dir string, data []byte) error {
	temp, err := os.CreateTemp(dir, helperFilePrefix+"*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary elevated helper: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write elevated helper: %w", err)
	}
	if err := temp.Chmod(0o700); err != nil {
		_ = temp.Close()
		return fmt.Errorf("set elevated helper mode: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close elevated helper: %w", err)
	}
	if err := os.Rename(tempPath, target); err != nil {
		return fmt.Errorf("install elevated helper: %w", err)
	}
	return nil
}

// rejectHelperReparsePoints refuses a helper directory chain that resolves
// through a reparse point. A junction or symlink here would let a same-user
// process show the genuine helper to verification while the elevated launch
// resolves the path to a different binary. The user profile above the app data
// root is out of scope because it can be legitimately redirected.
func rejectHelperReparsePoints(dir string) error {
	for _, path := range []string{filepath.Dir(dir), dir} {
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("inspect helper path %q: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("helper path %q is a reparse point", path)
		}
	}
	return nil
}

// removeStaleHelpers deletes older content-addressed helpers, ignoring the one
// in use and any file still locked by a running helper.
func removeStaleHelpers(dir, keep string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, helperFilePrefix) || !strings.HasSuffix(name, helperFileSuffix) {
			continue
		}
		candidate := filepath.Join(dir, name)
		if strings.EqualFold(candidate, keep) {
			continue
		}
		_ = os.Remove(candidate)
	}
}
