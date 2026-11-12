package ui

import (
	"context"
	"slices"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"

	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/xxmi"
)

// customDLLSource is offered beside the libraries providers, whose IDs it must not collide with.
const (
	customDLLSource = "__custom_dll__"
	followLatest    = "__latest__"
)

// importErrorCodes are kept in sync with the import errors in internal/xxmi/migrate_user_data.go.
var importErrorCodes = []string{
	"XXMI_IMPORT_FOLDER_CONFLICT",
	"XXMI_IMPORT_TARGET_NOT_EMPTY",
	"XXMI_IMPORT_NO_SPACE",
	"XXMI_IMPORT_VERSION_UNKNOWN",
	"XXMI_IMPORT_SOURCE_CHANGED",
	"XXMI_IMPORT_PREVIEW_REQUIRED",
	"XXMI_GAME_RUNNING",
}

var importerFolderErrorCodes = []string{
	"XXMI_IMPORTER_FILE_ACCESS_DENIED",
	"XXMI_IMPORTER_FOLDER_ACCESS_DENIED",
	"XXMI_IMPORTER_FOLDER_IN_USE",
	"XXMI_IMPORTER_FOLDER_IS_LAUNCHER",
}

var customDLLErrorCodes = []string{"XXMI_CUSTOM_DLL_INVALID", "XXMI_CUSTOM_DLL_MISSING"}

type xxmiManage struct {
	// root is the edited XXMI root; nil shows the saved one.
	root         *string
	importOpen   bool
	resetOpen    bool
	unsafePrompt []string
	// pickingSharedDLL shows the custom DLL field before a DLL is picked, which stores nothing yet.
	pickingSharedDLL bool
	importMode       xxmi.ImportVersionMode
	userData         xxmi.ImportUserDataMode
	importStep       int
}

type xxmiExternal struct {
	path         *string
	autoScanned  bool
	dllVersion   string
	versions     map[string]string
	showDisabled bool
}

func firstCode(message string, codes []string) string {
	for _, code := range codes {
		if strings.Contains(message, code) {
			return code
		}
	}
	return ""
}

// importerFolderMessage translates an importer folder error, whose code may carry "CODE: detail" with a rollback
// failure on the next line.
func importerFolderMessage(err error) string {
	message := err.Error()
	code := firstCode(message, importerFolderErrorCodes)
	if code == "" {
		return ""
	}
	detail := strings.TrimPrefix(message[strings.Index(message, code)+len(code):], ":")
	detail, _, _ = strings.Cut(strings.TrimLeft(detail, " \t"), "\n")
	return T("page.setting.xxmi.fn.installImporterPackage.errors."+code, "detail", strings.TrimSpace(detail))
}

func customDLLMessage(err error) string {
	if code := firstCode(err.Error(), customDLLErrorCodes); code != "" {
		return T("page.setting.xxmi.builtin.customDllErrors." + code)
	}
	return ""
}

func sameVersion(a, b string) bool {
	normalize := func(v string) string {
		v = strings.TrimSpace(v)
		if strings.HasPrefix(v, "v") || strings.HasPrefix(v, "V") {
			v = v[1:]
		}
		return v
	}
	return a != "" && b != "" && normalize(a) == normalize(b)
}

// ignoresSharedDLL reports an importer that follows the shared provider but would not launch the shared custom DLL
// until unsafe mode is on.
func ignoresSharedDLL(config xxmi.ImporterConfig) bool {
	return config.Mode == xxmi.RuntimeXXMI && config.LibsProvider == "" && config.CustomDLL == "" &&
		!config.Migoto.UnsafeMode
}

func xxmiBody(children ...*fairy.El) *fairy.El {
	return stableScrollY(fairy.Col(fairy.Col(children...).WFull().MaxW(672).Pad(16).Gap(24)).Grow(1).MinH(0).WFull().
		Align(fairy.Center))
}

func (s *Shell) xxmiManagePage() *fairy.El {
	if s.xxmiExternal() {
		return xxmiBody(s.externalLauncherSection(), s.launchGuardSettings())
	}
	return s.builtinDashboard()
}

// forgetXXMISettings drops the cached XXMI settings that an import or a reset rewrote, so they are read again.
func (s *Shell) forgetXXMISettings() {
	for key := range s.settings.values {
		if strings.HasPrefix(key, "xxmi.") {
			delete(s.settings.values, key)
		}
	}
}

func (s *Shell) builtinDashboard() *fairy.El {
	t := kit.T()
	x := &s.xxmi
	m := &x.manage
	const prefix = "page.setting.xxmi.builtin."

	sections := []*fairy.El{}
	if pending := installableUpdates(s.xxmiUpdates()); len(pending) > 0 {
		rows := []*fairy.El{}
		for _, entry := range pending {
			pkg, version := entry.Package, entry.LatestVersion
			rows = append(rows, fairy.Row(
				fairy.Text(updateLine(entry)).Font(monoFont).FontSize(12).LineHeight(16).
					Color(t.MutedForeground).Grow(1).MinW(0).Wrap(),
				kit.Button(T(prefix+"skipVersion"), func() {
					s.xxmiAction("skip:"+pkg, func(ctx context.Context) error {
						return s.deps.XXMI.SkipVersion(ctx, pkg, version)
					}, x.refresh, nil)
				}, kit.Ghost, kit.XS, kit.Loading(x.busy["skip:"+pkg]))).Gap(12).Align(fairy.Center).PadXY(0, 8))
		}
		install := kit.Button(T(prefix+"installUpdates"), func() {
			packages := packagesOf(pending)
			s.xxmiAction("install-all-updates", func(ctx context.Context) error {
				_, err := s.deps.XXMI.InstallUpdates(ctx, "", packages)
				return err
			}, x.refresh, nil)
		}, kit.Small, kit.Icon(lucide.Download), kit.Loading(x.busy["install-all-updates"]))
		sections = append(sections, fairy.Col(
			fairy.Row(fairy.Text(T(prefix+"updatesPending", "count", len(pending))).FontSize(14).LineHeight(20).
				Medium().Color(t.MutedForeground).Grow(1).AlignSelf(fairy.Start), install).
				Gap(16).Align(fairy.Center).PadEdges(0, 4, 8, 4),
			fairy.Col(rows...).Gap(4).PadEdges(0, 14, 0, 30).BorderEdges(0, 0, 0, 1, t.Primary),
		).Gap(4))
	}

	sections = append(sections, settingsGroup(T(prefix+"options"), s.builtinOptions()...), s.launchGuardSettings(),
		settingsGroup(T(prefix+"packages"), s.builtinPackages()...))

	sections = append(sections, settingsGroup(T(prefix+"reset"), fairy.Row(
		fairy.Text(T(prefix+"resetDescription")).FontSize(12).LineHeight(16).Color(t.MutedForeground).
			Grow(1).MinW(0).Wrap(),
		kit.Button(T(prefix+"resetConfirm"), func() { m.resetOpen = true }, kit.Destructive,
			kit.Icon(lucide.RotateCcw)),
	).Gap(16).Align(fairy.Center).PadXY(0, 12)))
	return xxmiBody(sections...)
}

