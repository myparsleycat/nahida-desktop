package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"

	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/xxmi"
	"nahida.live/desktop/internal/xxmi/optimizer"
)

// followShared is offered beside the libraries providers and versions, whose names it must not collide with.
const followShared = "__shared__"

const (
	importerTabGeneral  = "general"
	importerTabPackage  = "package"
	importerTabGame     = "game"
	importerTabAdvanced = "advanced"
	importerTabD3DX     = "d3dx"
	importerTabTools    = "tools"
	importerTabRuntimes = "runtimes"

	allSections = "__all__"
)

var modFolderErrorCodes = []string{
	"XXMI_MOD_FOLDER_TARGET_NOT_EMPTY",
	"XXMI_MOD_FOLDER_INVALID_TARGET",
	"XXMI_IMPORT_NO_SPACE",
	"XXMI_GAME_RUNNING",
	"XXMI_BUSY",
}

// importerPage is the draft of one importer's settings. Switching importers starts a new draft.
type importerPage struct {
	key      string
	diag     diagnosticsPage
	runtimes runtimesPage
	draft    *xxmi.ImporterConfig
	// d3dx holds the edited d3dx.ini values by "section.key", saved with the draft.
	d3dx   map[string]string
	tab    string
	saving bool
	// importedDLL is a picked DLL that is only in the draft until it is saved, so the overview does not list it.
	importedDLL *xxmi.CustomDLL
	pendingDLL  *xxmi.CustomDLL
	// pickingDLL shows the custom DLL field before a DLL is picked, which changes nothing in the draft yet.
	pickingDLL    bool
	allowUnsigned bool
	packageDialog string
	optimization  *optimizer.Report
	detected      *[]xxmi.GameFolderCandidate
	numbers       map[string]string

	modFolder       query[xxmi.ImporterModFolder]
	modFolderTarget *string
	d3dxOptions     query[[]xxmi.D3DXOption]
	// d3dxFailure keeps the failed read on screen while Retry reads the options again.
	d3dxFailure  error
	d3dxSearch   string
	d3dxStatus   string
	d3dxSection  string
	verification query[*xxmi.PackageVerification]
}

type packageInstall struct {
	version       string
	allowUnsigned bool
}

func cloneConfig(c xxmi.ImporterConfig) xxmi.ImporterConfig {
	data, err := json.Marshal(c)
	if err != nil {
		return c
	}
	var out xxmi.ImporterConfig
	if err := json.Unmarshal(data, &out); err != nil {
		return c
	}
	return out
}

func sameConfig(a, b xxmi.ImporterConfig) bool {
	left, errLeft := json.Marshal(a)
	right, errRight := json.Marshal(b)
	return errLeft == nil && errRight == nil && bytes.Equal(left, right)
}

