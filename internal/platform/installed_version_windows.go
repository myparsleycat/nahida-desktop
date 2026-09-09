//go:build windows

package platform

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// The NSIS installer owns this key and writes DisplayVersion once, from the
// version baked into the installer. The in-app updater swaps the binary in
// place without re-running the installer, so the value drifts behind the
// running version until this sync repairs it. Keep the path in sync with
// UNINST_KEY_NAME in build/windows/nsis/wails_tools.nsh
// (INFO_COMPANYNAME + INFO_PRODUCTNAME).
const uninstallDisplayVersionKeyPath = `Software\Microsoft\Windows\CurrentVersion\Uninstall\nahida.liveNahida Desktop`

// SyncInstalledVersion repairs the Add/Remove Programs DisplayVersion after
// an in-place updater swap. It is a no-op for portable and dev runs and never
// creates the key: uninstalled copies must not grow an uninstall entry.
// Best effort and safe to call on every startup.
func SyncInstalledVersion() error {
	if !Packaged() || !nsisInstalled() {
		return nil
	}
	return syncInstalledVersion(registry.CURRENT_USER, uninstallDisplayVersionKeyPath, AppVersion)
}

func syncInstalledVersion(hive registry.Key, path, version string) error {
	key, err := registry.OpenKey(hive, path, registry.QUERY_VALUE|registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open uninstall display version key: %w", err)
	}
	defer func() { _ = key.Close() }()
	current, _, err := key.GetStringValue("DisplayVersion")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("read installed display version: %w", err)
	}
	if current == version {
		return nil
	}
	if err := key.SetStringValue("DisplayVersion", version); err != nil {
		return fmt.Errorf("set installed display version: %w", err)
	}
	return nil
}
