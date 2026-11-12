package ui

import (
	"context"
	"embed"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"
	"github.com/myparsleycat/fairy/svg"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/modruntime"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/reshade"
	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/xxmi"
)

//go:embed assets/xxmi
var xxmiAssets embed.FS

const (
	monoFont = "Consolas"

	xxmiManagePage   = ""
	xxmiReShadePage  = "reshade"
	xxmiScrollGutter = 10
)

var importerKeys = []string{"GIMI", "SRMI", "HIMI", "ZZMI", "WWMI", "EFMI"}

const xxmiIconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" ` +
	`stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">`

var (
	xxmiLayoutDashboard = svg.Lazy(xxmiIconSVG + `<rect width="7" height="9" x="3" y="3" rx="1"/>` +
		`<rect width="7" height="5" x="14" y="3" rx="1"/><rect width="7" height="9" x="14" y="12" rx="1"/>` +
		`<rect width="7" height="5" x="3" y="16" rx="1"/></svg>`)
	xxmiScanSearch = svg.Lazy(xxmiIconSVG + `<path d="M3 7V5a2 2 0 0 1 2-2h2"/><path d="M17 3h2a2 2 0 0 1 2 2v2"/>` +
		`<path d="M21 17v2a2 2 0 0 1-2 2h-2"/><path d="M7 21H5a2 2 0 0 1-2-2v-2"/>` +
		`<circle cx="12" cy="12" r="3"/><path d="m16 16-1.9-1.9"/></svg>`)
)

// query is one backend read shown by the XXMI screens. Invalidating it reads it again on the next frame and keeps
// the last value on screen meanwhile.
type query[T any] struct {
	value   T
	loaded  bool
	current bool
	loading bool
	err     error
	seq     int
	at      time.Time
}

func (q *query[T]) invalidate() {
	q.current, q.err = false, nil
	q.seq++
}

// set stamps at as well: fetchFresh would otherwise find a value set by an event stale at once and read it again,
// and a read that emits the event that sets it would repeat on every frame.
func (q *query[T]) set(value T) {
	q.value, q.loaded, q.current, q.err, q.at = value, true, true, nil, time.Now()
	q.seq++
}

// fetch starts reading q unless it is current or being read. A failure stays until q is invalidated, as the React
// screens did not retry these reads on their own.
func fetch[T any](s *Shell, q *query[T], read func(context.Context) (T, error)) *query[T] {
	if q.current || q.loading || q.err != nil {
		return q
	}
	q.loading = true
	seq := q.seq
	s.background(func(ctx context.Context) {
		value, err := read(ctx)
		fairy.Go(func() {
			q.loading = false
			if seq != q.seq {
				return
			}
			if err != nil {
				q.err = err
				return
			}
			q.value, q.loaded, q.current, q.at = value, true, true, time.Now()
		})
	})
	return q
}

// releaseMaxAge matches the React queries' staleTime for GitHub release lists; an older list is read again in the
// background while the previous one stays on screen.
const releaseMaxAge = time.Hour

func fetchFresh[T any](s *Shell, q *query[T], maxAge time.Duration, read func(context.Context) (T, error)) *query[T] {
	if q.current && time.Since(q.at) > maxAge {
		q.current = false
	}
	return fetch(s, q, read)
}

// invalidateImporterFiles reads again what an importer page shows from its folder: d3dx.ini options, the mod
// folder link and the mod runtimes, which a launch, a DLL restore or a package repair rewrite.
func (s *Shell) invalidateImporterFiles(key string) {
	x := &s.xxmi
	if x.importer.key == key {
		x.importer.d3dxOptions.invalidate()
		x.importer.modFolder.invalidate()
	}
	if q := x.modRuntimes[key]; q != nil {
		q.invalidate()
	}
}

func keyed[T any](m map[string]*query[T], key string) *query[T] {
	q := m[key]
	if q == nil {
		q = &query[T]{}
		m[key] = q
	}
	return q
}

func invalidateAll[T any](m map[string]*query[T]) {
	for _, q := range m {
		q.invalidate()
	}
}

type xxmiScreen struct {
	// page is "" for the manage page, "reshade", or an importer key.
	page string

	overview      query[xxmi.Overview]
	updates       query[[]xxmi.UpdateStatus]
	icons         query[[]xxmi.ImporterIcon]
	launcher      query[*xxmi.ExternalLauncher]
	data          query[xxmi.Data]
	cachedLibs    query[[]xxmi.CachedLibs]
	legacy        query[[]xxmi.LegacyRuntimeInfo]
	releases      map[string]*query[[]xxmi.ReleaseInfo]
	configs       map[string]*query[xxmi.ImporterConfig]
	verifications map[string]*query[*xxmi.PackageVerification]
	importPreview map[string]*query[[]xxmi.ImportPackageVersion]
	diagnostics   map[string]*query[*xxmi.DiagnosticsSession]
	diagSupport   map[string]*query[xxmi.DiagnosticsSupport]
	modRuntimes   map[string]*query[modruntime.Listing]

	reshadeStatus   query[reshade.Status]
	reshadeVersions query[[]string]
	effects         query[[]reshade.EffectPackage]

	busy            map[string]bool
	showUninstalled bool
	updateTarget    string
	launchWarned    map[string]bool
	// launchFailures holds, by importer, the launch stages that failed since its last launch started: "auto-update"
	// and "mod-runtimes".
	launchFailures map[string][]string

	manage   xxmiManage
	external xxmiExternal
	importer importerPage
	reshade  reshadePage
	guard    launchGuard
}

