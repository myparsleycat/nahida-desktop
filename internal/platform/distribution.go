package platform

import (
	"os"
	"path/filepath"
)

const nsisUninstallerName = "uninstall.exe"

// SupportsAutostart reports whether this process may register to run at Windows
// logon. Only packaged NSIS installs can; portable zips share the production
// binary but do not ship uninstall.exe.
func SupportsAutostart() bool {
	return Packaged() && nsisInstalled()
}

func nsisInstalled() bool {
	dir, err := InstallDir()
	if err != nil {
		return false
	}
	return nsisInstalledFromDir(dir)
}

func nsisInstalledFromDir(dir string) bool {
	if dir == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, nsisUninstallerName))
	return err == nil && !info.IsDir()
}
