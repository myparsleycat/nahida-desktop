//go:build windows

package platform

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

func RegisterNahidaURLProtocol(executable string) error {
	return registerURLProtocol("nahida", "Nahida Desktop deep link", executable)
}

func registerURLProtocol(scheme, description, executable string) error {
	executable = filepath.Clean(executable)
	root, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\`+scheme, registry.SET_VALUE|registry.CREATE_SUB_KEY)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := root.SetStringValue("", description); err != nil {
		return err
	}
	if err := root.SetStringValue("URL Protocol", ""); err != nil {
		return err
	}
	icon, _, err := registry.CreateKey(root, `DefaultIcon`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	if err := icon.SetStringValue("", fmt.Sprintf(`"%s",0`, executable)); err != nil {
		_ = icon.Close()
		return err
	}
	if err := icon.Close(); err != nil {
		return err
	}
	command, _, err := registry.CreateKey(root, `shell\open\command`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	if err := command.SetStringValue("", fmt.Sprintf(`"%s" "%%1"`, executable)); err != nil {
		_ = command.Close()
		return err
	}
	return command.Close()
}