func (s *Shell) builtinOptions() []*fairy.El {
	t := kit.T()
	x := &s.xxmi
	m := &x.manage
	ov := x.overview.value
	const prefix = "page.setting.xxmi.builtin."

	root := ov.Root
	if m.root != nil {
		root = *m.root
	}
	rows := []*fairy.El{s.pathField("xxmi.root", T(prefix+"root"), T(prefix+"rootDescription"), root,
		func(value string) { m.root = &value },
		kit.Button(T("g.save"), func() {
			value := root
			s.xxmiAction("set-root", func(ctx context.Context) error {
				return s.deps.XXMI.SetRoot(ctx, value)
			}, func() {
				m.root = nil
				x.refresh()
			}, nil)
		}, kit.Disabled(root == "" || root == ov.Root), kit.Loading(x.busy["set-root"])))}

	// Detection may scan every drive, so it loads apart from the overview, and only while nothing is configured;
	// importing again would overwrite the configured importers.
	if !ov.Configured {
		detected := fetch(s, &x.launcher, s.deps.XXMI.DetectExternalLauncher)
		if detected.value != nil && detected.current {
			launcher := detected.value
			rows = append(rows, xxmiAlert(lucide.Info, false, T(prefix+"externalDetected"),
				launcher.Path+"\n"+T(prefix+"externalWarning"),
				kit.Button(T(prefix+"import"), func() {
					m.importOpen, m.importMode, m.userData, m.importStep = true, "", xxmi.ImportUserDataKeep, 0
				}, kit.Outline, kit.Small)))
		}
	}

	if s.xxmiSettingsLoaded() {
		modes := []kit.Item{}
		for _, mode := range []string{"auto", "notify", "off"} {
			modes = append(modes, kit.Item{Value: mode, Label: T(prefix + "launchUpdateModes." + mode)})
		}
		mode := s.stringSetting(setting.KeyXXMIAutoUpdate)
		if mode == "" {
			mode = "auto"
		}
		rows = append(rows,
			selectRow("xxmi.auto-update", T(prefix+"launchUpdate"), "", mode, modes,
				func(value string) { s.setSetting(setting.KeyXXMIAutoUpdate, value) }),
			toggleRow(T(prefix+"prereleases"), T(prefix+"prereleasesDescription"),
				s.boolSetting(setting.KeyXXMIIncludePrereleases),
				func(on bool) { s.setSetting(setting.KeyXXMIIncludePrereleases, on) }, false))
	}

	sharedDLL := ov.SharedCustomDLL
	sharedSource := sharedDLL != "" || m.pickingSharedDLL
	providers := []kit.Item{}
	for _, provider := range ov.LibsProviders {
		providers = append(providers, kit.Item{Value: provider, Label: T(prefix + "libsProviders." + provider)})
	}
	providers = append(providers, kit.Item{Value: customDLLSource, Label: T(prefix + "customDll")})
	source := ov.SharedLibsProvider
	if sharedSource {
		source = customDLLSource
	}
	rows = append(rows, selectRow("xxmi.shared-provider", T(prefix+"sharedLibsProvider"),
		T(prefix+"sharedLibsDescription"), source, providers, func(value string) {
			m.pickingSharedDLL = value == customDLLSource
			// Selecting a provider drops the shared custom DLL with it, so the current provider is selected again
			// when only the DLL has to go.
			if value == customDLLSource || value == ov.SharedLibsProvider && sharedDLL == "" {
				return
			}
			s.xxmiAction("set-shared-provider", func(ctx context.Context) error {
				return s.deps.XXMI.SetSharedLibsProvider(ctx, value)
			}, x.refresh, nil)
			x.overview.invalidate()
		}))

	if sharedSource {
		name := ""
		for _, dll := range ov.CustomDLLs {
			if dll.ID == sharedDLL {
				name = dll.Name
			}
		}
		rows = append(rows, fairy.Col(s.customDLLField("xxmi.shared-dll", sharedDLL, name, x.busy["set-shared-dll"],
			s.selectSharedCustomDLL)).
			Pad(12).Radius(6).Fill(t.Muted.Alpha(0.5)))
	}

	if sharedDLL != "" {
		var ignored, labels []string
		for _, importer := range ov.Importers {
			config := s.importerConfig(importer.Key)
			if !config.loaded || !ignoresSharedDLL(config.value) {
				continue
			}
			// The shared version cannot be edited while the DLL hides its row, so the one these importers still
			// deploy is named here.
			version := config.value.XXMIVersion.Pinned
			if config.value.XXMIVersion.Follow == "shared" {
				version = ov.SharedLibsVersion
			}
			if version == "" {
				version = T(prefix + "latest")
			}
			ignored = append(ignored, importer.Key)
			labels = append(labels, importer.Key+" ("+version+")")
		}
		if len(ignored) > 0 {
			rows = append(rows, xxmiAlert(lucide.TriangleAlert, false, "",
				T(prefix+"customDllFallbackProvider", "importers", strings.Join(labels, ", "),
					"provider", T(prefix+"libsProviders."+ov.SharedLibsProvider)),
				kit.Button(T(prefix+"customDllEnableUnsafe"), func() { s.xxmi.manage.unsafePrompt = ignored },
					kit.Outline, kit.Small)))
		}
	}

	if !sharedSource {
		versions := []kit.Item{{Value: followLatest, Label: T(prefix + "latest")}}
		if ov.SharedLibsProvider != "" {
			for _, release := range s.libsProviderReleases(ov.SharedLibsProvider).value {
				versions = append(versions, kit.Item{Value: release.Version, Label: release.Version})
			}
		}
		value := ov.SharedLibsVersion
		if value == "" {
			value = followLatest
		}
		rows = append(rows, selectRow("xxmi.shared-version", T(prefix+"sharedLibsVersion"), "", value, versions,
			func(value string) {
				if value == followLatest {
					value = ""
				}
				s.xxmiAction("set-shared-version", func(ctx context.Context) error {
					return s.deps.XXMI.SetSharedLibsVersion(ctx, value)
				}, x.refresh, nil)
			}))
	}
	return rows
}

