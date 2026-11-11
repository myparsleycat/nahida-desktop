package app

import "nahida.live/desktop/internal/platform"

// autostartIdentifier is the HKCU Run value name; earlier versions registered under the same name.
const autostartIdentifier = "nahida-desktop"

type autostartController interface {
	Enable(args ...string) error
	Disable() error
}

func syncAutostart(controller autostartController, enabled bool) error {
	return syncAutostartState(controller, enabled, platform.Packaged(), platform.SupportsAutostart())
}

func syncAutostartState(controller autostartController, enabled, packaged, allowed bool) error {
	if !packaged {
		return nil
	}
	if enabled && allowed {
		return controller.Enable("--hidden")
	}
	return controller.Disable()
}
