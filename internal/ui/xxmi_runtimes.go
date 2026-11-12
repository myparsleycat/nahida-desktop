package ui

import (
	"context"
	"maps"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/samber/lo"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/modruntime"
	"nahida.live/desktop/internal/xxmi"
)

const modRuntimeGroup = "nhd-runtime"

var (
	modRuntimeErrorPattern = regexp.MustCompile(`(MOD_RUNTIME_[A-Z_]+)(?::([^\n]*))?`)
	enabledRuntimeStates   = []string{"current", "outdated", "modified"}
	installedRuntimeStates = []string{"current", "outdated", "modified", "disabled"}
)

// runtimesPage holds the importer page's mod runtime edits.
type runtimesPage struct {
	// drafts are the edited settings of each runtime until they are saved.
	drafts   map[string]map[string]string
	removing *modruntime.Status
	key      *runtimeKeyDialog
	numbers  map[string]string
}

type runtimeKeyDialog struct {
	runtime, id, title string
	keyRecording
}

func modRuntimeMessage(err error) string {
	match := modRuntimeErrorPattern.FindStringSubmatch(err.Error())
	if match == nil {
		return ""
	}
	key := "page.setting.xxmi.modRuntimes.errors." + match[1]
	if !catalog.Has(key) {
		return ""
	}
	return T(key, "detail", match[2])
}

// modRuntimeOps are the actions on one runtime. Each has its own busy id so the spinner shows on the button that
// started it, and any of them running blocks the others.
var modRuntimeOps = []string{"install", "disable", "remove", "save"}

func modRuntimeBusyID(importer, name, op string) string {
	return "mod-runtime:" + importer + ":" + name + ":" + op
}

func (s *Shell) modRuntimeBusy(importer, name string) bool {
	return slices.ContainsFunc(modRuntimeOps, func(op string) bool {
		return s.xxmi.busy[modRuntimeBusyID(importer, name, op)]
	})
}

func (s *Shell) runModRuntime(importer string, runtime modruntime.Status, op, success string,
	action func(ctx context.Context, importer, name string) (modruntime.Status, error), done func(),
) {
	if s.modRuntimeBusy(importer, runtime.Name) {
		return
	}
	listing := keyed(s.xxmi.modRuntimes, importer)
	s.xxmiAction(modRuntimeBusyID(importer, runtime.Name, op), func(ctx context.Context) error {
		_, err := action(ctx, importer, runtime.Name)
		// Installing or removing a runtime adds or drops a mod in the library.
		fairy.Go(func() {
			listing.invalidate()
			if s.mods.started {
				s.loadGames(false)
			}
		})
		return err
	}, func() {
		s.toasts.Show(kit.ToastSuccess, T(success, "name", runtime.Name), "")
		if done != nil {
			done()
		}
	}, modRuntimeMessage)
}

func (s *Shell) modRuntimesTab(ic *importerContext) []*fairy.El {
	t := kit.T()
	x := &s.xxmi
	key := ic.key
	const prefix = "page.setting.xxmi.modRuntimes."
	header := fairy.Col(
		fairy.Text(T(prefix+"title")).FontSize(14).LineHeight(20).Medium().Color(t.MutedForeground),
		fairy.Text(T(prefix+"description", "folder", modRuntimeGroup)).
			FontSize(12).
			LineHeight(16).
			Color(t.MutedForeground).
			Wrap(),
	).Gap(2).PadEdges(0, 4, 8, 4)
	section := func(rows ...*fairy.El) []*fairy.El {
		return []*fairy.El{fairy.Col(header, settingsGroup("", rows...)).Gap(4)}
	}

	listing := fetch(s, keyed(x.modRuntimes, key), func(ctx context.Context) (modruntime.Listing, error) {
		return s.deps.ModRuntime.List(ctx, key)
	})
	switch {
	case !listing.loaded && listing.err != nil:
		return section(fairy.Row(
			fairy.Text(T(prefix+"loadFailed")).Color(t.Destructive).Grow(1),
			kit.Button(T("page.setting.xxmi.dllVersionRetry"), listing.invalidate, kit.Outline, kit.Small),
		).Gap(16).Align(fairy.Center).PadXY(0, 12))
	case !listing.loaded:
		return section(fairy.Row(kit.Spinner(14), fairy.Text(T(prefix+"loading")).Color(t.MutedForeground)).Gap(8).
			Align(fairy.Center).PadXY(0, 12))
	}

	l := listing.value
	rows := []*fairy.El{toggleRow(T(prefix+"autoUpdate"), T(prefix+"autoUpdateDescription"),
		ic.c.AutoUpdateModRuntimes,
		func(on bool) { ic.set(func(c *xxmi.ImporterConfig) { c.AutoUpdateModRuntimes = on }) }, false)}
	if l.ModsRoot == "" {
		rows = append(rows, xxmiAlert(lucide.TriangleAlert, false, "", T(prefix+"noModsFolder")))
	}
	for _, runtime := range l.Runtimes {
		rows = append(rows, s.modRuntimeRow(key, l.ModsRoot != "", runtime))
	}
	return section(rows...)
}