func (s *Shell) selectSharedCustomDLL(path string) {
	x := &s.xxmi
	var ignored []string
	s.xxmiAction("set-shared-dll", func(ctx context.Context) error {
		dll, err := s.deps.XXMI.ImportCustomDLL(ctx, path)
		if err != nil {
			return err
		}
		if err := s.deps.XXMI.SetSharedCustomDLL(ctx, dll.ID); err != nil {
			return err
		}
		for _, importer := range importerKeys {
			config, err := s.deps.XXMI.GetImporterConfig(ctx, importer)
			if err == nil && ignoresSharedDLL(config) && findImporter(x.overview.value, importer) != nil {
				ignored = append(ignored, importer)
			}
		}
		return nil
	}, func() {
		x.refresh()
		s.toasts.Show(kit.ToastSuccess, T("page.setting.xxmi.builtin.customDllSet"), "")
		x.manage.unsafePrompt = ignored
	}, customDLLMessage)
}

// enableSharedUnsafeMode turns on unsafe mode for the importers that would ignore the shared custom DLL, keeping
// the ones that failed in the prompt.
func (s *Shell) enableSharedUnsafeMode() {
	x := &s.xxmi
	keys := x.manage.unsafePrompt
	var failed []string
	var errs []error
	s.xxmiAction("enable-unsafe", func(ctx context.Context) error {
		for _, key := range keys {
			config, err := s.deps.XXMI.GetImporterConfig(ctx, key)
			if err == nil {
				config.Migoto.UnsafeMode = true
				err = s.deps.XXMI.SaveImporterConfig(ctx, key, config)
			}
			if err != nil {
				failed = append(failed, key)
				errs = append(errs, err)
			}
		}
		return nil
	}, func() {
		for i, err := range errs {
			s.xxmiFail(err, "enable-unsafe", map[string]any{"importer": failed[i]}, failed[i]+": "+err.Error())
		}
		x.manage.unsafePrompt = failed
		x.refresh()
	}, nil)
}

func (s *Shell) builtinPackages() []*fairy.El {
	x := &s.xxmi
	ov := x.overview.value
	const prefix = "page.setting.xxmi.builtin."

	rows := []*fairy.El{}
	for _, issue := range ov.CacheIssues {
		rows = append(rows, xxmiAlert(lucide.TriangleAlert, true, T(prefix+"cacheIssue"), issue))
	}

	var libs []packageVersion
	prunable := false
	for _, entry := range ov.LibsCache {
		label := entry.Version
		if entry.InUse {
			names := strings.Join(entry.InUseVersions, ", ")
			if names == "" {
				names = entry.Version
			}
			label = names + " · " + T(prefix+"inUse")
		}
		libs = append(libs, packageVersion{label: label, active: entry.InUse})
		prunable = prunable || !entry.Referenced
	}
	var removed []string
	rows = append(rows, packageRow(T(prefix+"libs"), libs, kit.Button(T(prefix+"prune"), func() {
		s.xxmiAction("prune-libs", func(ctx context.Context) error {
			var err error
			removed, err = s.deps.XXMI.PruneLibsCache(ctx)
			return err
		}, func() {
			x.refresh()
			s.toasts.Show(kit.ToastSuccess, T(prefix+"pruned", "count", len(removed)), "")
		}, nil)
	}, kit.Outline, kit.Small, kit.Icon(lucide.Trash), kit.Disabled(!prunable), kit.Loading(x.busy["prune-libs"]))))

	var legacy []packageVersion
	for _, entry := range ov.LegacyRuntimes {
		legacy = append(legacy, packageVersion{label: entry.ID})
	}
	rows = append(rows, packageRow(T(prefix+"legacy"), legacy,
		kit.Button(T(prefix+"importLegacy"), func() {
			s.pickFile(platform.OpenDialogOptions{
				Title: T(
					prefix + "importLegacy",
				),
				Filters:    []platform.FileFilter{{Name: "ZIP", Extensions: []string{"zip"}}},
				Properties: []string{"openFile"},
				Role:       "xxmi-legacy-import",
			}, func(paths []string) {
				s.xxmiAction("import-legacy", func(ctx context.Context) error {
					_, err := s.deps.XXMI.ImportLegacyRuntimeZip(ctx, paths[0])
					return err
				}, func() {
					x.refresh()
					s.toasts.Show(kit.ToastSuccess, T(prefix+"importedLegacy"), "")
				}, nil)
			})
		}, kit.Outline, kit.Small, kit.Icon(lucide.FileArchive), kit.Loading(x.busy["import-legacy"])),
		kit.Button(T(prefix+"downloadLegacy"), func() {
			s.xxmiAction("update-legacy", func(ctx context.Context) error {
				_, err := s.deps.XXMI.UpdateLegacyRuntime(ctx)
				return err
			}, x.refresh, nil)
		}, kit.Outline, kit.Small, kit.Icon(lucide.Download), kit.Loading(x.busy["update-legacy"]))))

	var fps []packageVersion
	for _, version := range ov.FPSVersions {
		fps = append(fps, packageVersion{label: version})
	}
	latest := ""
	if releases := s.packageReleases("gi-fps-unlocker").value; len(releases) > 0 {
		latest = releases[0].Version
	}
	rows = append(rows, packageRow(T(prefix+"fpsUnlocker"), fps, kit.Button(T(prefix+"downloadFPS"), func() {
		s.xxmiAction("download-fps", func(ctx context.Context) error {
			return s.deps.XXMI.EnsureFPSUnlockerVersion(ctx, latest)
		}, x.refresh, nil)
	}, kit.Outline, kit.Small, kit.Icon(lucide.Download),
		kit.Disabled(latest == "" || slices.Contains(ov.FPSVersions, latest)), kit.Loading(x.busy["download-fps"]))))
	return rows
}

