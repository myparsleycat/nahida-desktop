package app

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"time"

	"github.com/myparsleycat/fairy"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/ui"
)

const (
	wallpaperWatchInterval = 500 * time.Millisecond
	// wallpaperRetryDelay spaces attempts to attach again while Explorer restarts, which give up after
	// wallpaperRetryLimit.
	wallpaperRetryDelay = 2 * time.Second
	wallpaperRetryLimit = 30
)

// wallpaperHost shows a model viewer behind the desktop icons and keeps it there. Its methods run on the UI
// thread.
type wallpaperHost struct {
	shell *ui.Shell
	font  string
	log   *infra.Log

	spec    *ui.ViewerWallpaper
	viewer  *ui.ModelViewer
	window  *fairy.Window
	desktop *platform.DesktopWallpaper
	// held is the user's pause, which outlasts reattaching; covered is set while windows cover the monitor.
	held    bool
	covered bool
	// generation identifies the shown wallpaper, so a watch or retry of a replaced one does nothing.
	generation uint64
	stopWatch  context.CancelFunc
	failures   infra.DiagnosticThrottle
}

var _ ui.WallpaperHost = (*wallpaperHost)(nil)

func newWallpaperHost(font string, log *infra.Log) *wallpaperHost {
	return &wallpaperHost{font: font, log: log}
}

// Set shows spec as the wallpaper, playing, and stores it, so the next start shows it again. When it cannot be
// shown, the wallpaper set before comes back. The first wallpaper turns on running in the background, so it stays
// after the main window closes.
func (h *wallpaperHost) Set(spec ui.ViewerWallpaper) error {
	data, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("encode wallpaper: %w", err)
	}

	prev, held := h.spec, h.held
	h.held = false
	if err := h.show(spec); err != nil {
		h.spec, h.held = prev, held
		if prev != nil {
			h.showOrRetry(0)
		}
		return err
	}

	if prev == nil {
		h.shell.SaveSetting(setting.KeyGeneralRunInBackground, true)
	}
	h.shell.SaveSetting(setting.KeyModelViewerWallpaper, string(data))
	return nil
}

// Clear removes the wallpaper and forgets it.
func (h *wallpaperHost) Clear() {
	h.Close()
	h.spec, h.held = nil, false
	h.shell.SaveSetting(setting.KeyModelViewerWallpaper, "")
}

// Active reports whether a wallpaper is set.
func (h *wallpaperHost) Active() bool { return h.spec != nil }

// Shown reports whether the wallpaper's window exists, which it does not while it waits to be shown again.
func (h *wallpaperHost) Shown() bool { return h.window != nil }

// Held reports whether the user paused the wallpaper.
func (h *wallpaperHost) Held() bool { return h.held }

// SetHeld pauses or resumes the wallpaper for the rest of the session.
func (h *wallpaperHost) SetHeld(held bool) {
	h.held = held
	h.applyPause()
}

func (h *wallpaperHost) applyPause() {
	if h.viewer != nil {
		h.viewer.SetPaused(h.held || h.covered)
	}
}

// restore shows the wallpaper stored as data, unless one was set since startup.
func (h *wallpaperHost) restore(data string) {
	if data == "" || h.spec != nil {
		return
	}
	var spec ui.ViewerWallpaper
	if err := json.Unmarshal([]byte(data), &spec); err != nil {
		h.log.Warn(fmt.Sprintf("Stored desktop wallpaper is unreadable and was removed: %v", err), "Wallpaper.restore")
		h.shell.SaveSetting(setting.KeyModelViewerWallpaper, "")
		return
	}
	h.spec = &spec
	h.showOrRetry(0)
}

// showOrRetry shows h.spec, or tries again after a delay while its mod folder is unreachable, as on a drive that
// mounts after sign-in, or Explorer is not ready to take it. The folder is checked off the UI thread, as a check
// of an offline network share blocks until the share times out.
func (h *wallpaperHost) showOrRetry(attempt int) {
	generation, path := h.generation, h.spec.Path
	go func() {
		_, statErr := os.Stat(path)
		fairy.GoQuiet(func() {
			if generation != h.generation || h.spec == nil {
				return
			}
			if statErr != nil {
				// A window Explorer detached would otherwise stay until the folder comes back.
				h.Close()
				if attempt == 0 {
					h.log.Warn(fmt.Sprintf("Desktop wallpaper mod folder is not reachable yet: %v", statErr),
						"Wallpaper.show")
				}
				h.retry(h.generation, attempt+1)
				return
			}
			if err := h.show(*h.spec); err != nil {
				h.retry(h.generation, attempt+1)
			}
		})
	}()
}

// show replaces the wallpaper with a new window that draws spec. A failure is logged before it is returned.
func (h *wallpaperHost) show(spec ui.ViewerWallpaper) error {
	h.Close()
	h.spec = &spec
	generation := h.generation

	stage := "monitor"
	monitor, err := wallpaperMonitor(spec.Monitor)
	if err == nil {
		stage = "window"
		err = h.open(spec, monitor.Bounds)
	}
	if err != nil {
		h.Close()
		h.spec = &spec
		return h.failures.ReportError(h.log, err, "Wallpaper", infra.Diagnostic{
			Operation: "show", Stage: stage, Fields: map[string]any{"modPath": spec.Path, "monitor": spec.Monitor},
		})
	}
	h.failures.Report(h.log, nil, "Wallpaper", infra.Diagnostic{})
	h.log.Info(fmt.Sprintf("Desktop wallpaper shown on %s %v (raised desktop %t): %s", monitor.Device, monitor.Bounds,
		h.desktop.Raised(), spec.Path), "Wallpaper.show")
	h.watch(generation, spec.Monitor, monitor.Bounds)
	return nil
}

