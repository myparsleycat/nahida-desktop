//go:build windows

package platform

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// The NSIS installer registers this Explorer verb for directories and owns
// its Icon and command values. The app only refreshes the display label so
// it follows the selected app language. The key is never created here:
// portable runs must not grow an installer-provided context menu.
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

// UpdateModelViewerContextMenu rewrites the installer-registered Explorer
// verb label to the app language. It is a no-op when the key is absent
// (portable run). The NSIS entry point enforces per-user installation;
// legacy HKLM values are left untouched and never require elevation.
func UpdateModelViewerContextMenu(language string) error {
	updated, err := updateModelViewerMenu(registry.CURRENT_USER, modelViewerMenuKeyPath, language)
	if err == nil && updated {
		_, _, _ = shellChangeNotify.Call(shcneAssocChanged, 0, 0, 0)
	}
	return err
}

func updateModelViewerMenu(hive registry.Key, path, language string) (bool, error) {
	key, err := registry.OpenKey(hive, path, registry.QUERY_VALUE|registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open model viewer context menu key: %w", err)
	}
	defer func() { _ = key.Close() }()
	if err := key.SetStringValue("", modelViewerMenuLabel(language)); err != nil {
		return false, fmt.Errorf("set model viewer context menu label: %w", err)
	}
	return true, nil
}