type packageVersion struct {
	label  string
	active bool
}

func packageRow(title string, versions []packageVersion, actions ...*fairy.El) *fairy.El {
	t := kit.T()
	list := fairy.Row().Wrap().Gap(16).RowGap(4)
	for _, version := range versions {
		text := fairy.Text(version.label).Font(monoFont).FontSize(12).LineHeight(16).Color(t.MutedForeground)
		if version.active {
			text.Medium().Color(t.Foreground)
		}
		list.Child(text)
	}
	if len(versions) == 0 {
		list.Child(
			fairy.Text(T("page.setting.xxmi.builtin.notInstalled")).
				FontSize(12).
				LineHeight(16).
				Color(t.MutedForeground),
		)
	}
	return fairy.Col(
		fairy.Row(fairy.Text(title).FontSize(14).LineHeight(20).Medium().Grow(1).MinW(0),
			fairy.Row(actions...).Gap(8).RowGap(8).Wrap().Justify(fairy.JustifyEnd)).Gap(12).
			Align(fairy.Center),
		list,
	).Gap(8).PadXY(0, 12)
}

// pathField is a folder input with a browse button and any further controls after it.
func (s *Shell) pathField(id, label, description, value string, onChange func(string), extra ...*fairy.El) *fairy.El {
	t := kit.T()
	head := fairy.Col(fairy.Text(label).FontSize(14).LineHeight(20).Medium()).Gap(2)
	if description != "" {
		head.Child(fairy.Text(description).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap())
	}
	browse := T("page.setting.xxmi.builtin.browse")
	row := fairy.Row(
		kit.TextField(value, onChange, kit.Label(label)).Key(id).Grow(1).MinW(0),
		kit.IconButton(lucide.FolderOpen, func() {
			s.pickFile(
				platform.OpenDialogOptions{Title: label, DefaultPath: value, Properties: []string{"openDirectory"}},
				func(paths []string) { onChange(paths[0]) },
			)
		}, kit.Outline, kit.Tooltip(browse)).Label(browse),
	).Gap(8).Align(fairy.Center)
	for _, el := range extra {
		row.Child(el)
	}
	return fairy.Col(head, row).Gap(6).PadXY(0, 12)
}

// customDLLField picks a custom d3d11.dll through the file dialog or a file dropped on the row. Importing the
// picked file and storing it is up to onPick.
func (s *Shell) customDLLField(id, selectedID, selectedName string, busy bool, onPick func(path string)) *fairy.El {
	t := kit.T()
	const prefix = "page.setting.xxmi.builtin."
	name, detail := T(prefix+"customDll"), T(prefix+"customDllDropHint")
	if selectedID != "" {
		detail = selectedID
		if selectedName != "" {
			name = selectedName
		}
	}
	// The button stays busy from the dialog through the import, as React's onClickPromise did.
	picking := fairy.Local[bool]("xxmi.dll-picking:" + id)
	return fairy.Row(
		fairy.Col(fairy.Text(name).FontSize(14).LineHeight(20).Medium().Wrap(),
			fairy.Text(detail).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap()).Gap(2).
			Grow(1).MinW(0),
		kit.Button(T(prefix+"customDllSelect"), func() {
			*picking = true
			s.pickFileSettled(platform.OpenDialogOptions{
				Title:      T(prefix + "customDllSelect"),
				Filters:    []platform.FileFilter{{Name: "DLL", Extensions: []string{"dll"}}},
				Properties: []string{"openFile"},
				Role:       "xxmi-custom-dll",
			}, func(paths []string) { onPick(paths[0]) }, func() { *picking = false })
		}, kit.Outline, kit.Small, kit.Icon(lucide.FileUp), kit.Loading(busy || *picking)),
	).Key(id).Gap(24).Align(fairy.Center).Radius(6).Animate(motion.Bezier(0.2, 0.4, 0, 0.2, 1)).
		OnFileDrop(func(drop fairy.FileDrop) {
			if len(drop.Paths) != 1 || !strings.EqualFold(fileExt(drop.Paths[0]), ".dll") {
				s.toasts.Show(kit.ToastError, T(prefix+"customDllOnlyDll"), "")
				return
			}
			onPick(drop.Paths[0])
		}).DropFill(t.Primary.Alpha(0.05)).DropBorder(t.Primary.Alpha(0.2))
}

func fileExt(path string) string {
	if i := strings.LastIndexByte(path, '.'); i >= 0 && !strings.ContainsAny(path[i:], `\/`) {
		return path[i:]
	}
	return ""
}

func (s *Shell) manageDialogs(cx *fairy.Cx) {
	x := &s.xxmi
	m := &x.manage
	const prefix = "page.setting.xxmi.builtin."
	if m.resetOpen {
		busy := x.busy["reset-builtin"]
		dismiss := func() {
			if !busy {
				m.resetOpen = false
			}
		}
		kit.ShowDialog(cx, kit.DialogProps{
			Title:       T(prefix + "resetTitle"),
			Description: T(prefix + "resetConfirmDescription"),
			Actions: []*fairy.El{
				kit.Button(T("g.cancel"), dismiss, kit.Outline, kit.Disabled(busy)),
				kit.Button(T(prefix+"resetConfirm"), s.resetBuiltin, kit.Destructive, kit.Loading(busy)),
			},
			OnClose: dismiss,
			Alert:   true,
			Key:     "xxmi.reset",
		})
	}
	if len(m.unsafePrompt) > 0 {
		busy := x.busy["enable-unsafe"]
		dismiss := func() {
			if !busy {
				m.unsafePrompt = nil
			}
		}
		kit.ShowDialog(cx, kit.DialogProps{
			Title:       T(prefix + "customDllEnableUnsafeTitle"),
			Description: T(prefix+"customDllEnableUnsafeShared", "importers", strings.Join(m.unsafePrompt, ", ")),
			Actions: []*fairy.El{
				kit.Button(T("g.cancel"), dismiss, kit.Outline, kit.Disabled(busy)),
				kit.Button(T(prefix+"customDllEnableUnsafeConfirm"), s.enableSharedUnsafeMode, kit.Loading(busy)),
			},
			OnClose: dismiss,
			Alert:   true,
			Key:     "xxmi.shared-unsafe",
		})
	}
	if m.importOpen && x.launcher.value != nil {
		s.importDialog(cx, x.launcher.value.Path)
	}
}

