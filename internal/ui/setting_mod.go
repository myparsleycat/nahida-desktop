package ui

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/setting"
)

var modSettingKeys = []string{
	setting.KeyModArchiveExtractPathMode,
	setting.KeyModDeleteArchiveAfterExtract,
	setting.KeyModMoveFolderInsteadOfCopy,
	setting.KeyModAutoInspectFix,
	setting.KeyModSearchModPreview,
	setting.KeyModAutoResolveDownloadTarget,
	setting.KeyModAutoResolveDownloadTargetSources,
	setting.KeyModCopyShaderFixesOnEnable,
	setting.KeyModDisabledPrefixStyle,
	setting.KeyModSidebarLayout,
	setting.KeyModGridLayoutMode,
	setting.KeyModGridModelPreview,
	setting.KeyModGridResponsiveBaseWidth,
	setting.KeyModGridFixedCardWidth,
	setting.KeyModGridFixedColumnCount,
}

// downloadSources are the sources a download target can be resolved for, in the order the React screen used.
var downloadSources = []string{"gamebanana", "nahidaLive", "hui", "drive"}

type modSection struct {
	compressionLoading bool
	// compressionFailed hides the compression card after a failed load until the section opens again, as React
	// hid it, instead of reading the state again on every frame.
	compressionFailed bool
	// compressionBusy is set while a compression request runs; the controls wait for it.
	compressionBusy bool
	thresholdDraft  *string
}

func (s *Shell) modSettings(windowWidth float32) *fairy.El {
	if !s.settingsLoaded(modSettingKeys...) {
		return s.settingsLoading()
	}
	const management = "page.setting.mod.mod_management."
	extractModes := []kit.Item{
		{Value: "flatten_single_root", Label: T(management + "archiveExtractPathModes.flatten_single_root")},
		{Value: "keep_archive_root", Label: T(management + "archiveExtractPathModes.keep_archive_root")},
		{Value: "ask_every_time", Label: T(management + "archiveExtractPathModes.ask_every_time")},
	}
	prefixStyles := []kit.Item{
		{Value: "space", Label: T(management + "disabledPrefixStyles.space")},
		{Value: "underscore", Label: T(management + "disabledPrefixStyles.underscore")},
	}

	return settingsBody(
		settingsGroup(T(management+"title"),
			settingRow(T(management+"archiveExtractPathMode"), T(management+"archiveExtractPathModeDescription"),
				s.settingSelect(setting.KeyModArchiveExtractPathMode, extractModes, 220,
					kit.Label(T(management+"archiveExtractPathMode")))),
			s.settingSwitch(setting.KeyModDeleteArchiveAfterExtract, T(management+"deleteArchiveAfterExtract"),
				T(management+"deleteArchiveAfterExtractDescription")),
			s.settingSwitch(setting.KeyModMoveFolderInsteadOfCopy, T(management+"moveFolderInsteadOfCopy"),
				T(management+"moveFolderInsteadOfCopyDescription")),
			s.settingSwitch(setting.KeyModAutoInspectFix, T(management+"autoInspectFix"),
				T(management+"autoInspectFixDescription")),
			s.settingSwitch(setting.KeyModCopyShaderFixesOnEnable, T(management+"copyShaderFixesOnEnable"),
				T(management+"copyShaderFixesOnEnableDescription")),
			settingRow(T(management+"disabledPrefixStyle"), T(management+"disabledPrefixStyleDescription"),
				s.settingSelect(setting.KeyModDisabledPrefixStyle, prefixStyles, 180,
					kit.Label(T(management+"disabledPrefixStyle")))),
			s.settingSwitch(setting.KeyModSearchModPreview, T(management+"searchModPreview"),
				T(management+"searchModPreviewDescription")),
			s.downloadTargetSettings(windowWidth),
		),
		settingsGroup(T("page.setting.mod.layout.title"), s.modLayoutSettings()...),
		s.compressionSettings(),
	)
}

func (s *Shell) downloadTargetSettings(windowWidth float32) *fairy.El {
	const management = "page.setting.mod.mod_management."
	t := kit.T()
	enabled := s.boolSetting(setting.KeyModAutoResolveDownloadTarget)
	selected := s.stringsSetting(setting.KeyModAutoResolveDownloadTargetSources)
	sources := fairy.Col().Gap(8).Pad(12).Radius(t.RadiusMd()).Fill(t.Muted.Alpha(0.5))
	columns := 3
	if windowWidth < 640 {
		columns = 1
	}
	var sourceRow *fairy.El
	for i, source := range downloadSources {
		if i%columns == 0 {
			sourceRow = fairy.Row().Gap(8)
			sources.Child(sourceRow)
		}
		sourceRow.Child(kit.Checkbox(slices.Contains(selected, source), func(checked bool) {
			current := s.stringsSetting(setting.KeyModAutoResolveDownloadTargetSources)
			next := slices.DeleteFunc(slices.Clone(current), func(item string) bool { return item == source })
			if checked {
				next = append(next, source)
			}
			s.setSetting(setting.KeyModAutoResolveDownloadTargetSources, next)
		}, kit.Label(T(management+"downloadSources."+source)), kit.Disabled(!enabled)).Grow(1).Basis(0).MinW(0))
	}
	for i := len(downloadSources) % columns; i > 0 && i < columns; i++ {
		sourceRow.Child(fairy.Box().Grow(1).Basis(0).MinW(0))
	}
	return fairy.Col(
		s.settingSwitch(setting.KeyModAutoResolveDownloadTarget, T(management+"autoResolveDownloadTarget"),
			T(management+"autoResolveDownloadTargetDescription")).PadXY(0, 0),
		sources,
	).Gap(12).PadXY(0, 12)
}

