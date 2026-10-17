//go:build windows

package xxmi

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"

	"nahida.live/desktop/internal/platform"
)

// errGameLauncherNotFound stops a direct Arknights: Endfield launch whose GRYPHLINK launcher cannot be found.
// The renderer keys its message off this literal; keep it in sync with use-launch-guard.tsx.
const errGameLauncherNotFound = "XXMI_GAME_LAUNCHER_NOT_FOUND"

// efmiParentExe returns the GRYPHLINK Games.exe a direct Arknights: Endfield launch starts the game under, or ""
// when the launch needs none. Started any other way, the game asks to be launched from the official launcher
// and exits, so like the reference launcher the game process is created with Games.exe as its parent. The
// Steam release has no such check, and only a native start can name a parent.
func (x *XXMI) efmiParentExe(key string, cfg ImporterConfig, gameExe string) (string, error) {
	if key != "EFMI" || cfg.GameLaunch != "Direct" || cfg.ProcessStartMethod != "Native" || gameExe == "" {
		return "", nil
	}
	if slices.ContainsFunc(strings.Split(filepath.Clean(gameExe), `\`), func(part string) bool {
		return strings.EqualFold(part, "steamapps")
	}) {
		return "", nil
	}

	if cfg.EFMI != nil && strings.TrimSpace(cfg.EFMI.LauncherFolder) != "" {
		folder := strings.TrimSpace(cfg.EFMI.LauncherFolder)
		if err := validateLocalFolder("game launcher folder", folder, true); err != nil {
			return "", fmt.Errorf("%s: %w", errGameLauncherNotFound, err)
		}
		exe, err := gryphlinkGamesExe(folder)
		if err != nil {
			return "", fmt.Errorf("%s: %s: %w", errGameLauncherNotFound, folder, err)
		}
		return exe, nil
	}

	// The Epic Games release keeps the launcher in the game folder, and GRYPHLINK installs games to
	// <launcher>\games\<game folder> unless told otherwise.
	gameFolder := filepath.Dir(gameExe)
	candidates := []string{gameFolder, filepath.Dir(filepath.Dir(gameFolder))}
	if x.gryphlinkFolders != nil {
		candidates = append(candidates, x.gryphlinkFolders()...)
	}
	for _, folder := range candidates {
		if exe, err := gryphlinkGamesExe(folder); err == nil {
			return exe, nil
		}
	}
	return "", fmt.Errorf("%s: no GRYPHLINK launcher was found for %s", errGameLauncherNotFound, gameExe)
}

// gryphlinkGamesExe returns the Games.exe of the newest launcher version installed in folder or its parent,
// the one that holds Launcher.exe.
func gryphlinkGamesExe(folder string) (string, error) {
	folder = filepath.Clean(folder)
	if !fileExists(filepath.Join(folder, "Launcher.exe")) {
		folder = filepath.Dir(folder)
		if !fileExists(filepath.Join(folder, "Launcher.exe")) {
			return "", errors.New("no Launcher.exe in the folder or its parent")
		}
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		return "", err
	}

	// Each launcher update installs into a folder named after its version.
	found, newest := "", []int(nil)
	for _, entry := range entries {
		version, ok := dottedVersion(entry.Name())
		exe := filepath.Join(folder, entry.Name(), "Games.exe")
		if !ok || !fileExists(exe) {
			continue
		}
		if found == "" || slices.Compare(version, newest) > 0 {
			found, newest = exe, version
		}
	}
	if found == "" {
		return "", errors.New("no Games.exe in a launcher version folder")
	}
	return found, nil
}

func dottedVersion(name string) ([]int, bool) {
	parts := strings.Split(name, ".")
	version := make([]int, len(parts))
	for i, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return nil, false
		}
		version[i] = number
	}
	return version, true
}

// installedGryphlinkFolders lists the folders the GRYPHLINK launcher is recorded at: its own install path
// entries, its uninstall entries, and its default install locations.
func installedGryphlinkFolders() []string {
	var folders []string
	add := func(folder string) {
		folder = strings.Trim(strings.TrimSpace(folder), `"`)
		if folder != "" && !slices.ContainsFunc(folders, func(other string) bool {
			return platform.SamePathFold(other, folder)
		}) {
			folders = append(folders, folder)
		}
	}

	for _, entry := range registrySubkeys(registry.CURRENT_USER, `Software\GRYPHLINK\Launcher`) {
		if folder, _, err := entry.GetStringValue("install_path"); err == nil {
			add(folder)
		}
		_ = entry.Close()
	}

	names := []string{"gryphlink", "hypergryph launcher", "鹰角启动器"}
	for _, source := range []struct {
		hive registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
	} {
		for _, entry := range registrySubkeys(source.hive, source.path) {
			name, _, err := entry.GetStringValue("DisplayName")
			if err == nil && slices.Contains(names, strings.ToLower(strings.TrimSpace(name))) {
				if location, _, err := entry.GetStringValue("InstallLocation"); err == nil {
					add(location)
				}
			}
			_ = entry.Close()
		}
	}

	for _, folder := range []string{
		`C:\Program Files\GRYPHLINK`,
		`C:\Program Files\Hypergryph Launcher`,
		`C:\Program Files (x86)\GRYPHLINK`,
		`C:\Program Files (x86)\Hypergryph Launcher`,
	} {
		add(folder)
	}
	return folders
}

// registrySubkeys opens every subkey of path for reading; the caller closes them.
func registrySubkeys(hive registry.Key, path string) []registry.Key {
	parent, err := registry.OpenKey(hive, path, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	names, err := parent.ReadSubKeyNames(0)
	_ = parent.Close()
	if err != nil {
		return nil
	}
	keys := make([]registry.Key, 0, len(names))
	for _, name := range names {
		if key, err := registry.OpenKey(hive, path+`\`+name, registry.QUERY_VALUE); err == nil {
			keys = append(keys, key)
		}
	}
	return keys
}