func (s *Shell) resetBuiltin() {
	x := &s.xxmi
	s.xxmiAction("reset-builtin", s.deps.XXMI.ResetBuiltinRuntime, func() {
		x.manage.resetOpen, x.manage.root = false, nil
		x.refresh()
		s.forgetXXMISettings()
		s.toasts.Show(kit.ToastSuccess, T("page.setting.xxmi.builtin.resetDone"), "")
	}, func(err error) string {
		if strings.Contains(err.Error(), "XXMI_BUSY") {
			return T("page.setting.xxmi.builtin.resetBusy")
		}
		return ""
	})
}

// importDialog previews the external launcher's versions and imports its importers into the built-in runtime.
func (s *Shell) importDialog(cx *fairy.Cx, path string) {
	t := kit.T()
	x := &s.xxmi
	m := &x.manage
	const prefix = "page.setting.xxmi.builtin."
	preview := fetch(s, keyed(x.importPreview, path), func(ctx context.Context) ([]xxmi.ImportPackageVersion, error) {
		return s.deps.XXMI.PreviewExternalLauncherImport(ctx, path)
	})
	importing := x.busy["import-external"]
	root := x.overview.value.Root
	if m.root != nil && strings.TrimSpace(*m.root) != "" {
		// A cleared root field imports into the saved root, which the overview resolves to the default.
		root = strings.TrimSpace(*m.root)
	}

	versions := preview.value
	checkFailed, hasUpdates, canPin := false, false, true
	for _, version := range versions {
		checkFailed = checkFailed || version.CheckFailed
		hasUpdates = hasUpdates || version.UpdateAvailable
		canPin = canPin && version.InstalledVersion != ""
	}
	mode := m.importMode
	if mode == "" {
		mode = xxmi.ImportVersionLatest
		if len(versions) == 0 || checkFailed || hasUpdates {
			mode = xxmi.ImportVersionPinned
		}
	}
	if checkFailed {
		mode = xxmi.ImportVersionPinned
	}
	ready := preview.current && !preview.loading
	canImport := ready && (mode != xxmi.ImportVersionPinned || canPin)

	body := fairy.Col().Gap(16)
	switch {
	case preview.loading:
		body.Child(
			fairy.Row(kit.Spinner(16), fairy.Text(T(prefix+"importCheckingVersions")).FontSize(14).LineHeight(20)).
				Gap(8).Align(fairy.Center).Color(t.MutedForeground).Role(fairy.RoleStatusBar),
		)
	case preview.err != nil:
		body.Child(xxmiAlert(lucide.TriangleAlert, true, "", preview.err.Error()))
	default:
		table := fairy.Col(fairy.Row(
			fairy.Text(T(prefix+"importPackage")).LineHeight(20).Medium().Basis(0).Grow(1),
			fairy.Text(T(prefix+"importInstalledVersion")).LineHeight(20).Medium().Basis(0).Grow(1),
			fairy.Text(T(prefix+"importLatestVersion")).LineHeight(20).Medium().Basis(0).Grow(1),
		).H(40).Gap(16).PadXY(8, 0).Align(fairy.Center).BorderEdges(0, 0, 1, 0, t.Border)).FontSize(14)
		for i, version := range versions {
			name := strings.TrimPrefix(version.Package, "importer:")
			if version.Package == "xxmi-libs" {
				name = T(prefix + "libs")
			}
			installed := version.InstalledVersion
			if installed == "" {
				installed = T(prefix + "notInstalled")
			}
			latest := version.LatestVersion
			if version.CheckFailed {
				latest = T("g.unknown")
			}
			row := fairy.Row(
				fairy.Text(name).LineHeight(20).Basis(0).Grow(1).Wrap(),
				fairy.Text(installed).Font(monoFont).LineHeight(20).Basis(0).Grow(1).Wrap(),
				fairy.Text(latest).Font(monoFont).LineHeight(20).Basis(0).Grow(1).Wrap(),
			).Gap(16).Pad(8).Align(fairy.Center).HoverFill(t.Muted.Alpha(0.5))
			if i < len(versions)-1 {
				row.BorderEdges(0, 0, 1, 0, t.Border)
			}
			table.Child(row)
		}
		body.Child(table)
		if checkFailed {
			body.Child(xxmiAlert(lucide.Info, false, "", T(prefix+"importVersionCheckFailed")))
		}
		body.Child(
			fairy.Text(T(prefix + "importVersionHint")).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap(),
		)
	}
	if preview.err != nil || checkFailed {
		body.Child(kit.Button(T(prefix+"importRetryVersions"), keyed(x.importPreview, path).invalidate, kit.Outline,
			kit.Disabled(preview.loading || importing)))
	}
	// The version step exists only when there is a choice to make, as a step without an enabled answer
	// would block Next.
	var steps []*fairy.El
	if ready && (hasUpdates || checkFailed) {
		steps = append(steps, fairy.Col(
			fairy.Text(T(prefix+"importVersionChoice")).FontSize(14).LineHeight(20).Medium(),
			xxmiImportChoices(string(mode), []kit.Item{
				{Value: string(xxmi.ImportVersionLatest), Label: T(prefix + "importUpdateLatest"),
					Disabled: importing || checkFailed},
				{Value: string(xxmi.ImportVersionPinned), Label: T(prefix + "importPinInstalled"),
					Disabled: importing || !canPin},
			}, func(value string) { m.importMode = xxmi.ImportVersionMode(value) }),
		).Gap(16).Key("xxmi.import.version-mode"))
	}
	if ready {
		steps = append(steps, fairy.Col(
			fairy.Text(T(prefix+"importUserDataTitle")).FontSize(14).LineHeight(20).Medium(),
			xxmiImportChoices(string(m.userData), []kit.Item{
				{Value: string(xxmi.ImportUserDataKeep), Label: T(prefix + "importUserDataKeep"),
					Description: T(prefix + "importUserDataKeepHint"), Disabled: importing},
				{Value: string(xxmi.ImportUserDataMove), Label: T(prefix + "importUserDataMove"),
					Description: T(prefix + "importUserDataMoveHint"), Disabled: importing},
			}, func(value string) { m.userData = xxmi.ImportUserDataMode(value) }),
		).Gap(16).Key("xxmi.import.user-data"))
	}
	total := len(steps)
	step := min(m.importStep, max(total-1, 0))
	last := step >= total-1

	input := xxmi.ImportExternalLauncherInput{Path: path, Root: root, UserData: m.userData, VersionMode: mode,
		Versions: versions}
	advance := func() {
		if !canImport || importing {
			return
		}
		if last {
			s.importExternal(input)
			return
		}
		m.importStep = step + 1
	}
	if total > 0 {
		// Enter on an answer moves on, or imports on the last step, as in the React questionnaire.
		body.Child(steps[step].OnKey(func(k keyboard.KeyEvent) bool {
			if k.Key != keyboard.KeyEnter || k.Mods&^keyboard.ModCtrl != 0 {
				return false
			}
			if !k.Repeat {
				advance()
			}
			return true
		}))
	}

	dismiss := func() {
		if !importing {
			m.importOpen = false
		}
	}
	actions := []*fairy.El{kit.Button(T("g.cancel"), dismiss, kit.Outline, kit.Disabled(importing))}
	if step > 0 {
		actions = append(actions, kit.Button(T("g.previous"), func() { m.importStep = step - 1 }, kit.Outline,
			kit.Disabled(importing)))
	}
	if last {
		actions = append(actions, kit.Button(T(prefix+"import"), advance, kit.Disabled(!canImport),
			kit.Loading(importing)))
	} else {
		actions = append(actions, kit.Button(T("g.next"), advance, kit.Disabled(!canImport)))
	}
	if total > 1 {
		// The footer right-aligns its actions; the progress sits at its left edge, centred on the buttons.
		actions = append(actions, fairy.Row(
			fairy.Text(itoa(step+1)+" / "+itoa(total)).FontSize(12).LineHeight(16).Medium().Color(t.MutedForeground),
		).Align(fairy.Center).Left(16).Top(16).Bottom(16))
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title:           T(prefix + "import"),
		Description:     T(prefix+"importUserDataDescription", "root", root),
		Body:            body,
		BodyKey:         "xxmi.import." + itoa(step),
		Width:           512,
		Actions:         actions,
		OnClose:         dismiss,
		HideCloseButton: importing,
		Key:             "xxmi.import",
	})
}