func (s *Shell) modLayoutSettings() []*fairy.El {
	const layout = "page.setting.mod.layout."
	t := kit.T()
	sidebarModes := []kit.Item{
		{Value: "row", Label: T(layout + "sidebar.modes.row")},
		{Value: "grid", Label: T(layout + "sidebar.modes.grid")},
	}
	gridModes := []kit.Item{
		{Value: "responsive", Label: T(layout + "grid.modes.responsive")},
		{Value: "fixed_card_width", Label: T(layout + "grid.modes.fixed_card_width")},
		{Value: "fixed_column_count", Label: T(layout + "grid.modes.fixed_column_count")},
	}
	rows := []*fairy.El{
		settingRow(T(layout+"sidebar.mode"), T(layout+"sidebar.modeDescription"),
			s.settingSelect(setting.KeyModSidebarLayout, sidebarModes, 220, kit.Label(T(layout+"sidebar.mode")))),
		s.settingSwitch(setting.KeyModGridModelPreview, T(layout+"gridModelPreview"),
			T(layout+"gridModelPreviewDescription")),
		fairy.Col(
			fairy.Text(T(layout+"grid.mode")).FontSize(14).LineHeight(20).Medium(),
			fairy.Text(T(layout+"grid.modeDescription")).FontSize(12).LineHeight(16).Color(t.MutedForeground),
		).Gap(4).PadXY(0, 12),
	}
	grid := fairy.Col(s.settingSelect(setting.KeyModGridLayoutMode, gridModes, 220, kit.Label(T(layout+"grid.mode"))).
		AlignSelf(fairy.End)).Gap(20).PadXY(0, 12)
	switch s.stringSetting(setting.KeyModGridLayoutMode) {
	case "responsive":
		grid.Child(settingRow(
			T(layout+"grid.responsiveBaseWidth"),
			T(layout+"grid.responsiveBaseWidthDescription"),
			s.settingNumber(
				setting.KeyModGridResponsiveBaseWidth,
				T(layout+"grid.responsiveBaseWidth"),
				96,
			),
		).PadXY(0, 0))
	case "fixed_card_width":
		grid.Child(settingRow(T(layout+"grid.fixedCardWidth"), T(layout+"grid.fixedCardWidthDescription"),
			s.settingNumber(setting.KeyModGridFixedCardWidth, T(layout+"grid.fixedCardWidth"), 96)).PadXY(0, 0))
	case "fixed_column_count":
		grid.Child(settingRow(T(layout+"grid.fixedColumnCount"),
			T(layout+"grid.fixedColumnCountDescription"),
			s.settingNumber(setting.KeyModGridFixedColumnCount, T(layout+"grid.fixedColumnCount"), 96)).PadXY(0, 0))
	}
	return append(rows, grid)
}

func (s *Shell) compressionSettings() *fairy.El {
	const prefix = "page.setting.mod.compression."
	st := &s.settings
	if st.compression == nil {
		if !st.modSection.compressionFailed {
			s.loadCompression()
		}
		return fairy.Box()
	}
	state := *st.compression
	t := kit.T()
	running := state.Status == "checking" || state.Status == "compressing" || state.Status == "decompressing"
	locked := !state.CanConfigure || st.modSection.compressionBusy

	methods := []kit.Item{{Value: "zstd", Label: "Zstd"}, {Value: "xpress4k", Label: "XPRESS4K"}}
	rows := []*fairy.El{
		settingRow(T(prefix+"method"), T(prefix+"methods."+state.Method+".description"),
			kit.Select("settings.compression.method", state.Method, methods, func(method string) {
				s.setCompressionConfig(method, state.ThresholdMiB)
			}, kit.Disabled(locked), kit.Label(T(prefix+"method"))).W(160)),
	}
	if state.Method == "zstd" {
		rows = append(rows, settingRow(T(prefix+"threshold"), T(prefix+"thresholdDescription"),
			fairy.Row(s.compressionThreshold(state, locked), fairy.Text("MiB").FontSize(14).LineHeight(20).
				Color(t.MutedForeground)).Gap(8).
				Align(fairy.Center)))
	}

	checked := state.Enabled
	if running && state.TargetEnabled != nil {
		checked = *state.TargetEnabled
	}
	head := fairy.Col(fairy.Text(T(prefix + "status." + state.Status)).FontSize(14).LineHeight(20).Medium()).
		Gap(4).Grow(1).MinW(0)
	if state.CurrentFileName != "" {
		head.Child(
			fairy.Text(state.CurrentFileName).FontSize(12).LineHeight(16).Color(t.MutedForeground).MaxW(384).Ellipsis(),
		)
	}
	status := fairy.Col(fairy.Row(head,
		kit.Switch(checked, s.setCompressionEnabled, kit.Disabled(!state.CanToggle || st.modSection.compressionBusy)).
			Label(T(prefix+"toggle")),
	).Gap(16).Align(fairy.Center)).Gap(12).Pad(12).Radius(6).Fill(t.Muted.Alpha(0.5))
	if running {
		progress := compressionProgress(state)
		status.Child(fairy.Col(
			kit.Progress(progress/100),
			fairy.Row(
				fairy.Text(T(prefix+"files", "processed", state.ProcessedFiles, "total", state.TotalFiles)).
					FontSize(12).LineHeight(16),
				fairy.Spacer(),
				fairy.Text(fmt.Sprintf("%s / %s · %d%%", formatSize(state.ProcessedBytes), formatSize(state.TotalBytes),
					int(math.Round(progress)))).FontSize(12).LineHeight(16),
			).FontSize(12).Color(t.MutedForeground),
		).Gap(6))
	}
	if state.Status == "error" {
		status.Child(fairy.Text(T(prefix + "error")).FontSize(12).LineHeight(16).Color(t.Destructive))
	}
	rows = append(rows, status)
	return settingsGroup(T(prefix+"title"), rows...)
}

