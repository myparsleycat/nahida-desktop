package ui

import (
	"errors"
	"fmt"
	"math"
	"runtime/debug"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"
	"github.com/myparsleycat/fairy/svg"

	"nahida.live/desktop/internal/agent"
	"nahida.live/desktop/internal/infra"
)

const (
	titleBarHeight    float32 = 36
	navWidth          float32 = 52
	navButtonSize     float32 = 36
	windowButtonWidth float32 = 46
)

var (
	gameBananaYellow = fairy.Hex(0xeab308)
	closeHover       = fairy.Hex(0xe81123)
)

// Closed releases what the main window held when it closes; a later window starts afresh.
func (s *Shell) Closed() {
	s.modVideos.closeAll()
	s.driveVideos.closeAll()
	s.drive.videos.cancelAll()
}

// noteDeviceGeneration logs when fairy replaced a lost graphics device. Images and textures are uploaded again on
// the new device, which fairy itself does not log unless the new device is a software one.
func (s *Shell) noteDeviceGeneration() {
	gen := fairy.DeviceGeneration()
	if gen == 0 || gen == s.deviceGen {
		return
	}
	if s.deviceGen != 0 {
		s.deps.Log.Warn(map[string]any{
			"operation": "graphics-device", "stage": "recreated", "generation": gen, "previousGeneration": s.deviceGen,
		}, "Fairy")
	}
	s.deviceGen = gen
}

// View builds the main window.
func (s *Shell) View(cx *fairy.Cx) *fairy.El {
	dark := s.Dark()
	t := Theme(dark, s.blur, s.font)
	if dark != s.dark {
		s.dark = dark
		cx.Window().SetColors(dark, t.Background, t.Foreground)
	}
	kit.Use(t)
	s.win = cx.Window()
	s.router.Record(s.location())
	s.modVideos.sweep()
	s.driveVideos.sweep()
	s.drive.videos.sweep()
	s.noteDeviceGeneration()

	body := fairy.Row().WFull().Grow(1).MinH(0)
	// The sign-in screen stands alone, as the React root left the sidebar out on /auth.
	if s.router.Current() != RouteAuth {
		body.Child(s.nav())
	}
	root := fairy.Col(
		s.titleBar(),
		body.Child(s.page(cx)),
	).WFull().HFull().Color(t.Foreground)
	if cx.Window().Backdrop() == fairy.BackdropNone {
		root.Fill(t.Background)
	}
	s.offerUpdateOnRouteChange()
	s.showPathPrompt(cx)
	s.showConfirm(cx)
	cx.Overlay(s.toasts.View(), nil)
	// The window buttons are the last overlay so they stay above dialogs and their backdrops, as the React title
	// bar raised them above modal layers.
	cx.Overlay(fairy.Box(s.windowControls(cx.Window()).Top(0).Right(0)).Key("ui.window-controls").WFull().HFull().
		PassThrough().Role(fairy.RoleNone), nil)
	return root
}

// showWindowDialog shows panel as a dialog that fills most of the window below the title bar, as React's
// full-size dialogs did with h-full max-h-[92vh] min-w-[95vw] under the title bar.
func showWindowDialog(cx *fairy.Cx, key string, panel *fairy.El, onClose func()) {
	t := kit.T()
	w, h := cx.Size()
	panel.W(w * 0.95).H(max(min(h*0.92, h-titleBarHeight-32), 1)).OnClick(func() {})
	cx.Overlay(fairy.Box(panel).Key(key).WFull().HFull().Center().PadEdges(titleBarHeight, 0, 0, 0).Fill(t.Scrim).
		Enter(fairy.Fade()).OnClick(onClose), onClose)
}

