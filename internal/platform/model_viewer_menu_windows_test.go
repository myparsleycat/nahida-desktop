//go:build windows

package platform

import (
	"errors"
	"fmt"
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

	updated, err := updateModelViewerMenu(registry.CURRENT_USER, path, "ko")
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
	if err := r.SetStringValue("", "stale label"); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, suffix := range []string{`\shell\verb`, `\shell`, ``} {
			_ = registry.DeleteKey(registry.CURRENT_USER, root+suffix)
		}
	})

	updated, err := updateModelViewerMenu(registry.CURRENT_USER, path, "ja")
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
