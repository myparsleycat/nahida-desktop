package app

import "github.com/wailsapp/wails/v3/pkg/application"

func newWindow(_ *application.App, manager *Window) application.Window {
	if manager == nil {
		return nil
	}
	return manager.Create()
}