func (s *Shell) titleBar() *fairy.El {
	t := kit.T()
	bar := fairy.Row().Key("ui.titlebar").WFull().H(titleBarHeight).Shrink(0).PadEdges(0, 0, 0, 8).Gap(6).
		Align(fairy.Center).DragRegion().WindowChrome()
	if s.backendDown() {
		bar.Child(fairy.Row(
			fairy.Icon(lucide.ServerOff).Size(12, 12),
			fairy.Text(T("titlebar.server_inactive_badge")).NoWrap(),
		).H(20).PadXY(6, 0).Gap(4).Align(fairy.Center).Radius(6).Fill(t.Muted).Border(1, t.Border).
			FontSize(11).Medium().Color(t.MutedForeground).Tooltip(T("titlebar.server_inactive_tooltip")))
	}
	if badges := s.activityBadges(); badges != nil {
		bar.Child(badges.WindowChromeExclude())
	}
	bar.Child(fairy.Spacer())
	if action := s.updateAction(); action != nil {
		bar.Child(action.MarginEdges(0, 8, 0, 0).WindowChromeExclude())
	}
	// The window buttons are drawn by windowControls in an overlay; the title bar keeps their place.
	return bar.Child(fairy.Box().W(3 * windowButtonWidth).HFull().Shrink(0))
}

func (s *Shell) windowControls(w *fairy.Window) *fairy.El {
	t := kit.T()
	control := func(icon *svg.Vector, size float32, label string, onClick func()) *fairy.El {
		return fairy.Box(fairy.Icon(icon).Size(size, size)).
			W(windowButtonWidth).
			HFull().
			Center().
			Color(t.Foreground.Alpha(0.8)).
			HoverFill(fairy.RGBA(0, 0, 0, 0.1)).
			Label(label).
			OnClick(onClick)
	}
	maximize, maximizeLabel := lucide.Square, T("titlebar.controls.maximise")
	if w.IsMaximized() {
		maximize, maximizeLabel = lucide.Copy, T("titlebar.controls.restore")
	}
	minimize := control(lucide.Minus, 14, T("titlebar.controls.minimise"), w.Minimize)
	maximizeButton := control(maximize, 12, maximizeLabel, w.ToggleMaximize).MaximizeButton()
	if t.Dark {
		minimize.HoverFill(fairy.RGBA(255, 255, 255, 0.1))
		maximizeButton.HoverFill(fairy.RGBA(255, 255, 255, 0.1))
	}
	return fairy.Row(
		minimize,
		maximizeButton,
		control(lucide.X, 14, T("titlebar.controls.close"), w.Close).
			HoverFill(closeHover).HoverColor(fairy.Hex(0xffffff)),
	).H(titleBarHeight).WindowChrome()
}

func (s *Shell) backendDown() bool {
	return s.backend == infra.BackendOffline || s.backend == infra.BackendMaintenance
}

// updateAction is the title bar's update state: download progress, a download offer when updates only notify or
// an automatic download needs the user, or the way back to a downloaded update.
func (s *Shell) updateAction() *fairy.El {
	t := kit.T()
	status := s.updater
	switch {
	case status.IsDownloading:
		label := T("updater.titlebar.downloading")
		if status.DownloadTotal > 0 {
			label = T("updater.titlebar.downloadingProgress",
				"percent", int(math.Round(float64(status.DownloadWritten)/float64(status.DownloadTotal)*100)))
		}
		return fairy.Row(fairy.Spinner().Size(12, 12), fairy.Text(label).NoWrap()).
			H(24).PadXY(8, 0).Gap(6).Align(fairy.Center).FontSize(11.5).Medium().Color(t.MutedForeground)
	case status.UpdateDownloaded:
		return kit.Button(T("updater.titlebar.action"), s.promptUpdate, kit.Ghost, kit.XS,
			kit.Icon(lucide.Download)).FontSize(11.5)
	case status.UpdateAvailable && (status.ShouldPromptForUpdate || status.Mode == "notify"):
		return kit.Button(T("updater.titlebar.downloadAction"), s.downloadUpdate, kit.Ghost, kit.XS,
			kit.Icon(lucide.Download), kit.Loading(s.updateBusy)).FontSize(11.5)
	}
	return nil
}

