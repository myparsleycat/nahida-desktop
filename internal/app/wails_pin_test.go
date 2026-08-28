package app

import (
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestWailsReplaceAndCookieAPIArePinned(t *testing.T) {
	_, thisFile, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mod), "replace github.com/wailsapp/wails/v3 => github.com/myparsleycat/wails/v3") {
		t.Fatal("go.mod must pin the nahida Wails fork")
	}
	dirOut, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/wailsapp/wails/v3").Output()
	if err != nil {
		t.Fatal(err)
	}
	wailsDir := strings.TrimSpace(string(dirOut))
	cookieAPI, err := os.ReadFile(filepath.Join(wailsDir, "pkg", "application", "webview_cookie.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(cookieAPI)
	if !strings.Contains(text, "func (w *WebviewWindow) GetCookies") || !strings.Contains(text, "func (w *WebviewWindow) DeleteCookies") {
		t.Fatal("fork is missing the public cookie API")
	}
	if strings.Contains(text, "InvokeSync(func() {\n\t\timpl.getCookies") {
		t.Fatal("GetCookies must not block the UI thread waiting for completion")
	}
	options, err := os.ReadFile(filepath.Join(wailsDir, "pkg", "application", "webview_window_options.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(options), "DisableWailsRuntime bool") {
		t.Fatal("fork is missing DisableWailsRuntime")
	}
	loginSrc, err := os.ReadFile(filepath.Join(root, "internal", "app", "gamebanana_login.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(loginSrc), "DisableWailsRuntime:        true") {
		t.Fatal("login window must disable the Wails runtime")
	}
	_ = application.ErrWebviewCookiesUnsupported
}