func xxmiImportChoices(value string, items []kit.Item, choose func(string)) *fairy.El {
	t := kit.T()
	choices := fairy.Col().Gap(8).Role(fairy.RoleGroup).FocusGroup()
	for _, item := range items {
		active := item.Value == value
		caption := fairy.Col(fairy.Text(item.Label).FontSize(14).LineHeight(20).Wrap()).Gap(2).Grow(1).MinW(0)
		if item.Description != "" {
			caption.Child(fairy.Text(item.Description).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap())
		}
		fill := fairy.Transparent
		if t.Dark {
			fill = opacity(t.Input, 0.2)
		}
		border := t.Input
		if active {
			fill, border = t.Muted, t.Primary.Alpha(0.4)
		}
		indicator := fairy.Box().Size(16, 16).Shrink(0).Radius(8).Border(1, t.Input).Center().
			MarginEdges(2, 0, 0, 0)
		if t.Dark {
			indicator.Fill(opacity(t.Input, 0.3))
		}
		if active {
			indicator.Border(1, t.Primary).Fill(t.Primary).
				Child(fairy.Box().Size(8, 8).Radius(4).Fill(t.PrimaryForeground))
		}
		choice := fairy.Row(indicator, caption).Key(item.Value).MinH(44).PadXY(12, 10).Gap(10).Align(fairy.Start).
			Radius(8).Border(1, border).Fill(fill).Role(fairy.RoleRadio).Checked(active).Label(item.Label).
			Animate(motion.Fast)
		if item.Disabled {
			choice.Disabled(true).Opacity(0.5)
		} else {
			choice.HoverFill(t.Muted.Alpha(0.5)).Cursor(fairy.CursorPointer).Focusable().FocusSelected(active).
				FocusVisibleBorder(t.Ring).FocusRing(3, t.Ring.Alpha(0.5)).OnClick(func() { choose(item.Value) })
		}
		choices.Child(choice)
	}
	return choices
}

func (s *Shell) importExternal(input xxmi.ImportExternalLauncherInput) {
	x := &s.xxmi
	s.xxmiAction("import-external", func(ctx context.Context) error {
		_, err := s.deps.XXMI.ImportExternalLauncher(ctx, input)
		return err
	}, func() {
		x.manage.importOpen, x.manage.root = false, nil
		x.refresh()
		s.forgetXXMISettings()
		s.toasts.Show(kit.ToastSuccess, T("page.setting.xxmi.builtin.imported"), "")
		// Listing games makes the backend follow the imported importer folders with the games that use them.
		if s.mods.started {
			s.loadGames(false)
		}
	}, func(err error) string {
		if code := firstCode(err.Error(), importErrorCodes); code != "" {
			return T("page.setting.xxmi.builtin.importErrors." + code)
		}
		return ""
	})
}

