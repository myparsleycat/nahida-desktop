package ui

import (
	"context"
	"reflect"
	"slices"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"
	"github.com/myparsleycat/fairy/svg"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/setting"
)

type settingsSection string

const (
	sectionGeneral  settingsSection = "gen"
	sectionMod      settingsSection = "mod"
	sectionDrive    settingsSection = "drive"
	sectionAccount  settingsSection = "acc"
	sectionTransfer settingsSection = "transfer"
	sectionNetwork  settingsSection = "network"
	sectionTools    settingsSection = "tools"
	sectionAgent    settingsSection = "agent"
	sectionAdvanced settingsSection = "adv"
	sectionFeedback settingsSection = "feedback"
)

type settingsSectionInfo struct {
	id    settingsSection
	icon  *svg.Vector
	label string
}

func settingsSections() []settingsSectionInfo {
	return []settingsSectionInfo{
		{sectionGeneral, lucide.Settings, T("page.setting.tabs.general")},
		{sectionMod, lucide.Gamepad, T("page.setting.tabs.mod")},
		{sectionDrive, lucide.HardDrive, T("page.setting.tabs.drive")},
		{sectionAccount, lucide.User, T("page.setting.tabs.account")},
		{sectionTransfer, lucide.ArrowUpDown, T("page.setting.tabs.transfer")},
		{sectionNetwork, lucide.Network, T("page.setting.tabs.network")},
		{sectionTools, lucide.Wrench, T("page.setting.tabs.tools")},
		{sectionAgent, lucide.Bot, T("page.agent.title")},
		{sectionAdvanced, lucide.ServerCrash, T("page.setting.tabs.advanced")},
		{sectionFeedback, lucide.MessageSquareWarning, T("page.setting.tabs.feedback")},
	}
}

// settingsSectionOf reads the section of a /setting path; an unknown or missing one is the general section.
func settingsSectionOf(path string) settingsSection {
	rest, _ := strings.CutPrefix(path, string(RouteSetting)+"/")
	id, _, _ := strings.Cut(rest, "/")
	for _, section := range settingsSections() {
		if string(section.id) == id {
			return section.id
		}
	}
	return sectionGeneral
}

// settingsScreen is the state of the settings screen. values caches setting values by key; a key that is
// missing is loaded the next time a section shows it.
type settingsScreen struct {
	section settingsSection
	values  map[string]any
	loading map[string]bool
	loadErr error
	writes  chan func()
	// saving counts the writes of each key that have not finished.
	saving map[string]int
	// drafts holds the text of number fields while they are edited, by setting key.
	drafts map[string]string

	updateCheck updateCheck
	general     generalSection
	compression *mod.CompressionState
	modSection  modSection
	passwords   passwordList
	network     networkSection
	advanced    advancedSection
	feedback    feedbackSection
	agent       agentSettingsSection
}

func newSettingsScreen() settingsScreen {
	return settingsScreen{
		section: sectionGeneral,
		values:  map[string]any{},
		loading: map[string]bool{},
		writes:  make(chan func(), 256),
		saving:  map[string]int{},
		drafts:  map[string]string{},
	}
}

// openSettings shows section with fresh values: React mounted each section anew, which read its data again and
// retried a failed load.
func (s *Shell) openSettings(section settingsSection) {
	st := &s.settings
	st.section = section
	switch section {
	case sectionMod:
		st.modSection.compressionFailed = false
	case sectionAdvanced:
		if st.advanced.rows != nil {
			s.loadAdvanced()
		}
	case sectionAgent:
		if a := st.agent; !a.loading && !a.busy && !a.mcpSaving && a.login == nil {
			st.agent = agentSettingsSection{}
		}
	default:
	}
}

// runSettingWrites saves settings one at a time in the order they were changed, once startup maintenance is
// done, as the Wails service guard made setting writes wait.
func (s *Shell) runSettingWrites() {
	if s.deps.Ready != nil {
		_ = s.deps.Ready(context.Background())
	}
	for write := range s.settings.writes {
		write()
	}
}

