package ui

import (
	"context"
	"net/url"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/agent"
	"nahida.live/desktop/internal/auth"
	"nahida.live/desktop/internal/backup"
	"nahida.live/desktop/internal/drive"
	"nahida.live/desktop/internal/feedback"
	"nahida.live/desktop/internal/gamebanana"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/modruntime"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/reshade"
	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/tools"
	"nahida.live/desktop/internal/transfer"
	"nahida.live/desktop/internal/xxmi"
)

type Deps struct {
	Events    *Events
	Settings  *setting.Setting
	Updater   *infra.Updater
	Auth      *auth.Auth
	Agent     *agent.Service
	Mod       *mod.Mod
	Feedback  *feedback.Service
	Dialog    *platform.Dialog
	Platform  *platform.Shell
	AppStatus platform.AppStatus
	FS        *platform.FS
	Tools     *tools.Tools
	Transfer  *transfer.Transfer
	// GameBanana browses gamebanana.com and signs in through the login window the app wires into it.
	GameBanana *gamebanana.GameBanana
	// HTTP fetches remote images.
	HTTP    *infra.Client
	XXMI    *xxmi.XXMI
	ReShade *reshade.ReShade
	// Drive lists, uploads to and downloads from the nahida.live drive.
	Drive  *drive.Drive
	Backup *backup.Backup
	// ModRuntime manages the mod runtimes the importer pages install.
	ModRuntime *modruntime.Manager
	// Input reports whether the elevated input helper is enabled and running.
	Input *platform.Input
	Log   *infra.Log
	// Restart relaunches the application.
	Restart func() error
	// Ready waits for startup maintenance, which every Mod call and the recycle bin wait for, as the Wails
	// service guard made them.
	Ready func(context.Context) error
	// LibraryReady waits only until importer Mods links are repaired, which read-only mod loads need.
	LibraryReady func(context.Context) error
	// Dev shows the development screen.
	Dev bool
	// CDNTrace reads Cloudflare's trace for the development screen.
	CDNTrace *infra.CDNTrace
}

// Shell is the main window's frame: title bar, navigation, the current screen, toasts and dialogs. Its
// state belongs to the UI thread; background work hands results back with fairy.Go.
type Shell struct {
	deps   Deps
	font   string
	router Router
	// pendingStart is a start page that needs the session, kept until the session is known. Any navigation
	// drops it.
	pendingStart Route
	// pageFailure is the screen whose build panicked, shown as the error screen until a retry or navigation.
	pageFailure      *pageFailure
	pageErrorDetails bool

	toasts kit.Toaster
	// progress holds the loading toasts that progress events update in place, by the event's subject.
	progress    map[string]kit.ToastID
	confirms    []Confirmation
	confirmSeq  int
	themeChoice string
	dark        bool
	blur        bool // the backdrop blur setting

	updater       infra.UpdaterStatus
	updateOffered bool
	// updateDismissing holds the prompt after Later until a status without it arrives, so a status read before
	// the dismissal reached the backend does not offer the update again.
	updateDismissing bool
	updateBusy       bool
	// offeredRoute is the route the update offer last saw, so leaving the sign-in screen offers a held update.
	offeredRoute   Route
	transfer       *transfer.WindowProgress
	backend        infra.BackendStatus
	session        *auth.Session
	sessionKnown   bool
	hasToken       bool
	agentAvailable bool
	activity       activityState

	settings    settingsScreen
	mods        modScreen
	transfers   transferScreen
	xxmi        xxmiScreen
	gameBanana  gameBananaScreen
	drive       driveScreen
	backup      backupScreen
	development developmentScreen
	tools       toolsScreen
	agent       agentScreen
	login       loginState
	// modVideos and driveVideos play the preview videos of the mod and drive screens.
	modVideos   previewVideos
	driveVideos previewVideos
	// previewImageFailures keeps the cache keys of mod preview images that failed to load, so each is logged once.
	previewImageFailures map[string]bool
	// modPreviewPaths keeps the file each mod preview cache key was last loaded from; see sizedModPreview.
	modPreviewPaths map[string]modPreviewPath
	// modCardShown keeps, by mod path, the preview each grid card last drew; see modCardPreview.
	modCardShown map[string]shownCardPreview
	// modCardShownListing is the listing modCardShown was last pruned against.
	modCardShownListing *mod.FolderGroup
	// deviceGen is the graphics device generation the last build saw, for logging a recreated device.
	deviceGen uint64
	// paths are the download location prompts waiting for an answer, oldest first.
	paths []pathPrompt
	// download is the mod manager download the user is choosing a folder for.
	download *pathPrompt
	// win is the main window, for handlers that read the clipboard, the modifier keys or move focus.
	win *fairy.Window
}

