package app

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"nahida.live/desktop/internal/platform"
)

const autostartIdentifier = "nahida-desktop"

type autostartController interface {
	EnableWithOptions(application.AutostartOptions) error
	Disable() error
}

func syncAutostart(controller autostartController, enabled bool) error {
	return syncAutostartState(controller, enabled, platform.Packaged(), platform.SupportsAutostart())
}

func syncAutostartState(controller autostartController, enabled, packaged, allowed bool) error {
	if !packaged {
		return nil
	}
	if !allowed {
		enabled = false
	}
	if enabled {
		return controller.EnableWithOptions(application.AutostartOptions{
			Identifier: autostartIdentifier,
			Arguments:  []string{"--hidden"},
		})
	}
	return controller.Disable()
}
