//go:build windows

package gameplatform

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// FindSteam locates the Steam client through the registry values its installer and the running client write.
func FindSteam() (Steam, bool) {
	for _, source := range []struct {
		root  registry.Key
		path  string
		value string
	}{
		{registry.CURRENT_USER, `Software\Valve\Steam`, "SteamExe"},
		{registry.CURRENT_USER, `Software\Valve\Steam`, "SteamPath"},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Valve\Steam`, "InstallPath"},
		{registry.LOCAL_MACHINE, `SOFTWARE\Valve\Steam`, "InstallPath"},
	} {
		key, err := registry.OpenKey(source.root, source.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		value, _, err := key.GetStringValue(source.value)
		_ = key.Close()
		if err != nil || value == "" {
			continue
		}

		// SteamExe names the executable; the other values name its folder.
		exe := filepath.Clean(filepath.FromSlash(value))
		if source.value != "SteamExe" {
			exe = filepath.Join(exe, "steam.exe")
		}
		if info, err := os.Stat(exe); err == nil && info.Mode().IsRegular() {
			return Steam{Exe: exe}, true
		}
	}
	return Steam{}, false
}

// EpicManifestPath returns the file in which the Epic Games Launcher records installed games.
func EpicManifestPath() string {
	return filepath.Join(os.Getenv("PROGRAMDATA"), "Epic", "UnrealEngineLauncher", "LauncherInstalled.dat")
}