func newXXMIScreen() xxmiScreen {
	return xxmiScreen{
		releases:       map[string]*query[[]xxmi.ReleaseInfo]{},
		configs:        map[string]*query[xxmi.ImporterConfig]{},
		verifications:  map[string]*query[*xxmi.PackageVerification]{},
		importPreview:  map[string]*query[[]xxmi.ImportPackageVersion]{},
		diagnostics:    map[string]*query[*xxmi.DiagnosticsSession]{},
		diagSupport:    map[string]*query[xxmi.DiagnosticsSupport]{},
		modRuntimes:    map[string]*query[modruntime.Listing]{},
		busy:           map[string]bool{},
		launchWarned:   map[string]bool{},
		launchFailures: map[string][]string{},
		external:       xxmiExternal{versions: map[string]string{}},
		reshade:        reshadePage{pending: map[string]bool{}},
		guard:          newLaunchGuard(),
	}
}

// refresh reads every XXMI value again, as the React screens invalidated every "xxmi:" query after a change.
func (x *xxmiScreen) refresh() {
	x.overview.invalidate()
	x.updates.invalidate()
	x.icons.invalidate()
	x.launcher.invalidate()
	x.data.invalidate()
	x.cachedLibs.invalidate()
	x.legacy.invalidate()
	invalidateAll(x.releases)
	invalidateAll(x.configs)
	invalidateAll(x.verifications)
	invalidateAll(x.diagnostics)
	invalidateAll(x.diagSupport)
	invalidateAll(x.importPreview)
}

func (s *Shell) registerXXMIEvents(on func(string, func([]any))) {
	x := &s.xxmi
	on("xxmi:updates", func(data []any) {
		if updates, ok := first(data).([]xxmi.UpdateStatus); ok {
			x.updates.set(updates)
		}
	})
	on("xxmi:diagnostics-changed", func(data []any) {
		payload, _ := first(data).(map[string]any)
		if importer, ok := payload["importer"].(string); ok {
			keyed(x.diagnostics, importer).invalidate()
		}
	})
	on("xxmi:running-changed", func(data []any) {
		running, _ := first(data).(map[string]bool)
		s.setXXMIRunning(running)
	})
	on("xxmi:launch-progress", func(data []any) {
		payload, _ := first(data).(map[string]any)
		s.xxmiLaunchProgress(payload)
	})
	on("xxmi:package-progress", func(data []any) {
		payload, _ := first(data).(map[string]any)
		pkg, _ := payload["package"].(string)
		stage, _ := payload["stage"].(string)
		if pkg == "" || stage == "" {
			return
		}
		key := "xxmi-package:" + pkg
		if stage == "downloaded" || stage == "failed" {
			s.endProgress(key)
			return
		}
		downloaded, ok := number(payload["downloaded"])
		if stage != "download" || !ok {
			return
		}
		total, _ := number(payload["total"])
		label := pkg
		if version, _ := payload["version"].(string); version != "" {
			label += " " + version
		}
		s.showProgress(key, label+" · "+T("page.setting.xxmi.builtin.downloadingPackage")+" "+
			downloadProgress(downloaded, total))
	})
	on("xxmi:import-progress", func(data []any) {
		payload, _ := first(data).(map[string]any)
		stage, _ := payload["stage"].(string)
		copied, ok := number(payload["copied"])
		if stage != "copy" || !ok {
			s.endProgress("xxmi-import")
			return
		}
		total, _ := number(payload["total"])
		s.showProgress("xxmi-import", fmt.Sprint(payload["importer"], " ", payload["name"], " · ",
			T("page.setting.xxmi.builtin.movingUserData"), " ", downloadProgress(copied, total)))
	})
	on("xxmi:texture-compress-progress", func(data []any) {
		payload, _ := first(data).(map[string]any)
		done, okDone := number(payload["done"])
		total, okTotal := number(payload["total"])
		if okDone && okTotal && x.guard.textures != nil && x.guard.textures.progress != nil {
			x.guard.textures.progress = &[2]int{int(done), int(total)}
		}
	})
	on("reshade:status", func([]any) {
		x.reshadeStatus.invalidate()
		x.reshadeVersions.invalidate()
		x.effects.invalidate()
	})
	on("reshade:progress", func(data []any) {
		payload, _ := first(data).(map[string]any)
		kind, _ := payload["kind"].(string)
		name, _ := payload["name"].(string)
		if kind == "" || name == "" {
			return
		}
		key := "reshade:" + kind + ":" + name
		stage, _ := payload["stage"].(string)
		downloaded, ok := number(payload["downloaded"])
		if stage != "download" || !ok {
			s.endProgress(key)
			return
		}
		total, _ := number(payload["total"])
		label := name
		if kind == "binary" {
			label = "ReShade " + name
		}
		s.showProgress(key, label+" · "+T("page.setting.xxmi.builtin.downloadingPackage")+" "+
			downloadProgress(downloaded, total))
	})
	on("setting:update", func(data []any) {
		update, _ := first(data).(map[string]any)
		if key, _ := update["key"].(string); key == setting.KeyXXMIIncludePrereleases {
			// The backend filters the cached release lists with the stored setting.
			invalidateAll(x.releases)
		}
	})
}