func (s *Shell) modRuntimeRow(importer string, hasMods bool, runtime modruntime.Status) *fairy.El {
	t := kit.T()
	p := &s.xxmi.importer.runtimes
	busy := s.modRuntimeBusy(importer, runtime.Name)
	acting := func(op string) bool { return s.xxmi.busy[modRuntimeBusyID(importer, runtime.Name, op)] }
	const prefix = "page.setting.xxmi.modRuntimes."
	state := runtime.State

	title := fairy.Row(fairy.Text(runtime.Name).FontSize(14).LineHeight(20).Medium()).
		Wrap().
		Gap(8).
		RowGap(8).
		Align(fairy.Center)
	if state != "" {
		variant := kit.Destructive
		switch state {
		case "current":
			variant = kit.Secondary
		case "disabled":
			variant = kit.Outline
		}
		title.Child(kit.Badge(T(prefix+"state."+state), variant))
	}
	versions := T(prefix+"bundled", "bundled", runtime.BundledVersion)
	if runtime.Version != "" && state != "foreign" {
		versions = T(prefix+"versions", "installed", runtime.Version, "bundled", runtime.BundledVersion)
	}
	title.Child(fairy.Text(versions).FontSize(12).LineHeight(16).Color(t.MutedForeground))

	row := fairy.Col(title).Key("xxmi.mod-runtime:"+runtime.Name).Gap(8).PadXY(0, 12)
	if usedBy := prefix + "usedBy." + runtime.Name; catalog.Has(usedBy) {
		row.Child(fairy.Text(T(usedBy)).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap())
	}
	if runtime.Path != "" && state != "missing" {
		row.Child(fairy.Text(runtime.Path).Font(monoFont).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap())
	}
	if state == "foreign" {
		row.Child(xxmiAlert(lucide.TriangleAlert, true, "",
			T(prefix+"foreignDescription", "name", runtime.Name, "path", runtime.Path)))
	}

	if hasMods && state != "" {
		actions := fairy.Row().Wrap().Gap(8).RowGap(8)
		if state != "current" && state != "foreign" {
			actions.Child(kit.Button(T(prefix+"action."+state), func() {
				s.runModRuntime(importer, runtime, "install", prefix+"done."+state, s.deps.ModRuntime.Install, nil)
			}, kit.Small, kit.Disabled(busy), kit.Loading(acting("install"))))
		}
		if slices.Contains(enabledRuntimeStates, state) {
			actions.Child(kit.Button(T(prefix+"disable"), func() {
				s.runModRuntime(importer, runtime, "disable", prefix+"disabled", s.deps.ModRuntime.Disable, nil)
			}, kit.Outline, kit.Small, kit.Disabled(busy), kit.Loading(acting("disable"))))
		}
		if state != "missing" {
			path := runtime.Path
			actions.Child(kit.Button(T(prefix+"openFolder"), func() { s.openPath(path) }, kit.Outline, kit.Small,
				kit.Icon(lucide.FolderOpen)))
		}
		if state != "missing" && state != "foreign" {
			actions.Child(kit.Button(T(prefix+"remove"), func() { p.removing = &runtime }, kit.Destructive, kit.Small,
				kit.Disabled(busy), kit.Loading(acting("remove"))))
		}
		row.Child(actions)
	}
	if hasMods && slices.Contains(installedRuntimeStates, state) && len(runtime.Settings) > 0 {
		row.Child(s.modRuntimeSettings(importer, runtime, busy, acting("save")))
	}
	return row
}

func runtimeSettingValues(settings []modruntime.SettingValue, value func(modruntime.SettingValue) string,
) map[string]string {
	values := make(map[string]string, len(settings))
	for _, setting := range settings {
		values[setting.ID] = value(setting)
	}
	return values
}

