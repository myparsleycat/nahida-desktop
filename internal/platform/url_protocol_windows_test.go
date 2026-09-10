//go:build windows

package platform

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

func TestRegisterURLProtocolWritesPerUserCommand(t *testing.T) {
	scheme := fmt.Sprintf("nahida-test-%d", time.Now().UnixNano())
	keyPath := `Software\Classes\` + scheme
	t.Cleanup(func() {
		for _, suffix := range []string{`\shell\open\command`, `\shell\open`, `\shell`, `\DefaultIcon`, ``} {
			_ = registry.DeleteKey(registry.CURRENT_USER, keyPath+suffix)
		}
	})
	executable := filepath.Join(t.TempDir(), "Nahida Desktop.exe")
	updated, err := registerURLProtocol(scheme, "Nahida test protocol", executable)
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Fatal("fresh registration must be written")
	}

	root, err := registry.OpenKey(registry.CURRENT_USER, keyPath, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	description, _, err := root.GetStringValue("")
	if err != nil || description != "Nahida test protocol" {
		t.Fatalf("description = %q, error = %v", description, err)
	}
	protocol, _, err := root.GetStringValue("URL Protocol")
	if err != nil || protocol != "" {
		t.Fatalf("URL Protocol = %q, error = %v", protocol, err)
	}

	commandKey, err := registry.OpenKey(registry.CURRENT_USER, keyPath+`\shell\open\command`, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = commandKey.Close() }()
	command, _, err := commandKey.GetStringValue("")
	wantCommand := fmt.Sprintf(`"%s" "%%1"`, filepath.Clean(executable))
	if err != nil || command != wantCommand {
		t.Fatalf("command = %q, want %q, error = %v", command, wantCommand, err)
	}
}

func TestRegisterURLProtocolSkipsWhenCurrent(t *testing.T) {
	scheme := fmt.Sprintf("nahida-test-%d", time.Now().UnixNano())
	keyPath := `Software\Classes\` + scheme
	t.Cleanup(func() {
		for _, suffix := range []string{`\shell\open\command`, `\shell\open`, `\shell`, `\DefaultIcon`, ``} {
			_ = registry.DeleteKey(registry.CURRENT_USER, keyPath+suffix)
		}
	})
	executable := filepath.Join(t.TempDir(), "Nahida Desktop.exe")
	if _, err := registerURLProtocol(scheme, "Nahida test protocol", executable); err != nil {
		t.Fatal(err)
	}

	updated, err := registerURLProtocol(scheme, "Nahida test protocol", executable)
	if err != nil {
		t.Fatal(err)
	}
	if updated {
		t.Fatal("matching registration must be skipped")
	}
}

func TestRegisterURLProtocolRepairsStaleCommand(t *testing.T) {
	scheme := fmt.Sprintf("nahida-test-%d", time.Now().UnixNano())
	keyPath := `Software\Classes\` + scheme
	t.Cleanup(func() {
		for _, suffix := range []string{`\shell\open\command`, `\shell\open`, `\shell`, `\DefaultIcon`, ``} {
			_ = registry.DeleteKey(registry.CURRENT_USER, keyPath+suffix)
		}
	})
	commandRoot, _, err := registry.CreateKey(
		registry.CURRENT_USER,
		keyPath+`\shell\open\command`,
		registry.SET_VALUE|registry.CREATE_SUB_KEY,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := commandRoot.SetStringValue("", `"C:\stale\Nahida.exe" "%1"`); err != nil {
		_ = commandRoot.Close()
		t.Fatal(err)
	}
	if err := commandRoot.Close(); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(t.TempDir(), "Nahida Desktop.exe")

	updated, err := registerURLProtocol(scheme, "Nahida test protocol", executable)
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Fatal("stale registration must be repaired")
	}
	commandKey, err := registry.OpenKey(registry.CURRENT_USER, keyPath+`\shell\open\command`, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = commandKey.Close() }()
	command, _, err := commandKey.GetStringValue("")
	wantCommand := fmt.Sprintf(`"%s" "%%1"`, filepath.Clean(executable))
	if err != nil || command != wantCommand {
		t.Fatalf("command = %q, want %q, error = %v", command, wantCommand, err)
	}
}
