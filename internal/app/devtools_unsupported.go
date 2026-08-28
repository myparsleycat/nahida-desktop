//go:build server || !windows

package app

import "github.com/wailsapp/wails/v3/pkg/application"

func openWindowDevTools(application.Window) {}