func (s *Shell) modRuntimeSettings(importer string, runtime modruntime.Status, busy, saving bool) *fairy.El {
	t := kit.T()
	p := &s.xxmi.importer.runtimes
	const prefix = "page.setting.xxmi.modRuntimes.settings."
	saved := runtimeSettingValues(runtime.Settings, func(v modruntime.SettingValue) string { return v.Value })
	defaults := runtimeSettingValues(runtime.Settings, func(v modruntime.SettingValue) string { return v.Default })
	draft := p.drafts[runtime.Name]
	values := saved
	if draft != nil {
		values = draft
	}
	dirty := draft != nil && !maps.Equal(draft, saved)
	label := func(id string) string {
		key := prefix + runtime.Name + "." + id
		if catalog.Has(key) {
			return T(key)
		}
		return id
	}
	hint := func(id string) string {
		key := prefix + runtime.Name + ".hints." + id
		if catalog.Has(key) {
			return T(key)
		}
		return ""
	}
	update := func(id, value string) {
		next := maps.Clone(values)
		next[id] = value
		if p.drafts == nil {
			p.drafts = map[string]map[string]string{}
		}
		p.drafts[runtime.Name] = next
	}

	note := prefix + "hint"
	if runtime.State == "outdated" || runtime.State == "modified" {
		note = prefix + "repairHint"
	}
	box := fairy.Col(
		fairy.Text(T(prefix+"title")).FontSize(14).LineHeight(20).Medium(),
		fairy.Text(T(note)).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap(),
	).Gap(4).Pad(12).Radius(6).Fill(t.Muted.Alpha(0.5))
	for _, setting := range runtime.Settings {
		value, ok := values[setting.ID]
		if !ok {
			value = setting.Default
		}
		id := setting.ID
		switch setting.Kind {
		case modruntime.SettingBool:
			box.Child(toggleRow(label(id), hint(id), value == "1", func(on bool) {
				next := "0"
				if on {
					next = "1"
				}
				update(id, next)
			}, false).Pad(0))
		case modruntime.SettingInt, modruntime.SettingNumber:
			box.Child(s.runtimeNumberRow(runtime.Name, setting, label(id), hint(id), value,
				func(next string) { update(id, next) }).Pad(0))
		case modruntime.SettingColor:
			box.Child(runtimeColorRow(runtime.Name+"."+id, label(id), hint(id), value,
				func(next string) { update(id, next) }))
		default:
			var others []string
			for _, other := range runtime.Settings {
				if other.Kind == modruntime.SettingKey && other.ID != id {
					binding, ok := values[other.ID]
					if !ok {
						binding = other.Default
					}
					others = append(others, binding)
				}
			}
			keys := fairy.Row().Gap(3).Align(fairy.Center)
			for _, keyLabel := range keyLabelsOf(value) {
				keys.Child(kit.Kbd(keyLabel))
			}
			title, current := label(id), value
			edit := fairy.Row(keys).PadXY(8, 4).Radius(t.RadiusMd()).Border(1, t.Border).HoverFill(hoverFill(t)).
				Cursor(fairy.CursorPointer).Focusable().Label(title).OnClick(func() {
				p.key = &runtimeKeyDialog{runtime: runtime.Name, id: id, title: title,
					keyRecording: keyRecording{value: current, otherKeys: others}}
			})
			box.Child(settingRow(title, "", edit).PadXY(0, 8))
		}
	}
	box.Child(fairy.Row(
		fairy.Box().Grow(1),
		kit.Button(T(prefix+"reset"), func() {
			if p.drafts == nil {
				p.drafts = map[string]map[string]string{}
			}
			p.drafts[runtime.Name] = maps.Clone(defaults)
		}, kit.Ghost, kit.Small, kit.Disabled(maps.Equal(values, defaults))),
		kit.Button(T(prefix+"save"), func() {
			submitted := maps.Clone(values)
			s.runModRuntime(importer, runtime, "save", prefix+"saved",
				func(ctx context.Context, importer, name string) (modruntime.Status, error) {
					return s.deps.ModRuntime.SetSettings(ctx, importer, name, submitted)
				}, func() {
					// Edits made while the save was pending stay in the draft.
					if maps.Equal(p.drafts[runtime.Name], submitted) {
						delete(p.drafts, runtime.Name)
					}
				})
		}, kit.Small, kit.Disabled(!dirty || busy), kit.Loading(saving)),
	).Gap(8).PadEdges(8, 0, 0, 0))
	return box
}