// setXXMIRunning applies the running flags to the overview. The event carries only the flags, so a different
// importer set or a read in flight needs the whole overview again.
func (s *Shell) setXXMIRunning(running map[string]bool) {
	q := &s.xxmi.overview
	if !q.loaded {
		return
	}
	importers := q.value.Importers
	same := !q.loading && len(importers) == len(running)
	for _, importer := range importers {
		if _, ok := running[importer.Key]; !ok {
			same = false
		}
	}
	if !same {
		q.invalidate()
		return
	}
	next := slices.Clone(importers)
	for i := range next {
		next[i].Running = running[next[i].Key]
	}
	q.value.Importers = next
}

func (s *Shell) xxmiLaunchProgress(payload map[string]any) {
	x := &s.xxmi
	importer, _ := payload["importer"].(string)
	if importer == "" {
		return
	}
	if code, _ := payload["warningCode"].(string); code != "" {
		// A launch retried after a guard dialog reports the same warning again.
		if id := importer + ":" + code; !x.launchWarned[id] {
			x.launchWarned[id] = true
			s.toasts.Show(kit.ToastWarning, T("page.setting.xxmi.builtin.launchWarnings."+code), "")
		}
	} else if warning, _ := payload["warning"].(string); warning != "" {
		s.toasts.Show(kit.ToastWarning, warning, "")
	}

	stage, ok := payload["stage"].(string)
	if !ok {
		return
	}
	_, failedStep := payload["detail"].(string)
	// Every launch reports auto-update first, so a successful one starts the importer's failures afresh.
	switch {
	case failedStep && (stage == "auto-update" || stage == "mod-runtimes"):
		x.launchFailures[importer] = append(x.launchFailures[importer], stage)
	case stage == "auto-update":
		delete(x.launchFailures, importer)
	}
	switch {
	case stage == "auto-update" && failedStep:
		s.toasts.Show(kit.ToastWarning, T("page.setting.xxmi.builtin.autoUpdateFailed", "importer", importer), "")
	case stage == "mod-runtimes" && failedStep:
		s.toasts.Show(kit.ToastWarning, T("page.setting.xxmi.modRuntimes.autoUpdateFailed", "importer", importer), "")
	case stage == "ini-optimizer":
		if optimized, ok := number(payload["optimized"]); ok && optimized > 0 {
			s.toasts.Show(kit.ToastSuccess, T("page.setting.xxmi.builtin.optimized", "count", int(optimized)), "")
		}
	}
	key := "xxmi-launch:" + importer
	if stage == "finish" || stage == "failed" {
		s.endProgress(key)
		for id := range x.launchWarned {
			if strings.HasPrefix(id, importer+":") {
				delete(x.launchWarned, id)
			}
		}
		x.overview.invalidate()
		// A launch checks for updates before installing them, so the last reported status is outdated.
		x.updates.invalidate()
		s.invalidateImporterFiles(importer)
		return
	}
	status := "launchPreparing"
	switch stage {
	case "ensure-runtime":
		status = "launchDownloading"
	case "update-ini", "mod-runtimes", "game-tweaks", "ini-optimizer", "pre-launch":
		status = "launchConfiguring"
	case "elevate", "inject-launch", "post-load":
		status = "launchStarting"
	}
	s.showProgress(key, importer+" · "+T("page.setting.xxmi.builtin."+status))
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

func downloadProgress(done, total float64) string {
	if total > 0 {
		return itoa(int(min(100, math.Round(done/total*100)))) + "%"
	}
	return itoa(int(math.Round(done/(1024*1024)))) + " MiB"
}

// xxmiFail logs err and shows message, or the error text, as a toast. Cancellation is not a failure.
func (s *Shell) xxmiFail(err error, operation string, fields map[string]any, message string) {
	if infra.IsCancellationError(err) {
		return
	}
	err = infra.ReportError(s.deps.Log, err, "XXMI", infra.Diagnostic{Operation: operation, Fields: fields})
	if message == "" {
		message = err.Error()
	}
	s.toasts.Show(kit.ToastError, message, "")
}

// xxmiAction runs work in the background while the control named id shows as busy. A failure is reported as a
// toast with the text message returns, or the error text when message is nil; done runs on the UI thread after a
// success.
func (s *Shell) xxmiAction(id string, work func(context.Context) error, done func(), message func(error) string) {
	x := &s.xxmi
	if x.busy[id] {
		return
	}
	x.busy[id] = true
	s.background(func(ctx context.Context) {
		err := work(ctx)
		fairy.Go(func() {
			delete(x.busy, id)
			if err != nil {
				text := ""
				if message != nil {
					text = message(err)
				}
				s.xxmiFail(err, id, nil, text)
				return
			}
			if done != nil {
				done()
			}
		})
	})
}

// pickFile shows the open dialog in the background and runs picked on the UI thread with the chosen paths.
func (s *Shell) pickFile(options platform.OpenDialogOptions, picked func(paths []string)) {
	s.pickFileSettled(options, picked, nil)
}

// pickFileSettled is pickFile that also calls settled on the UI thread when the dialog closes, picked or not,
// before picked runs.
func (s *Shell) pickFileSettled(options platform.OpenDialogOptions, picked func(paths []string), settled func()) {
	go func() {
		result, err := s.deps.Dialog.ShowOpenDialog(options)
		fairy.Go(func() {
			if settled != nil {
				settled()
			}
			if err != nil {
				s.xxmiFail(err, "open-dialog", map[string]any{"role": options.Role}, "")
				return
			}
			if !result.Canceled && len(result.FilePaths) > 0 {
				picked(result.FilePaths)
			}
		})
	}()
}

func (s *Shell) xxmiOverview() *query[xxmi.Overview] {
	return fetch(s, &s.xxmi.overview, s.deps.XXMI.GetOverview)
}

func (s *Shell) xxmiExternal() bool {
	q := &s.xxmi.overview
	return q.loaded && q.value.LauncherMode == xxmi.LauncherExternal
}

func (s *Shell) xxmiUpdates() []xxmi.UpdateStatus {
	q := &s.xxmi.overview
	if !q.loaded || !q.value.Configured || s.xxmiExternal() {
		return s.xxmi.updates.value
	}
	return fetchFresh(s, &s.xxmi.updates, releaseMaxAge, func(ctx context.Context) ([]xxmi.UpdateStatus, error) {
		return s.deps.XXMI.CheckUpdates(ctx, false)
	}).value
}

// installableUpdates lists each available unpinned package once; a shared package appears once per importer.
func installableUpdates(updates []xxmi.UpdateStatus) []xxmi.UpdateStatus {
	var out []xxmi.UpdateStatus
	for _, entry := range updates {
		if entry.Available && !entry.Pinned && !slices.ContainsFunc(out, func(u xxmi.UpdateStatus) bool {
			return u.Package == entry.Package
		}) {
			out = append(out, entry)
		}
	}
	return out
}

// importerUpdates leaves out pinned packages, which installing skips, and shared libraries, which update every
// importer following them and are offered on the manage page.
func importerUpdates(updates []xxmi.UpdateStatus, key string) []xxmi.UpdateStatus {
	var out []xxmi.UpdateStatus
	for _, entry := range updates {
		if entry.Importer == key && entry.Available && !entry.Pinned && !entry.Shared {
			out = append(out, entry)
		}
	}
	return out
}

func (s *Shell) xxmiReleases(
	key string,
	read func(context.Context) ([]xxmi.ReleaseInfo, error),
) *query[[]xxmi.ReleaseInfo] {
	return fetchFresh(s, keyed(s.xxmi.releases, key), releaseMaxAge, read)
}

func (s *Shell) packageReleases(pkg string) *query[[]xxmi.ReleaseInfo] {
	return s.xxmiReleases("package:"+pkg, func(ctx context.Context) ([]xxmi.ReleaseInfo, error) {
		return s.deps.XXMI.ListReleases(ctx, pkg)
	})
}

func (s *Shell) libsProviderReleases(provider string) *query[[]xxmi.ReleaseInfo] {
	return s.xxmiReleases("libs:"+provider, func(ctx context.Context) ([]xxmi.ReleaseInfo, error) {
		return s.deps.XXMI.GetLibsProviderReleases(ctx, provider)
	})
}

func (s *Shell) importerConfig(key string) *query[xxmi.ImporterConfig] {
	return fetch(s, keyed(s.xxmi.configs, key), func(ctx context.Context) (xxmi.ImporterConfig, error) {
		return s.deps.XXMI.GetImporterConfig(ctx, key)
	})
}

func findImporter(ov xxmi.Overview, key string) *xxmi.EnabledImporter {
	for i := range ov.Importers {
		if ov.Importers[i].Key == key {
			return &ov.Importers[i]
		}
	}
	return nil
}

// navigateXXMI opens a page of the XXMI screen from a path below /xxmi.
func (s *Shell) navigateXXMI(path string) {
	page := strings.Trim(strings.TrimPrefix(path, string(RouteXXMI)), "/")
	if page != xxmiReShadePage && !slices.Contains(importerKeys, page) {
		page = xxmiManagePage
	}
	s.openXXMIPage(page)
}

// openXXMIPage switches the XXMI page, asking first when the importer page has unsaved changes.
func (s *Shell) openXXMIPage(page string) {
	x := &s.xxmi
	if page == x.page {
		return
	}
	if s.leaveImporterPage(func() { s.openXXMIPage(page) }) {
		return
	}
	x.page = page
}

func (s *Shell) xxmiPage(cx *fairy.Cx) *fairy.El {
	t := kit.T()
	x := &s.xxmi
	overview := s.xxmiOverview()
	external := s.xxmiExternal()
	if external && x.page != xxmiManagePage {
		// Only the built-in launcher has importer and ReShade pages, and the external one hides the way to them.
		x.page = xxmiManagePage
	}

	var body *fairy.El
	switch x.page {
	case xxmiManagePage:
		body = fairy.Box().Grow(1)
		if overview.loaded {
			body = s.xxmiManagePage()
		}
	case xxmiReShadePage:
		body = s.reshadeSettingsPage()
	default:
		body = s.importerSettingsPage(x.page)
	}

	row := fairy.Row().WFull().HFull()
	if overview.loaded && !external {
		row.Child(s.xxmiSidebar())
	}
	s.xxmiDialogs(cx)
	return row.Child(fairy.Col(s.xxmiHeader(), body.Key("xxmi.page:"+x.page)).Grow(1).MinW(0).HFull().
		Fill(t.Background.Alpha(0.7)))
}

func (s *Shell) xxmiHeader() *fairy.El {
	t := kit.T()
	x := &s.xxmi
	q := &x.overview
	header := fairy.Row(fairy.Text("XXMI").Font(monoFont).FontSize(12).LineHeight(16).Medium()).H(40).Shrink(0).
		PadXY(16, 0).Gap(12).Align(fairy.Center).BorderEdges(0, 0, 1, 0, t.Border)

	header.Child(fairy.Spacer())

	mode := ""
	if q.loaded {
		mode = string(xxmi.LauncherBuiltin)
		if q.value.LauncherMode == xxmi.LauncherExternal {
			mode = string(xxmi.LauncherExternal)
		}
	}
	const busyPrefix = "set-launcher-mode:"
	switching := ""
	for _, value := range []xxmi.LauncherMode{xxmi.LauncherBuiltin, xxmi.LauncherExternal} {
		if x.busy[busyPrefix+string(value)] {
			switching = string(value)
		}
	}
	busy := switching != ""
	toggle := xxmiButtonGroup(mode, switching, []kit.Item{
		{Value: string(xxmi.LauncherBuiltin), Label: T("page.setting.xxmi.launcherMode.builtin"),
			Disabled: !q.loaded || mode == string(xxmi.LauncherBuiltin) || busy},
		{
			Value:    string(xxmi.LauncherExternal),
			Label:    T("page.setting.xxmi.launcherMode.external"),
			Disabled: !q.loaded || mode == string(xxmi.LauncherExternal) || busy,
		},
	}, func(value string) {
		if !q.loaded || value == mode || busy {
			return
		}
		s.xxmiAction(busyPrefix+value, func(ctx context.Context) error {
			return s.deps.XXMI.SetLauncherMode(ctx, xxmi.LauncherMode(value))
		}, x.refresh, nil)
	}, kit.SizeXS)
	return header.Child(toggle.Label(T("page.setting.xxmi.launcherMode.title")))
}

// xxmiButtonGroup shows items as joined buttons with value checked; the item named by loading shows a spinner.
func xxmiButtonGroup(value, loading string, items []kit.Item, onChange func(string), size kit.Size) *fairy.El {
	t := kit.T()
	group := fairy.Row().Role(fairy.RoleGroup).Shrink(0)
	scale := kit.Option(nil)
	if size == kit.SizeXS {
		scale = kit.XS
	}
	for i, item := range items {
		active := item.Value == value
		variant := kit.Outline
		if active {
			variant = kit.Default
		}
		button := kit.Button(item.Label, func() { onChange(item.Value) }, variant, scale, kit.Disabled(item.Disabled),
			kit.Loading(loading != "" && item.Value == loading)).
			Key(item.Value).Checked(active).Radius(0)
		if i == 0 {
			button.Radii(t.RadiusLg(), 0, 0, t.RadiusLg())
		}
		if i == len(items)-1 {
			button.Radii(0, t.RadiusLg(), t.RadiusLg(), 0)
		}
		if i > 0 {
			border := fairy.Transparent
			if !active {
				border = t.Border
				if t.Dark {
					border = t.Input
				}
			}
			button.BorderEdges(1, 1, 1, 0, border)
		}
		group.Child(button)
	}
	return group
}

func xxmiTabs(value string, items []kit.Item, onChange func(string), line bool) *fairy.El {
	t := kit.T()
	list := fairy.Row().H(32).Pad(3).Align(fairy.Center).Role(fairy.RoleTabList).FocusGroup()
	if line {
		list.WFull().Gap(4)
	} else {
		list.Radius(t.RadiusLg()).Fill(t.Muted)
	}
	for _, item := range items {
		active := item.Value == value
		ink := t.Foreground.Alpha(0.6)
		if t.Dark {
			ink = t.MutedForeground
		}
		if active {
			ink = t.Foreground
		}
		tab := fairy.Row(fairy.Text(item.Label).FontSize(14).LineHeight(20).Medium().NoWrap()).Key(item.Value).
			H(25).PadXY(6, 2).Align(fairy.Center).Justify(fairy.JustifyCenter).Radius(t.RadiusMd()).
			Border(1, fairy.Transparent).Color(ink).Role(fairy.RoleTab).Selected(active).Animate(motion.Fast)
		if line {
			tab.Grow(1).Basis(0).MinW(0)
			if active {
				tab.Child(fairy.Box().H(2).Left(0).Right(0).Bottom(-5).Fill(t.Foreground))
			}
		} else if active {
			tab.Fill(t.Background).Shadow(0, 1, 3, 0, fairy.RGBA(0, 0, 0, 0.1)).
				Shadow(0, 1, 2, -1, fairy.RGBA(0, 0, 0, 0.1))
			if t.Dark {
				tab.Fill(opacity(t.Input, 0.3)).Border(1, t.Input)
			}
		}
		if item.Disabled {
			tab.Disabled(true).Opacity(0.5)
		} else {
			tab.Focusable().FocusSelected(active).FocusVisibleBorder(t.Ring).FocusRing(3, t.Ring.Alpha(0.5)).
				HoverColor(t.Foreground).Cursor(fairy.CursorPointer).OnClick(func() { onChange(item.Value) })
		}
		list.Child(tab)
	}
	return list
}

func (s *Shell) xxmiSidebar() *fairy.El {
	t := kit.T()
	x := &s.xxmi
	ov := x.overview.value
	pending := installableUpdates(s.xxmiUpdates())

	navRow := func(page string, icon *fairy.El, label string, badge int) *fairy.El {
		active := x.page == page
		row := fairy.Row(icon, fairy.Text(label).FontSize(14).LineHeight(20).Grow(1).MinW(0).Ellipsis()).
			Key("xxmi.nav:"+page).
			PadXY(8, 8).
			Gap(10).
			Align(fairy.Center).
			Radius(6).
			Color(t.SidebarForeground).
			HoverFill(t.SidebarAccent).
			Animate(motion.Fast).
			Cursor(fairy.CursorPointer).
			Focusable().
			FocusRing(3, t.Ring.Alpha(0.5)).
			Label(label).
			Selected(active).
			OnClick(func() { s.openXXMIPage(page) })
		if badge > 0 {
			row.Child(kit.Badge(itoa(badge)))
		}
		if active {
			row.Fill(t.SidebarAccent).Color(t.SidebarAccentForeground)
		}
		return row
	}
	reshadeIcon := fairy.Box().Size(16, 16).Shrink(0)
	if img := fairy.Images.Sized("xxmi.asset:reshade.webp", 16, 16, fairy.FitContain, func() ([]byte, error) {
		return xxmiAssets.ReadFile("assets/xxmi/reshade.webp")
	}); img != nil {
		reshadeIcon.Child(fairy.ImageEl(img).WFull().HFull().Fit(fairy.FitContain))
	}

	var installed, uninstalled []string
	for _, key := range importerKeys {
		if findImporter(ov, key) != nil {
			installed = append(installed, key)
		} else {
			uninstalled = append(uninstalled, key)
		}
	}
	checkLabel := T("page.setting.xxmi.builtin.checkUpdates")
	importers := fairy.Col(
		fairy.Row(
			fairy.Text(strings.ToUpper(T("page.setting.xxmi.builtin.importers"))).
				FontSize(10).
				LineHeight(15).
				LetterSpacing(1).
				Medium().
				Color(t.MutedForeground).
				Grow(1),
			kit.IconButton(lucide.RefreshCw, s.checkXXMIUpdates, kit.Ghost, kit.XS,
				kit.Loading(x.busy["check-updates"]), kit.Tooltip(checkLabel)).Label(checkLabel),
		).PadEdges(0, 8, 2, 8).Align(fairy.Center),
	).Gap(2)
	for _, key := range installed {
		importers.Child(s.xxmiImporterRow(key))
	}
	if len(uninstalled) > 0 {
		// The section stays open while its importer page shows, so the active row stays visible.
		open := x.showUninstalled || slices.Contains(uninstalled, x.page)
		turn := float32(0)
		if open {
			turn = math.Pi / 2
		}
		label := T("page.setting.xxmi.builtin.notInstalled")
		importers.Child(fairy.Row(
			fairy.Icon(lucide.ChevronRight).Size(14, 14).Rotate(turn).ScaleMotion(motion.Snappy),
			fairy.Text(label).LineHeight(16).Grow(1),
			fairy.Text(itoa(len(uninstalled))).LineHeight(16),
		).Key("xxmi.uninstalled").PadXY(8, 4).Gap(4).Align(fairy.Center).Radius(6).FontSize(12).Color(t.MutedForeground).
			HoverFill(t.SidebarAccent).Animate(motion.Fast).Cursor(fairy.CursorPointer).Focusable().
			FocusRing(3, t.Ring.Alpha(0.5)).Label(label).Expanded(open).
			OnClick(func() { x.showUninstalled = !open }).MarginEdges(6, 0, 0, 0))
		if open {
			for _, key := range uninstalled {
				importers.Child(s.xxmiImporterRow(key))
			}
		}
	}

	return fairy.Col(
		fairy.Col(
			navRow(xxmiManagePage, fairy.Icon(xxmiLayoutDashboard).Size(16, 16).Color(t.MutedForeground),
				T("page.setting.xxmi.builtin.manage"), len(pending)),
			navRow(xxmiReShadePage, reshadeIcon, T("page.setting.xxmi.builtin.reshade.title"), 0),
		).Gap(2),
		importers,
	).W(256).HFull().Shrink(0).PadXY(8, 12).Gap(12).ScrollY().
		Fill(t.Background.Alpha(0.5)).BorderEdges(0, 1, 0, 0, t.Border)
}

func (s *Shell) checkXXMIUpdates() {
	x := &s.xxmi
	var result []xxmi.UpdateStatus
	s.xxmiAction("check-updates", func(ctx context.Context) error {
		var err error
		result, err = s.deps.XXMI.CheckUpdates(ctx, true)
		return err
	}, func() {
		x.refresh()
		x.updates.set(result)
		if len(installableUpdates(result)) == 0 {
			s.toasts.Show(kit.ToastSuccess, T("page.setting.xxmi.builtin.noUpdates"), "")
		}
	}, nil)
}

func (s *Shell) xxmiImporterRow(key string) *fairy.El {
	t := kit.T()
	x := &s.xxmi
	importer := findImporter(x.overview.value, key)
	active := x.page == key

	title := fairy.Row(fairy.Text(key).FontSize(14).LineHeight(20).Medium().Ellipsis().MinW(0)).
		Gap(6).
		Align(fairy.Center)
	var parts []string
	if importer != nil {
		if version := importer.PackageInfo.DeployedVersion; version != "" {
			title.Child(fairy.Text(version).FontSize(12).LineHeight(16).Color(t.MutedForeground).NoWrap().Shrink(0))
		}
		if importer.Running {
			running := T("page.setting.xxmi.builtin.running")
			title.Child(fairy.Box().Size(8, 8).Shrink(0).Radius(4).Fill(t.Success).Label(running))
		}
		parts = append(parts, "XXMI")
		if importer.Mode == xxmi.RuntimeLegacy {
			parts[0] = "3DMigoto"
		}
	}
	if importer == nil || importer.PackageInfo.DeployedVersion == "" {
		parts = append(parts, T("page.setting.xxmi.builtin.notInstalled"))
	}
	if importer != nil && importer.CustomDLL {
		parts = append(parts, T("page.setting.xxmi.builtin.customDll"))
	}

	status := fairy.Text(strings.Join(parts, " · ")).FontSize(12).LineHeight(16).Color(t.MutedForeground).Ellipsis()
	open := fairy.Row(
		s.importerIcon(key, 32),
		fairy.Col(title, status).Gap(2).Grow(1).MinW(0),
	).Grow(1).MinW(0).Gap(10).Pad(8).Align(fairy.Center).Cursor(fairy.CursorPointer).Focusable().FocusRing(3, t.Ring.Alpha(0.5)).
		Label(key).Selected(active).
		OnClick(func() { s.openXXMIPage(key) })
	row := fairy.Row(open).Key("xxmi.importer:"+key).Gap(4).Align(fairy.Center).Radius(6).PadEdges(0, 6, 0, 0).
		Color(t.SidebarForeground).HoverFill(t.SidebarAccent).Animate(motion.Fast)
	if active {
		row.Fill(t.SidebarAccent).Color(t.SidebarAccentForeground)
	}
	if importer == nil {
		return row
	}
	if len(importerUpdates(x.updates.value, key)) > 0 {
		label := T("page.setting.xxmi.builtin.updateAvailable")
		row.Child(kit.IconButton(lucide.CircleArrowUp, func() { x.updateTarget = key }, kit.Ghost, kit.Small,
			kit.Disabled(importer.Running), kit.Tooltip(label)).Label(label).Color(t.Primary).HoverColor(t.Primary))
	}
	launch := T("page.setting.xxmi.builtin.launch")
	return row.Child(kit.IconButton(lucide.Play, func() { s.startImporter(key) }, kit.Ghost, kit.Small,
		kit.Disabled(importer.Running), kit.Loading(x.guard.starting[key]), kit.Tooltip(launch)).Label(launch))
}

// importerIcon shows the importer's custom icon when one is set and readable, and its bundled icon otherwise.
func (s *Shell) importerIcon(key string, size float32) *fairy.El {
	x := &s.xxmi
	icons := fetch(s, &x.icons, s.deps.XXMI.GetImporterIcons)
	var img *fairy.Image
	for _, icon := range icons.value {
		if icon.Key != key || !icon.Custom || icon.Unavailable || icon.Path == "" {
			continue
		}
		// A custom icon is named by its content hash, so the key carries the read that listed it to try a
		// refreshed list again after a failure.
		cacheKey := fmt.Sprint("xxmi.icon:", icon.Path, ":", icons.seq)
		if fairy.Images.SizedErr(cacheKey, size, size, fairy.FitContain) == nil {
			path := icon.Path
			img = fairy.Images.Sized(cacheKey, size, size, fairy.FitContain, func() ([]byte, error) {
				return os.ReadFile(path)
			})
		}
	}
	if img == nil {
		file := strings.ToLower(key) + ".png"
		if !slices.Contains(importerKeys, key) {
			file = "gimi.png"
		}
		img = fairy.Images.Sized("xxmi.asset:"+file, size, size, fairy.FitContain, func() ([]byte, error) {
			return xxmiAssets.ReadFile("assets/xxmi/" + file)
		})
	}
	box := fairy.Box().Size(size, size).Shrink(0).Role(fairy.RoleHidden)
	if img != nil {
		box.Child(fairy.ImageEl(img).WFull().HFull().Fit(fairy.FitContain).Radius(6))
	}
	return box
}

func (s *Shell) xxmiDialogs(cx *fairy.Cx) {
	x := &s.xxmi
	if x.updateTarget != "" {
		key := x.updateTarget
		updates := importerUpdates(x.updates.value, key)
		s.xxmiUpdateDialog(cx, key, updates, T("page.setting.xxmi.builtin.installUpdates"), x.busy["install-updates"],
			func() {
				packages := packagesOf(updates)
				s.xxmiAction("install-updates", func(ctx context.Context) error {
					_, err := s.deps.XXMI.InstallUpdates(ctx, key, packages)
					return err
				}, func() {
					x.updateTarget = ""
					x.refresh()
				}, nil)
			}, func() { x.updateTarget = "" })
	}
	s.launchGuardDialogs(cx)
	s.manageDialogs(cx)
	s.importerDialogs(cx)
	s.reshadeDialogs(cx)
}

func packagesOf(updates []xxmi.UpdateStatus) []string {
	packages := make([]string, 0, len(updates))
	for _, entry := range updates {
		packages = append(packages, entry.Package)
	}
	return packages
}

// xxmiUpdateDialog lists an importer's updates with their release notes before installing them.
func (s *Shell) xxmiUpdateDialog(cx *fairy.Cx, importer string, updates []xxmi.UpdateStatus, confirmLabel string,
	busy bool, onConfirm, onClose func(),
) {
	t := kit.T()
	list := fairy.Col().Gap(12).MaxH(384).ScrollY()
	for _, entry := range updates {
		item := fairy.Col(fairy.Text(updateLine(entry)).Font(monoFont).FontSize(12).LineHeight(16).Wrap()).
			Gap(8).
			Pad(12).
			Radius(8).
			Fill(t.Muted.Alpha(0.5))
		releases := s.packageReleases(entry.Package)
		for _, release := range releases.value {
			if release.Version == entry.LatestVersion && release.Notes != "" {
				item.Child(
					fairy.Col(fairy.Text(release.Notes).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap()).
						MaxH(192).
						ScrollY(),
				)
			}
		}
		list.Child(item)
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title:       T("page.setting.xxmi.builtin.updateDialogTitle", "importer", importer),
		Description: T("page.setting.xxmi.builtin.updateDialogDescription"),
		Body: fairy.Col(list, xxmiDialogButtons(
			kit.Button(T("g.cancel"), onClose, kit.Outline, kit.Disabled(busy)),
			kit.Button(confirmLabel, onConfirm, kit.Icon(lucide.Download), kit.Loading(busy)),
		)).Gap(16),
		Width: 448,
		OnClose: func() {
			if !busy {
				onClose()
			}
		},
		Key: "xxmi.update-dialog",
	})
}

