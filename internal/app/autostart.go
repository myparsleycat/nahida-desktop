package app

import "github.com/wailsapp/wails/v3/pkg/application"

const autostartIdentifier = "nahida-desktop"

type autostartController interface {
	EnableWithOptions(application.AutostartOptions) error
	Disable() error
}

func syncAutostart(controller autostartController, enabled bool) error {
	if enabled {
		return controller.EnableWithOptions(application.AutostartOptions{
			Identifier: autostartIdentifier,
			Arguments:  []string{"--hidden"},
		})
	}
	return controller.Disable()
}