// runtimeNumberRow edits an int or a decimal within its bounds. The backend accepts plain decimals only, so a
// decimal is rounded to the digits of its step.
func (s *Shell) runtimeNumberRow(runtime string, setting modruntime.SettingValue, title, hint, value string,
	onChange func(string),
) *fairy.El {
	p := &s.xxmi.importer.runtimes
	id := runtime + "." + setting.ID
	text, editing := p.numbers[id]
	if !editing {
		text = value
	}
	description := strings.TrimSpace(hint + " " + T("page.setting.xxmi.modRuntimes.settings.range",
		"min", formatSettingNumber(setting.Min), "max", formatSettingNumber(setting.Max)))
	parse := func(text string) (float64, bool) {
		n, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		return n, err == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
	}
	commit := func(n float64) {
		n = min(setting.Max, max(setting.Min, n))
		if setting.Kind == modruntime.SettingInt {
			onChange(strconv.Itoa(int(math.Round(n))))
			return
		}
		digits := 0
		if _, fraction, ok := strings.Cut(strconv.FormatFloat(setting.Step, 'f', -1, 64), "."); ok {
			digits = len(fraction)
		}
		rounded, _ := strconv.ParseFloat(strconv.FormatFloat(n, 'f', digits, 64), 64)
		onChange(formatSettingNumber(rounded))
	}
	field := kit.TextField(text, func(next string) {
		if p.numbers == nil {
			p.numbers = map[string]string{}
		}
		p.numbers[id] = next
		if n, ok := parse(next); ok {
			commit(n)
		}
	}, kit.Label(title)).Key("xxmi.runtime-number:" + id).W(112).Grow(0).OnBlur(func() { delete(p.numbers, id) })
	field = onArrowStep(field, func(direction int) {
		delete(p.numbers, id)
		step := setting.Step
		if setting.Kind == modruntime.SettingInt || step <= 0 {
			step = 1
		}
		n, ok := parse(value)
		if !ok {
			n = setting.Min
		}
		// A number input steps onto the grid of min plus whole steps, so an off-grid value moves to the next one.
		const epsilon = 1e-9
		units := (n - setting.Min) / step
		if direction > 0 {
			units = math.Floor(units+epsilon) + 1
		} else {
			units = math.Ceil(units-epsilon) - 1
		}
		commit(setting.Min + units*step)
	})
	return settingRow(title, description, field)
}

func formatSettingNumber(n float64) string { return strconv.FormatFloat(n, 'f', -1, 64) }

// runtimeColorRow edits a color setting, "r g b" with each channel from 0 to 1 in 8-bit steps rounded to three
// digits, as React's color input stored it.
func runtimeColorRow(id, title, hint, value string, onChange func(string)) *fairy.El {
	picker := kit.ColorPicker("xxmi.runtime-color:"+id, runtimeColor(value), func(c fairy.Color, _ bool) {
		channels := lo.Map([]float32{c.R, c.G, c.B}, func(v float32, _ int) string {
			rounded, _ := strconv.ParseFloat(strconv.FormatFloat(math.Round(float64(v)*255)/255, 'f', 3, 64), 64)
			return formatSettingNumber(rounded)
		})
		onChange(strings.Join(channels, " "))
	}, kit.Label(title), kit.ColorSwatchField)
	return settingRow(title, hint, picker.W(64)).PadXY(0, 8)
}

func runtimeColor(value string) fairy.Color {
	c := fairy.Color{A: 1}
	channels := strings.Split(value, " ")
	for i, channel := range []*float32{&c.R, &c.G, &c.B} {
		if i < len(channels) {
			n, _ := strconv.ParseFloat(channels[i], 64)
			*channel = float32(min(1, max(0, n)))
		}
	}
	return c
}

func (s *Shell) modRuntimeDialogs(cx *fairy.Cx, importer string) {
	p := &s.xxmi.importer.runtimes
	const prefix = "page.setting.xxmi.modRuntimes."
	if runtime := p.removing; runtime != nil {
		dismiss := func() { p.removing = nil }
		kit.ShowDialog(cx, kit.DialogProps{
			Title:       T(prefix+"removeTitle", "name", runtime.Name),
			Description: T(prefix + "removeDescription"),
			Actions: []*fairy.El{
				kit.Button(T("g.cancel"), dismiss, kit.Outline),
				kit.Button(T(prefix+"remove"), func() {
					p.removing = nil
					s.runModRuntime(importer, *runtime, "remove", prefix+"removed", s.deps.ModRuntime.Remove, nil)
				}, kit.Destructive),
			},
			OnClose: dismiss,
			Alert:   true,
			Key:     "xxmi.mod-runtime-remove",
		})
	}

	d := p.key
	if d == nil {
		return
	}
	dismiss := func() { p.key = nil }
	save := func() {
		if p.drafts == nil {
			p.drafts = map[string]map[string]string{}
		}
		listing := keyed(s.xxmi.modRuntimes, importer).value
		for _, runtime := range listing.Runtimes {
			if runtime.Name != d.runtime {
				continue
			}
			values := p.drafts[d.runtime]
			if values == nil {
				values = runtimeSettingValues(runtime.Settings,
					func(v modruntime.SettingValue) string { return v.Value })
			}
			values = maps.Clone(values)
			values[d.id] = d.value
			p.drafts[d.runtime] = values
		}
		p.key = nil
	}

	kit.ShowDialog(cx, kit.DialogProps{
		Title: d.title, Description: T("page.setting.xxmi.modRuntimes.settings.keyDescription"),
		Body: keyRecorder(&d.keyRecording, d.title, "xxmi.runtime-key-manual", save),
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), dismiss, kit.Outline),
			kit.Button(T("g.save"), save),
		},
		OnClose: dismiss,
		Key:     "xxmi.mod-runtime-key",
	})
}
