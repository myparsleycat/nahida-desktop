package platform

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const autostartRunKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// Autostart registers the running executable to start at logon under one value of HKCU\...\Run. The value
// name must stay the same across versions so that disabling removes an entry an earlier version wrote.
type Autostart struct {
	name   string
	subKey string
}

func NewAutostart(name string) *Autostart {
	return &Autostart{name: name, subKey: autostartRunKey}
}

func (a *Autostart) Enable(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("autostart: resolve executable: %w", err)
	}
	parts := []string{syscall.EscapeArg(exe)}
	for _, arg := range args {
		parts = append(parts, syscall.EscapeArg(arg))
	}

	key, _, err := registry.CreateKey(registry.CURRENT_USER, a.subKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("autostart: open registry key: %w", err)
	}
	defer func() { _ = key.Close() }()
	if err := key.SetStringValue(a.name, strings.Join(parts, " ")); err != nil {
		return fmt.Errorf("autostart: write registry value: %w", err)
	}
	return nil
}

func (a *Autostart) Disable() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, a.subKey, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("autostart: open registry key: %w", err)
	}
	defer func() { _ = key.Close() }()
	if err := key.DeleteValue(a.name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("autostart: delete registry value: %w", err)
	}
	return nil
}