// settingsLoaded reports whether keys are all loaded, starting a load for the ones that are not.
func (s *Shell) settingsLoaded(keys ...string) bool {
	st := &s.settings
	var missing []string
	for _, key := range keys {
		if _, ok := st.values[key]; !ok && !st.loading[key] {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 && st.loadErr == nil {
		for _, key := range missing {
			st.loading[key] = true
		}
		go func() {
			values, err := s.deps.Settings.GetMany(context.Background(), missing)
			if err != nil {
				err = infra.ReportError(s.deps.Log, err, "Settings.GetMany", infra.Diagnostic{
					Operation: "settings.load", Fields: map[string]any{"keys": missing},
				})
			}
			fairy.Go(func() {
				for _, key := range missing {
					delete(st.loading, key)
				}
				if err != nil {
					st.loadErr = err
					return
				}
				for key, value := range values {
					st.values[key] = value
				}
			})
		}()
	}
	for _, key := range keys {
		if _, ok := st.values[key]; !ok {
			return false
		}
	}
	return true
}

// revertOnSaveFailure lists the switches whose failed save React answered by showing the previous value with
// its own message.
var revertOnSaveFailure = []string{setting.KeyModAutoInspectFix, setting.KeyToolsWuwaFixerUpdateNotification}

// setSetting shows value at once and saves it in the background. A failed save drops the cached value, so the
// stored one is loaded again, or for revertOnSaveFailure shows the value from before the change.
func (s *Shell) setSetting(key string, value any) {
	st := &s.settings
	previous, hadPrevious := st.values[key]
	st.values[key] = value
	if key == setting.KeyGeneralTheme {
		s.themeChoice, _ = value.(string)
	}
	st.saving[key]++
	st.writes <- func() {
		err := s.deps.Settings.Set(context.Background(), key, value)
		if err == nil {
			fairy.Go(func() { s.settingSaved(key) })
			return
		}
		err = infra.ReportError(s.deps.Log, err, "Settings.Set", infra.Diagnostic{
			Operation: "settings.save", Fields: map[string]any{"key": key},
		})
		fairy.Go(func() {
			s.settingSaved(key)
			revert := slices.Contains(revertOnSaveFailure, key)
			if reflect.DeepEqual(st.values[key], value) {
				if revert && hadPrevious {
					st.values[key] = previous
				} else {
					delete(st.values, key)
				}
			}
			if revert {
				s.toasts.Show(kit.ToastError, T("page.setting.tools.wuwaFixer.autoUpdateNotification.save_failed"), "")
				return
			}
			s.toasts.Show(kit.ToastError, T("shell.setting_save_failed"), err.Error())
		})
	}
}

func (s *Shell) settingSaved(key string) {
	if s.settings.saving[key]--; s.settings.saving[key] <= 0 {
		delete(s.settings.saving, key)
	}
}

func (s *Shell) settingSaving(key string) bool { return s.settings.saving[key] > 0 }

// settingChanged applies a saved value, which the setting package may have normalized.
func (s *Shell) settingChanged(key string, value any) {
	if key == setting.KeyGeneralTheme {
		if theme, ok := value.(string); ok {
			s.themeChoice = theme
		}
	}
	if _, ok := s.settings.values[key]; ok {
		s.settings.values[key] = value
	}
	if strings.HasPrefix(key, "backup.") && s.backup.overview != nil {
		s.loadBackupOverview()
	}
}

// reloadSettings drops what the screens cached after a raw edit in the advanced section, which bypasses the
// normalized values and their events.
func (s *Shell) reloadSettings() {
	st := &s.settings
	clear(st.values)
	st.loadErr = nil
	st.compression = nil
	st.network = networkSection{}
	st.advanced.rows = nil
	go func() {
		ctx := context.Background()
		theme, themeErr := s.deps.Settings.GetTheme(ctx)
		lang, langErr := s.deps.Settings.GetLanguage(ctx)
		fairy.Go(func() {
			if themeErr == nil {
				s.themeChoice = theme
			}
			if langErr == nil {
				s.setLanguage(lang)
			}
		})
	}()
}

func (s *Shell) boolSetting(key string) bool {
	value, _ := s.settings.values[key].(bool)
	return value
}

func (s *Shell) stringSetting(key string) string {
	return asString(s.settings.values[key])
}

func (s *Shell) intSetting(key string) int {
	switch value := s.settings.values[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	}
	return 0
}

func (s *Shell) stringsSetting(key string) []string {
	switch value := s.settings.values[key].(type) {
	case []string:
		return value
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	}
	return nil
}

func (s *Shell) settingsPage(cx *fairy.Cx) *fairy.El {
	t := kit.T()
	sections := settingsSections()
	active := sections[0]
	for _, section := range sections {
		if section.id == s.settings.section {
			active = section
		}
	}

	items := fairy.Col().Gap(2).Role(fairy.RoleList)
	for _, section := range sections {
		items.Child(s.settingsNavItem(section, section.id == active.id))
	}
	nav := fairy.Col(
		fairy.Text("SECTIONS").FontSize(10).LineHeight(15).LetterSpacing(1).Medium().Color(t.MutedForeground).
			PadEdges(0, 8, 8, 8),
		items,
	)
	aside := fairy.Col(
		fairy.Row(
			fairy.Box(fairy.Icon(lucide.Settings).Size(14, 14).Color(t.Accent)).Size(28, 28).Center().Radius(4).
				Fill(t.Accent.Alpha(0.2)),
			fairy.Text(T("page.setting.title")).FontSize(14).LineHeight(20).SemiBold().LetterSpacing(-0.35),
		).Gap(10).Align(fairy.Center).Pad(16).BorderEdges(0, 0, 1, 0, t.Border),
		fairy.Col(nav).Grow(1).MinH(0).ScrollY().PadXY(8, 12),
		s.settingsFooter().BorderEdges(1, 0, 0, 0, t.Border),
	).W(256).HFull().Shrink(0).Fill(t.Background.Alpha(0.5)).BorderEdges(0, 1, 0, 0, t.Border)

	header := fairy.Row(
		fairy.Text(T("page.setting.title")).Medium(),
		fairy.Text("/").Color(t.MutedForeground),
		fairy.Text(active.label),
	).H(40).Shrink(0).PadXY(16, 0).Gap(6).Align(fairy.Center).Font(monoFont).FontSize(12).LineHeight(16).
		BorderEdges(0, 0, 1, 0, t.Border)
	width, height := cx.Size()
	content := fairy.Col(s.settingsSection(active.id, width, height).Key("settings." + string(active.id))).
		WFull().MaxW(576)
	viewport := stableScrollY(fairy.Col(content).Grow(1).MinH(0).WFull().Align(fairy.Center))
	main := fairy.Col(header, viewport).Grow(1).MinW(0).HFull().Fill(t.Background.Alpha(0.7))
	return fairy.Row(aside, main).WFull().HFull()
}

func (s *Shell) settingsNavItem(section settingsSectionInfo, active bool) *fairy.El {
	t := kit.T()
	chip, ink := t.Muted, t.MutedForeground
	if active {
		chip, ink = t.Accent.Alpha(0.2), t.Accent
	}
	row := fairy.Row(
		fairy.Box(fairy.Icon(section.icon).Size(14, 14)).Size(20, 20).Center().Radius(4).Fill(chip).Color(ink).
			Animate(motion.Fast),
		fairy.Text(section.label).FontSize(12).LineHeight(16).Grow(1).MinW(0).Ellipsis(),
	).Key("settings.nav."+string(section.id)).PadXY(8, 8).Gap(10).Align(fairy.Center).Radius(6).
		HoverFill(t.SidebarAccent).
		Animate(motion.Fast).Cursor(fairy.CursorPointer).Label(section.label).Selected(active).
		OnClick(func() { s.openSettings(section.id) })
	if active {
		row.Fill(t.SidebarAccent).Child(fairy.Icon(lucide.ChevronRight).Size(12, 12).Color(t.Accent))
	}
	return row
}

func (s *Shell) settingsSection(section settingsSection, width, height float32) *fairy.El {
	switch section {
	case sectionGeneral:
		return s.generalSettings()
	case sectionMod:
		return s.modSettings(width)
	case sectionDrive:
		return s.driveSettings()
	case sectionAccount:
		return s.accountSettings()
	case sectionTransfer:
		return s.transferSettings()
	case sectionNetwork:
		return s.networkSettings()
	case sectionTools:
		return s.toolsSettings()
	case sectionAdvanced:
		return s.advancedSettings(height)
	case sectionFeedback:
		return s.feedbackSettings()
	case sectionAgent:
		return s.agentSettings()
	}
	return s.generalSettings()
}

// settingsLoading shows nothing until the section's values arrive, or the load error with a retry.
func (s *Shell) settingsLoading() *fairy.El {
	if s.settings.loadErr == nil {
		return fairy.Box()
	}
	t := kit.T()
	return fairy.Col(
		fairy.Text(T("shell.setting_load_failed")).Color(t.Destructive),
		kit.Button(T("page.setting.acc.retry"), func() { s.settings.loadErr = nil }, kit.Outline, kit.Small),
	).Gap(10).Pad(16).Align(fairy.Start)
}

// settingsBody lays out the sections of one settings page.
func settingsBody(children ...*fairy.El) *fairy.El {
	return fairy.Col(children...).Pad(16).Gap(24)
}

// settingsGroup is a titled group of rows tied together by a rule on the left.
func settingsGroup(title string, rows ...*fairy.El) *fairy.El {
	t := kit.T()
	group := fairy.Col().Gap(4).FontSize(14)
	if title != "" {
		group.Child(fairy.Text(title).Medium().LineHeight(20).Color(t.MutedForeground).PadEdges(0, 4, 8, 4))
	}
	return group.Child(
		fairy.Col(rows...).Gap(4).PadEdges(0, 14, 0, 30).BorderEdges(0, 0, 0, 1, t.Foreground.Alpha(0.2)),
	)
}

// settingRow puts a title and description on the left and the control on the right.
func settingRow(title, description string, control *fairy.El) *fairy.El {
	t := kit.T()
	text := fairy.Col(fairy.Text(title).FontSize(14).LineHeight(20).Medium().Wrap()).Grow(1).MinW(0)
	if description != "" {
		text.Child(fairy.Text(description).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap())
	}
	row := fairy.Row(text).Gap(24).Align(fairy.Center).PadXY(0, 12)
	if control != nil {
		row.Child(control.Shrink(0))
	}
	return row
}

// settingSwitch is a row whose switch saves a boolean setting.
func (s *Shell) settingSwitch(key, title, description string) *fairy.El {
	return settingRow(title, description,
		kit.Switch(s.boolSetting(key), func(on bool) { s.setSetting(key, on) }).Label(title))
}

// settingSelect is a select that saves a string setting.
func (s *Shell) settingSelect(key string, items []kit.Item, width float32, opts ...kit.Option) *fairy.El {
	return kit.Select("settings."+key, s.stringSetting(key), items, func(value string) { s.setSetting(key, value) },
		opts...).W(width)
}

// settingNumberBounds are the limits React clamped these fields to before saving, the same ones the setting
// package applies.
var settingNumberBounds = map[string]numberBounds{
	setting.KeyTransferDownloadConcurrency:         within(16, 64),
	setting.KeyTransferDownloadBandwidthLimitMibps: within(0, 1024),
	setting.KeyTransferUploadConcurrency:           within(4, 16),
	setting.KeyModGridResponsiveBaseWidth:          within(240, 640),
	setting.KeyModGridFixedCardWidth:               within(240, 640),
	setting.KeyModGridFixedColumnCount:             within(1, 8),
}

// settingNumberFallback is what React saved for text that is not a number: an empty transfer field read as 0 and
// clamped to the minimum, and the grid fields fell back to fixed widths and a column count.
var settingNumberFallback = map[string]int{
	setting.KeyTransferDownloadConcurrency:         16,
	setting.KeyTransferDownloadBandwidthLimitMibps: 0,
	setting.KeyTransferUploadConcurrency:           4,
	setting.KeyModGridResponsiveBaseWidth:          400,
	setting.KeyModGridFixedCardWidth:               360,
	setting.KeyModGridFixedColumnCount:             4,
}

// settingNumber is a number field that saves an integer setting, clamped to its bounds, when it loses focus or
// Enter is pressed. Up and Down step the stored value.
func (s *Shell) settingNumber(key, label string, width float32) *fairy.El {
	st := &s.settings
	bounds := settingNumberBounds[key]
	text, editing := st.drafts[key]
	if !editing {
		text = itoa(s.intSetting(key))
	}
	save := func(n int) {
		if n = bounds.clamp(n); n != s.intSetting(key) {
			s.setSetting(key, n)
		}
	}
	commit := func() {
		draft, ok := st.drafts[key]
		if !ok {
			return
		}
		delete(st.drafts, key)
		n, ok := parseInt(draft)
		if !ok {
			if n, ok = settingNumberFallback[key]; !ok {
				return
			}
		}
		save(n)
	}
	field := kit.TextField(text, func(value string) { st.drafts[key] = value }, kit.Label(label)).
		Key("settings.number." + key).W(width).Grow(0).OnBlur(commit).OnSubmit(func(string) { commit() })
	return onArrowStep(field, func(direction int) {
		n := s.intSetting(key)
		if typed, ok := parseInt(st.drafts[key]); ok {
			n = typed
		}
		delete(st.drafts, key)
		save(n + direction)
	})
}

// onArrowStep makes Up and Down step a number field as the arrows of a number input did; step receives 1 or -1.
func onArrowStep(field *fairy.El, step func(direction int)) *fairy.El {
	return field.OnInputKey(func(k keyboard.KeyEvent) bool {
		switch {
		case k.Is(keyboard.KeyUp, 0):
			step(1)
		case k.Is(keyboard.KeyDown, 0):
			step(-1)
		default:
			return false
		}
		return true
	})
}

// openExternal opens url in the browser and reports a failure as a toast.
func (s *Shell) openExternal(url string) {
	go func() {
		if err := s.deps.Platform.OpenExternal(url); err != nil {
			s.toasts.Show(kit.ToastError, url, err.Error())
		}
	}()
}
