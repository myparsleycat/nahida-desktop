package blenderlink

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const blenderRunTimeout = 90 * time.Second

func defaultAddonEnv() addonEnv {
	return addonEnv{
		configRoot: blenderConfigRoot,
		installed:  installedBlenders,
		running:    runningBlenders,
		run:        runBlender,
		portable:   portableBlender,
		image:      processImage,
	}
}

// portableBlender reports a Blender that keeps its configuration beside its executable: in a portable folder
// since 4.2, or in the release folder's config folder before.
func portableBlender(executable string) bool {
	dir := filepath.Dir(executable)
	if info, err := os.Stat(filepath.Join(dir, "portable")); err == nil && info.IsDir() {
		return true
	}
	v, ok := executableVersion(executable)
	if !ok {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, v.String(), "config"))
	return err == nil && info.IsDir()
}

// blenderConfigRoot is the folder holding one user configuration folder per Blender release.
func blenderConfigRoot() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Blender Foundation", "Blender"), nil
}

// installedBlenders finds the executables the Blender installer, Steam or the default install folder left.
func installedBlenders(ctx context.Context) (map[blenderVersion]string, error) {
	var candidates []string
	var errs []error
	for _, root := range []struct {
		key    registry.Key
		access uint32
	}{
		{registry.LOCAL_MACHINE, registry.WOW64_64KEY},
		{registry.LOCAL_MACHINE, registry.WOW64_32KEY},
		{registry.CURRENT_USER, 0},
	} {
		locations, err := uninstallLocations(root.key, root.access)
		if err != nil {
			errs = append(errs, err)
		}
		for _, location := range locations {
			candidates = append(candidates, filepath.Join(location, "blender.exe"))
		}
	}
	if programFiles := os.Getenv("ProgramFiles"); programFiles != "" {
		matches, _ := filepath.Glob(filepath.Join(programFiles, "Blender Foundation", "Blender *", "blender.exe"))
		candidates = append(candidates, matches...)
	}

	found := map[blenderVersion]string{}
	for _, executable := range candidates {
		if err := ctx.Err(); err != nil {
			return found, err
		}
		if v, ok := executableVersion(executable); ok && found[v] == "" {
			found[v] = executable
		}
	}
	return found, errors.Join(errs...)
}

func uninstallLocations(root registry.Key, access uint32) ([]string, error) {
	key, err := registry.OpenKey(root, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`,
		registry.ENUMERATE_SUB_KEYS|access)
	if errors.Is(err, registry.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = key.Close() }()
	names, err := key.ReadSubKeyNames(-1)
	if err != nil {
		return nil, err
	}

	var locations []string
	for _, name := range names {
		sub, err := registry.OpenKey(key, name, registry.QUERY_VALUE|access)
		if err != nil {
			continue
		}
		display, _, _ := sub.GetStringValue("DisplayName")
		location, _, _ := sub.GetStringValue("InstallLocation")
		_ = sub.Close()
		if strings.EqualFold(strings.TrimSpace(display), "Blender") && location != "" {
			locations = append(locations, location)
		}
	}
	return locations, nil
}

// executableVersion reads the release from the folder named after it beside blender.exe, which holds the
// bundled scripts.
func executableVersion(executable string) (blenderVersion, bool) {
	if info, err := os.Stat(executable); err != nil || info.IsDir() {
		return blenderVersion{}, false
	}
	entries, err := os.ReadDir(filepath.Dir(executable))
	if err != nil {
		return blenderVersion{}, false
	}
	for _, entry := range entries {
		v, ok := parseBlenderVersion(entry.Name())
		if !ok || !entry.IsDir() || strings.Count(entry.Name(), ".") != 1 {
			continue
		}
		if info, err := os.Stat(filepath.Join(filepath.Dir(executable), entry.Name(), "scripts")); err == nil &&
			info.IsDir() {
			return v, true
		}
	}
	return blenderVersion{}, false
}

// runningBlenders maps each release with a running blender.exe to that executable.
func runningBlenders(ctx context.Context) (map[blenderVersion]string, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = windows.CloseHandle(snapshot) }()

	found := map[blenderVersion]string{}
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if !strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), "blender.exe") {
			continue
		}
		executable, ok := processImage(entry.ProcessID)
		if !ok {
			continue
		}
		if v, ok := executableVersion(executable); ok {
			found[v] = executable
		}
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, err
	}
	return found, nil
}

func processImage(pid uint32) (string, bool) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", false
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	buffer := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return "", false
	}
	return windows.UTF16ToString(buffer[:size]), true
}

// runBlender runs a Python script in Blender without a window and returns what Blender printed.
func runBlender(ctx context.Context, executable, script string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, blenderRunTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "--background", "--python-exit-code", "1",
		"--python-expr", script)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	output, err := command.CombinedOutput()
	return string(output), err
}
