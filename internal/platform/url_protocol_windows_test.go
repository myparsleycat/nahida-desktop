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
	if err := registerURLProtocol(scheme, "Nahida test protocol", executable); err != nil {
		t.Fatal(err)
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
