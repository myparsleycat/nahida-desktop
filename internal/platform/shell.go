package platform

import (
	"net/url"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// AppVersion is overridden for release builds with:
//
//	-ldflags "-X nahida.live/desktop/internal/platform.AppVersion=<version>"
//
// Development builds intentionally keep the next stable line as their default.
var AppVersion = "3.3.0"

// AppStatus is the Electron util:getAppStatus payload.
type AppStatus struct {
	Version           string `json:"version"`
	IsPackaged        bool   `json:"isPackaged"`
	IsDev             bool   `json:"isDev"`
	SupportsAutostart bool   `json:"supportsAutostart"`
	Platform          string `json:"platform"`
}

type Shell struct {
	version   string
	openURL   func(string) error
	openPath  func(string) error
	trashItem func(string) error
}

func NewShell() *Shell {
	return &Shell{
		version:   AppVersion,
		openURL:   defaultOpenURL,
		openPath:  defaultOpenPath,
		trashItem: trashItem,
	}
}

func (s *Shell) GetAppStatus() AppStatus {
	packaged := Packaged()
	return AppStatus{
		Version:           s.version,
		IsPackaged:        packaged,
		IsDev:             !packaged,
		SupportsAutostart: SupportsAutostart(),
		Platform:          nodePlatform(),
	}
}

func (s *Shell) OpenExternal(str string) error {
	if target, ok := parseExternalURL(str); ok {
		if err := s.openURL(target); err == nil {
			return nil
		}
	}
	return s.openPath(str)
}

func (s *Shell) OpenPath(path string) error {
	return s.openPath(path)
}

func (s *Shell) Trash(path string) error {
	return s.trashItem(path)
}

func (s *Shell) CopyStr(str string) error {
	return writeClipboardText(str)
}

func (s *Shell) OpenCmd(path string) error {
	return openCmd(path)
}

func (s *Shell) GetClipboardFiles() []string {
	return clipboardFiles()
}

func parseExternalURL(str string) (string, bool) {
	u, err := url.Parse(str)
	if err != nil || u.Scheme == "" || len(u.Scheme) == 1 {
		return "", false
	}
	return u.String(), true
}

// nodePlatform matches Electron's hardcoded "win32" (see PA-030).
func nodePlatform() string {
	return "win32"
}

func defaultOpenURL(target string) error {
	if app := application.Get(); app != nil {
		return app.Browser.OpenURL(target)
	}
	return openWithHandler(target)
}

func defaultOpenPath(target string) error {
	return openShellPath(target)
}