func xxmiDialogButtons(actions ...*fairy.El) *fairy.El {
	if len(actions) > 0 {
		actions[0].AutoFocus()
	}
	return fairy.Row(actions...).Wrap().Gap(8).RowGap(8).Justify(fairy.JustifyEnd)
}

func updateLine(entry xxmi.UpdateStatus) string {
	installed := entry.Installed
	if installed == "" {
		installed = T("page.setting.xxmi.builtin.notInstalled")
	}
	return entry.Package + ": " + installed + " → " + entry.LatestVersion
}

// xxmiAlert is a bordered notice with an icon, an optional title and trailing controls.
func xxmiAlert(icon *svg.Vector, danger bool, title, body string, actions ...*fairy.El) *fairy.El {
	t := kit.T()
	ink := t.Foreground
	if danger {
		ink = t.Destructive
	}
	text := fairy.Col().Gap(2).Grow(1).MinW(0)
	if title != "" {
		text.Child(fairy.Text(title).FontSize(14).LineHeight(20).Medium().Color(ink).Wrap())
	}
	if body != "" {
		bodyInk := t.MutedForeground
		if danger {
			bodyInk = t.Destructive.Alpha(0.9)
		}
		text.Child(fairy.Text(body).FontSize(14).LineHeight(20).Color(bodyInk).Wrap())
	}
	row := fairy.Row(fairy.Icon(icon).Size(16, 16).Color(ink).Shrink(0).MarginEdges(2, 0, 0, 0), text).
		Gap(8).PadXY(10, 8).Align(fairy.Start).Fill(t.Card).Color(t.CardForeground).Radius(8).Border(1, t.Border)
	if len(actions) > 0 {
		row.Child(fairy.Row(actions...).Gap(8).Shrink(0).AlignSelf(fairy.Center))
	}
	return row
}