func compressionProgress(state mod.CompressionState) float64 {
	switch {
	case state.TotalBytes > 0:
		return min(100, float64(state.ProcessedBytes)/float64(state.TotalBytes)*100)
	case state.TotalFiles > 0:
		return min(100, float64(state.ProcessedFiles)/float64(state.TotalFiles)*100)
	}
	return 0
}

func (s *Shell) compressionThreshold(state mod.CompressionState, locked bool) *fairy.El {
	section := &s.settings.modSection
	text := itoa(state.ThresholdMiB)
	if section.thresholdDraft != nil {
		text = *section.thresholdDraft
	}
	commit := func() {
		if section.thresholdDraft == nil {
			return
		}
		draft := *section.thresholdDraft
		section.thresholdDraft = nil
		current := s.settings.compression
		if current == nil {
			return
		}
		next := current.ThresholdMiB
		if n, ok := parseInt(draft); ok {
			next = min(64, max(1, n))
		}
		if next != current.ThresholdMiB {
			s.setCompressionConfig(current.Method, next)
		}
	}
	return kit.TextField(text, func(value string) { section.thresholdDraft = &value },
		kit.Disabled(locked), kit.Label(T("page.setting.mod.compression.threshold"))).
		Key("settings.compression.threshold").W(96).Grow(0).OnBlur(commit).OnSubmit(func(string) { commit() })
}

func (s *Shell) loadCompression() {
	section := &s.settings.modSection
	if section.compressionLoading {
		return
	}
	section.compressionLoading = true
	s.background(func(ctx context.Context) {
		state, err := s.deps.Mod.GetCompressionState(ctx)
		if err != nil {
			_ = infra.ReportError(s.deps.Log, err, "Mod.GetCompressionState", infra.Diagnostic{
				Operation: "mod.compression-state", Stage: "settings",
			})
		}
		fairy.Go(func() {
			section.compressionLoading = false
			section.compressionFailed = err != nil
			if err == nil && s.settings.compression == nil {
				s.settings.compression = &state
			}
		})
	})
}

func (s *Shell) setCompressionConfig(method string, thresholdMiB int) {
	s.compressionRequest("page.setting.mod.compression.saveFailed", "set-config",
		func(ctx context.Context) (mod.CompressionState, error) {
			return s.deps.Mod.SetCompressionConfig(
				ctx,
				mod.CompressionConfig{Method: method, ThresholdMiB: thresholdMiB},
			)
		})
}

func (s *Shell) setCompressionEnabled(enabled bool) {
	s.compressionRequest("page.setting.mod.compression.toggleFailed", "set-enabled",
		func(ctx context.Context) (mod.CompressionState, error) {
			return s.deps.Mod.SetCompressionEnabled(ctx, enabled)
		})
}

// compressionRequest runs one compression change at a time and shows the state it returns.
func (s *Shell) compressionRequest(failure, stage string, request func(context.Context) (mod.CompressionState, error)) {
	section := &s.settings.modSection
	if section.compressionBusy {
		return
	}
	section.compressionBusy = true
	s.background(func(ctx context.Context) {
		state, err := request(ctx)
		if err != nil {
			err = infra.ReportError(s.deps.Log, err, "Mod.Compression", infra.Diagnostic{
				Operation: "mod.compression", Stage: stage,
			})
		}
		fairy.Go(func() {
			section.compressionBusy = false
			if err != nil {
				s.toasts.Show(kit.ToastError, T(failure), err.Error())
				return
			}
			s.settings.compression = &state
		})
	})
}