// NewShell subscribes to the backend events the shell shows. font is the family from RegisterFonts.
func NewShell(deps Deps, font string) *Shell {
	s := &Shell{
		deps: deps, font: font, router: NewRouter(RouteMod), themeChoice: setting.ThemeSystem, blur: true,
		progress: map[string]kit.ToastID{}, activity: newActivityState(),
	}
	s.settings = newSettingsScreen()
	s.mods = newModScreen()
	s.transfers = newTransferScreen()
	s.xxmi = newXXMIScreen()
	s.gameBanana = newGameBananaScreen()
	s.drive = newDriveScreen()
	go s.runSettingWrites()
	on := deps.Events.On
	s.registerModEvents(on)
	s.registerXXMIEvents(on)
	s.registerBackupEvents(on)
	s.registerToolEvents(on)
	s.registerAgentEvents(on)
	on("pathSelector:modeSelect", func(data []any) { s.promptPath(first(data)) })
	on("setting:update", func(data []any) {
		update, _ := first(data).(map[string]any)
		key, _ := update["key"].(string)
		s.settingChanged(key, update["value"])
	})
	on("renderer:reload", func([]any) { s.reloadSettings() })
	on("mod:compressionProgress", func(data []any) {
		if state, ok := first(data).(mod.CompressionState); ok {
			s.settings.compression = &state
		}
	})
	on(feedback.EventProgress, func(data []any) {
		if progress, ok := first(data).(feedback.Progress); ok {
			setUploadProgress(&s.settings.feedback.progress, progress)
			if report := s.xxmi.importer.diag.report; report != nil {
				setUploadProgress(&report.progress, progress)
			}
		}
	})
	on("github:token-rejected", func([]any) { s.settings.network.github = nil })
	on("language:update", func(data []any) {
		if lang, ok := first(data).(string); ok {
			s.setLanguage(lang)
		}
	})
	on("updater:status-changed", func(data []any) {
		if status, ok := first(data).(infra.UpdaterStatus); ok {
			s.setUpdater(status)
		}
	})
	on("transfer:update", func(data []any) {
		snapshots, _ := first(data).([]transfer.Snapshot)
		s.setTransfers(snapshots)
		s.driveTransfersChanged(snapshots)
	})
	on(elevatedStatusEvent, func(data []any) {
		if status, ok := first(data).(platform.ElevatedHelperStatus); ok {
			s.setElevatedHelper(status)
		}
	})
	on("backend:status", func(data []any) {
		status, ok := first(data).(string)
		if !ok {
			return
		}
		previous := s.backend
		s.backend = infra.BackendStatus(status)
		// A token stored while the server was unreachable gives a session once it answers again, as React
		// fetched the session again when the status turned online.
		if s.backend == infra.BackendOnline && previous != infra.BackendOnline && s.session == nil && s.hasToken {
			go s.loadSession()
		}
		if !s.backendDown() {
			s.refreshDrives(true)
		}
	})
	on("auth:update", func(data []any) {
		session, _ := first(data).(*auth.Session)
		s.setSession(session, session != nil)
	})
	on("fn:toast", func(data []any) {
		title, _ := first(data).(string)
		body := ""
		if len(data) > 1 {
			if args, ok := data[1].(map[string]any); ok {
				body, _ = args["description"].(string)
			}
		}
		s.toasts.Show(kit.ToastInfo, title, body)
	})
	on("fn:navi", func(data []any) {
		path, _ := first(data).(string)
		s.navigatePath(path)
	})
	return s
}

func first(data []any) any {
	if len(data) == 0 {
		return nil
	}
	return data[0]
}

// Load reads the state the first frame needs and starts loading the rest. Call it before the window opens.
func (s *Shell) Load(ctx context.Context) {
	if lang, err := s.deps.Settings.GetLanguage(ctx); err == nil {
		s.setLanguage(lang)
	} else {
		s.report(err, "read-language")
	}
	if theme, err := s.deps.Settings.GetTheme(ctx); err == nil {
		s.themeChoice = theme
	} else {
		s.report(err, "read-theme")
	}
	if value, err := s.deps.Settings.Get(ctx, setting.KeyGeneralBackdropBlur); err == nil {
		s.setBlur(value)
	} else {
		s.report(err, "read-backdrop-blur")
	}
	if value, err := s.deps.Settings.Get(ctx, setting.KeyGeneralDefaultStartPage); err == nil {
		if route, ok := RouteOf(asString(value)); ok {
			s.router = NewRouter(route)
			if route.requiresSession() {
				s.router, s.pendingStart = NewRouter(RouteMod), route
			}
		}
	} else {
		s.report(err, "read-start-page")
	}
	s.restoreGameBananaView(ctx)
	if status, err := s.deps.Updater.GetStatus(ctx); err == nil {
		s.setUpdater(status)
	}
	s.backend = infra.BackendStatus(s.deps.Auth.GetBackendStatus())

	go s.loadSession()
	s.refreshAgent()
	s.loadActivities()
}

