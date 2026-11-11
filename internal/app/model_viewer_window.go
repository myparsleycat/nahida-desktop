package app

import (
	"context"

	"github.com/myparsleycat/fairy"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/ui"
)

// modelViewerWindows keeps one model viewer window per mod folder for --model-viewer launches; the mod list shows
// the viewer in a dialog instead. Its methods run on the UI thread.
type modelViewerWindows struct {
	shell *ui.Shell
	icon  []byte
	font  string
	log   *infra.Log
	// lastClosed runs after the last viewer window closed.
	lastClosed func()
	// windows maps each open viewer window to its mod folder.
	windows map[*fairy.Window]string
}

func newModelViewerWindows(
	shell *ui.Shell,
	icon []byte,
	font string,
	log *infra.Log,
	lastClosed func(),
) *modelViewerWindows {
	return &modelViewerWindows{
		shell: shell, icon: icon, font: font, log: log, lastClosed: lastClosed, windows: map[*fairy.Window]string{},
	}
}

// Open shows the viewer of the mod folder at path, focusing the window that already shows it.
func (m *modelViewerWindows) Open(path string) {
	for window, folder := range m.windows {
		if !platform.SamePathFold(folder, path) {
			continue
		}
		if window.IsMinimized() {
			window.Restore()
		}
		window.Show()
		platform.ForceForegroundWindow(window.HWND())
		return
	}

	viewer := m.shell.NewModelViewer(path)
	dark, background, text := viewer.Colors()
	var window *fairy.Window
	window, err := fairy.NewWindow(fairy.Options{
		Title:      viewer.Title(),
		Width:      1200,
		Height:     800,
		MinWidth:   800,
		MinHeight:  600,
		Dark:       dark,
		Background: background,
		TextColor:  text,
		Font:       m.font,
		Icon:       m.icon,
		View:       viewer.View,
		OnClose: func() bool {
			viewer.Close()
			delete(m.windows, window)
			if len(m.windows) == 0 && m.lastClosed != nil {
				m.lastClosed()
			}
			return true
		},
	})
	if err != nil {
		viewer.Close()
		_ = infra.ReportError(m.log, err, "ModelViewerWindow", infra.Diagnostic{
			Operation: "create", Fields: map[string]any{"modPath": path},
		})
		return
	}
	m.windows[window] = path
	window.Show()
	platform.ForceForegroundWindow(window.HWND())
}

// modelViewerClosed ends a launch that only opened model viewers, unless the app keeps running in the
// background.
func (rt *runtime) modelViewerClosed() {
	if rt.window.native() != nil {
		return
	}
	background, err := rt.setting.GetRunInBackground(context.Background())
	if err != nil {
		_ = infra.ReportError(rt.log, err, "ModelViewerWindow", infra.Diagnostic{
			Operation: "close", Stage: "run-in-background",
		})
		return
	}
	if !background {
		rt.quit()
	}
}
