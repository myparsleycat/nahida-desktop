//go:build windows

package platform

import (
	"errors"
	"fmt"
	"image"
	"slices"
	"unsafe"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/win"
	"golang.org/x/sys/windows"
)

const (
	// progmanSpawnWorker asks Progman to create the WorkerW that draws the wallpaper behind the desktop icons.
	// The undocumented message takes wParam 0xD and lParam 1, as Lively sends it.
	progmanSpawnWorker = 0x052C
	smtoNormal         = 0x0000
	progmanTimeoutMs   = 1000

	qunsRunningD3DFullScreen = 3

	// coveredShare is the part of a monitor's work area a window must hide to pause the wallpaper.
	coveredShare = 0.95
)

var (
	procSendMessageTimeoutW          = modUser32.NewProc("SendMessageTimeoutW")
	procFindWindowExW                = modUser32.NewProc("FindWindowExW")
	procSHQueryUserNotificationState = windows.NewLazySystemDLL("shell32.dll").NewProc("SHQueryUserNotificationState")
)

// desktopClasses are the shell windows that show the desktop rather than cover it.
var desktopClasses = []string{
	"WorkerW", "Progman", "Shell_TrayWnd", "Shell_SecondaryTrayWnd", "NotifyIconOverflowWindow",
	"Windows.UI.Core.CoreWindow", "MultitaskingViewFrame", "XamlExplorerHostIslandWindow", "WindowsDashboard",
	"TopLevelWindowForOverflowXamlIsland", "_cls_desk_",
}

// Monitor is one display in virtual-screen device pixels.
type Monitor struct {
	Device  string
	Bounds  image.Rectangle
	Work    image.Rectangle
	Primary bool
	handle  win.HMONITOR
}

// Monitors returns the attached displays.
func Monitors() ([]Monitor, error) {
	infos, err := win.HDC(0).EnumDisplayMonitors(nil)
	if err != nil {
		return nil, fmt.Errorf("enumerate monitors: %w", err)
	}
	monitors := make([]Monitor, 0, len(infos))
	for _, info := range infos {
		mi, err := info.HMon.GetMonitorInfo()
		if err != nil {
			continue
		}
		monitors = append(monitors, Monitor{
			Device:  mi.SzDevice(),
			Bounds:  rectangle(mi.RcMonitor),
			Work:    rectangle(mi.RcWork),
			Primary: mi.DwFlags&co.MONITORINFOF_PRIMARY != 0,
			handle:  info.HMon,
		})
	}
	if len(monitors) == 0 {
		return nil, errors.New("enumerate monitors: none attached")
	}
	return monitors, nil
}

// DesktopWallpaper is a window placed behind the desktop icons. Its methods that change the window run on the
// thread that owns it.
type DesktopWallpaper struct {
	hwnd    win.HWND
	progman win.HWND
	workerW win.HWND
	defView win.HWND
	// parent is Progman on the raised desktop of Windows 11 24H2 and later, and the WorkerW before it.
	parent win.HWND
	raised bool
}

// AttachDesktopWallpaper moves the top-level window hwnd behind the desktop icons over bounds, given in
// virtual-screen device pixels, and shows it without activating it.
func AttachDesktopWallpaper(hwnd uintptr, bounds image.Rectangle) (*DesktopWallpaper, error) {
	d := &DesktopWallpaper{hwnd: win.HWND(hwnd)}
	if err := d.findLayer(); err != nil {
		return nil, err
	}
	if err := d.attach(); err != nil {
		return nil, err
	}
	if err := d.Place(bounds); err != nil {
		return nil, err
	}
	d.hwnd.ShowWindow(co.SW_SHOWNA)
	return d, nil
}

// Raised reports whether the desktop uses the layered shell view of Windows 11 24H2 and later.
func (d *DesktopWallpaper) Raised() bool { return d.raised }