// loadSession reads the session, retrying once when a token is stored and the server came online meanwhile.
func (s *Shell) loadSession() {
	ctx := context.Background()
	session, err := s.deps.Auth.GetSession(ctx)
	if err != nil {
		s.report(err, "read-session")
	}
	hasToken, err := s.deps.Auth.HasToken(ctx)
	if err != nil {
		s.report(err, "read-token")
	}
	backend := infra.BackendStatus(s.deps.Auth.GetBackendStatus())
	if session == nil && hasToken && backend == infra.BackendOnline {
		if session, err = s.deps.Auth.GetSession(ctx); err != nil {
			s.report(err, "restore-session")
		}
	}
	fairy.Go(func() {
		if s.backend == infra.BackendUnknown {
			s.backend = backend
		}
		s.setSession(session, session != nil || hasToken)
	})
}

func asString(value any) string {
	if p, ok := value.(*string); ok && p != nil {
		return *p
	}
	text, _ := value.(string)
	return text
}

// OnFocus refreshes state that can change without an event while the window is in the background.
func (s *Shell) OnFocus(focused bool) {
	if !focused {
		return
	}
	go func() {
		if status, err := s.deps.Updater.GetStatus(context.Background()); err == nil {
			fairy.Go(func() { s.setUpdater(status) })
		}
	}()
	s.refreshAgent()
	s.modFocused()
	s.refreshFixInspections()
	switch s.router.Current() {
	case RouteGameBanana:
		s.gbRefresh()
	case RouteDrive, RouteShare:
		if s.signedIn() && !s.backendDown() {
			s.refreshDrives(false)
		}
	case RouteBackup:
		s.backupFocused()
	default:
	}
}

// refreshAgent shows the agent screen once a provider with a usable credential is configured.
func (s *Shell) refreshAgent() {
	go func() {
		settings, err := s.deps.Agent.GetSettings(context.Background())
		available := err == nil && settings.Provider != "" && strings.TrimSpace(settings.Endpoint) != "" &&
			strings.TrimSpace(settings.Model) != "" &&
			(settings.Provider == "custom" || settings.Credential.Kind == "api" || settings.Credential.Kind == "oauth")
		fairy.Go(func() { s.agentAvailable = available })
	}()
}

// OnKey goes back and forward with the mouse side buttons and Alt+Left and Alt+Right.
func (s *Shell) OnKey(k keyboard.KeyEvent) bool {
	switch {
	case k.Is(keyboard.KeyBrowserBack, 0), k.Is(keyboard.KeyLeft, keyboard.ModAlt):
		return s.travel(s.router.Back)
	case k.Is(keyboard.KeyBrowserForward, 0), k.Is(keyboard.KeyRight, keyboard.ModAlt):
		return s.travel(s.router.Forward)
	case k.Is(keyboard.KeyEscape, 0) && s.router.Current() == RouteMod && s.mods.mergeMode:
		s.setMergeMode(false)
		return true
	case k.Is('F', keyboard.ModCtrl) && s.router.Current() == RouteMod:
		s.switchModSearch()
		return true
	case (s.router.Current() == RouteDrive || s.router.Current() == RouteShare) && s.signedIn():
		return s.driveKey(s.driveView(s.router.Current()), k)
	}
	return false
}

// travel moves through the history after the unsaved importer edits are confirmed, as React's blocker held
// history moves too.
func (s *Shell) travel(move func() bool) bool {
	step := func() bool {
		if !move() {
			return false
		}
		s.restoreLocation(s.router.Location())
		return true
	}
	if s.router.Current() == RouteXXMI && s.leaveImporterPage(func() { step() }) {
		return true
	}
	return step()
}

// location is the frontend path of what the screen shows, which the router keeps as a history entry.
func (s *Shell) location() string {
	route := s.router.Current()
	sub := ""
	switch route {
	case RouteSetting:
		sub = string(s.settings.section)
	case RouteTools:
		sub = string(s.tools.active)
	case RouteBackup:
		sub = s.backup.section
	case RouteXXMI:
		sub = s.xxmi.page
	case RouteDrive, RouteShare:
		sub = s.driveView(route).folder
	default:
	}
	if sub == "" {
		return string(route)
	}
	return string(route) + "/" + sub
}

