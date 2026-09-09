//go:build windows

package platform

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

func newInstalledVersionTestKey(t *testing.T) string {
	t.Helper()
	path := fmt.Sprintf(`Software\Classes\nahida-installed-version-test-%d`, time.Now().UnixNano())
	t.Cleanup(func() {
		_ = registry.DeleteKey(registry.CURRENT_USER, path)
	})
	return path
}

func setInstalledVersionTestValue(t *testing.T, path, value string) {
	t.Helper()
	key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE|registry.CREATE_SUB_KEY)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := key.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	if value != "" {
		if err := key.SetStringValue("DisplayVersion", value); err != nil {
			t.Fatal(err)
		}
	}
}

func readInstalledVersionTestValue(t *testing.T, path string) (string, error) {
	t.Helper()
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer func() { _ = key.Close() }()
	value, _, err := key.GetStringValue("DisplayVersion")
	return value, err
}

func TestSyncInstalledVersionSkipsWhenKeyAbsent(t *testing.T) {
	path := newInstalledVersionTestKey(t)

	if err := syncInstalledVersion(registry.CURRENT_USER, path, "3.8.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.OpenKey(
		registry.CURRENT_USER,
		path,
		registry.QUERY_VALUE,
	); !errors.Is(
		err,
		registry.ErrNotExist,
	) {
		t.Fatalf("missing key must stay absent, error = %v", err)
	}
}

func TestSyncInstalledVersionRewritesStaleVersion(t *testing.T) {
	path := newInstalledVersionTestKey(t)
	setInstalledVersionTestValue(t, path, "3.7.0")

	if err := syncInstalledVersion(registry.CURRENT_USER, path, "3.8.0"); err != nil {
		t.Fatal(err)
	}
	got, err := readInstalledVersionTestValue(t, path)
	if err != nil || got != "3.8.0" {
		t.Fatalf("display version = %q, want %q, error = %v", got, "3.8.0", err)
	}
}

func TestSyncInstalledVersionKeepsCurrentVersion(t *testing.T) {
	path := newInstalledVersionTestKey(t)
	setInstalledVersionTestValue(t, path, "3.8.0")

	if err := syncInstalledVersion(registry.CURRENT_USER, path, "3.8.0"); err != nil {
		t.Fatal(err)
	}
	got, err := readInstalledVersionTestValue(t, path)
	if err != nil || got != "3.8.0" {
		t.Fatalf("display version = %q, want %q, error = %v", got, "3.8.0", err)
	}
}

func TestSyncInstalledVersionFillsMissingValue(t *testing.T) {
	path := newInstalledVersionTestKey(t)
	setInstalledVersionTestValue(t, path, "")

	if err := syncInstalledVersion(registry.CURRENT_USER, path, "3.8.0"); err != nil {
		t.Fatal(err)
	}
	got, err := readInstalledVersionTestValue(t, path)
	if err != nil || got != "3.8.0" {
		t.Fatalf("display version = %q, want %q, error = %v", got, "3.8.0", err)
	}
}
