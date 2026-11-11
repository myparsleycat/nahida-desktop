package ui

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/reshade"
	"nahida.live/desktop/internal/xxmi"
)

const (
	effectsAvailable = "available"
	effectsInstalled = "installed"
)

type reshadePage struct {
	// pending holds a game's switch state until its saved config is read back, so the switch does not flip twice.
	pending map[string]bool
	// wantedVersion is the latest version choice. Choices apply one at a time in order, because a version is
	// downloaded before it is stored and an earlier choice could otherwise finish last.
	wantedVersion  *string
	settingVersion bool

	selected      []string
	effectsBusy   bool
	category      string
	search        string
	presetEffects *reshade.PresetEffects
	presetBusy    bool
}

func (s *Shell) reshadeSettingsPage() *fairy.El {
	t := kit.T()
	x := &s.xxmi
	r := &x.reshade
	const prefix = "page.setting.xxmi.builtin.reshade."
	status := fetch(s, &x.reshadeStatus, s.deps.ReShade.Status).value
	versions := fetchFresh(s, &x.reshadeVersions, releaseMaxAge, func(ctx context.Context) ([]string, error) {
		return s.deps.ReShade.Versions(ctx, false)
	})
	target := status.Pinned
	if target == "" {
		target = status.Latest
	}

	binary := T(prefix + "notInstalled")
	if status.Active != "" {
		binary = T(prefix+"installed", "version", status.Active)
		if status.UpdateAvailable {
			binary += "\n" + T(prefix+"updateAvailable", "version", status.Latest)
		}
	}
	installLabel := T(prefix + "download")
	if status.Active != "" {
		installLabel = T(prefix + "update")
	}
	versionItems := []kit.Item{{Value: followLatest, Label: T("page.setting.xxmi.builtin.latest")}}
	choices := versions.value
	if !versions.loaded && status.Pinned != "" {
		choices = []string{status.Pinned}
	}
	for _, version := range choices {
		versionItems = append(versionItems, kit.Item{Value: version, Label: version})
	}
	version := status.Pinned
	if r.wantedVersion != nil {
		version = *r.wantedVersion
	}
	if version == "" {
		version = followLatest
	}
	binarySection := settingsGroup(T(prefix+"title"),
		settingRow(T(prefix+"binary"), binary, kit.Button(installLabel, func() {
			s.xxmiAction("reshade-install", func(ctx context.Context) error {
				_, err := s.deps.ReShade.Install(ctx)
				return err
			}, x.reshadeStatus.invalidate, nil)
		}, kit.Outline, kit.Small, kit.Icon(lucide.Download), kit.Disabled(target == "" || status.Active == target),
			kit.Loading(x.busy["reshade-install"]))),
		selectRow("reshade.version", T(prefix+"version"), T(prefix+"versionDescription"), version, versionItems,
			func(value string) {
				if value == followLatest {
					value = ""
				}
				r.wantedVersion = &value
				s.applyReShadeVersion()
			}),
	)

	overview := x.overview.value
	games := []*fairy.El{}
	for _, importer := range overview.Importers {
		games = append(games, s.reshadeGameRow(importer.Key))
	}
	if len(overview.Importers) == 0 {
		games = append(games, fairy.Text(T(prefix+"gamesEmpty")).FontSize(12).LineHeight(16).
			Color(t.MutedForeground).PadXY(0, 12))
	}

	folders := fairy.Row(kit.Button(T(prefix+"addPreset"), s.addReShadePresets, kit.Small, kit.Icon(lucide.Plus),
		kit.Loading(x.busy["reshade-presets"]))).Wrap().Gap(8).RowGap(8).PadXY(0, 12)
	for _, folder := range []string{"shaders", "textures", "addons", "presets"} {
		folders.Child(kit.Button(T(prefix+"folderNames."+folder), func() { s.openReShadeFolder("", folder) },
			kit.Outline, kit.Small, kit.Icon(lucide.FolderOpen)))
	}

	return xxmiBody(
		binarySection,
		settingsGroup(T(prefix+"games"), games...),
		s.effectPackagesSection(),
		settingsGroup(T(prefix+"folders"), folders),
	)
}