func (s *Shell) nav() *fairy.El {
	t := kit.T()
	separator := func() *fairy.El { return fairy.Box().W(navButtonSize).H(1).Fill(t.Border) }

	items := []*fairy.El{
		s.transferButton(),
		s.navButton(RouteGameBanana, lucide.Banana, "GameBanana", gameBananaYellow, false),
		separator(),
		s.navButton(RouteMod, lucide.Gamepad, T("page.mod.title"), t.Accent, false),
		s.navButton(RouteTools, lucide.Wrench, routeTitle(RouteTools), t.Accent, false),
	}
	if s.agentAvailable {
		items = append(items, s.navButton(RouteAgent, lucide.Sparkles, T("page.agent.title"), t.Accent, false))
	}
	items = append(items, s.navButton(RouteXXMI, lucide.Package, "XXMI", t.Accent, false), separator())
	if s.signedIn() {
		down := s.backendDown()
		drive, share := T("page.drive.title"), T("page.share_drive.title")
		if down {
			drive, share = T("page.drive.title_server_error"), T("page.share_drive.title_server_error")
		}
		items = append(items,
			s.navButton(RouteDrive, lucide.HardDrive, drive, t.Accent, down),
			s.navButton(RouteShare, lucide.Share2, share, t.Accent, down),
			s.navButton(RouteBackup, lucide.DatabaseBackup, T("page.backup.title"), t.Accent, false),
			separator(),
		)
	}
	items = append(items, s.navButton(RouteSetting, lucide.Settings, T("page.setting.title"), t.Accent, false))
	if s.deps.Dev {
		items = append(items, s.navButton(RouteDevelopment, lucide.BugPlay, "Development", t.Accent, false))
	}
	// The bottom padding centers the buttons on the whole window, title bar included, as the React rail did.
	return fairy.Col(fairy.Col(items...).Gap(8).Align(fairy.Center)).Key("ui.nav").W(navWidth).HFull().Shrink(0).
		PadEdges(0, 0, titleBarHeight, 0).Justify(fairy.JustifyCenter).Align(fairy.Center).Role(fairy.RoleNone)
}

// navButton opens route. dimmed marks a screen whose server is unreachable; it still opens.
func (s *Shell) navButton(route Route, icon *svg.Vector, label string, active fairy.Color, dimmed bool) *fairy.El {
	t := kit.T()
	selected := s.router.Current() == route
	ink := t.Foreground
	if selected {
		ink = active
	}
	glyph := fairy.Icon(icon).Size(28, 28)
	if route == RouteTransfer {
		scale := float32(1)
		if s.transfer != nil {
			scale = 0.9
		}
		glyph.Scale(scale).ScaleMotion(motion.Bezier(0.3, 0.4, 0, 0.2, 1))
	}
	b := fairy.Box(glyph).Key(string(route)).Size(navButtonSize, navButtonSize).Center().
		Radius(t.RadiusMd()).Color(ink).HoverFill(hoverFill(t)).Animate(motion.Fast).Cursor(fairy.CursorPointer).
		Tooltip(label).Label(label).Selected(selected).OnClick(func() {
		// Opening GameBanana from the mod screen shows the game the mod screen shows, as the React sidebar did.
		if route == RouteGameBanana && s.router.Current() == RouteMod {
			s.gameBanana.syncFromMod = true
		}
		// The agent button opens the latest global conversation, as the React sidebar did.
		if route == RouteAgent {
			s.openAgentScope(agent.AgentScope{Type: "global"})
			return
		}
		// The settings button opens the general section, as the React sidebar linked to /setting/gen.
		if route == RouteSetting && s.router.Current() != RouteSetting {
			s.openSettings(sectionGeneral)
		}
		// The tools button opens the dashboard, as the React sidebar linked to /tools. Page tools such as the
		// menu maker have no tool list or breadcrumb, so this is their way back.
		if route == RouteTools && s.router.Current() == RouteTools {
			s.openTool("")
		}
		s.navigate(route)
	})
	// A double click opens the top folder, as the React sidebar did.
	if route == RouteDrive || route == RouteShare {
		b.OnDoubleClick(func() { s.openDriveFolder(s.driveView(route), "", "") })
	}
	if dimmed {
		b.Opacity(0.5)
	}
	return b
}