func normalizedFolder(folder string) string {
	return strings.ToLower(strings.TrimRight(strings.ReplaceAll(folder, "/", `\`), `\`))
}

func (p *importerPage) reset(key string) {
	*p = importerPage{key: key, tab: importerTabGeneral, numbers: map[string]string{}, d3dxStatus: "all",
		d3dxSection: allSections}
}

func (p *importerPage) dirty(saved *query[xxmi.ImporterConfig]) bool {
	return p.draft != nil && saved.loaded && !sameConfig(*p.draft, saved.value) || len(p.d3dx) > 0
}

func (p *importerPage) discard() {
	p.draft, p.d3dx, p.pickingDLL = nil, nil, false
}

// leaveImporterPage asks before dropping unsaved importer settings and runs proceed once the user agrees. It
// reports whether it took over the navigation.
func (s *Shell) leaveImporterPage(proceed func()) bool {
	x := &s.xxmi
	p := &x.importer
	if p.key == "" || x.page != p.key {
		return false
	}
	if !p.saving && !p.dirty(keyed(x.configs, p.key)) {
		return false
	}
	const prefix = "page.setting.xxmi.builtin."
	s.Confirm(Confirmation{
		Title: T(prefix + "leaveTitle"), Description: T(prefix + "leaveDescription"),
		ConfirmLabel: T(prefix + "leaveConfirm"), Danger: true,
		OnResult: func(confirmed bool) {
			if confirmed {
				p.discard()
				proceed()
			}
		},
	})
	return true
}

// importerContext holds what the page derives from the saved config, the draft and the overview.
type importerContext struct {
	s      *Shell
	p      *importerPage
	key    string
	saved  *query[xxmi.ImporterConfig]
	c      xxmi.ImporterConfig
	ov     xxmi.Overview
	entry  *xxmi.EnabledImporter
	custom bool
}

func (ic *importerContext) set(change func(*xxmi.ImporterConfig)) {
	next := cloneConfig(ic.c)
	change(&next)
	ic.p.draft = &next
	ic.c = next
}

func (ic *importerContext) selectedDLL(c xxmi.ImporterConfig) string {
	switch {
	case c.Mode != xxmi.RuntimeXXMI:
		return ""
	case c.CustomDLL != "":
		return c.CustomDLL
	case c.LibsProvider == "":
		return ic.ov.SharedCustomDLL
	}
	return ""
}

func (ic *importerContext) appliedDLL(c xxmi.ImporterConfig) string {
	if !c.Migoto.UnsafeMode {
		return ""
	}
	return ic.selectedDLL(c)
}

// launchesCustomDLL accounts for a DLL kept in the importer folder, which the overview reports for the saved
// config and which outlives a draft that drops the selection.
func (ic *importerContext) launchesCustomDLL(c xxmi.ImporterConfig) bool {
	kept := ic.custom && c.Migoto.UnsafeMode && (!ic.saved.loaded || ic.appliedDLL(ic.saved.value) == "")
	return ic.appliedDLL(c) != "" || kept
}

// deployedProvider is the provider whose libraries the importer deploys. A legacy runtime and a user-provided DLL
// deploy the first provider's signed libraries.
func (ic *importerContext) deployedProvider(c xxmi.ImporterConfig) string {
	if c.Mode == xxmi.RuntimeXXMI && !ic.launchesCustomDLL(c) {
		if c.LibsProvider != "" {
			return c.LibsProvider
		}
		return ic.ov.SharedLibsProvider
	}
	if len(ic.ov.LibsProviders) > 0 {
		return ic.ov.LibsProviders[0]
	}
	return ""
}

func (ic *importerContext) installedVersion() string {
	if ic.entry != nil && ic.entry.InstalledVersion != nil {
		return *ic.entry.InstalledVersion
	}
	if v := ic.p.verification.value; v != nil {
		return v.Version
	}
	return ""
}

func (ic *importerContext) isInstalledPackage(version string) bool {
	installed := ic.installedVersion()
	folder := ic.saved.value.ImporterFolder
	if ic.entry != nil {
		folder = ic.entry.ImporterFolder
	}
	return installed != "" && normalizedFolder(ic.c.ImporterFolder) == normalizedFolder(folder) &&
		sameVersion(version, installed)
}

func (s *Shell) importerSettingsPage(key string) *fairy.El {
	t := kit.T()
	x := &s.xxmi
	p := &x.importer
	if p.key != key {
		p.reset(key)
	}
	saved := s.importerConfig(key)
	if !saved.loaded {
		return fairy.Box().Grow(1)
	}
	fetch(s, &p.verification, func(ctx context.Context) (*xxmi.PackageVerification, error) {
		return s.deps.XXMI.GetImporterPackageVerification(ctx, key)
	})

	ic := &importerContext{s: s, p: p, key: key, saved: saved, c: saved.value, ov: x.overview.value}
	if p.draft != nil {
		ic.c = *p.draft
	}
	ic.entry = findImporter(ic.ov, key)
	ic.custom = ic.entry != nil && ic.entry.CustomDLL

	const prefix = "page.setting.xxmi.builtin."
	dirty := p.dirty(saved)
	pinned := ic.c.PackageVersion.Pinned
	needsInstall := pinned != "" && !ic.isInstalledPackage(pinned)
	// A disabled importer never launches, so it can be saved, and so turned off, without its package.
	saveBlocked := ic.c.Enabled && needsInstall

	launchTip := ""
	if dirty {
		launchTip = T(prefix + "unsavedChanges")
	}
	header := fairy.Col(fairy.Row(
		s.importerIcon(key, 28),
		// The React title inherited the 16px body size.
		fairy.Text(key).FontSize(16).LineHeight(24).SemiBold().Grow(1),
		kit.Button(
			T(prefix+"launch"),
			func() { s.startImporter(key) },
			kit.Outline,
			kit.Icon(lucide.Play),
			kit.Disabled(
				dirty || needsInstall || p.saving,
			),
			kit.Loading(x.guard.starting[key]),
			kit.Tooltip(launchTip),
		),
		kit.Button(T("g.save"), func() { s.saveImporter(ic, nil) }, kit.Disabled(!dirty || saveBlocked),
			kit.Loading(p.saving)),
	).Gap(8).Align(fairy.Center)).Gap(12)
	if saveBlocked {
		header.Child(xxmiAlert(lucide.TriangleAlert, false, "", T(prefix+"packageInstallRequired")))
	}
	if notice := s.diagnosticsNotice(key, func() { p.tab = importerTabTools }); notice != nil {
		header.Child(notice)
	}
	tabs := []kit.Item{
		{Value: importerTabGeneral, Label: T(prefix + "general")},
		{Value: importerTabPackage, Label: T(prefix + "packageTab")},
	}
	hasGameTweaks := ic.c.GIMI != nil || ic.c.SRMI != nil || ic.c.HIMI != nil || ic.c.WWMI != nil
	if hasGameTweaks {
		tabs = append(tabs, kit.Item{Value: importerTabGame, Label: T(prefix + "gameTweaks")})
	}
	tabs = append(tabs,
		kit.Item{Value: importerTabAdvanced, Label: T(prefix + "advanced")},
		kit.Item{Value: importerTabD3DX, Label: "d3dx.ini"},
		kit.Item{Value: importerTabTools, Label: T(prefix + "tools")},
	)
	hasRuntimes := s.deps.ModRuntime.Supports(key)
	if hasRuntimes {
		tabs = append(tabs, kit.Item{Value: importerTabRuntimes, Label: T("page.setting.xxmi.modRuntimes.tab")})
	}
	if p.tab == importerTabGame && !hasGameTweaks || p.tab == importerTabRuntimes && !hasRuntimes {
		p.tab = importerTabGeneral
	}
	header.Child(xxmiTabs(p.tab, tabs, func(value string) { p.tab = value }, true))

	var body []*fairy.El
	switch p.tab {
	case importerTabPackage:
		body = s.importerPackageTab(ic)
	case importerTabGame:
		body = s.importerGameTab(ic)
	case importerTabAdvanced:
		body = s.importerAdvancedTab(ic)
	case importerTabD3DX:
		body = s.importerD3DXTab(ic)
	case importerTabTools:
		body = s.importerToolsTab(ic)
	case importerTabRuntimes:
		body = s.modRuntimesTab(ic)
	default:
		body = s.importerGeneralTab(ic)
	}

	content := fairy.Col(body...).Key("xxmi.importer-tab:" + p.tab).WFull().MaxW(672).Pad(16).Gap(24)
	if dirty {
		content.PadEdges(16, 16, 64, 16)
	}
	bodyHost := fairy.Box(stableScrollY(fairy.Col(content).Grow(1).MinH(0).WFull().Align(fairy.Center))).Grow(1).
		MinH(0).WFull()
	page := fairy.Col(
		fairy.Col(header.WFull().MaxW(672).PadEdges(16, 16, 1, 16)).Shrink(0).WFull().Align(fairy.Center).
			PadEdges(0, xxmiScrollGutter, 0, 0).BorderEdges(0, 0, 1, 0, t.Border),
		bodyHost,
	).Grow(1).MinH(0).WFull()
	if p.saving {
		page.Disabled(true)
	}
	if dirty {
		notice := fairy.Row(
			fairy.Text(T(prefix+"unsavedChanges")).FontSize(12).LineHeight(16).Grow(1),
			kit.Button(T(prefix+"discard"), p.discard, kit.Ghost, kit.XS, kit.Disabled(p.saving)),
		).PadXY(12, 6).Gap(12).Align(fairy.Center).Radius(6).Fill(t.Popover).Color(t.PopoverForeground).
			Border(1, t.Border).Shadow(0, 4, 6, -1, fairy.RGBA(0, 0, 0, 0.1)).
			Shadow(0, 2, 4, -2, fairy.RGBA(0, 0, 0, 0.1))
		bodyHost.Child(
			fairy.Col(fairy.Col(notice).WFull().MaxW(672).PadXY(16, 0)).Left(0).Right(xxmiScrollGutter).Bottom(16).
				Align(fairy.Center),
		)
	}
	return page
}

// saveImporter saves the draft, or next when installing a package with it. The d3dx.ini values go in first, so
// the settings the save writes win over one for an option they manage.
func (s *Shell) saveImporter(ic *importerContext, install *packageInstall) {
	x := &s.xxmi
	p := ic.p
	if p.saving {
		return
	}
	next := ic.c
	if install == nil && next.Enabled && next.PackageVersion.Pinned != "" &&
		!ic.isInstalledPackage(next.PackageVersion.Pinned) {
		return
	}
	key, saved := ic.key, ic.saved.value
	d3dx := p.d3dx
	nextProvider, savedProvider := ic.deployedProvider(next), ic.deployedProvider(saved)
	sharedVersion := ic.ov.SharedLibsVersion
	p.saving = true
	s.background(func(ctx context.Context) {
		err := func() error {
			// A disabled importer never launches, so a game folder that no longer holds the game must not keep it
			// from being turned off.
			if next.GameFolder != "" && next.Enabled {
				folder, err := s.deps.XXMI.ValidateGameFolder(ctx, key, next.GameFolder)
				if err != nil {
					return err
				}
				next.GameFolder = folder.Path
			}
			if len(d3dx) > 0 {
				if err := s.deps.XXMI.SetD3DXOptions(ctx, key, d3dx); err != nil {
					return err
				}
			}
			if install != nil {
				return s.deps.XXMI.InstallImporterPackage(ctx, xxmi.InstallImporterPackageInput{
					Importer: key, Version: install.version, AllowUnsigned: install.allowUnsigned, Config: &next,
				})
			}
			pin := next.XXMIVersion.Pinned
			pinChanged := pin != "" && pin != saved.XXMIVersion.Pinned
			if pinChanged {
				if err := s.deps.XXMI.EnsureLibsVersion(ctx, pin); err != nil {
					return err
				}
			}
			if nextProvider != "" && (pinChanged || nextProvider != savedProvider) {
				if next.XXMIVersion.Follow == "shared" {
					pin = sharedVersion
				}
				if err := s.deps.XXMI.EnsureLibsProvider(ctx, nextProvider, pin); err != nil {
					return err
				}
			}
			return s.deps.XXMI.SaveImporterConfig(ctx, key, next)
		}()
		fairy.Go(func() {
			p.saving = false
			if err != nil {
				message := importerFolderMessage(err)
				if message == "" && strings.Contains(err.Error(), "XXMI_BUSY") {
					message = T("page.setting.xxmi.builtin.d3dx.busy")
				}
				s.xxmiFail(err, "save-importer", map[string]any{"importer": key}, message)
				return
			}
			if p.key == key {
				p.packageDialog, p.draft, p.d3dx, p.pickingDLL = "", nil, nil, false
				p.verification.invalidate()
				p.d3dxOptions.invalidate()
				p.modFolder.invalidate()
			}
			x.refresh()
			// Listing games makes the backend follow a moved importer folder with the games that use it.
			if s.mods.started {
				s.loadGames(false)
			}
		})
	})
}

// numberBounds limits a number field as the React input's min and max did; has* leave a side open.
type numberBounds struct {
	low, high       int
	hasLow, hasHigh bool
}

func atLeast(low int) numberBounds      { return numberBounds{low: low, hasLow: true} }
func within(low, high int) numberBounds { return numberBounds{low, high, true, true} }

func (b numberBounds) clamp(n int) int {
	if b.hasLow {
		n = max(n, b.low)
	}
	if b.hasHigh {
		n = min(n, b.high)
	}
	return n
}

func (s *Shell) numberRow(
	ic *importerContext, id, title, description string, value int, bounds numberBounds, onChange func(int),
) *fairy.El {
	p := ic.p
	text, editing := p.numbers[id]
	if !editing {
		text = strconv.Itoa(value)
	}
	field := kit.TextField(text, func(next string) {
		// A partial entry such as "-" stays as typed instead of committing as 0.
		p.numbers[id] = next
		if n, err := strconv.Atoi(strings.TrimSpace(next)); err == nil {
			onChange(n)
		}
	}, kit.Label(title)).Key("xxmi.number:" + id).W(112).Grow(0).OnBlur(func() {
		delete(p.numbers, id)
		if clamped := bounds.clamp(value); clamped != value {
			onChange(clamped)
		}
	})
	field = onArrowStep(field, func(direction int) {
		delete(p.numbers, id)
		onChange(bounds.clamp(value + direction))
	})
	return settingRow(title, description, field)
}

func textRow(id, title, description, value string, onChange func(string)) *fairy.El {
	t := kit.T()
	head := fairy.Col(fairy.Text(title).FontSize(14).LineHeight(20).Medium()).Gap(2)
	if description != "" {
		head.Child(fairy.Text(description).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap())
	}
	return fairy.Col(head, kit.TextField(value, onChange, kit.Label(title)).Key(id).Font(monoFont)).Gap(6).PadXY(0, 12)
}

func (s *Shell) importerGeneralTab(ic *importerContext) []*fairy.El {
	t := kit.T()
	x := &s.xxmi
	p := ic.p
	c := ic.c
	key := ic.key
	const prefix = "page.setting.xxmi.builtin."

	gameFolder := s.pathField("xxmi.game-folder", T(prefix+"gameFolder"), T(prefix+"gameFolderDescription"),
		c.GameFolder, func(value string) { ic.set(func(c *xxmi.ImporterConfig) { c.GameFolder = value }) },
		kit.Button(T(prefix+"detectGame"), func() {
			s.detectGameFolders(key, func(found []xxmi.GameFolderCandidate) {
				if p.key == key {
					p.detected = &found
				}
			})
		}, kit.Outline, kit.Icon(xxmiScanSearch), kit.Loading(x.busy["detect-game:"+key])))
	general := []*fairy.El{
		s.importerIconField(key),
		toggleRow(T(prefix+"enabled"), "", c.Enabled,
			func(on bool) { ic.set(func(c *xxmi.ImporterConfig) { c.Enabled = on }) }, false),
		s.pathField("xxmi.importer-folder", T(prefix+"importerFolder"), T(prefix+"importerFolderDescription"),
			c.ImporterFolder, func(value string) { ic.set(func(c *xxmi.ImporterConfig) { c.ImporterFolder = value }) }),
		s.modFolderField(ic),
		gameFolder,
		settingRow(T(prefix+"mode"), T(prefix+"modeDescription"), xxmiButtonGroup(string(c.Mode), "", []kit.Item{
			{Value: string(xxmi.RuntimeXXMI), Label: "XXMI"},
			{Value: string(xxmi.RuntimeLegacy), Label: "3DMigoto"},
		}, func(value string) { ic.set(func(c *xxmi.ImporterConfig) { c.Mode = xxmi.RuntimeMode(value) }) }, kit.SizeDefault)).
			Wrap(),
	}
	if c.Mode == xxmi.RuntimeLegacy {
		legacy := fetch(s, &x.legacy, s.deps.XXMI.GetLegacyRuntimes)
		items := []kit.Item{{Value: followLatest, Label: T(prefix + "newestCached")}}
		for _, runtime := range legacy.value {
			items = append(items, kit.Item{Value: runtime.ID, Label: runtime.ID})
		}
		value := c.LegacyRuntime
		if value == "" {
			value = followLatest
		}
		general = append(general, fairy.Col(
			selectRow("xxmi.legacy-runtime", T(prefix+"legacy"), "", value, items, func(value string) {
				if value == followLatest {
					value = ""
				}
				ic.set(func(c *xxmi.ImporterConfig) { c.LegacyRuntime = value })
			}).Pad(0),
			xxmiAlert(lucide.TriangleAlert, false, "", T(prefix+"legacyWarning")),
		).Gap(12).Pad(12).Radius(6).Fill(t.Muted.Alpha(0.5)))
	}

	launches := []kit.Item{}
	for _, value := range []string{"Direct", "Steam", "Epic", "Custom", "Manual"} {
		launches = append(launches, kit.Item{Value: value, Label: T(prefix + "gameLaunch" + value)})
	}
	launch := []*fairy.El{selectRow("xxmi.game-launch", T(prefix+"gameLaunch"), T(prefix+"gameLaunchDescription"),
		c.GameLaunch, launches, func(value string) { ic.set(func(c *xxmi.ImporterConfig) { c.GameLaunch = value }) })}
	nested := func(children ...*fairy.El) *fairy.El {
		return fairy.Col(children...).Gap(12).Pad(12).Radius(6).Fill(t.Muted.Alpha(0.5))
	}
	if c.EFMI != nil && c.GameLaunch == "Direct" {
		launch = append(launch, nested(s.pathField("xxmi.launcher-folder", T(prefix+"launcherFolder"),
			T(prefix+"launcherFolderDescription"), c.EFMI.LauncherFolder, func(value string) {
				ic.set(func(c *xxmi.ImporterConfig) { c.EFMI = &xxmi.EFMIOptions{LauncherFolder: value} })
			}).Pad(0)))
	}
	if c.GameLaunch == "Custom" {
		launch = append(launch, nested(
			textRow("xxmi.custom-launch", T(prefix+"customLaunch"), T(prefix+"customLaunchDescription"),
				c.CustomLaunch.Command, func(value string) {
					ic.set(func(c *xxmi.ImporterConfig) { c.CustomLaunch.Command = value })
				}).Pad(0),
			xxmiAlert(lucide.ShieldAlert, false, "", T(prefix+"elevatedCommandWarning")),
		))
	}
	if c.GameLaunch == "Steam" {
		steam := []*fairy.El{toggleRow(T(prefix+"configurePlatformLaunchOptions"),
			T(prefix+"configurePlatformLaunchOptionsDescription"), c.ConfigurePlatformLaunchOptions, func(on bool) {
				ic.set(func(c *xxmi.ImporterConfig) { c.ConfigurePlatformLaunchOptions = on })
			}, false).Pad(0)}
		if key == "ZZMI" {
			steam = append(steam, toggleRow(T(prefix+"skipPlatformGameLauncher"),
				T(prefix+"skipPlatformGameLauncherDescription"), c.SkipPlatformGameLauncher, func(on bool) {
					ic.set(func(c *xxmi.ImporterConfig) { c.SkipPlatformGameLauncher = on })
				}, !c.ConfigurePlatformLaunchOptions).Pad(0))
		}
		launch = append(launch, nested(steam...))
	}
	if c.GIMI != nil && c.GIMI.UnlockFPS {
		launch = append(launch, selectRow("xxmi.window-mode", T(prefix+"windowMode"), T(prefix+"windowModeDescription"),
			c.WindowMode, plainItems("Windowed", "Borderless", "Fullscreen", "Exclusive Fullscreen"),
			func(value string) { ic.set(func(c *xxmi.ImporterConfig) { c.WindowMode = value }) }))
	}
	if key == "WWMI" || key == "EFMI" || key == "ZZMI" {
		other := T(prefix + "rendererGameDefault")
		if key == "ZZMI" {
			other = "DirectX 12"
		}
		renderer := "d3d11"
		if c.SkipD3D11Switch {
			renderer = "other"
		}
		launch = append(launch, selectRow("xxmi.renderer", T(prefix+"renderer"), T(prefix+"rendererDescription"),
			renderer, []kit.Item{{Value: "d3d11", Label: "DirectX 11"}, {Value: "other", Label: other}},
			func(value string) { ic.set(func(c *xxmi.ImporterConfig) { c.SkipD3D11Switch = value != "d3d11" }) }))
	}
	launchOptionsDescription := ""
	if c.WWMI != nil {
		launchOptionsDescription = T(prefix + "launchOptionsDescriptionWWMI")
	}
	launch = append(launch, toggleRow(T(prefix+"useLaunchOptions"), launchOptionsDescription, c.UseLaunchOptions,
		func(on bool) { ic.set(func(c *xxmi.ImporterConfig) { c.UseLaunchOptions = on }) }, false,
		kit.TextField(c.LaunchOptions, func(value string) {
			ic.set(func(c *xxmi.ImporterConfig) { c.LaunchOptions = value })
		}, kit.Label(T(prefix+"useLaunchOptions"))).Key("xxmi.launch-options").Font(monoFont)))

	// The 3DMigoto loader injects on its own schedule, so ReShade cannot be loaded ahead of it.
	reshadeUnsupported := c.Mode != xxmi.RuntimeXXMI && c.InjectionMethod != "Native"
	reshadeDescription := T(prefix + "reshade.toggleDescription")
	if reshadeUnsupported {
		reshadeDescription = T(prefix + "reshade.legacyUnsupported")
	}
	launch = append(launch, toggleRow(T(prefix+"reshade.title"), reshadeDescription, c.ReShade.Enabled,
		func(on bool) { ic.set(func(c *xxmi.ImporterConfig) { c.ReShade.Enabled = on }) },
		reshadeUnsupported && !c.ReShade.Enabled,
		settingRow("", T(prefix+"reshade.managePageDescription"), kit.Button(T(prefix+"reshade.managePage"),
			func() { s.openXXMIPage(xxmiReShadePage) }, kit.Outline, kit.Small)).Pad(0)))

	injection := c.InjectionMethod
	if injection == "" {
		injection = "Default"
	}
	launch = append(launch,
		selectRow("xxmi.start-method", T(prefix+"startMethod"), T(prefix+"startMethodDescription"),
			c.ProcessStartMethod, plainItems("Native", "Shell"),
			func(value string) { ic.set(func(c *xxmi.ImporterConfig) { c.ProcessStartMethod = value }) }),
		selectRow("xxmi.priority", T(prefix+"priority"), "", c.ProcessPriority,
			plainItems("Low", "BelowNormal", "Normal", "AboveNormal", "High", "Realtime"),
			func(value string) { ic.set(func(c *xxmi.ImporterConfig) { c.ProcessPriority = value }) }),
		selectRow("xxmi.inject-mode", T(prefix+"injectMode"), T(prefix+"injectModeDescription"),
			c.XXMIDLLInjectMode, plainItems("Hook", "Inject", "Bypass"),
			func(value string) { ic.set(func(c *xxmi.ImporterConfig) { c.XXMIDLLInjectMode = value }) }),
		selectRow("xxmi.injection-method", T(prefix+"injectionMethod"), T(prefix+"injectionMethodDescription"),
			injection, []kit.Item{
				{Value: "Default", Label: T(prefix + "injectionDefault")},
				{Value: "Native", Label: T(prefix + "injectionNative")},
			}, func(value string) { ic.set(func(c *xxmi.ImporterConfig) { c.InjectionMethod = value }) }),
		s.numberRow(ic, "timeout", T(prefix+"timeout"), T(prefix+"timeoutDescription"), c.ProcessTimeout,
			atLeast(0), func(n int) { ic.set(func(c *xxmi.ImporterConfig) { c.ProcessTimeout = n }) }),
		s.numberRow(ic, "init-delay", T(prefix+"initDelay"), T(prefix+"initDelayDescription"), c.XXMIDLLInitDelay,
			atLeast(0), func(n int) { ic.set(func(c *xxmi.ImporterConfig) { c.XXMIDLLInitDelay = n }) }),
	)
	return []*fairy.El{settingsGroup("", general...), settingsGroup(T(prefix+"launchOptions"), launch...)}
}

func (s *Shell) importerIconField(key string) *fairy.El {
	t := kit.T()
	x := &s.xxmi
	const prefix = "page.setting.xxmi.builtin."
	var icon *xxmi.ImporterIcon
	for i := range x.icons.value {
		if x.icons.value[i].Key == key {
			icon = &x.icons.value[i]
		}
	}
	unavailable := x.icons.err != nil || icon != nil && icon.Unavailable
	text := fairy.Col(
		fairy.Text(T(prefix+"importerIcon")).FontSize(14).LineHeight(20).Medium(),
		fairy.Text(T(prefix+"importerIconDescription")).FontSize(12).LineHeight(16).Color(t.MutedForeground),
	).Gap(2).Grow(1).MinW(0)
	if unavailable {
		text.Child(fairy.Text(T(prefix + "importerIconLoadFailed")).FontSize(12).LineHeight(16).Color(t.Destructive))
	}
	apply := func(id string, change func(context.Context) (xxmi.ImporterIconChange, error)) {
		var result xxmi.ImporterIconChange
		s.xxmiAction(id, func(ctx context.Context) error {
			var err error
			result, err = change(ctx)
			return err
		}, func() {
			x.icons.invalidate()
			message := prefix + "importerIconChanged"
			if result.ShortcutUpdated {
				message = prefix + "importerIconShortcutUpdated"
			}
			s.toasts.Show(kit.ToastSuccess, T(message), "")
		}, func(err error) string {
			fairy.Go(x.icons.invalidate)
			if strings.Contains(err.Error(), "XXMI_SHORTCUT_ICON_INVALID") {
				return T(prefix + "importerIconInvalid")
			}
			return ""
		})
	}
	actions := fairy.Row().Gap(8).Shrink(0)
	if icon != nil && icon.Custom || x.icons.err != nil {
		actions.Child(kit.Button(T(prefix+"importerIconReset"), func() {
			apply("reset-icon:"+key, func(ctx context.Context) (xxmi.ImporterIconChange, error) {
				return s.deps.XXMI.ResetImporterIcon(ctx, key)
			})
		}, kit.Outline, kit.Small, kit.Icon(lucide.RotateCcw), kit.Loading(x.busy["reset-icon:"+key])))
	}
	actions.Child(kit.Button(T(prefix+"importerIconChange"), func() {
		s.pickFile(platform.OpenDialogOptions{
			Title: T(prefix + "importerIconChange"),
			Filters: []platform.FileFilter{{
				Name: T(prefix + "importerIconFilter"), Extensions: []string{"png", "jpg", "jpeg", "webp", "avif"},
			}},
			Properties: []string{"openFile"}, Role: "xxmi-importer-icon",
		}, func(paths []string) {
			apply("set-icon:"+key, func(ctx context.Context) (xxmi.ImporterIconChange, error) {
				return s.deps.XXMI.SetImporterIcon(ctx, key, paths[0])
			})
		})
	}, kit.Outline, kit.Small, kit.Icon(lucide.ImageUp), kit.Loading(x.busy["set-icon:"+key])))
	return fairy.Row(fairy.Row(s.importerIcon(key, 40), text).Gap(12).Grow(1).MinW(0).Align(fairy.Center), actions).
		Gap(24).Align(fairy.Center).PadXY(0, 12)
}

// modFolderField shows where the importer's Mods folder points. XXMI reads only <importer folder>\Mods, so a folder
// elsewhere is the target of a junction there.
func (s *Shell) modFolderField(ic *importerContext) *fairy.El {
	t := kit.T()
	p := ic.p
	key := ic.key
	const prefix = "page.setting.xxmi.builtin."
	folder := fetch(s, &p.modFolder, func(ctx context.Context) (xxmi.ImporterModFolder, error) {
		return s.deps.XXMI.GetImporterModFolder(ctx, key)
	})
	if !folder.loaded {
		return fairy.Box()
	}
	f := folder.value
	linked := f.LinkTarget != ""
	disabledReason := ""
	if normalizedFolder(ic.c.ImporterFolder) != normalizedFolder(ic.saved.value.ImporterFolder) {
		disabledReason = T(prefix + "unsavedChanges")
	}
	label := T(prefix + "modFolder")
	title := fairy.Row(fairy.Text(label).FontSize(14).LineHeight(20).Medium()).Gap(8).Align(fairy.Center)
	description := T(prefix + "modFolderDescription")
	value := f.Path
	if linked {
		variant := kit.Secondary
		if f.TargetMissing {
			variant = kit.Destructive
		}
		title.Child(kit.Badge(T(prefix+"modFolderLinked"), variant, kit.Icon(lucide.Link)))
		description = T(prefix+"modFolderLinkedDescription", "path", f.Path)
		value = f.LinkTarget
	}
	change := T(prefix + "modFolderChange")
	if disabledReason != "" {
		change = disabledReason
	}
	row := fairy.Row(
		kit.TextField(value, nil, kit.Label(label)).Key("xxmi.mod-folder:"+key).Grow(1).MinW(0),
		kit.IconButton(lucide.FolderOpen, func() {
			s.pickFile(
				platform.OpenDialogOptions{Title: label, DefaultPath: value, Properties: []string{"openDirectory"}},
				func(paths []string) { p.modFolderTarget = &paths[0] },
			)
		}, kit.Outline, kit.Disabled(disabledReason != ""), kit.Tooltip(change)).Label(T(prefix+"modFolderChange")),
	).Gap(8).Align(fairy.Center)
	if linked {
		unlink := T(prefix + "modFolderUnlink")
		if disabledReason != "" {
			unlink = disabledReason
		}
		row.Child(kit.IconButton(lucide.Unlink, func() {
			empty := ""
			p.modFolderTarget = &empty
		}, kit.Outline, kit.Disabled(disabledReason != ""), kit.Tooltip(unlink)).Label(T(prefix + "modFolderUnlink")))
	}
	field := fairy.Col(fairy.Col(title, fairy.Text(description).FontSize(12).LineHeight(16).
		Color(t.MutedForeground).Wrap()).Gap(2), row).
		Gap(6).PadXY(0, 12)
	if f.TargetMissing {
		field.Child(xxmiAlert(lucide.TriangleAlert, true, "", T(prefix+"modFolderTargetMissing")))
	}
	return field
}

func (s *Shell) applyModFolder(key, target string) {
	x := &s.xxmi
	p := &x.importer
	var next xxmi.ImporterModFolder
	s.xxmiAction("mod-folder:"+key, func(ctx context.Context) error {
		var err error
		next, err = s.deps.XXMI.SetImporterModFolder(ctx, key, target)
		return err
	}, func() {
		if p.key == key {
			p.modFolder.set(next)
			p.modFolderTarget = nil
		}
		message := "page.setting.xxmi.builtin.modFolderUnlinked"
		if target != "" {
			message = "page.setting.xxmi.builtin.modFolderChanged"
		}
		s.toasts.Show(kit.ToastSuccess, T(message), "")
		for _, warning := range next.Warnings {
			s.toasts.Show(kit.ToastWarning, warning, "")
		}
		// The game keeps the Mods path while what it holds changes, so its listings are read again.
		if s.mods.started {
			s.loadGames(false)
		}
	}, func(err error) string {
		if code := firstCode(err.Error(), modFolderErrorCodes); code != "" {
			return T("page.setting.xxmi.builtin.modFolderErrors." + code)
		}
		return ""
	})
}

func (s *Shell) importerPackageTab(ic *importerContext) []*fairy.El {
	t := kit.T()
	p := ic.p
	c := ic.c
	ov := ic.ov
	const prefix = "page.setting.xxmi.builtin."

	xxmiRuntime := c.Mode == xxmi.RuntimeXXMI
	followsShared := c.XXMIVersion.Follow == "shared"
	selectedDLL := ic.selectedDLL(c)
	ownsDLL := c.CustomDLL != "" || p.pickingDLL
	selectedName := ""
	for _, dll := range ov.CustomDLLs {
		if dll.ID == selectedDLL {
			selectedName = dll.Name
		}
	}
	if p.importedDLL != nil && p.importedDLL.ID == selectedDLL {
		selectedName = p.importedDLL.Name
	}

	// One choice covers the provider and its version: the shared settings, a provider, or a custom DLL. A config
	// saved while both followed the shared settings separately shows the provider it deploys.
	source := c.LibsProvider
	switch {
	case ownsDLL:
		source = customDLLSource
	case source != "":
	case followsShared || ov.SharedCustomDLL != "" || ov.SharedLibsProvider == "":
		source = followShared
	default:
		source = ov.SharedLibsProvider
	}
	sharedSettings := ""
	if ov.SharedLibsProvider != "" {
		if ov.SharedCustomDLL != "" {
			name := selectedName
			if name == "" {
				name = ov.SharedCustomDLL
			}
			sharedSettings = T(prefix+"customDll") + " · " + name
		} else {
			version := ov.SharedLibsVersion
			if version == "" {
				version = T(prefix + "latest")
			}
			sharedSettings = T(prefix+"libsProviders."+ov.SharedLibsProvider) + " · " + version
		}
	}
	// Only a provider has releases to choose from. A pin left from another injection method is still tracked on
	// the legacy runtime, so it stays reachable.
	choosesVersion := c.InjectionMethod != "Native" || c.XXMIVersion.Pinned != ""
	if xxmiRuntime {
		choosesVersion = source != followShared && source != customDLLSource && !ic.launchesCustomDLL(c)
	}

	var sections []*fairy.El
	if xxmiRuntime || choosesVersion {
		var rows []*fairy.El
		if xxmiRuntime {
			providers := []kit.Item{{Value: followShared, Label: T(prefix + "libsFollowSharedSettings")}}
			for _, provider := range ov.LibsProviders {
				providers = append(providers, kit.Item{Value: provider, Label: T(prefix + "libsProviders." + provider)})
			}
			providers = append(providers, kit.Item{Value: customDLLSource, Label: T(prefix + "customDll")})
			description := ""
			if source == followShared && sharedSettings != "" {
				description = T(prefix+"libsSharedSettings", "settings", sharedSettings)
			}
			current := ic.deployedProvider(c)
			rows = append(
				rows,
				selectRow("xxmi.libs-provider", T(prefix+"libsProvider"), description, source, providers,
					func(value string) {
						p.pickingDLL = value == customDLLSource
						if value == customDLLSource {
							return
						}
						if value == followShared {
							ic.set(func(c *xxmi.ImporterConfig) {
								c.LibsProvider, c.CustomDLL = "", ""
								c.XXMIVersion = xxmi.VersionPin{Follow: "shared"}
							})
							return
						}
						ic.set(func(c *xxmi.ImporterConfig) {
							c.LibsProvider, c.CustomDLL = value, ""
							// A pinned version names a release of the provider it was picked from, so only that provider
							// keeps the version in effect, the shared one included.
							keeps := ic.deployedProvider(*c) == current
							version := c.XXMIVersion
							if followsShared {
								keeps = value == ov.SharedLibsProvider
								version = xxmi.VersionPin{Follow: "latest"}
								if ov.SharedLibsVersion != "" {
									version = xxmi.VersionPin{Pinned: ov.SharedLibsVersion}
								}
							}
							if !keeps {
								version = xxmi.VersionPin{Follow: "latest"}
							}
							c.XXMIVersion = version
						})
					}),
			)
		}
		if xxmiRuntime && ownsDLL {
			name := ""
			if c.CustomDLL != "" {
				name = selectedName
			}
			rows = append(rows, fairy.Col(s.customDLLField("xxmi.importer-dll", c.CustomDLL, name,
				s.xxmi.busy["importer-dll:"+ic.key], func(path string) { s.pickImporterDLL(ic, path) })).
				Pad(12).Radius(6).Fill(t.Muted.Alpha(0.5)))
		}
		if selectedDLL != "" && !c.Migoto.UnsafeMode {
			rows = append(rows, xxmiAlert(lucide.TriangleAlert, false, "", T(prefix+"customDllInactive"),
				kit.Button(T(prefix+"customDllEnableUnsafe"), func() {
					ic.set(func(c *xxmi.ImporterConfig) { c.Migoto.UnsafeMode = true })
				}, kit.Outline, kit.Small)))
		}
		if ic.custom {
			// Restoring saves the config on the backend, so a stale draft would turn unsafe mode back on.
			tip := ""
			if p.dirty(ic.saved) {
				tip = T(prefix + "unsavedChanges")
			}
			key := ic.key
			var warnings []string
			rows = append(rows, xxmiAlert(lucide.ShieldAlert, false, "", T(prefix+"customDllDescription"),
				kit.Button(T(prefix+"restoreOfficialDll"), func() {
					s.xxmiAction("restore-dll:"+key, func(ctx context.Context) error {
						var err error
						warnings, err = s.deps.XXMI.RestoreOfficialDLL(ctx, key)
						return err
					}, func() {
						s.xxmi.refresh()
						p.verification.invalidate()
						s.invalidateImporterFiles(key)
						s.toasts.Show(kit.ToastSuccess, T(prefix+"officialDllRestored"), "")
						for _, warning := range warnings {
							s.toasts.Show(kit.ToastWarning, warning, "")
						}
					}, nil)
				}, kit.Outline, kit.Small, kit.Disabled(p.dirty(ic.saved)), kit.Tooltip(tip),
					kit.Loading(s.xxmi.busy["restore-dll:"+key]))))
		}
		if choosesVersion {
			var notes []string
			if !xxmiRuntime {
				notes = append(notes, T(prefix+"libsVersionLegacy"))
			}
			if followsShared {
				version := ov.SharedLibsVersion
				if version == "" {
					version = T(prefix + "latest")
				}
				notes = append(notes, T(prefix+"libsSharedCurrent", "version", version))
			}
			items := []kit.Item{}
			// The legacy runtime has no provider to follow the shared settings through, and a config saved with
			// only the version following them keeps that choice until it is changed.
			if !xxmiRuntime || followsShared {
				items = append(items, kit.Item{Value: followShared, Label: T(prefix + "libsFollowShared")})
			}
			items = append(items, kit.Item{Value: followLatest, Label: T(prefix + "latest")})
			if provider := ic.deployedProvider(c); provider != "" {
				for _, release := range s.libsProviderReleases(provider).value {
					items = append(items, kit.Item{Value: release.Version, Label: release.Version})
				}
			}
			value := c.XXMIVersion.Pinned
			switch {
			case followsShared:
				value = followShared
			case value == "":
				value = followLatest
			}
			rows = append(rows, selectRow("xxmi.libs-version", T(prefix+"libsVersion"), strings.Join(notes, " "),
				value, items, func(value string) {
					ic.set(func(c *xxmi.ImporterConfig) {
						// The shared provider shows as the importer's own while only its version is, so choosing a
						// version makes both its own.
						if xxmiRuntime {
							c.LibsProvider = source
						}
						switch value {
						case followShared:
							c.XXMIVersion = xxmi.VersionPin{Follow: "shared"}
						case followLatest:
							c.XXMIVersion = xxmi.VersionPin{Follow: "latest"}
						default:
							// A new pin starts fixed; picking another version keeps the notices the user chose.
							notify := c.XXMIVersion.Pinned != "" && c.XXMIVersion.Notify
							c.XXMIVersion = xxmi.VersionPin{Pinned: value, Notify: notify}
						}
					})
				}))
			if !followsShared && c.XXMIVersion.Pinned != "" {
				rows = append(rows, toggleRow(T(prefix+"libsNotify"), T(prefix+"libsNotifyDescription"),
					c.XXMIVersion.Notify, func(on bool) {
						ic.set(func(c *xxmi.ImporterConfig) {
							c.XXMIVersion = xxmi.VersionPin{Pinned: c.XXMIVersion.Pinned, Notify: on}
						})
					}, false))
			}
		}
		sections = append(sections, settingsGroup(T(prefix+"libs"), rows...))
	}

	installed := ic.installedVersion()
	shownVersion := T("page.setting.xxmi.packageVersionUnknown")
	if installed != "" && ic.isInstalledPackage(installed) {
		shownVersion = installed
	}
	pin := c.PackageVersion.Pinned
	if pin == "" {
		pin = T(prefix + "latest")
	}
	facts := fairy.Row(
		fairy.RichText(fairy.Span{Text: T(prefix+"currentPin") + ": "},
			fairy.Span{Text: pin, Weight: 500, Color: t.Foreground}).LineHeight(16),
		fairy.Text(T("page.setting.xxmi.packageVersionCurrent", "version", shownVersion)).LineHeight(16),
	).Wrap().Gap(16).RowGap(4).FontSize(12).Color(t.MutedForeground)
	if v := p.verification.value; v != nil {
		method := T(prefix + "verificationNone")
		switch v.Method {
		case "ecdsa":
			method = T(prefix + "verificationECDSA")
		case "digest":
			method = T(prefix + "verificationDigest")
		}
		facts.Child(
			fairy.Text(T(prefix+"installedVerification", "version", v.Version, "method", method)).LineHeight(16),
		)
	}
	versions := fairy.Col(s.versionOption(c.PackageVersion.Pinned == "", T(prefix+"latest"), nil, func() {
		p.allowUnsigned = false
		ic.set(func(c *xxmi.ImporterConfig) { c.PackageVersion = xxmi.VersionPin{Follow: "latest"} })
	}, nil)).Gap(2).MaxH(288).ScrollY().PadXY(0, 12)
	for _, release := range s.packageReleases("importer:" + ic.key).value {
		version := release.Version
		var extras []*fairy.El
		if ic.isInstalledPackage(version) {
			extras = append(extras, fairy.Text(T(prefix+"packageInstalled")).FontSize(12).LineHeight(16).
				Color(t.MutedForeground))
		}
		if !release.Signed {
			extras = append(extras, kit.Badge(T(prefix+"unsigned"), kit.Destructive))
		}
		details := T(prefix + "packageDetails")
		versions.Child(s.versionOption(c.PackageVersion.Pinned == version, version, extras, func() {
			p.allowUnsigned = false
			// An uninstalled version cannot be saved, so it is pinned only once the dialog installs it.
			if ic.isInstalledPackage(version) {
				ic.set(func(c *xxmi.ImporterConfig) { c.PackageVersion = xxmi.VersionPin{Pinned: version} })
			} else {
				p.packageDialog = version
			}
		}, kit.IconButton(lucide.FileText, func() {
			p.allowUnsigned = false
			p.packageDialog = version
		}, kit.Ghost, kit.Small, kit.Tooltip(details)).Label(details+" "+version)))
	}
	sections = append(sections, settingsGroup(T(prefix+"packageVersion"),
		facts.PadXY(0, 12), versions,
		toggleRow(T(prefix+"overwriteINI"), T(prefix+"overwriteINIDescription"), c.OverwriteINI,
			func(on bool) { ic.set(func(c *xxmi.ImporterConfig) { c.OverwriteINI = on }) }, false)))
	return sections
}

func (s *Shell) versionOption(
	selected bool,
	label string,
	extras []*fairy.El,
	onSelect func(),
	action *fairy.El,
) *fairy.El {
	t := kit.T()
	check := fairy.Box().Size(16, 16).Shrink(0)
	if selected {
		check.Child(fairy.Icon(lucide.Check).Size(16, 16).Color(t.Foreground))
	}
	text := fairy.Text(label).FontSize(14).LineHeight(20)
	if action != nil {
		text.Font(monoFont)
	}
	choice := fairy.Row(check, text).Grow(1).MinW(0).Gap(8).PadXY(12, 8).Align(fairy.Center).Wrap().Radius(6).
		HoverFill(t.Muted.Alpha(0.6)).Animate(motion.Fast).Cursor(fairy.CursorPointer).Focusable().
		FocusRing(3, t.Ring.Alpha(0.5)).Label(label).Checked(selected).OnClick(onSelect)
	for _, extra := range extras {
		choice.Child(extra)
	}
	row := fairy.Row(choice).Key("xxmi.version:" + label).Gap(8).Align(fairy.Center).Radius(6)
	if selected {
		row.Fill(t.Muted)
	}
	if action != nil {
		row.Child(fairy.Row(action).PadEdges(0, 8, 0, 0).Shrink(0))
	}
	return row
}

func (s *Shell) pickImporterDLL(ic *importerContext, path string) {
	p := ic.p
	key := ic.key
	var dll xxmi.CustomDLL
	s.xxmiAction("importer-dll:"+key, func(ctx context.Context) error {
		var err error
		dll, err = s.deps.XXMI.ImportCustomDLL(ctx, path)
		return err
	}, func() {
		if p.key != key {
			return
		}
		p.importedDLL = &dll
		saved := s.importerConfig(key)
		current := saved.value
		if p.draft != nil {
			current = *p.draft
		}
		if current.Migoto.UnsafeMode {
			next := cloneConfig(current)
			next.CustomDLL = dll.ID
			p.draft = &next
			return
		}
		p.pendingDLL = &dll
	}, customDLLMessage)
}

func (s *Shell) importerGameTab(ic *importerContext) []*fairy.El {
	c := ic.c
	const prefix = "page.setting.xxmi.builtin."
	var rows []*fairy.El
	// Only WWMI has optional game-side settings; GIMI always turns off Dynamic Character Resolution, and SRMI and
	// HIMI adjust nothing.
	if c.WWMI != nil {
		rows = append(rows, toggleRow(T(prefix+"configureGame"), T(prefix+"configureGameDescriptionWWMI"),
			c.ConfigureGame, func(on bool) { ic.set(func(c *xxmi.ImporterConfig) { c.ConfigureGame = on }) }, false))
	}
	if g := c.GIMI; g != nil {
		rows = append(rows,
			toggleRow(T(prefix+"unlockFPS"), T(prefix+"unlockFPSDescriptionGIMI"), g.UnlockFPS,
				func(on bool) { ic.set(func(c *xxmi.ImporterConfig) { c.GIMI.UnlockFPS = on }) }, false,
				s.numberRow(
					ic,
					"gimi-fps",
					T(prefix+"fpsTarget"),
					T(prefix+"fpsTargetDescriptionGIMI"),
					g.UnlockFPSValue,
					within(30, 1000),
					func(n int) { ic.set(func(c *xxmi.ImporterConfig) { c.GIMI.UnlockFPSValue = n }) },
				)),
			toggleRow(T(prefix+"enableHDR"), T(prefix+"enableHDRDescription"), g.EnableHDR,
				func(on bool) { ic.set(func(c *xxmi.ImporterConfig) { c.GIMI.EnableHDR = on }) }, false))
	}
	if c.SRMI != nil {
		rows = append(rows, toggleRow(T(prefix+"unlockFPS"), T(prefix+"unlockFPSDescriptionSRMI"), c.SRMI.UnlockFPS,
			func(on bool) { ic.set(func(c *xxmi.ImporterConfig) { c.SRMI.UnlockFPS = on }) }, false))
	}
	if h := c.HIMI; h != nil {
		rows = append(rows, toggleRow(T(prefix+"unlockFPS"), T(prefix+"unlockFPSDescriptionHIMI"), h.UnlockFPS,
			func(on bool) { ic.set(func(c *xxmi.ImporterConfig) { c.HIMI.UnlockFPS = on }) }, false,
			s.numberRow(ic, "himi-fps", T(prefix+"fpsTarget"), "", h.UnlockFPSValue, within(30, 1000),
				func(n int) { ic.set(func(c *xxmi.ImporterConfig) { c.HIMI.UnlockFPSValue = n }) })))
	}
	sections := []*fairy.El{}
	if w := c.WWMI; w != nil {
		rows = append(rows,
			toggleRow(T(prefix+"unlockFPS"), T(prefix+"unlockFPSDescriptionWWMI"), w.UnlockFPS,
				func(on bool) { ic.set(func(c *xxmi.ImporterConfig) { c.WWMI.UnlockFPS = on }) }, false),
			toggleRow(T(prefix+"forceMaxLODBias"), T(prefix+"forceMaxLODBiasDescription"), w.ForceMaxLODBias,
				func(on bool) { ic.set(func(c *xxmi.ImporterConfig) { c.WWMI.ForceMaxLODBias = on }) }, false),
			toggleRow(T(prefix+"disableWoundedFX"), T(prefix+"disableWoundedFXDescription"), w.DisableWoundedFX,
				func(on bool) {
					ic.set(func(c *xxmi.ImporterConfig) { c.WoundedFXDecided, c.WWMI.DisableWoundedFX = true, on })
				}, false))
		sections = append(sections, settingsGroup("",
			// Picking a tier here answers the question the first launch would otherwise ask.
			selectRow("xxmi.resource-tier", T(prefix+"resourceTier"), T(prefix+"resourceTierDescription"),
				w.ResourceTier, plainItems(wwmiResourceTiers...), func(value string) {
					ic.set(
						func(c *xxmi.ImporterConfig) { c.WWMI.ResourceTier, c.WWMI.ResourceTierDecided = value, true },
					)
				}),
			s.numberRow(ic, "wwmi-lod", T(prefix+"meshLODDistanceBaseFOV"),
				T(prefix+"meshLODDistanceBaseFOVDescription"), w.MeshLODDistanceBaseFOV, numberBounds{},
				func(n int) { ic.set(func(c *xxmi.ImporterConfig) { c.WWMI.MeshLODDistanceBaseFOV = n }) })))
	}
	return append([]*fairy.El{settingsGroup("", rows...)}, sections...)
}

func (s *Shell) importerAdvancedTab(ic *importerContext) []*fairy.El {
	x := &s.xxmi
	c := ic.c
	const prefix = "page.setting.xxmi.builtin."
	var commands []*fairy.El
	for _, hook := range []struct {
		name   string
		value  xxmi.CommandHook
		target func(*xxmi.ImporterConfig) *xxmi.CommandHook
	}{
		{"preLaunch", c.RunPreLaunch, func(c *xxmi.ImporterConfig) *xxmi.CommandHook { return &c.RunPreLaunch }},
		{"postLoad", c.RunPostLoad, func(c *xxmi.ImporterConfig) *xxmi.CommandHook { return &c.RunPostLoad }},
	} {
		title := T(prefix + hook.name)
		commands = append(commands, toggleRow(title, T(prefix+hook.name+"Description"), hook.value.Enabled,
			func(on bool) { ic.set(func(c *xxmi.ImporterConfig) { hook.target(c).Enabled = on }) }, false,
			kit.TextField(hook.value.Command, func(value string) {
				ic.set(func(c *xxmi.ImporterConfig) { hook.target(c).Command = value })
			}, kit.Label(title)).Key("xxmi.command:"+hook.name).Font(monoFont),
			toggleRow(T(prefix+"waitForCommand"), "", hook.value.Wait,
				func(on bool) { ic.set(func(c *xxmi.ImporterConfig) { hook.target(c).Wait = on }) }, false).Pad(0)))
	}
	if c.GameLaunch != "Direct" {
		commands = append(commands, textRow("xxmi.game-process", T(prefix+"gameProcessExe"),
			T(prefix+"gameProcessExeDescription"), c.GameProcessExe, func(value string) {
				ic.set(func(c *xxmi.ImporterConfig) { c.GameProcessExe = strings.TrimSpace(value) })
			}))
	}

	libsPin := c.XXMIVersion.Pinned
	if c.XXMIVersion.Follow == "shared" {
		libsPin = ic.ov.SharedLibsVersion
	}
	legacyInjector := c.Mode == xxmi.RuntimeLegacy && c.InjectionMethod != "Native"
	extraDescription := T(prefix + "extraLibrariesDescription")
	extraDisabled := false
	if legacyInjector {
		cached := fetch(s, &x.cachedLibs, s.deps.XXMI.ListCachedLibs).value
		if len(cached) == 0 {
			extraDescription = T(prefix+"libs") + ": " + T(prefix+"notInstalled")
		}
		extraDisabled = !slices.ContainsFunc(cached, func(entry xxmi.CachedLibs) bool {
			return libsPin == "" || entry.Version == libsPin
		})
	}
	commands = append(commands, toggleRow(T(prefix+"extraLibraries"), extraDescription, c.ExtraLibraries.Enabled,
		func(on bool) { ic.set(func(c *xxmi.ImporterConfig) { c.ExtraLibraries.Enabled = on }) }, extraDisabled,
		kit.TextField(strings.Join(c.ExtraLibraries.Paths, ";"), func(value string) {
			var paths []string
			for _, path := range strings.Split(value, ";") {
				if path = strings.TrimSpace(path); path != "" {
					paths = append(paths, path)
				}
			}
			ic.set(func(c *xxmi.ImporterConfig) { c.ExtraLibraries.Paths = paths })
		}, kit.Label(T(prefix+"extraLibraries"))).Key("xxmi.extra-libraries").Font(monoFont)))

	m := c.Migoto
	var migoto []*fairy.El
	for _, field := range []struct {
		name  string
		value bool
		set   func(*xxmi.MigotoOptions, bool)
	}{
		{"enforceRendering", m.EnforceRendering, func(o *xxmi.MigotoOptions, v bool) { o.EnforceRendering = v }},
		{"enableHunting", m.EnableHunting, func(o *xxmi.MigotoOptions, v bool) { o.EnableHunting = v }},
		{"dumpShaders", m.DumpShaders, func(o *xxmi.MigotoOptions, v bool) { o.DumpShaders = v }},
		{"muteWarnings", m.MuteWarnings, func(o *xxmi.MigotoOptions, v bool) { o.MuteWarnings = v }},
		{"clearUnknownSettings", m.ClearUnknownSettings, func(o *xxmi.MigotoOptions, v bool) { o.ClearUnknownSettings = v }},
	} {
		migoto = append(migoto, toggleRow(T(prefix+field.name), T(prefix+field.name+"Description"), field.value,
			func(on bool) { ic.set(func(c *xxmi.ImporterConfig) { field.set(&c.Migoto, on) }) }, false))
	}
	levels := []kit.Item{}
	for _, level := range []string{"Disabled", "Warning", "Info", "Debug"} {
		levels = append(levels, kit.Item{Value: level, Label: T(prefix + "logLevel" + level)})
	}
	disableModes := []kit.Item{}
	for _, mode := range []string{"Mods", "All"} {
		disableModes = append(disableModes, kit.Item{Value: mode, Label: T(prefix + "inputDisableMode" + mode)})
	}
	unsafeDescription := T(prefix + "unsafeModeDescription")
	if m.UnsafeMode {
		unsafeDescription += " " + T(prefix+"unsafeWarning")
	}
	migoto = append(migoto,
		selectRow("xxmi.log-level", T(prefix+"logLevel"), T(prefix+"logLevelDescription"), m.LogLevel, levels,
			func(value string) { ic.set(func(c *xxmi.ImporterConfig) { c.Migoto.LogLevel = value }) }),
		toggleRow(T(prefix+"input"), T(prefix+"inputDescription"), m.Input,
			func(on bool) { ic.set(func(c *xxmi.ImporterConfig) { c.Migoto.Input = on }) }, false),
		selectRow("xxmi.input-disable", T(prefix+"inputDisableMode"), T(prefix+"inputDisableModeDescription"),
			m.InputDisableMode, disableModes,
			func(value string) { ic.set(func(c *xxmi.ImporterConfig) { c.Migoto.InputDisableMode = value }) }),
		textRow("xxmi.toggle-input", T(prefix+"toggleInput"), T(prefix+"toggleInputDescription"), m.ToggleInput,
			func(value string) { ic.set(func(c *xxmi.ImporterConfig) { c.Migoto.ToggleInput = value }) }),
		toggleRow(T(prefix+"unsafeMode"), unsafeDescription, m.UnsafeMode,
			func(on bool) { ic.set(func(c *xxmi.ImporterConfig) { c.Migoto.UnsafeMode = on }) }, false),
	)
	return []*fairy.El{settingsGroup(T(prefix+"commands"), commands...), settingsGroup("3DMigoto", migoto...)}
}

// importerD3DXTab edits the options the importer's d3dx.ini sets. Options the settings write are locked and show
// what the settings write.
func (s *Shell) importerD3DXTab(ic *importerContext) []*fairy.El {
	t := kit.T()
	p := ic.p
	key := ic.key
	const prefix = "page.setting.xxmi.builtin.d3dx."
	options := fetch(s, &p.d3dxOptions, func(ctx context.Context) ([]xxmi.D3DXOption, error) {
		return s.deps.XXMI.GetD3DXOptions(ctx, key)
	})
	failure := options.err
	if !options.loading {
		p.d3dxFailure = nil
	} else if failure == nil {
		failure = p.d3dxFailure
	}
	switch {
	case !options.loaded && failure != nil:
		retry := func() {
			p.d3dxFailure = options.err
			p.d3dxOptions.invalidate()
		}
		return []*fairy.El{fairy.Row(
			fairy.Col(fairy.Text(T(prefix+"loadFailed")).FontSize(14).LineHeight(20).Color(t.Destructive),
				fairy.Text(failure.Error()).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap()).
				Gap(2).Grow(1).MinW(0),
			kit.Button(T("page.setting.xxmi.dllVersionRetry"), retry, kit.Outline, kit.Small,
				kit.Loading(options.loading)),
		).Gap(16).Align(fairy.Center).PadXY(4, 0)}
	case !options.loaded:
		return nil
	case len(options.value) == 0:
		return []*fairy.El{
			fairy.Text(T(prefix+"empty")).FontSize(14).LineHeight(20).Color(t.MutedForeground).PadXY(4, 0),
		}
	}

	enforce := ic.c.Migoto.EnforceRendering
	locked := func(o xxmi.D3DXOption) bool {
		return o.Managed == xxmi.D3DXOptionManagedLaunch || o.Managed == xxmi.D3DXOptionManagedRendering && enforce
	}
	id := func(o xxmi.D3DXOption) string { return o.Section + "." + o.Key }
	var sections []string
	groups := map[string][]xxmi.D3DXOption{}
	for _, option := range options.value {
		name := strings.ToLower(option.Section)
		if _, ok := groups[name]; !ok {
			sections = append(sections, name)
		}
		groups[name] = append(groups[name], option)
	}
	// A section a new read no longer lists falls back to showing every section.
	if !slices.Contains(sections, p.d3dxSection) {
		p.d3dxSection = allSections
	}
	sectionItems := []kit.Item{{Value: allSections, Label: T(prefix + "sectionAll")}}
	for _, name := range sections {
		sectionItems = append(sectionItems, kit.Item{Value: name, Label: "[" + groups[name][0].Section + "]"})
	}

	search := kit.TextField(p.d3dxSearch, func(value string) { p.d3dxSearch = value }, kit.Icon(lucide.Search),
		kit.Placeholder(T("g.search")), kit.Label(T("g.search"))).Key("xxmi.d3dx-search").Grow(1).MinW(0).
		PadXY(8, 0).Gap(4)
	if t.Dark {
		search.Fill(fairy.Transparent)
	}
	out := []*fairy.El{
		fairy.Text(T(prefix+"applyNotice")).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap().PadXY(4, 0),
		fairy.Row(
			search,
			kit.Select("xxmi.d3dx-status", p.d3dxStatus, []kit.Item{
				{Value: "all", Label: T(prefix + "statusAll")},
				{Value: "editable", Label: T(prefix + "statusEditable")},
				{Value: "managed", Label: T(prefix + "statusManaged")},
			}, func(value string) { p.d3dxStatus = value }, kit.Label(T(prefix+"statusFilter"))).MinW(144).MaxW(240),
			kit.Select("xxmi.d3dx-section", p.d3dxSection, sectionItems, func(value string) { p.d3dxSection = value },
				kit.Label(T(prefix+"sectionFilter"))).MinW(144).MaxW(240),
		).Gap(8).Align(fairy.Center),
	}
	needle := strings.ToLower(strings.TrimSpace(p.d3dxSearch))
	shown := 0
	for _, name := range sections {
		if p.d3dxSection != allSections && name != p.d3dxSection {
			continue
		}
		var rows []*fairy.El
		for _, option := range groups[name] {
			lock := locked(option)
			value, edited := p.d3dx[id(option)]
			if !edited || lock {
				value = option.Value
			}
			matchesStatus := p.d3dxStatus == "all" || p.d3dxStatus == "editable" && !lock ||
				p.d3dxStatus == "managed" && lock
			// The row being edited stays while its value stops matching, as the React list kept the focused row.
			focused := s.win != nil && s.win.Focused("xxmi.d3dx:"+id(option))
			if !focused && (!matchesStatus || !strings.Contains(strings.ToLower(id(option)+"\n"+value), needle)) {
				continue
			}
			label := fairy.Col(fairy.Text(option.Key).Font(monoFont).FontSize(14).LineHeight(20).Medium().Wrap()).
				Gap(2).Grow(1).MinW(0)
			if lock {
				hint := T(prefix + "managedHint")
				if option.Managed == xxmi.D3DXOptionManagedRendering {
					hint = T(prefix + "managedRenderingHint")
				}
				label.Child(fairy.Text(hint).FontSize(12).LineHeight(16).Color(t.Foreground.Alpha(0.7)).Wrap())
			}
			opt := option
			rows = append(rows, fairy.Row(
				label,
				kit.TextField(value, func(next string) {
					if p.d3dx == nil {
						p.d3dx = map[string]string{}
					}
					if next == opt.Value {
						delete(p.d3dx, id(opt))
					} else {
						p.d3dx[id(opt)] = next
					}
				}, kit.Label(option.Key), kit.Disabled(lock)).Key("xxmi.d3dx:"+id(option)).W(288).Grow(0).Font(monoFont),
			).Gap(24).Align(fairy.Center).PadXY(0, 12))
		}
		if len(rows) == 0 {
			continue
		}
		shown++
		out = append(out, fairy.Col(
			fairy.Text("["+groups[name][0].Section+"]").Font(monoFont).FontSize(14).LineHeight(20).Medium().
				Color(t.MutedForeground).PadEdges(0, 4, 8, 4),
			settingsGroup("", rows...),
		).Gap(4))
	}
	if shown == 0 {
		out = append(
			out,
			fairy.Text(T(prefix+"noMatch")).FontSize(12).LineHeight(16).Color(t.MutedForeground).PadXY(4, 0),
		)
	}
	return out
}

func (s *Shell) importerToolsTab(ic *importerContext) []*fairy.El {
	t := kit.T()
	x := &s.xxmi
	p := ic.p
	c := ic.c
	key := ic.key
	const prefix = "page.setting.xxmi.builtin."

	previewID, applyID := "optimize:"+key+":preview", "optimize:"+key+":apply"
	optimize := func(dryRun bool) {
		if x.busy[previewID] || x.busy[applyID] {
			return
		}
		id := applyID
		if dryRun {
			id = previewID
		}
		resetCache := c.IniOptimizer.ResetCache
		var report optimizer.Report
		s.xxmiAction(id, func(ctx context.Context) error {
			var err error
			report, err = s.deps.XXMI.OptimizeMods(ctx,
				xxmi.OptimizeModsInput{Importer: key, DryRun: dryRun, ResetCache: resetCache})
			return err
		}, func() {
			if dryRun {
				p.optimization = &report
				return
			}
			p.optimization = nil
			s.toasts.Show(kit.ToastSuccess, T(prefix+"optimized", "count", len(report.Changes)), "")
		}, nil)
	}
	previewing, applying := x.busy[previewID], x.busy[applyID]
	busy := previewing || applying
	optimizer := []*fairy.El{
		toggleRow(T(prefix+"optimizeAtLaunch"), T(prefix+"optimizeAtLaunchDescription"), c.IniOptimizer.Enabled,
			func(on bool) { ic.set(func(c *xxmi.ImporterConfig) { c.IniOptimizer.Enabled = on }) }, false),
		toggleRow(T(prefix+"resetOptimizerCache"), T(prefix+"resetOptimizerCacheDescription"),
			c.IniOptimizer.ResetCache,
			func(on bool) { ic.set(func(c *xxmi.ImporterConfig) { c.IniOptimizer.ResetCache = on }) }, false),
		fairy.Row(
			kit.Button(T(prefix+"previewOptimization"), func() { optimize(true) }, kit.Outline,
				kit.Disabled(busy), kit.Loading(previewing)),
			kit.Button(T(prefix+"applyOptimization"), func() { optimize(false) },
				kit.Disabled(busy || p.optimization == nil || len(p.optimization.Changes) == 0), kit.Loading(applying)),
		).Gap(8).PadXY(0, 8),
	}
	if report := p.optimization; report != nil {
		list := fairy.Col().Gap(4).MaxH(256).ScrollY().Pad(8).Radius(6).Fill(t.Muted.Alpha(0.5)).
			Font(monoFont).FontSize(12)
		for _, change := range report.Changes {
			line := change.Action + ": " + change.Path
			if change.Line != 0 {
				line += ":" + strconv.Itoa(change.Line)
			}
			list.Child(fairy.Text(line + " · " + change.Reason).Wrap())
		}
		if len(report.Changes) == 0 {
			list.Child(fairy.Text(T(prefix + "noOptimizationChanges")))
		}
		optimizer = append(optimizer, list)
	}

	dirty := p.dirty(ic.saved)
	repairPackageTip := T(prefix + "repairPackageDescription")
	if dirty {
		repairPackageTip = T(prefix + "unsavedChanges")
	}
	var warnings []string
	maintenance := fairy.Row(
		kit.Button(T(prefix+"repairRuntime"), func() {
			s.xxmiAction("repair-runtime:"+key, func(ctx context.Context) error {
				var err error
				warnings, err = s.deps.XXMI.RepairRuntime(ctx, key)
				return err
			}, func() {
				s.toasts.Show(kit.ToastSuccess, T(prefix+"runtimeRepaired"), "")
				for _, warning := range warnings {
					s.toasts.Show(kit.ToastWarning, warning, "")
				}
			}, nil)
		}, kit.Outline, kit.Tooltip(T(prefix+"repairRuntimeDescription")), kit.Loading(x.busy["repair-runtime:"+key])),
		kit.Button(T(prefix+"repairPackage"), func() {
			s.xxmiAction("repair-package:"+key, func(ctx context.Context) error {
				return s.deps.XXMI.RepairImporterPackage(ctx, key)
			}, func() {
				x.refresh()
				p.verification.invalidate()
				s.invalidateImporterFiles(key)
				s.toasts.Show(kit.ToastSuccess, T(prefix+"packageRepaired"), "")
			}, importerFolderMessage)
		}, kit.Outline, kit.Disabled(dirty), kit.Tooltip(repairPackageTip), kit.Loading(x.busy["repair-package:"+key])),
		kit.Button(T(prefix+"openImporterFolder"), func() {
			s.xxmiAction("open-folder:"+key, func(ctx context.Context) error {
				return s.deps.XXMI.OpenImporterFolder(ctx, key)
			}, nil, nil)
		}, kit.Outline),
		kit.Button(T(prefix+"createShortcut"), func() {
			s.xxmiAction("shortcut:"+key, func(ctx context.Context) error {
				_, err := s.deps.XXMI.CreateShortcut(ctx, key)
				return err
			}, func() { s.toasts.Show(kit.ToastSuccess, T(prefix+"shortcutCreated"), "") }, nil)
		}, kit.Outline, kit.Tooltip(T(prefix+"createShortcutDescription")), kit.Loading(x.busy["shortcut:"+key])),
	).Wrap().Gap(8).RowGap(8).PadXY(0, 12)
	return []*fairy.El{
		settingsGroup(T(prefix+"iniOptimizer"), optimizer...),
		settingsGroup(T(prefix+"maintenance"), maintenance),
		s.diagnosticsSection(key, ic.entry != nil && ic.entry.Running, ic.launchBlocked()),
	}
}

// launchBlocked reports whether the importer cannot launch from its page: unsaved changes or a package to install
// first would launch something other than what the page shows.
func (ic *importerContext) launchBlocked() bool {
	pinned := ic.c.PackageVersion.Pinned
	return ic.p.dirty(ic.saved) || pinned != "" && !ic.isInstalledPackage(pinned) || ic.p.saving
}

func (s *Shell) importerDialogs(cx *fairy.Cx) {
	t := kit.T()
	x := &s.xxmi
	p := &x.importer
	if p.key == "" || x.page != p.key {
		return
	}
	key := p.key
	saved := keyed(x.configs, key)
	if !saved.loaded {
		return
	}
	ic := &importerContext{s: s, p: p, key: key, saved: saved, c: saved.value, ov: x.overview.value}
	if p.draft != nil {
		ic.c = *p.draft
	}
	ic.entry = findImporter(ic.ov, key)
	ic.custom = ic.entry != nil && ic.entry.CustomDLL
	const prefix = "page.setting.xxmi.builtin."

	s.diagnosticsDialogs(cx, key, (ic.entry == nil || !ic.entry.Running) && !ic.launchBlocked())
	s.modRuntimeDialogs(cx, key)

	if version := p.packageDialog; version != "" {
		var release *xxmi.ReleaseInfo
		for _, r := range keyed(x.releases, "package:importer:"+key).value {
			if r.Version == version {
				release = &r
			}
		}
		description := T(prefix + "packageDetails")
		body := fairy.Col().Gap(16)
		var actions []*fairy.El
		dismiss := func() {
			if !p.saving {
				p.packageDialog, p.allowUnsigned = "", false
			}
		}
		actions = append(actions, kit.Button(T("g.cancel"), dismiss, kit.Outline, kit.Disabled(p.saving)))
		if release != nil {
			installed := ic.isInstalledPackage(release.Version)
			if !installed {
				description = T(prefix + "packageNotInstalled")
			}
			facts := fairy.Row().Gap(12).Align(fairy.Center).FontSize(12).Color(t.MutedForeground)
			if installed {
				facts.Child(fairy.Text(T(prefix + "packageInstalled")).LineHeight(16))
			}
			if release.Signed {
				facts.Child(fairy.Text(T(prefix + "signed")).LineHeight(16))
			} else {
				facts.Child(kit.Badge(T(prefix+"unsigned"), kit.Destructive))
			}
			notes := release.Notes
			if notes == "" {
				notes = T(prefix + "noPackageNotes")
			}
			body.Child(facts).Child(fairy.Col(fairy.Text(notes).FontSize(14).LineHeight(20).
				Color(t.MutedForeground).Wrap()).MaxH(320).ScrollY())
			if !installed && !release.Signed {
				body.Child(fairy.Row(
					fairy.Icon(lucide.ShieldAlert).Size(16, 16).Shrink(0),
					fairy.Text(T(prefix+"allowUnsigned")).Grow(1).Wrap(),
					kit.Switch(p.allowUnsigned, func(on bool) { p.allowUnsigned = on }, kit.Disabled(p.saving)).
						Label(T(prefix+"allowUnsigned")),
				).Gap(8).Align(fairy.Center).Color(t.Destructive))
			}
			if !installed {
				signed := release.Signed
				actions = append(actions, kit.Button(T(prefix+"install"), func() {
					if !signed && !p.allowUnsigned {
						return
					}
					ic.set(func(c *xxmi.ImporterConfig) { c.PackageVersion = xxmi.VersionPin{Pinned: version} })
					s.saveImporter(ic, &packageInstall{version: version, allowUnsigned: !signed && p.allowUnsigned})
				}, kit.Disabled(!signed && !p.allowUnsigned), kit.Loading(p.saving)))
			}
		}
		kit.ShowDialog(cx, kit.DialogProps{
			Title: key + " · " + version, Description: description, Body: body, Actions: actions, Width: 512,
			OnClose: dismiss, HideCloseButton: p.saving, Key: "xxmi.package",
		})
	}

	if p.detected != nil {
		kit.ShowDialog(cx, kit.DialogProps{
			Title: T(prefix + "detectGame"),
			Body: gameFolderChoices(*p.detected, func(path string) {
				ic.set(func(c *xxmi.ImporterConfig) { c.GameFolder = path })
				p.detected = nil
			}).MaxH(320).Gap(8),
			OnClose: func() { p.detected = nil },
			Key:     "xxmi.detected-folders",
		})
	}

	if dll := p.pendingDLL; dll != nil {
		kit.ShowDialog(cx, kit.DialogProps{
			Title:       T(prefix + "customDllEnableUnsafeTitle"),
			Description: T(prefix + "customDllEnableUnsafeDescription"),
			Actions: []*fairy.El{
				kit.Button(T("g.cancel"), func() { p.pendingDLL = nil }, kit.Outline),
				kit.Button(T(prefix+"customDllEnableUnsafeConfirm"), func() {
					ic.set(func(c *xxmi.ImporterConfig) { c.CustomDLL, c.Migoto.UnsafeMode = dll.ID, true })
					p.pendingDLL = nil
				}),
			},
			OnClose: func() { p.pendingDLL = nil },
			Alert:   true,
			Key:     "xxmi.pending-dll",
		})
	}

	if target := p.modFolderTarget; target != nil && p.modFolder.loaded {
		unlinking := *target == ""
		linked := p.modFolder.value.LinkTarget != ""
		busy := x.busy["mod-folder:"+key]
		title, description, confirm := T(prefix+"modFolderChangeTitle"), T(prefix+"modFolderMoveDescription"),
			T(prefix+"modFolderChange")
		switch {
		case unlinking:
			title, description = T(prefix+"modFolderUnlinkTitle"), T(prefix+"modFolderUnlinkDescription")
			confirm = T(prefix + "modFolderUnlink")
		case linked:
			description = T(prefix + "modFolderRelinkDescription")
		}
		var body *fairy.El
		if !unlinking {
			body = fairy.Text(*target).
				FontSize(12).
				LineHeight(16).
				Wrap().
				PadXY(12, 8).
				Radius(6).
				Fill(t.Muted.Alpha(0.5))
		}
		dismiss := func() {
			if !busy {
				p.modFolderTarget = nil
			}
		}
		value := *target
		kit.ShowDialog(cx, kit.DialogProps{
			Title: title, Description: description, Body: body,
			Actions: []*fairy.El{
				kit.Button(T("g.cancel"), dismiss, kit.Outline, kit.Disabled(busy)),
				kit.Button(confirm, func() { s.applyModFolder(key, value) }, kit.Loading(busy)),
			},
			OnClose: dismiss,
			Alert:   true,
			Key:     "xxmi.mod-folder",
		})
	}
}