func (s *Shell) applyReShadeVersion() {
	r := &s.xxmi.reshade
	if r.settingVersion || r.wantedVersion == nil {
		return
	}
	version := *r.wantedVersion
	r.settingVersion = true
	s.background(func(ctx context.Context) {
		err := s.deps.ReShade.SetVersion(ctx, version)
		fairy.Go(func() {
			r.settingVersion = false
			if err != nil {
				s.xxmiFail(err, "reshade-set-version", map[string]any{"version": version}, "")
			}
			s.xxmi.reshadeStatus.invalidate()
			if r.wantedVersion != nil && *r.wantedVersion == version {
				r.wantedVersion = nil
				return
			}
			s.applyReShadeVersion()
		})
	})
}

func (s *Shell) openReShadeFolder(importer, folder string) {
	s.xxmiAction("reshade-folder:"+importer+":"+folder, func(ctx context.Context) error {
		paths, err := s.deps.ReShade.Paths(ctx, importer)
		if err != nil {
			return err
		}
		path := map[string]string{
			"shaders": paths.Shaders, "textures": paths.Textures, "addons": paths.Addons, "presets": paths.Presets,
			"game": paths.Game,
		}[folder]
		return s.deps.Platform.OpenPath(path)
	}, nil, nil)
}

func (s *Shell) addReShadePresets() {
	r := &s.xxmi.reshade
	const prefix = "page.setting.xxmi.builtin.reshade."
	s.pickFile(platform.OpenDialogOptions{
		Title: T(prefix + "addPreset"),
		Filters: []platform.FileFilter{
			{Name: T(prefix + "folderNames.presets"), Extensions: []string{"ini", "txt"}},
		},
		Properties: []string{"openFile", "multiSelections"},
		Role:       "reshade-preset",
	}, func(paths []string) {
		var effects reshade.PresetEffects
		s.xxmiAction("reshade-presets", func(ctx context.Context) error {
			var err error
			effects, err = s.deps.ReShade.AddPresets(ctx, paths)
			return err
		}, func() {
			s.toasts.Show(kit.ToastSuccess, T(prefix+"presetAdded", "count", len(paths)), "")
			if len(effects.Packages) > 0 || len(effects.Unknown) > 0 {
				r.presetEffects = &effects
			}
		}, func(err error) string {
			if strings.Contains(err.Error(), "RESHADE_PRESET_UNSUPPORTED") {
				return T(prefix + "presetUnsupported")
			}
			return ""
		})
	})
}

func (s *Shell) reshadeGameRow(importer string) *fairy.El {
	t := kit.T()
	r := &s.xxmi.reshade
	const prefix = "page.setting.xxmi.builtin.reshade."
	config := s.importerConfig(importer)
	pending, isPending := r.pending[importer]
	enabled := config.value.ReShade.Enabled
	if isPending {
		enabled = pending
	}
	c := config.value
	// The 3DMigoto loader injects on its own schedule, so ReShade cannot be loaded ahead of it.
	unsupported := config.loaded && c.Mode != xxmi.RuntimeXXMI && c.InjectionMethod != "Native"

	text := fairy.Col(fairy.Text(importer).FontSize(14).LineHeight(20).Medium()).Gap(2).Grow(1).MinW(0)
	if unsupported {
		text.Child(
			fairy.Text(T(prefix + "legacyUnsupported")).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap(),
		)
	}
	folder := T(prefix + "gameFolder")
	return fairy.Row(
		s.importerIcon(importer, 32),
		text,
		kit.IconButton(lucide.FolderOpen, func() { s.openReShadeFolder(importer, "game") }, kit.Ghost, kit.Small,
			kit.Tooltip(folder)).Label(importer+": "+folder),
		kit.Switch(enabled, func(next bool) { s.setReShadeEnabled(importer, next) },
			kit.Disabled(!config.loaded || isPending || unsupported && !enabled)).Label(importer),
	).Gap(12).Align(fairy.Center).PadXY(0, 12)
}

func (s *Shell) setReShadeEnabled(importer string, enabled bool) {
	x := &s.xxmi
	r := &x.reshade
	r.pending[importer] = enabled
	s.background(func(ctx context.Context) {
		// The config is read again so a change saved on the importer's page is not written back stale.
		current, err := s.deps.XXMI.GetImporterConfig(ctx, importer)
		if err == nil {
			current.ReShade.Enabled = enabled
			err = s.deps.XXMI.SaveImporterConfig(ctx, importer, current)
		}
		fairy.Go(func() {
			if err != nil {
				s.xxmiFail(err, "reshade-toggle", map[string]any{"importer": importer, "enabled": enabled},
					importer+": "+err.Error())
			}
			delete(r.pending, importer)
			keyed(x.configs, importer).invalidate()
		})
	})
}