// transferButton rings the transfer icon with the progress of the open transfers.
func (s *Shell) transferButton() *fairy.El {
	t := kit.T()
	label := T("page.transfer.title")
	// The button keeps its place in the tree whether or not the ring shows, so its icon scale can animate.
	slot := fairy.Box().Key("ui.nav.transfer").Size(navButtonSize, navButtonSize)
	if s.transfer != nil {
		value := min(max(s.transfer.Value/100, 0), 1)
		label = fmt.Sprintf("%s %d%%", label, int(math.Round(value*100)))
		slot.Child(kit.ProgressCircle(value, navButtonSize).Key("ring").Color(t.Primary).Absolute().
			Role(fairy.RoleHidden))
	}
	return slot.Child(s.navButton(RouteTransfer, lucide.ArrowUpDown, label, t.Accent, false))
}

func (s *Shell) page(cx *fairy.Cx) *fairy.El {
	t := kit.T()
	route := s.router.Current()
	s.hideDrives(route)
	if route != RouteMod {
		s.modVideos.closeAll()
	}
	if route != RouteDrive && route != RouteShare {
		s.driveVideos.closeAll()
		s.drive.videos.cancelAll()
	}
	if route != RouteBackup {
		s.backup.active = false
	}
	if route != RouteAgent {
		s.agent.active, s.agent.meterOpen = false, false
	}
	body := s.guardedPage(cx, route)
	background := t.Background.Alpha(0.7)
	switch route {
	case RouteMod, RouteAgent, RouteSetting, RouteBackup, RouteTools:
		background = fairy.Transparent
	case RouteXXMI:
		if s.xxmi.overview.loaded && !s.xxmiExternal() {
			background = fairy.Transparent
		}
	case RouteTransfer, RouteGameBanana, RouteDrive, RouteShare, RouteDevelopment, RouteAuth:
	}
	return fairy.Col(body.Key(string(route))).Key("ui.page").Grow(1).MinW(0).HFull().Fill(background).
		BorderEdges(1, 0, 0, 1, t.Border).Radii(t.RadiusXl(), 0, 0, 0).Clip()
}

// pageFailure is a screen whose build panicked. It stays on the error screen until the user retries or leaves,
// as rebuilding it would panic again on every frame.
type pageFailure struct {
	route   Route
	message string
}

// guardedPage builds the screen of route and shows the error screen instead when the build panics, as the
// React root's error boundary did.
func (s *Shell) guardedPage(cx *fairy.Cx, route Route) (body *fairy.El) {
	if s.pageFailure != nil && s.pageFailure.route != route {
		s.pageFailure = nil
	}
	if s.pageFailure != nil {
		return s.errorPage()
	}
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		message := fmt.Sprint(r)
		err, ok := r.(error)
		if !ok {
			err = errors.New(message)
		}
		_ = infra.ReportError(s.deps.Log, err, "Shell.page", infra.Diagnostic{
			Operation: "render-page", Stage: string(route),
			Fields: map[string]any{"stack": string(debug.Stack())},
		})
		s.pageFailure = &pageFailure{route: route, message: message}
		body = s.errorPage()
	}()
	return s.routePage(cx, route)
}

