package platform

import (
	"net/url"
)

// AppVersion is overridden for release builds with:
//
//	-ldflags "-X nahida.live/desktop/internal/platform.AppVersion=<version>"
//
// Development builds intentionally keep the next stable line as their default.
var AppVersion = "3.41.0"

// AppStatus is the Electron util:getAppStatus payload.
type AppStatus struct {
	Version                string `json:"version"`
	IsPackaged             bool   `json:"isPackaged"`
	IsDev                  bool   `json:"isDev"`
	SupportsAutostart      bool   `json:"supportsAutostart"`
	SupportsWindowBackdrop bool   `json:"supportsWindowBackdrop"`
	Platform               string `json:"platform"`
}

type Shell struct {
	version        string
	openURL        func(string) error
	openPath       func(string) error
	trashItem      func(string) error
	windowBackdrop func() bool
	diagnostic     func(error, string, map[string]any)
}

// ShellOptions supplies the parts of Shell that come from the UI framework, which this package does not
// import so the elevated helper does not link it. OpenURL defaults to the registered protocol handler, and
// a missing WindowBackdrop reports no backdrop support.
type ShellOptions struct {
	OpenURL        func(string) error
	WindowBackdrop func() bool
}

// ClipboardText is one atomic observation of the Windows text clipboard.
type ClipboardText struct {
	Sequence uint32
	Text     string
}

//wails:ignore
func (s *Shell) UseDiagnostic(report func(error, string, map[string]any)) {
	s.diagnostic = report
}

func NewShell(options ShellOptions) *Shell {
	s := &Shell{
		version:        AppVersion,
		openURL:        options.OpenURL,
		openPath:       defaultOpenPath,
		trashItem:      trashItem,
		windowBackdrop: options.WindowBackdrop,
	}
	if s.openURL == nil {
		s.openURL = openWithHandler
	}
	return s
}

func (s *Shell) GetAppStatus() AppStatus {
	packaged := Packaged()
	return AppStatus{
		Version:                s.version,
		IsPackaged:             packaged,
		IsDev:                  !packaged,
		SupportsAutostart:      SupportsAutostart(),
		SupportsWindowBackdrop: s.windowBackdrop != nil && s.windowBackdrop(),
		Platform:               nodePlatform(),
	}
}

func (s *Shell) OpenExternal(str string) error {
	if target, ok := parseExternalURL(str); ok {
		if err := s.openURL(target); err == nil {
			return nil
		} else if s.diagnostic != nil {
			s.diagnostic(err, "open-url-fallback", map[string]any{"fallback": "open-path"})
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

// ReadClipboardText returns Unicode text together with the clipboard sequence
// number used to reject stale hunting results.
//
//wails:ignore
func (s *Shell) ReadClipboardText() (ClipboardText, error) {
	return readClipboardText()
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

func defaultOpenPath(target string) error {
	return openShellPath(target)
}