func (s *Shell) effectPackagesSection() *fairy.El {
	t := kit.T()
	x := &s.xxmi
	r := &x.reshade
	const prefix = "page.setting.xxmi.builtin.reshade."
	header := fairy.Col(
		fairy.Text(T(prefix+"effects")).FontSize(14).LineHeight(20).Medium().Color(t.MutedForeground),
		fairy.Text(T(prefix+"effectsDescription")).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap(),
	).Gap(2).PadEdges(0, 4, 8, 4)
	section := func(children ...*fairy.El) *fairy.El {
		return fairy.Col(header, fairy.Col(fairy.Col(children...).Gap(8).PadXY(0, 12)).
			PadEdges(0, 14, 0, 30).BorderEdges(0, 0, 0, 1, t.Foreground.Alpha(0.2))).Gap(4)
	}

	query := fetch(s, &x.effects, s.deps.ReShade.ListEffectPackages)
	switch {
	case !query.loaded && query.err != nil:
		return section(fairy.Row(
			fairy.Text(T(prefix+"effectsLoadFailed")).FontSize(14).LineHeight(20).Color(t.Destructive).Grow(1),
			kit.Button(T("page.setting.xxmi.dllVersionRetry"), x.effects.invalidate, kit.Outline, kit.Small),
		).Gap(16).Align(fairy.Center))
	case !query.loaded:
		return section(fairy.Row(kit.Spinner(14), fairy.Text(T(prefix+"effectsLoading")).FontSize(14).LineHeight(20).
			Color(t.MutedForeground)).Gap(8).
			Align(fairy.Center))
	}

	var installed, available []reshade.EffectPackage
	for _, pkg := range query.value {
		if pkg.Installed {
			installed = append(installed, pkg)
		} else {
			available = append(available, pkg)
		}
	}
	// An emptied category has nothing to act on, so the other one is shown in its place.
	category := r.category
	switch {
	case len(installed) == 0:
		category = effectsAvailable
	case len(available) == 0:
		category = effectsInstalled
	case category == "":
		category = effectsAvailable
	}
	shown := available
	if category == effectsInstalled {
		shown = installed
	}
	// Hidden by the search or not, a checked package stays part of the batch.
	var picked []string
	for _, pkg := range shown {
		if slices.Contains(r.selected, pkg.ID) {
			picked = append(picked, pkg.ID)
		}
	}

	run := func(ids []string, action func(context.Context) error) {
		if r.effectsBusy {
			return
		}
		r.effectsBusy = true
		s.background(func(ctx context.Context) {
			err := action(ctx)
			fairy.Go(func() {
				r.effectsBusy = false
				if err != nil {
					s.xxmiFail(err, "reshade-effects", map[string]any{"packages": ids}, "")
				}
				// A failed batch still applies to the packages before the failure.
				x.effects.invalidate()
				r.selected = slices.DeleteFunc(r.selected, func(id string) bool { return slices.Contains(ids, id) })
			})
		})
	}
	remove := func(ids []string) func(context.Context) error {
		return func(ctx context.Context) error {
			for _, id := range ids {
				if err := s.deps.ReShade.RemoveEffectPackage(ctx, id); err != nil {
					return err
				}
			}
			return nil
		}
	}

	var actions *fairy.El
	if category == effectsInstalled {
		ids := picked
		actions = kit.Button(T(prefix+"removeSelected", "count", len(picked)), func() { run(ids, remove(ids)) },
			kit.Outline, kit.Small, kit.Icon(lucide.Trash), kit.Disabled(r.effectsBusy || len(picked) == 0))
	} else {
		ids := picked
		actions = fairy.Row(
			kit.Button(T(prefix+"selectRecommended"), func() {
				r.selected = nil
				for _, pkg := range available {
					if pkg.Recommended && pkg.Supported {
						r.selected = append(r.selected, pkg.ID)
					}
				}
			}, kit.Outline, kit.Small, kit.Disabled(r.effectsBusy)),
			kit.Button(T(prefix+"installSelected", "count", len(picked)), func() {
				run(ids, func(ctx context.Context) error {
					_, err := s.deps.ReShade.InstallEffectPackages(ctx, ids)
					return err
				})
			}, kit.Small, kit.Icon(lucide.Download), kit.Disabled(r.effectsBusy || len(picked) == 0),
				kit.Loading(r.effectsBusy)),
		).Gap(8)
	}

	tabs := xxmiTabs(category, []kit.Item{
		{Value: effectsAvailable, Label: T(prefix+"effectAvailable") + " (" + strconv.Itoa(len(available)) + ")",
			Disabled: len(available) == 0},
		{Value: effectsInstalled, Label: T(prefix+"effectInstalled") + " (" + strconv.Itoa(len(installed)) + ")",
			Disabled: len(installed) == 0},
	}, func(value string) { r.category = value }, false)

	needle := strings.ToLower(strings.TrimSpace(r.search))
	list := fairy.Col().Gap(4).MaxH(384).ScrollY().MarginEdges(0, -8, 0, -8)
	matched := 0
	for _, pkg := range shown {
		if !strings.Contains(strings.ToLower(pkg.Name+"\n"+pkg.Description), needle) {
			continue
		}
		matched++
		list.Child(s.effectPackageItem(pkg, slices.Contains(r.selected, pkg.ID), func(checked bool) {
			if checked {
				r.selected = append(r.selected, pkg.ID)
			} else {
				r.selected = slices.DeleteFunc(r.selected, func(id string) bool { return id == pkg.ID })
			}
		}, func() { run([]string{pkg.ID}, remove([]string{pkg.ID})) }))
	}
	if matched == 0 {
		list.Child(fairy.Text(T(prefix+"effectsNoMatch")).FontSize(12).LineHeight(16).
			Color(t.MutedForeground).PadXY(8, 12))
	}

	search := kit.TextField(r.search, func(value string) { r.search = value }, kit.Icon(lucide.Search),
		kit.Placeholder(T("g.search")), kit.Label(T("g.search"))).Key("reshade.effect-search").PadXY(8, 0).Gap(4)
	if t.Dark {
		search.Fill(fairy.Transparent)
	}
	return section(
		fairy.Row(tabs, fairy.Box().Grow(1), actions).Wrap().Gap(8).RowGap(8).Align(fairy.Center),
		search,
		list,
	)
}