func (s *Shell) errorPage() *fairy.El {
	t := kit.T()
	failure := s.pageFailure
	content := fairy.Col(
		fairy.Row(
			fairy.Icon(lucide.TriangleAlert).Size(16, 16).Color(t.Destructive).Shrink(0),
			fairy.Col(
				fairy.Text(T("page.root.error.title")).Medium().Color(t.Destructive),
				fairy.Text(T("page.root.error.description")).FontSize(13).Color(t.Destructive.Alpha(0.9)).Wrap(),
			).Gap(2).Grow(1).MinW(0),
		).Gap(12).PadXY(16, 12).Radius(t.RadiusLg()).Border(1, t.Border),
	).WFull().MaxW(448).Gap(16)
	if failure.message != "" {
		content.Child(kit.Collapsible("ui.page-error.details", T("page.root.error.details"), s.pageErrorDetails,
			func(open bool) { s.pageErrorDetails = open },
			fairy.Text(failure.message).Font(monoFont).FontSize(12).Color(t.Destructive).Wrap().Selectable().
				MaxH(192).ScrollY()).PadXY(12, 4).Radius(t.RadiusLg()).Fill(t.Muted.Alpha(0.5)))
	}
	content.Child(fairy.Row(
		fairy.Spacer(),
		kit.Button(
			T("page.root.error.back"),
			func() { s.travel(s.router.Back) },
			kit.Ghost,
			kit.Icon(lucide.ArrowLeft),
		),
		kit.Button(T("page.root.error.refresh"), func() { s.pageFailure = nil }, kit.Outline,
			kit.Icon(lucide.RefreshCw)),
	).Gap(8))
	return fairy.Col(content).Grow(1).WFull().MinH(0).Pad(24).Center()
}

func (s *Shell) routePage(cx *fairy.Cx, route Route) *fairy.El {
	var body *fairy.El
	switch route {
	case RouteSetting:
		body = s.settingsPage(cx)
	case RouteMod:
		body = s.modPage(cx)
	case RouteTransfer:
		body = s.transferPage(cx)
	case RouteXXMI:
		body = s.xxmiPage(cx)
	case RouteGameBanana:
		body = s.gameBananaPage(cx)
	case RouteDrive, RouteShare:
		body = s.drivePage(cx, route)
	case RouteBackup:
		body = s.backupPage(cx)
	case RouteTools:
		body = s.toolsPage(cx)
	case RouteAgent:
		body = s.agentPage(cx)
	case RouteAuth:
		body = s.authPage()
	case RouteDevelopment:
		body = s.developmentPage()
	default:
		body = notPorted(routeIcon(route), routeTitle(route))
	}
	return body
}

// notPorted stands in for a screen that has not moved to the native UI yet.
func notPorted(icon *svg.Vector, title string) *fairy.El {
	t := kit.T()
	return fairy.Col(
		fairy.Icon(icon).Size(40, 40).Color(textDisabled(t)),
		fairy.Text(title).FontSize(20).SemiBold(),
		fairy.Text(T("shell.not_ported")).Color(t.MutedForeground),
	).WFull().HFull().Center().Gap(10)
}

func routeTitle(route Route) string {
	switch route {
	case RouteTransfer:
		return T("page.transfer.title")
	case RouteGameBanana:
		return "GameBanana"
	case RouteMod:
		return T("page.mod.title")
	case RouteTools:
		return "Tools"
	case RouteAgent:
		return T("page.agent.title")
	case RouteXXMI:
		return "XXMI"
	case RouteDrive:
		return T("page.drive.title")
	case RouteShare:
		return T("page.share_drive.title")
	case RouteBackup:
		return T("page.backup.title")
	case RouteSetting:
		return T("page.setting.title")
	case RouteDevelopment:
		return "Development"
	case RouteAuth:
	}
	return "nahida.live"
}

func routeIcon(route Route) *svg.Vector {
	switch route {
	case RouteTransfer:
		return lucide.ArrowUpDown
	case RouteGameBanana:
		return lucide.Banana
	case RouteMod:
		return lucide.Gamepad
	case RouteTools:
		return lucide.Wrench
	case RouteAgent:
		return lucide.Sparkles
	case RouteXXMI:
		return lucide.Package
	case RouteDrive:
		return lucide.HardDrive
	case RouteShare:
		return lucide.Share2
	case RouteBackup:
		return lucide.DatabaseBackup
	case RouteSetting:
		return lucide.Settings
	case RouteDevelopment:
		return lucide.BugPlay
	case RouteAuth:
	}
	return lucide.Info
}