func (s *Shell) externalLauncherSection() *fairy.El {
	t := kit.T()
	x := &s.xxmi
	e := &x.external
	data := fetch(s, &x.data, s.deps.XXMI.GetXXMIData)
	saved := ""
	if data.value.XXMIPath != nil {
		saved = *data.value.XXMIPath
	}
	path := saved
	if e.path != nil {
		path = *e.path
	}

	pathRow := fairy.Row(
		kit.TextField(path, func(value string) { e.path = &value }, kit.Label(T("page.setting.xxmi.xxmiPath"))).
			Key("xxmi.external-path").Grow(1).MinW(0),
		kit.Button(T("page.setting.xxmi.autoScan"), s.findXXMIPath, kit.Outline, kit.Loading(x.busy["find-path"])),
		kit.Button(T("g.save"), func() { s.saveXXMIPath(path) }, kit.Disabled(path == "" || path == saved),
			kit.Loading(x.busy["save-path"])),
	).Gap(8).Align(fairy.Center)
	pathSection := fairy.Col(fairy.Text(T("page.setting.xxmi.xxmiPath")).FontSize(14).LineHeight(20).Medium(), pathRow).
		Gap(12)
	sections := []*fairy.El{pathSection}
	if e.autoScanned {
		pathSection.Child(xxmiAlert(lucide.Info, false, T("page.setting.xxmi.fn.findXXMIPath.alert.title"),
			T("page.setting.xxmi.fn.findXXMIPath.alert.description")))
	}
	sections = append(sections, kit.Separator(), s.externalDLLVersion(data))
	if saved != "" {
		if len(data.value.EnabledImporters) > 0 {
			rows := []*fairy.El{fairy.Col(
				fairy.Text(T("page.setting.xxmi.packageVersion")).FontSize(14).LineHeight(20).Medium(),
				fairy.Text(T("page.setting.xxmi.packageVersionDescription")).FontSize(12).LineHeight(16).
					Color(t.MutedForeground),
			).Gap(2)}
			for _, importer := range data.value.EnabledImporters {
				rows = append(rows, s.externalPackageRow(importer))
			}
			sections = append(sections, kit.Separator(), fairy.Col(rows...).Gap(4))
		}
		sections = append(sections, kit.Separator(), s.externalImporters(data.value))
	}
	return fairy.Col(sections...).Gap(24).FontSize(14)
}

func (s *Shell) findXXMIPath() {
	x := &s.xxmi
	var found *string
	x.external.autoScanned = false
	s.xxmiAction("find-path", func(ctx context.Context) error {
		var err error
		found, err = s.deps.XXMI.FindXXMIPath(ctx)
		return err
	}, func() {
		if found == nil || *found == "" {
			s.toasts.Show(kit.ToastError, T("page.setting.xxmi.fn.findXXMIPath.xxmiNotFound"), "")
			return
		}
		x.external.path, x.external.autoScanned = found, true
	}, nil)
}

func (s *Shell) saveXXMIPath(path string) {
	x := &s.xxmi
	// A folder without the launcher's config is a warning, as React showed it, not a failure to log.
	configMissing := false
	s.xxmiAction("save-path", func(ctx context.Context) error {
		err := s.deps.XXMI.SaveXXMIPath(ctx, path)
		if err != nil && strings.Contains(err.Error(), "XXMI Launcher Config.json not found") {
			configMissing = true
			return nil
		}
		return err
	}, func() {
		if configMissing {
			s.toasts.Show(kit.ToastWarning, T("page.setting.xxmi.fn.saveXXMIPath.configNotFound"), "")
			return
		}
		x.external.path, x.external.autoScanned = nil, false
		x.refresh()
	}, nil)
}

func (s *Shell) externalDLLVersion(data *query[xxmi.Data]) *fairy.El {
	t := kit.T()
	x := &s.xxmi
	e := &x.external
	const prefix = "page.setting.xxmi."
	hasPath := data.value.XXMIPath != nil && *data.value.XXMIPath != ""
	installed := ""
	if data.value.DLLVersion != nil {
		installed = *data.value.DLLVersion
	}
	releases := s.xxmiReleases("external-libs", s.deps.XXMI.GetLibsReleases)
	var tags []string
	for _, release := range releases.value {
		tags = append(tags, release.Tag)
	}
	version := e.dllVersion
	if version == "" && len(tags) > 0 {
		version = tags[0]
	}

	description := T(prefix + "dllVersionDescription")
	if hasPath {
		current := installed
		for _, tag := range tags {
			if sameVersion(tag, installed) {
				current = tag
			}
		}
		if current == "" {
			current = T(prefix + "dllVersionUnknown")
		}
		description += "\n" + T(prefix+"dllVersionCurrent", "version", current)
	}

	var control *fairy.El
	switch {
	case data.loaded && !hasPath:
		control = fairy.Text(T(prefix + "persistNotFoundXXMI")).Color(t.MutedForeground)
	case releases.err != nil:
		control = fairy.Row(fairy.Text(T(prefix+"dllVersionLoadFailed")).Color(t.Destructive),
			kit.Button(T(prefix+"dllVersionRetry"), keyed(x.releases, "external-libs").invalidate, kit.Outline,
				kit.Small)).Gap(8).Align(fairy.Center)
	case !releases.loaded:
		control = fairy.Row(kit.Spinner(14), fairy.Text(T(prefix+"dllVersionLoading"))).Gap(8).Align(fairy.Center).
			Color(t.MutedForeground)
	case len(tags) == 0:
		control = fairy.Text(T(prefix + "dllVersionEmpty")).Color(t.MutedForeground)
	default:
		items := []kit.Item{}
		for _, tag := range tags {
			items = append(items, kit.Item{Value: tag, Label: tag, Disabled: sameVersion(tag, installed)})
		}
		control = fairy.Row(
			kit.Select("xxmi.external-dll", version, items, func(value string) { e.dllVersion = value },
				kit.Label(T(prefix+"dllVersion")), kit.MaxHeight(256)).W(144),
			kit.Button(T("g.confirm"), func() {
				s.xxmiAction("install-dll", func(ctx context.Context) error {
					return s.deps.XXMI.InstallDLLVersion(ctx, xxmi.InstallDLLVersionInput{Version: version})
				}, x.refresh, func(err error) string {
					if strings.Contains(err.Error(), "XXMI Launcher") {
						return T(prefix + "fn.installDllVersion.launcherCloseFailed")
					}
					return T(prefix + "fn.installDllVersion.failed")
				})
			}, kit.Disabled(!hasPath || version == "" || sameVersion(version, installed)),
				kit.Loading(x.busy["install-dll"])),
		).Gap(8).Align(fairy.Center)
	}
	return settingRow(T(prefix+"dllVersion"), description, control).Pad(0)
}

