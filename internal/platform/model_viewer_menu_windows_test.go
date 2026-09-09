//go:build windows

package platform

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

func TestModelViewerMenuLabel(t *testing.T) {
	cases := []struct {
		language string
		want     string
	}{
		{"ko", "모델 뷰어로 보기"},
		{"en", "Open in Model Viewer"},
		{"ja", "モデルビューアーで開く"},
		{"zh", "使用模型查看器打开"},
		{"fr", "Open in Model Viewer"},
		{"", "Open in Model Viewer"},
	}
	for _, tc := range cases {
		if got := modelViewerMenuLabel(tc.language); got != tc.want {
			t.Fatalf("modelViewerMenuLabel(%q) = %q, want %q", tc.language, got, tc.want)
		}
	}
}

func TestUpdateModelViewerMenuSkipsWhenKeyAbsent(t *testing.T) {
	root := fmt.Sprintf(`Software\Classes\nahida-mv-test-%d`, time.Now().UnixNano())
	path := root + `\shell\verb`
	t.Cleanup(func() {
		for _, suffix := range []string{`\shell\verb`, `\shell`, ``} {
			_ = registry.DeleteKey(registry.CURRENT_USER, root+suffix)
		}
	})

	updated, err := updateModelViewerMenu(registry.CURRENT_USER, path, "ko", "")
	if err != nil {
		t.Fatal(err)
	}
	if updated {
		t.Fatal("missing key must not be reported as updated")
	}
	if _, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE); !errors.Is(err, registry.ErrNotExist) {
		t.Fatalf("missing key must stay absent, error = %v", err)
	}
}

func TestUpdateModelViewerMenuRewritesLabel(t *testing.T) {
	root := fmt.Sprintf(`Software\Classes\nahida-mv-test-%d`, time.Now().UnixNano())
	path := root + `\shell\verb`
	r, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE|registry.CREATE_SUB_KEY)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if r != 0 {
			_ = r.Close()
		}
		for _, suffix := range []string{`\shell\verb`, `\shell`, ``} {
			_ = registry.DeleteKey(registry.CURRENT_USER, root+suffix)
		}
	})
	if err := r.SetStringValue("", "stale label"); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = 0

	updated, err := updateModelViewerMenu(registry.CURRENT_USER, path, "ja", "")
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Fatal("existing key must be updated")
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = key.Close() }()
	got, _, err := key.GetStringValue("")
	if err != nil || got != modelViewerMenuLabel("ja") {
		t.Fatalf("label = %q, want %q, error = %v", got, modelViewerMenuLabel("ja"), err)
	}
}

func TestUpdateModelViewerMenuCreatesInstalledRegistration(t *testing.T) {
	root := fmt.Sprintf(`Software\Classes\nahida-mv-test-%d`, time.Now().UnixNano())
	path := root + `\shell\verb`
	t.Cleanup(func() {
		for _, suffix := range []string{`\shell\verb\command`, `\shell\verb`, `\shell`, ``} {
			_ = registry.DeleteKey(registry.CURRENT_USER, root+suffix)
		}
	})
	executable := filepath.Join(t.TempDir(), "Nahida Desktop.exe")

	updated, err := updateModelViewerMenu(registry.CURRENT_USER, path, "ko", executable)
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Fatal("installed registration must be created")
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = key.Close() }()
	for name, want := range map[string]string{
		"":                 modelViewerMenuLabel("ko"),
		"Icon":             fmt.Sprintf(`"%s",0`, filepath.Clean(executable)),
		"MultiSelectModel": "Single",
	} {
		got, _, err := key.GetStringValue(name)
		if err != nil || got != want {
			t.Fatalf("%s = %q, want %q, error = %v", name, got, want, err)
		}
	}
	commandKey, err := registry.OpenKey(registry.CURRENT_USER, path+`\command`, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = commandKey.Close() }()
	command, _, err := commandKey.GetStringValue("")
	wantCommand := fmt.Sprintf(`"%s" --model-viewer "%%1"`, filepath.Clean(executable))
	if err != nil || command != wantCommand {
		t.Fatalf("command = %q, want %q, error = %v", command, wantCommand, err)
	}
}

func TestUpdateModelViewerMenuRepairsInstalledRegistration(t *testing.T) {
	root := fmt.Sprintf(`Software\Classes\nahida-mv-test-%d`, time.Now().UnixNano())
	path := root + `\shell\verb`
	t.Cleanup(func() {
		for _, suffix := range []string{`\shell\verb\command`, `\shell\verb`, `\shell`, ``} {
			_ = registry.DeleteKey(registry.CURRENT_USER, root+suffix)
		}
	})
	key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE|registry.CREATE_SUB_KEY)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"":                 "stale label",
		"Icon":             "stale icon",
		"MultiSelectModel": "stale selection mode",
	} {
		if err := key.SetStringValue(name, value); err != nil {
			_ = key.Close()
			t.Fatal(err)
		}
	}
	if err := key.Close(); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(t.TempDir(), "Nahida Desktop.exe")

	updated, err := updateModelViewerMenu(registry.CURRENT_USER, path, "en", executable)
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Fatal("installed registration must be repaired")
	}
	key, err = registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = key.Close() }()
	for name, want := range map[string]string{
		"":                 modelViewerMenuLabel("en"),
		"Icon":             fmt.Sprintf(`"%s",0`, filepath.Clean(executable)),
		"MultiSelectModel": "Single",
	} {
		got, _, err := key.GetStringValue(name)
		if err != nil || got != want {
			t.Fatalf("%s = %q, want %q, error = %v", name, got, want, err)
		}
	}
	commandKey, err := registry.OpenKey(registry.CURRENT_USER, path+`\command`, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = commandKey.Close() }()
	command, _, err := commandKey.GetStringValue("")
	wantCommand := fmt.Sprintf(`"%s" --model-viewer "%%1"`, filepath.Clean(executable))
	if err != nil || command != wantCommand {
		t.Fatalf("command = %q, want %q, error = %v", command, wantCommand, err)
	}
}
