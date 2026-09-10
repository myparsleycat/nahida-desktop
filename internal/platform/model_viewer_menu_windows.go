//go:build windows

package platform

import (
	"errors"
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// The NSIS installer registers this Explorer verb for directories. The app
// also repairs the registration for installations upgraded in place from a
// version that predates the verb. Portable runs must not grow this menu.
const modelViewerMenuKeyPath = `Software\Classes\Directory\shell\nahida.live.ModelViewer`

const shcneAssocChanged = 0x08000000

var shellChangeNotify = windows.NewLazySystemDLL("shell32.dll").NewProc("SHChangeNotify")

var modelViewerMenuLabels = map[string]string{
	"en": "Open in Model Viewer",
	"ja": "モデルビューアーで開く",
	"ko": "모델 뷰어로 보기",
	"zh": "使用模型查看器打开",
}

func modelViewerMenuLabel(language string) string {
	if label, ok := modelViewerMenuLabels[language]; ok {
		return label
	}
	return modelViewerMenuLabels["en"]
}

// UpdateModelViewerContextMenu keeps the installer-registered Explorer verb
// in sync with the app language and repairs it after an in-place update. The
// NSIS entry point enforces per-user installation; legacy HKLM values are left
// untouched and never require elevation.
func UpdateModelViewerContextMenu(language string) error {
	executable := ""
	if Packaged() && nsisInstalled() {
		var err error
		executable, err = executablePath()
		if err != nil {
			return fmt.Errorf("resolve executable for model viewer context menu: %w", err)
		}
	}
	updated, err := updateModelViewerMenu(registry.CURRENT_USER, modelViewerMenuKeyPath, language, executable)
	if err == nil && updated {
		_, _, _ = shellChangeNotify.Call(shcneAssocChanged, 0, 0, 0)
	}
	return err
}

func updateModelViewerMenu(hive registry.Key, path, language, executable string) (bool, error) {
	label := modelViewerMenuLabel(language)
	icon := ""
	command := ""
	if executable != "" {
		executable = filepath.Clean(executable)
		icon = fmt.Sprintf(`"%s",0`, executable)
		command = fmt.Sprintf(`"%s" --model-viewer "%%1"`, executable)
	}
	key, err := registry.OpenKey(hive, path, registry.QUERY_VALUE|registry.SET_VALUE|registry.CREATE_SUB_KEY)
	if errors.Is(err, registry.ErrNotExist) {
		if executable == "" {
			return false, nil
		}
		key, _, err = registry.CreateKey(hive, path, registry.SET_VALUE|registry.CREATE_SUB_KEY)
	}
	if err != nil {
		return false, fmt.Errorf("open or create model viewer context menu key: %w", err)
	}
	defer func() { _ = key.Close() }()
	if modelViewerMenuMatches(key, label, icon, command) {
		return false, nil
	}
	if err := key.SetStringValue("", label); err != nil {
		return false, fmt.Errorf("set model viewer context menu label: %w", err)
	}
	if executable == "" {
		return true, nil
	}
	if err := key.SetStringValue("Icon", icon); err != nil {
		return false, fmt.Errorf("set model viewer context menu icon: %w", err)
	}
	if err := key.SetStringValue("MultiSelectModel", "Single"); err != nil {
		return false, fmt.Errorf("set model viewer context menu selection mode: %w", err)
	}
	commandKey, _, err := registry.CreateKey(key, "command", registry.SET_VALUE)
	if err != nil {
		return false, fmt.Errorf("create model viewer context menu command key: %w", err)
	}
	if err := commandKey.SetStringValue("", command); err != nil {
		_ = commandKey.Close()
		return false, fmt.Errorf("set model viewer context menu command: %w", err)
	}
	if err := commandKey.Close(); err != nil {
		return false, fmt.Errorf("close model viewer context menu command key: %w", err)
	}
	return true, nil
}

// modelViewerMenuMatches reports whether the stored registration already
// matches the values this version would write. Read failures and missing
// values count as mismatches so registration is repaired conservatively.
// icon is empty for portable runs, where only the label is managed.
func modelViewerMenuMatches(key registry.Key, label, icon, command string) bool {
	if !stringValueEquals(key, "", label) {
		return false
	}
	if icon == "" {
		return true
	}
	if !stringValueEquals(key, "Icon", icon) || !stringValueEquals(key, "MultiSelectModel", "Single") {
		return false
	}
	commandKey, err := registry.OpenKey(key, "command", registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer func() { _ = commandKey.Close() }()
	return stringValueEquals(commandKey, "", command)
}