// restoreLocation shows the part of a route that path names: the settings section, tool, backup section, XXMI
// page or drive folder.
func (s *Shell) restoreLocation(path string) {
	switch route, _ := RouteOf(path); route {
	case RouteSetting:
		s.openSettings(settingsSectionOf(path))
	case RouteXXMI:
		s.navigateXXMI(path)
	case RouteDrive, RouteShare:
		s.navigateDrive(route, path)
	case RouteBackup:
		s.backup.section = backupSectionOf(path)
	case RouteTools:
		s.navigateTools(path)
	default:
	}
}

func (s *Shell) navigate(route Route) {
	if route != RouteXXMI && s.router.Current() == RouteXXMI && s.leaveImporterPage(func() { s.navigate(route) }) {
		return
	}
	s.pendingStart = ""
	s.router.Navigate(route)
}

// navigatePath opens a frontend path, including a settings section such as /setting/network.
func (s *Shell) navigatePath(path string) {
	path, query, _ := strings.Cut(path, "?")
	route, ok := RouteOf(path)
	if !ok {
		return
	}
	if route == RouteGameBanana {
		if values, err := url.ParseQuery(query); err == nil {
			if id, ok := parseInt(values.Get("mod")); ok && id > 0 {
				s.gbSelectMod(id, "Mod")
			}
		}
	}
	s.restoreLocation(path)
	if route == RouteAgent {
		if values, err := url.ParseQuery(query); err == nil && values.Get("session") != "" {
			s.openAgentSession(values.Get("session"))
		}
	}
	s.navigate(route)
}

func (s *Shell) setLanguage(lang string) {
	SetLanguage(lang)
}

func (s *Shell) signedIn() bool { return s.session != nil }

func (s *Shell) setSession(session *auth.Session, hasToken bool) {
	s.session, s.sessionKnown, s.hasToken = session, true, hasToken
	// A stored token while the server is unreachable keeps the start page waiting for the session, which the
	// backend:status handler loads once the server answers.
	if s.pendingStart != "" && (session != nil || !hasToken || s.backend == infra.BackendOnline) {
		if session != nil {
			s.router = NewRouter(s.pendingStart)
		}
		s.pendingStart = ""
	}
	if session == nil && s.router.Current().requiresSession() {
		s.navigate(RouteMod)
	}
	if session == nil {
		s.drive, s.backup = newDriveScreen(), backupScreen{}
	}
}

// setUpdater records the updater state and offers a downloaded update once; Later dismisses it on the backend.
func (s *Shell) setUpdater(status infra.UpdaterStatus) {
	s.updater = status
	s.observeUpdateCheck(status)
	if !status.ShouldPromptForUpdate {
		s.updateDismissing = false
	}
	if !status.UpdateDownloaded {
		s.updateOffered = false
		return
	}
	s.offerUpdate()
}

// offerUpdateOnRouteChange offers a held update once the sign-in screen is left. View calls it every frame
// because the router has no change notification.
func (s *Shell) offerUpdateOnRouteChange() {
	if route := s.router.Current(); route != s.offeredRoute {
		s.offeredRoute = route
		s.offerUpdate()
	}
}

// offerUpdate asks about a downloaded update the backend wants prompted. The sign-in screen holds it until the
// user leaves, as the React root rendered the update dialog outside /auth only.
func (s *Shell) offerUpdate() {
	status := s.updater
	if status.UpdateDownloaded && status.ShouldPromptForUpdate && !s.updateOffered && !s.updateDismissing &&
		s.router.Current() != RouteAuth {
		s.promptUpdate()
	}
}

// Dark reports whether the shell shows the dark theme: the theme setting, or the Windows app mode when it
// follows the system.
func (s *Shell) Dark() bool {
	switch s.themeChoice {
	case setting.ThemeDark:
		return true
	case setting.ThemeLight:
		return false
	}
	return fairy.SystemDark()
}

// WindowColors returns the colours the main window opens with.
func (s *Shell) WindowColors() (dark bool, background, text fairy.Color) {
	s.dark = s.Dark()
	t := Theme(s.dark, s.blur, s.font)
	return s.dark, t.Background, t.Foreground
}

func (s *Shell) report(err error, stage string) {
	_ = infra.ReportError(s.deps.Log, err, "Shell", infra.Diagnostic{Operation: "shell", Stage: stage})
}

// showProgress shows the loading toast for key, or updates the one already showing, as the React screens did
// with toast.loading and a fixed id. Call it on the UI thread.
func (s *Shell) showProgress(key, title string) {
	if id, ok := s.progress[key]; ok {
		s.toasts.Update(id, kit.ToastLoading, title, "")
		return
	}
	s.progress[key] = s.toasts.Loading(title, "")
}

// endProgress removes the loading toast for key. Call it on the UI thread.
func (s *Shell) endProgress(key string) {
	if id, ok := s.progress[key]; ok {
		s.toasts.Dismiss(id)
		delete(s.progress, key)
	}
}