// toggleRow is a switch row whose nested rows show only while it is on, so disabled inputs never pile up.
func toggleRow(
	title, description string,
	on bool,
	onChange func(bool),
	disabled bool,
	children ...*fairy.El,
) *fairy.El {
	t := kit.T()
	row := settingRow(title, description, kit.Switch(on, onChange, kit.Disabled(disabled)).Label(title))
	if !on || len(children) == 0 {
		return row
	}
	return fairy.Col(row.Pad(0), fairy.Col(children...).Gap(12).Pad(12).Radius(6).Fill(t.Muted.Alpha(0.5))).
		Gap(12).PadXY(0, 12)
}

func selectRow(id, title, description, value string, items []kit.Item, onChange func(string), opts ...kit.Option,
) *fairy.El {
	return settingRow(title, description, kit.Select(id, value, items, onChange,
		append([]kit.Option{kit.Label(title)}, opts...)...).MinW(176).MaxW(320))
}

func plainItems(values ...string) []kit.Item {
	items := make([]kit.Item, 0, len(values))
	for _, value := range values {
		items = append(items, kit.Item{Value: value, Label: value})
	}
	return items
}

// xxmiSettingsLoaded loads the settings the XXMI pages show; until then their rows show nothing.
func (s *Shell) xxmiSettingsLoaded() bool {
	return s.settingsLoaded(setting.KeyXXMIAutoUpdate, setting.KeyXXMIIncludePrereleases, setting.KeyXXMILaunchGuard,
		setting.KeyXXMILaunchGuardDcr, setting.KeyXXMILaunchGuardSmoothMotion, setting.KeyXXMILaunchGuardLogging,
		setting.KeyXXMILaunchGuardTextures)
}

func (s *Shell) launchGuardSettings() *fairy.El {
	if !s.xxmiSettingsLoaded() {
		return fairy.Box()
	}
	const prefix = "page.setting.xxmi.launchGuard."
	var guards []*fairy.El
	for _, guard := range []struct{ key, name string }{
		{setting.KeyXXMILaunchGuardDcr, "dcr"},
		{setting.KeyXXMILaunchGuardSmoothMotion, "smoothMotion"},
		{setting.KeyXXMILaunchGuardLogging, "logging"},
		{setting.KeyXXMILaunchGuardTextures, "textures"},
	} {
		guards = append(guards, toggleRow(T(prefix+guard.name), T(prefix+guard.name+"Description"),
			s.boolSetting(guard.key), func(on bool) { s.setSetting(guard.key, on) }, false).Pad(0))
	}
	return settingsGroup(T(prefix+"title"), toggleRow(T(prefix+"enabled"), T(prefix+"enabledDescription"),
		s.boolSetting(setting.KeyXXMILaunchGuard), func(on bool) { s.setSetting(setting.KeyXXMILaunchGuard, on) },
		false, guards...))
}