func (s *Shell) effectPackageItem(pkg reshade.EffectPackage, checked bool, onChange func(bool), onRemove func(),
) *fairy.El {
	t := kit.T()
	const prefix = "page.setting.xxmi.builtin.reshade."
	busy := s.xxmi.reshade.effectsBusy
	title := fairy.Row(fairy.Text(pkg.Name).FontSize(14).LineHeight(20).Medium()).Wrap().Gap(6).Align(fairy.Center)
	if !pkg.Supported {
		title.Child(kit.Badge(T(prefix+"effectUnsupported"), kit.Outline))
	}
	text := fairy.Col(title).Gap(2).Grow(1).MinW(0)
	if pkg.Description != "" {
		text.Child(fairy.Text(pkg.Description).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap())
	}
	locked := busy || !pkg.Supported && !pkg.Installed
	if !locked {
		text.Cursor(fairy.CursorPointer).OnClick(func() { onChange(!checked) })
	}
	row := fairy.Row(
		kit.Checkbox(checked, onChange, kit.Disabled(locked)).Label(pkg.Name).MarginEdges(2, 0, 0, 0),
		text,
	).Key("reshade.effect:" + pkg.ID).Gap(12).Align(fairy.Start).Pad(8).Radius(6).HoverFill(t.Muted.Alpha(0.5))
	if pkg.Installed {
		label := T(prefix+"removeEffect", "name", pkg.Name)
		row.Child(kit.IconButton(lucide.Trash, onRemove, kit.Ghost, kit.Disabled(busy),
			kit.Tooltip(label)).Label(label))
	}
	return row
}

func (s *Shell) reshadeDialogs(cx *fairy.Cx) {
	r := &s.xxmi.reshade
	if r.presetEffects == nil {
		return
	}
	dismiss := func() { r.presetEffects = nil }
	s.presetEffectsDialog(cx, *r.presetEffects, T("page.setting.xxmi.builtin.reshade.presetEffectsInstall"), "",
		&r.presetBusy, dismiss, nil, dismiss)
}