// findLayer finds Progman, the icon view and the WorkerW, asking Progman to create the WorkerW first.
func (d *DesktopWallpaper) findLayer() error {
	progman, ok := win.FindWindow(win.ClassNameStr("Progman"), "")
	if !ok {
		return errors.New("find desktop: Progman window not found")
	}
	d.progman = progman
	if exStyle, err := progman.ExStyle(); err == nil {
		d.raised = exStyle&co.WS_EX_NOREDIRECTIONBITMAP != 0
	}
	var result uintptr
	_, _, _ = procSendMessageTimeoutW.Call(
		uintptr(progman), progmanSpawnWorker, 0xD, 1, smtoNormal, progmanTimeoutMs, uintptr(unsafe.Pointer(&result)),
	)

	for _, top := range enumerateTopLevel() {
		view := findWindowEx(top, 0, "SHELLDLL_DefView")
		if view == 0 {
			continue
		}
		d.defView = view
		if !d.raised {
			d.workerW = findWindowEx(0, top, "WorkerW")
		}
	}
	if d.raised {
		d.workerW = findWindowEx(progman, 0, "WorkerW")
	}

	switch {
	case d.defView == 0:
		return errors.New("find desktop: SHELLDLL_DefView window not found")
	case d.workerW == 0:
		return fmt.Errorf("find desktop: WorkerW window not found (raised %t)", d.raised)
	}
	d.parent = d.workerW
	if d.raised {
		d.parent = progman
	}
	return nil
}

func (d *DesktopWallpaper) attach() error {
	style, err := d.hwnd.Style()
	if err != nil {
		return fmt.Errorf("read window style: %w", err)
	}
	exStyle, err := d.hwnd.ExStyle()
	if err != nil {
		return fmt.Errorf("read window extended style: %w", err)
	}
	style &^= co.WS_CAPTION | co.WS_THICKFRAME | co.WS_SYSMENU | co.WS_MAXIMIZEBOX | co.WS_MINIMIZEBOX
	exStyle = exStyle&^co.WS_EX_APPWINDOW | co.WS_EX_TOOLWINDOW | co.WS_EX_NOACTIVATE
	if d.raised {
		// The raised desktop draws the icons in a layered view, and only a layered child under it and above the
		// WorkerW shows through. Godot fails to take WS_EX_LAYERED after SetParent, so it is set first.
		style |= co.WS_CHILD
		exStyle |= co.WS_EX_LAYERED
	}
	if _, err := d.hwnd.SetWindowLongPtr(co.GWLP_STYLE, uintptr(style)); err != nil {
		return fmt.Errorf("set window style: %w", err)
	}
	if _, err := d.hwnd.SetWindowLongPtr(co.GWLP_EXSTYLE, uintptr(exStyle)); err != nil {
		return fmt.Errorf("set window extended style: %w", err)
	}
	if d.raised {
		// Microsoft's guidance for the raised desktop: an opaque layered window keeps DirectX presents fast.
		if err := d.hwnd.SetLayeredWindowAttributes(0, 255, co.LWA_ALPHA); err != nil {
			return fmt.Errorf("set layered attributes: %w", err)
		}
	}

	if _, err := d.hwnd.SetParent(d.parent); err != nil {
		return fmt.Errorf("set parent: %w", err)
	}
	if d.raised {
		flags := co.SWP_NOMOVE | co.SWP_NOSIZE | co.SWP_NOACTIVATE
		if err := d.hwnd.SetWindowPos(d.defView, win.POINT{}, win.SIZE{}, flags); err != nil {
			return fmt.Errorf("order below desktop icons: %w", err)
		}
		d.keepWorkerBottom()
	}
	return nil
}

// keepWorkerBottom keeps the WorkerW, which draws the static wallpaper, below the window among Progman's
// children.
func (d *DesktopWallpaper) keepWorkerBottom() {
	first, err := d.progman.GetWindow(co.GW_CHILD)
	if err != nil || first == 0 {
		return
	}
	if last, err := first.GetWindow(co.GW_HWNDLAST); err == nil && last == d.workerW {
		return
	}
	_ = d.workerW.SetWindowPos(win.HWND(1), win.POINT{}, win.SIZE{}, co.SWP_NOMOVE|co.SWP_NOSIZE|co.SWP_NOACTIVATE)
}

