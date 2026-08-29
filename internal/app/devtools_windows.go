//go:build windows && !server

package app

import "github.com/wailsapp/wails/v3/pkg/application"

func openWindowDevTools(window application.Window) {
	if window != nil {
		window.OpenDevTools()
	}
}
