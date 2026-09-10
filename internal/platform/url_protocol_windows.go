//go:build windows

package platform

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

func RegisterNahidaURLProtocol(executable string) error {
	_, err := registerURLProtocol("nahida", "Nahida Desktop deep link", executable)
	return err
}

func registerURLProtocol(scheme, description, executable string) (bool, error) {
	executable = filepath.Clean(executable)
	icon := fmt.Sprintf(`"%s",0`, executable)
	command := fmt.Sprintf(`"%s" "%%1"`, executable)
	if urlProtocolRegistered(scheme, description, icon, command) {
		return false, nil
	}
	root, _, err := registry.CreateKey(
		registry.CURRENT_USER,
		`Software\Classes\`+scheme,
		registry.SET_VALUE|registry.CREATE_SUB_KEY,
	)
	if err != nil {
		return false, err
	}
	defer func() { _ = root.Close() }()
	if err := root.SetStringValue("", description); err != nil {
		return false, err
	}
	if err := root.SetStringValue("URL Protocol", ""); err != nil {
		return false, err
	}
	iconKey, _, err := registry.CreateKey(root, `DefaultIcon`, registry.SET_VALUE)
	if err != nil {
		return false, err
	}
	if err := iconKey.SetStringValue("", icon); err != nil {
		_ = iconKey.Close()
		return false, err
	}
	if err := iconKey.Close(); err != nil {
		return false, err
	}
	commandKey, _, err := registry.CreateKey(root, `shell\open\command`, registry.SET_VALUE)
	if err != nil {
		return false, err
	}
	if err := commandKey.SetStringValue("", command); err != nil {
		_ = commandKey.Close()
		return false, err
	}
	return true, commandKey.Close()
}

// urlProtocolRegistered reports whether the stored registration already
// matches the values this version would write. Read failures and missing
// values count as mismatches so registration is repaired conservatively;
// in particular the marker "URL Protocol" value must exist, not just be
// empty.
func urlProtocolRegistered(scheme, description, icon, command string) bool {
	root, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\`+scheme, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer func() { _ = root.Close() }()
	if !stringValueEquals(root, "", description) || !stringValueEquals(root, "URL Protocol", "") {
		return false
	}
	iconKey, err := registry.OpenKey(root, `DefaultIcon`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer func() { _ = iconKey.Close() }()
	if !stringValueEquals(iconKey, "", icon) {
		return false
	}
	commandKey, err := registry.OpenKey(root, `shell\open\command`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer func() { _ = commandKey.Close() }()
	return stringValueEquals(commandKey, "", command)
}

func stringValueEquals(key registry.Key, name, want string) bool {
	got, _, err := key.GetStringValue(name)
	return err == nil && got == want
}
