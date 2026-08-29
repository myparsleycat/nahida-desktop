package app

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func newTray(app *application.App, rt *runtime, icon []byte) *application.SystemTray {
	if app == nil || rt == nil {
		return nil
	}
	tray := app.SystemTray.New()
	if len(icon) > 0 {
		tray.SetIcon(icon)
	}
	tray.SetTooltip("Nahida Desktop")
	menu := app.Menu.New()
	menu.Add("Check for Updates...").OnClick(func(_ *application.Context) {
		if rt.updater == nil {
			return
		}
		go func() {
			if err := rt.updater.CheckForUpdates(context.Background(), true); err != nil && rt.log != nil {
				rt.log.Error(err.Error(), "updater.manualCheck")
			}
		}()
	})
	menu.Add("Setting").OnClick(func(_ *application.Context) {
		if rt.window != nil {
			rt.window.OpenSetting()
		}
	})
	menu.AddSeparator()
	menu.Add("Quit").OnClick(func(_ *application.Context) { app.Quit() })
	tray.SetMenu(menu)
	tray.OnClick(func() {
		if rt.window != nil {
			rt.window.Focus()
		}
	})
	return tray
}