func (s *Shell) externalPackageRow(importer xxmi.EnabledImporter) *fairy.El {
	t := kit.T()
	x := &s.xxmi
	e := &x.external
	const prefix = "page.setting.xxmi."
	key := importer.Key
	installed := ""
	if importer.InstalledVersion != nil {
		installed = *importer.InstalledVersion
	}
	releases := s.xxmiReleases("external-importer:"+key, func(ctx context.Context) ([]xxmi.ReleaseInfo, error) {
		return s.deps.XXMI.GetImporterReleases(ctx, key)
	})
	var tags []string
	for _, release := range releases.value {
		tags = append(tags, release.Tag)
	}
	version := e.versions[key]
	if version == "" && len(tags) > 0 {
		version = tags[0]
	}
	current := installed
	for _, tag := range tags {
		if sameVersion(tag, installed) {
			current = tag
		}
	}
	if current == "" {
		current = T(prefix + "packageVersionUnknown")
	}

	var control *fairy.El
	switch {
	case releases.err != nil:
		control = fairy.Row(fairy.Text(T(prefix+"packageVersionLoadFailed")).Color(t.Destructive),
			kit.Button(T(prefix+"packageVersionRetry"), keyed(x.releases, "external-importer:"+key).invalidate,
				kit.Outline, kit.Small)).Gap(8).Align(fairy.Center)
	case !releases.loaded:
		control = fairy.Row(kit.Spinner(14), fairy.Text(T(prefix+"packageVersionLoading"))).Gap(8).Align(fairy.Center).
			Color(t.MutedForeground)
	case len(tags) == 0:
		control = fairy.Text(T(prefix + "packageVersionEmpty")).Color(t.MutedForeground)
	default:
		items := []kit.Item{}
		for _, tag := range tags {
			items = append(items, kit.Item{Value: tag, Label: tag, Disabled: sameVersion(tag, installed)})
		}
		id := "install-package:" + key
		control = fairy.Row(
			kit.Select("xxmi.external-package:"+key, version, items, func(value string) { e.versions[key] = value },
				kit.Label(T(prefix+"packageVersion")), kit.MaxHeight(256)).W(144),
			kit.Button(T("g.confirm"), func() {
				s.xxmiAction(id, func(ctx context.Context) error {
					return s.deps.XXMI.InstallImporterPackage(ctx,
						xxmi.InstallImporterPackageInput{Importer: key, Version: version})
				}, x.refresh, func(err error) string {
					if message := importerFolderMessage(err); message != "" {
						return message
					}
					if strings.Contains(err.Error(), "XXMI Launcher") {
						return T(prefix + "fn.installImporterPackage.launcherCloseFailed")
					}
					return T(prefix + "fn.installImporterPackage.failed")
				})
			}, kit.Disabled(version == "" || sameVersion(version, installed)), kit.Loading(x.busy[id])),
		).Gap(8).Align(fairy.Center)
	}
	return settingRow(key, T(prefix+"packageVersionCurrent", "version", current), control).Pad(0)
}

func (s *Shell) externalImporters(data xxmi.Data) *fairy.El {
	t := kit.T()
	x := &s.xxmi
	e := &x.external
	const prefix = "page.setting.xxmi."
	header := fairy.Row(fairy.Text(T(prefix + "activeImporter")).FontSize(14).LineHeight(20).Medium().Grow(1)).
		Gap(12).Align(fairy.Center)
	if len(data.DisabledImporters) > 0 {
		label := T(prefix + "showDisabledImporters")
		toggle := func(on bool) { e.showDisabled = on }
		header.Child(fairy.Row(kit.Checkbox(e.showDisabled, toggle).Label(label),
			fairy.Text(label).FontSize(12).LineHeight(16).Color(t.MutedForeground).
				Role(fairy.RoleHidden).OnClick(func() { toggle(!e.showDisabled) }),
		).Gap(8).Align(fairy.Center))
	}

	type entry struct {
		importer xxmi.EnabledImporter
		enabled  bool
	}
	var entries []entry
	for _, importer := range data.EnabledImporters {
		entries = append(entries, entry{importer, true})
	}
	if e.showDisabled {
		for _, importer := range data.DisabledImporters {
			entries = append(entries, entry{importer, false})
		}
	}
	anyStarting := len(x.guard.starting) > 0
	grid := fairy.Row().Justify(fairy.JustifyEvenly).Gap(8).PadEdges(8, 0, 0, 0)
	for _, item := range entries {
		key, enabled := item.importer.Key, item.enabled
		version := T(prefix + "packageVersionUnknown")
		if item.importer.InstalledVersion != nil {
			version = *item.importer.InstalledVersion
		}
		icon := fairy.Box(s.importerIcon(key, 64)).Size(64, 64)
		if x.guard.starting[key] {
			icon.Child(fairy.Box(kit.Spinner(32)).Absolute().WFull().HFull().Center().Radius(6).
				Fill(fairy.Hex(0x000000).Alpha(0.6)))
		}
		tile := fairy.Col(icon, fairy.Text(key).FontSize(12).LineHeight(16), fairy.Text(version).FontSize(12).LineHeight(16)).
			Key("xxmi.tile:" + key).
			Gap(4).
			Align(fairy.Center).
			Label(key)
		switch {
		case !enabled:
			tile.Opacity(0.5)
		case anyStarting && !x.guard.starting[key]:
			tile.Opacity(0.5)
		default:
			tile.Cursor(fairy.CursorPointer).Focusable().FocusRing(3, t.Ring.Alpha(0.5)).OnClick(func() {
				if len(x.guard.starting) == 0 {
					s.startImporter(key)
				}
			})
		}
		toggle := kit.MenuItem{Label: T(prefix + "disableImporter"), Icon: lucide.EyeOff, Disabled: anyStarting}
		if !enabled {
			toggle.Label, toggle.Icon = T(prefix+"enableImporter"), lucide.Eye
		}
		toggle.OnClick = func() {
			s.xxmiAction("toggle-importer:"+key, func(ctx context.Context) error {
				return s.deps.XXMI.SetExternalImporterEnabled(ctx, key, !enabled)
			}, func() {
				// The importer list feeds the mod and tool screens as well as this page.
				x.refresh()
				if s.mods.started {
					s.loadGames(false)
				}
			}, nil)
		}
		grid.Child(
			kit.ContextMenu("xxmi.tile-menu:"+key, tile, func() []kit.MenuItem { return []kit.MenuItem{toggle} }),
		)
	}
	return fairy.Col(header, grid).Gap(4)
}