func (h *wallpaperHost) open(spec ui.ViewerWallpaper, bounds image.Rectangle) error {
	viewer := h.shell.NewWallpaperViewer(spec)
	h.viewer = viewer
	h.applyPause()
	dark, background, text := viewer.Colors()
	window, err := fairy.NewWindow(fairy.Options{
		Title:       viewer.Title(),
		Width:       640,
		Height:      360,
		Dark:        dark,
		Background:  background,
		TextColor:   text,
		Font:        h.font,
		StartHidden: true,
		Transparent: true,
		View:        viewer.WallpaperView,
	})
	if err != nil {
		return fmt.Errorf("create window: %w", err)
	}
	h.window = window
	desktop, err := platform.AttachDesktopWallpaper(window.HWND(), bounds)
	if err != nil {
		return fmt.Errorf("attach to desktop: %w", err)
	}
	h.desktop = desktop
	return nil
}

// Close takes the wallpaper window down and frees its viewer, keeping the wallpaper set.
func (h *wallpaperHost) Close() {
	h.generation++
	if h.stopWatch != nil {
		h.stopWatch()
		h.stopWatch = nil
	}
	if h.window != nil {
		// Hidden first, so the desktop repaints the area before the static wallpaper is set again.
		h.window.Hide()
		if h.desktop != nil {
			h.desktop.Detach()
		}
		h.window.Close()
	}
	if h.viewer != nil {
		h.viewer.Close()
	}
	h.window, h.viewer, h.desktop = nil, nil, nil
	h.covered = false
}

// watch pauses the wallpaper while windows cover its monitor, follows the monitor's bounds and shows the
// wallpaper again when Explorer detached it.
func (h *wallpaperHost) watch(generation uint64, device string, bounds image.Rectangle) {
	ctx, cancel := context.WithCancel(context.Background())
	h.stopWatch = cancel
	desktop, window := h.desktop, h.window
	go func() {
		ticker := time.NewTicker(wallpaperWatchInterval)
		defer ticker.Stop()
		covered := false
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			if window.HWND() == 0 || !desktop.Healthy() {
				fairy.GoQuiet(func() { h.reattach(generation) })
				return
			}
			monitor, err := wallpaperMonitor(device)
			if err != nil {
				continue
			}
			if monitor.Bounds != bounds {
				bounds = monitor.Bounds
				fairy.GoQuiet(func() { h.place(generation, monitor.Bounds) })
			}
			if now := platform.MonitorCovered(monitor); now != covered {
				covered = now
				fairy.GoQuiet(func() { h.cover(generation, now) })
			}
		}
	}()
}

func (h *wallpaperHost) place(generation uint64, bounds image.Rectangle) {
	if generation != h.generation || h.desktop == nil {
		return
	}
	if err := h.desktop.Place(bounds); err != nil {
		h.failures.Report(h.log, err, "Wallpaper", infra.Diagnostic{
			Operation: "place", Fields: map[string]any{"bounds": bounds.String()},
		})
	}
}

func (h *wallpaperHost) cover(generation uint64, covered bool) {
	if generation == h.generation {
		h.covered = covered
		h.applyPause()
	}
}

// reattach shows the wallpaper again in a new window after Explorer destroyed or replaced the desktop window it
// was attached to.
func (h *wallpaperHost) reattach(generation uint64) {
	if generation != h.generation || h.spec == nil {
		return
	}
	h.log.Info("Desktop wallpaper was detached from the desktop and is shown again", "Wallpaper.watch")
	h.showOrRetry(0)
}

// retry runs attempt of showOrRetry after a delay, unless the wallpaper was replaced or removed meanwhile.
func (h *wallpaperHost) retry(generation uint64, attempt int) {
	if attempt > wallpaperRetryLimit {
		h.log.Warn("Desktop wallpaper could not be shown and stays hidden until the next start", "Wallpaper.retry")
		return
	}
	time.AfterFunc(wallpaperRetryDelay, func() {
		fairy.GoQuiet(func() {
			if generation == h.generation && h.spec != nil {
				h.showOrRetry(attempt)
			}
		})
	})
}

// wallpaperMonitor finds the display named device, or the primary display when it is not attached.
func wallpaperMonitor(device string) (platform.Monitor, error) {
	monitors, err := platform.Monitors()
	if err != nil {
		return platform.Monitor{}, err
	}
	primary := monitors[0]
	for _, m := range monitors {
		if device != "" && m.Device == device {
			return m, nil
		}
		if m.Primary {
			primary = m
		}
	}
	return primary, nil
}

// restoreWallpaper shows the stored wallpaper once startup maintenance finished, as its viewer reads the mod.
func (rt *runtime) restoreWallpaper(host *wallpaperHost) {
	ctx := context.Background()
	if err := rt.startup.wait(ctx); err != nil {
		return
	}
	value, err := rt.setting.Get(ctx, setting.KeyModelViewerWallpaper)
	if err != nil {
		_ = infra.ReportError(rt.log, err, "Wallpaper", infra.Diagnostic{Operation: "restore", Stage: "setting"})
		return
	}
	data, _ := value.(string)
	if data == "" {
		return
	}
	fairy.GoQuiet(func() { host.restore(data) })
}