// Place moves the window over bounds, given in virtual-screen device pixels.
func (d *DesktopWallpaper) Place(bounds image.Rectangle) error {
	var origin win.POINT
	if err := d.parent.ClientToScreenPt(&origin); err != nil {
		return fmt.Errorf("read desktop origin: %w", err)
	}
	pos := win.POINT{X: int32(bounds.Min.X) - origin.X, Y: int32(bounds.Min.Y) - origin.Y}
	size := win.SIZE{Cx: int32(bounds.Dx()), Cy: int32(bounds.Dy())}
	flags := co.SWP_NOACTIVATE | co.SWP_NOZORDER | co.SWP_FRAMECHANGED
	if err := d.hwnd.SetWindowPos(0, pos, size, flags); err != nil {
		return fmt.Errorf("place wallpaper window: %w", err)
	}
	return nil
}

// Healthy reports whether the window is still behind the desktop icons. Explorer restarts and some display
// changes recreate Progman or the WorkerW, which leaves the window detached. On the raised desktop it also moves
// the WorkerW back below the window when the shell reordered it.
func (d *DesktopWallpaper) Healthy() bool {
	if !d.hwnd.IsWindow() || !d.parent.IsWindow() || !d.workerW.IsWindow() {
		return false
	}
	// GetParent returns the owner of a window without WS_CHILD, which the window lacks off the raised desktop.
	if parent, err := d.hwnd.GetAncestor(co.GA_PARENT); err != nil || parent != d.parent {
		return false
	}
	progman, ok := win.FindWindow(win.ClassNameStr("Progman"), "")
	if !ok || progman != d.progman {
		return false
	}
	if d.raised {
		d.keepWorkerBottom()
	}
	return true
}

// Detach repaints the static wallpaper the window covered. Call it before the window is destroyed.
func (d *DesktopWallpaper) Detach() {
	// Setting the wallpaper again destroys the WorkerW of the raised desktop, which repaints by itself.
	if d.raised {
		return
	}
	_ = win.SystemParametersInfo(co.SPI_SETDESKWALLPAPER, 0, nil, co.SPIF_UPDATEINIFILE)
}

// MonitorCovered reports whether a maximized or exclusive full-screen window, or one hiding nearly all of the
// work area, covers the monitor.
func MonitorCovered(m Monitor) bool {
	// The notification state is system-wide, so the full-screen app is taken to be the foreground window.
	var state uint32
	if hr, _, _ := procSHQueryUserNotificationState.Call(uintptr(unsafe.Pointer(&state))); hr == 0 &&
		state == qunsRunningD3DFullScreen &&
		win.GetForegroundWindow().MonitorFromWindow(co.MONITOR_DEFAULTTONULL) == m.handle {
		return true
	}

	for _, handle := range enumerateTopLevel() {
		if !handle.IsWindowVisible() || handle.IsIconic() || windowCloaked(handle) {
			continue
		}
		exStyle, err := handle.ExStyle()
		if err != nil || exStyle&co.WS_EX_TOOLWINDOW != 0 ||
			exStyle&(co.WS_EX_LAYERED|co.WS_EX_TRANSPARENT) == co.WS_EX_LAYERED|co.WS_EX_TRANSPARENT {
			continue
		}
		if className, err := handle.GetClassName(); err != nil || slices.Contains(desktopClasses, className) {
			continue
		}
		rect, err := handle.GetWindowRect()
		if err != nil {
			continue
		}
		onMonitor := handle.MonitorFromWindow(co.MONITOR_DEFAULTTONULL) == m.handle
		if coversMonitor(rectangle(rect), handle.IsZoomed() && onMonitor, m.Work) {
			return true
		}
	}
	return false
}

// coversMonitor reports whether a window with the given rectangle hides a monitor with the given work area.
func coversMonitor(window image.Rectangle, maximizedOnMonitor bool, work image.Rectangle) bool {
	if maximizedOnMonitor {
		return true
	}
	area := work.Dx() * work.Dy()
	if area <= 0 {
		return false
	}
	hidden := window.Intersect(work)
	return float64(hidden.Dx()*hidden.Dy()) >= coveredShare*float64(area)
}

func findWindowEx(parent, after win.HWND, className string) win.HWND {
	name, err := windows.UTF16PtrFromString(className)
	if err != nil {
		return 0
	}
	handle, _, _ := procFindWindowExW.Call(uintptr(parent), uintptr(after), uintptr(unsafe.Pointer(name)), 0)
	return win.HWND(handle)
}

func rectangle(r win.RECT) image.Rectangle {
	return image.Rect(int(r.Left), int(r.Top), int(r.Right), int(r.Bottom))
}
