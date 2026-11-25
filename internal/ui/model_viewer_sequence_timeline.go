package ui

import (
	"cmp"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/svg"

	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

// A shot's card on the timeline is as wide as timelinePixelsPerSecond times its length, within the card limits; its
// thumbnail keeps the thumbnail's aspect at timelineThumbHeight.
const (
	timelinePixelsPerSecond = 28
	timelineCardMinWidth    = 128
	timelineCardMaxWidth    = 320
	timelineThumbHeight     = 72
	timelineTransitionWidth = 40
)

// shotLabel names the clip a shot plays: its motion's, or else its mod clip's.
func (v *ModelViewer) shotLabel(shot WallpaperShot) string {
	if t := shot.Motion; t != nil {
		if clip := v.findClip(trackMotion, t.Clip); clip != nil && clip.Label != "" {
			return clip.Label
		}
		return t.Clip
	}
	if t := shot.Animation; t != nil && len(v.trackClips(trackMod)) > 1 {
		if clip := v.findClip(trackMod, t.Clip); clip != nil && clip.Label != "" {
			return clip.Label
		}
		return t.Clip
	}
	return T(sequencePrefix + "still")
}

func formatSeconds(d time.Duration) string {
	return T(sequencePrefix+"seconds_value", "value", formatViewerNumber(math.Round(d.Seconds()*10)/10))
}

// shotLengthText is a shot's length as the user set it, with the time loops take.
func (v *ModelViewer) shotLengthText(shot WallpaperShot) string {
	length := formatSeconds(v.shotLength(shot))
	if shot.Loops > 0 && v.shotCountable(shot) {
		return T(sequencePrefix+"loops_value", "value", itoa(shot.Loops)) + " · " + length
	}
	return length
}

// makerTimeline is the Wallpaper Maker's strip under the viewer: the sequence's actions above its shots, laid out
// in play order as cards as wide as they last, with the transition into each between them.
func (v *ModelViewer) makerTimeline(t kit.Theme) *fairy.El {
	m := v.maker
	shots := m.draft.Shots
	playing := -1
	var progress float64
	if s := v.sequence; s != nil && v.wallpaper == nil {
		playing = s.index
		if s.index < len(shots) {
			if length := v.shotLength(shots[s.index]); length > 0 {
				progress = min(s.elapsed(time.Now()).Seconds()/length.Seconds(), 1)
			}
		}
	}

	track := fairy.Row().Align(fairy.Center).Pad(2)
	for i := range shots {
		if i > 0 || len(shots) > 1 {
			track.Child(v.transitionChip(t, i))
		}
		fraction := -1.0
		if i == playing {
			fraction = progress
		}
		track.Child(v.shotCard(t, i, fraction))
	}
	track.Child(fairy.Box().W(8).Shrink(0))
	track.Child(v.addShotCard(t))
	if len(shots) == 0 {
		track.Child(fairy.Text(T(sequencePrefix+"empty")).FontSize(12).LineHeight(16).Color(t.MutedForeground).
			MarginEdges(0, 0, 0, 12).MaxW(320).Wrap())
	}

	return fairy.Col(
		v.timelineToolbar(t),
		fairy.Box(track).ScrollX().WFull(),
	).Gap(10).Pad(12).Radius(6).Fill(t.Muted.Alpha(0.3)).Shrink(0).Key("model-viewer.sequence.timeline")
}

func (v *ModelViewer) timelineToolbar(t kit.Theme) *fairy.El {
	m := v.maker
	busy := m.saving || m.loading
	empty := len(m.draft.Shots) == 0

	items := make([]kit.Item, 0, len(m.sequences)+1)
	for _, s := range m.sequences {
		label := s.Name
		if s.ID == m.draft.ID {
			label = m.draft.Name
		}
		items = append(items, kit.Item{Value: s.ID, Label: label})
	}
	if !m.saved(m.draft.ID) {
		items = append(items, kit.Item{Value: m.draft.ID, Label: m.draft.Name})
	}
	pick := kit.Select("model-viewer.sequence", m.draft.ID, items, v.switchSequence, kit.Small,
		kit.Label(T(sequencePrefix+"label")), kit.Disabled(busy)).W(200)
	icon := func(glyph *svg.Vector, label string, onClick func(), disabled bool) *fairy.El {
		return kit.IconButton(glyph, onClick, kit.Ghost, kit.Small, kit.Disabled(disabled), kit.Tooltip(label)).
			Label(label)
	}
	sequence := fairy.Row(
		pick,
		icon(lucide.Plus, T(sequencePrefix+"new"), func() {
			v.discardThen(func() { m.naming = &sequenceNaming{name: T(sequencePrefix + "default_name")} })
		}, busy),
		icon(lucide.Pencil, T(sequencePrefix+"rename"), func() {
			m.naming = &sequenceNaming{name: m.draft.Name, rename: true}
		}, busy),
		icon(lucide.Trash, T(sequencePrefix+"delete"), func() {
			m.confirm = &sequenceConfirm{
				title:       T(sequencePrefix + "dialog.delete.title"),
				description: T(sequencePrefix+"dialog.delete.description", "name", m.draft.Name),
				action:      T("g.delete"),
				destructive: true,
				run:         v.deleteDraft,
			}
		}, busy || !m.saved(m.draft.ID)),
	).Gap(6).Align(fairy.Center)
	if m.dirty {
		sequence.Child(fairy.Text(T(sequencePrefix + "unsaved")).FontSize(12).LineHeight(16).
			Color(t.MutedForeground).Shrink(0))
	}

	previewLabel, previewIcon := T(sequencePrefix+"preview"), lucide.Play
	if v.sequence != nil {
		previewLabel, previewIcon = T(sequencePrefix+"stop_preview"), lucide.Square
	}
	var total time.Duration
	for _, shot := range m.draft.Shots {
		total += v.shotLength(shot)
	}
	playback := fairy.Row(
		kit.Button(previewLabel, v.togglePreview, kit.Small, kit.Icon(previewIcon),
			kit.Disabled(empty && v.sequence == nil)),
		fairy.Text(T(sequencePrefix+"total", "value", formatSeconds(total))).FontSize(12).LineHeight(16).
			Color(t.MutedForeground).Shrink(0),
	).Gap(8).Align(fairy.Center)

	actions := fairy.Row(
		kit.Button(T("g.save"), v.saveDraft, kit.Outline, kit.Small, kit.Icon(lucide.Save), kit.Loading(m.saving),
			kit.Disabled(!m.dirty || m.loading)),
	).Gap(6).Align(fairy.Center)
	if v.shell.deps.Wallpaper != nil {
		actions.Child(v.monitorSelect().W(200))
		actions.Child(kit.Button(T(sequencePrefix+"set_wallpaper"), v.setSequenceWallpaper, kit.Small,
			kit.Icon(lucide.Wallpaper), kit.Disabled(empty)))
	}
	actions.Child(icon(lucide.X, T("g.close"), v.closeWallpaperMaker, m.saving))

	return fairy.Row(sequence, fairy.Box().Grow(1), playback, fairy.Box().Grow(1), actions).
		Gap(12).Align(fairy.Center).Wrap()
}

// shotCard is shot i on the timeline: its thumbnail, number, clip and length. fraction is the share of it a running
// preview has played, or negative while the preview plays another shot.
func (v *ModelViewer) shotCard(t kit.Theme, i int, fraction float64) *fairy.El {
	m := v.maker
	shot := &m.draft.Shots[i]
	width := float32(min(max(v.shotLength(*shot).Seconds()*timelinePixelsPerSecond, timelineCardMinWidth),
		timelineCardMaxWidth))

	thumb := fairy.Box().W(width).H(timelineThumbHeight).Radius(4).Clip().Fill(t.Background.Mix(t.Muted, 0.5))
	if img := shot.image(); img != nil {
		thumb.Child(fairy.ImageEl(img).WFull().HFull().Fit(fairy.FitCover))
	} else {
		thumb.Child(fairy.Icon(lucide.ImageOff).Size(20, 20).Color(t.MutedForeground)).Center()
	}
	thumb.Child(fairy.Text(itoa(i+1)).FontSize(11).LineHeight(14).SemiBold().Color(t.Foreground).
		PadXY(6, 1).Radius(4).Fill(t.Background.Alpha(0.8)).Top(4).Left(4))
	if fraction >= 0 {
		thumb.Child(fairy.Box().H(3).W(width * float32(fraction)).Fill(t.Primary).Bottom(0).Left(0))
	}

	selected := i == m.selected
	card := fairy.Col(
		thumb,
		fairy.Col(
			fairy.Text(v.shotLabel(*shot)).FontSize(12).LineHeight(16).Medium().Ellipsis(),
			fairy.Text(v.shotLengthText(*shot)).FontSize(11).LineHeight(14).Color(t.MutedForeground).Ellipsis(),
		).W(width).MinW(0),
	).Gap(6).Pad(6).Radius(8).Shrink(0).Key("model-viewer.sequence.shot:" + itoa(shot.key)).
		Role(fairy.RoleButton).Label(T(sequencePrefix+"shot_title", "index", itoa(i+1))).Selected(selected).
		Focusable().Cursor(fairy.CursorPointer).OnClick(func() { v.selectShot(i) })
	switch {
	case selected:
		card.Fill(t.Primary.Alpha(0.12)).Border(2, t.Primary)
	case fraction >= 0:
		card.Fill(t.Primary.Alpha(0.08)).Border(2, t.Primary.Alpha(0.4))
	default:
		card.Fill(t.Background.Alpha(0.6)).Border(2, t.Background.Alpha(0)).HoverBorder(t.Border)
	}
	return card
}

// transitionChip shows the transition into shot i; the first shot's comes from the last as the sequence loops.
func (v *ModelViewer) transitionChip(t kit.Theme, i int) *fairy.El {
	shot := v.maker.draft.Shots[i]
	glyph := lucide.ChevronsRight
	if i == 0 {
		glyph = lucide.Repeat
	}
	label := T(sequencePrefix+"transition_value", "value", formatViewerNumber(shot.transition().Seconds()))
	return fairy.Col(
		fairy.Icon(glyph).Size(14, 14).Color(t.MutedForeground),
		fairy.Text(formatViewerNumber(shot.transition().Seconds())+"s").FontSize(10).LineHeight(12).
			Color(t.MutedForeground),
	).W(timelineTransitionWidth).Shrink(0).Align(fairy.Center).Gap(2).Tooltip(label).Label(label).
		Role(fairy.RoleButton).Cursor(fairy.CursorPointer).OnClick(func() { v.selectShot(i) })
}

func (v *ModelViewer) addShotCard(t kit.Theme) *fairy.El {
	label := T(sequencePrefix + "add_shot")
	card := fairy.Col(
		fairy.Icon(lucide.Plus).Size(20, 20).Color(t.MutedForeground),
		fairy.Text(label).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap(),
	).W(timelineCardMinWidth).H(timelineThumbHeight+48).Shrink(0).Center().Gap(6).Pad(8).Radius(8).
		Border(1, t.Border).BorderDashed().Role(fairy.RoleButton).Label(label).Key("model-viewer.sequence.add")
	if v.scene == nil {
		return card.Opacity(0.5)
	}
	return card.Focusable().Cursor(fairy.CursorPointer).HoverFill(t.Muted.Alpha(0.5)).OnClick(v.addShot)
}

// makerInspector is the side column of the Wallpaper Maker: the selected shot's clips, length and transition, and
// the toggles, which edit the shot the viewer shows.
func (v *ModelViewer) makerInspector(
	t kit.Theme,
	variables []modelviewer.ModelViewerVariable,
	width float32,
) *fairy.El {
	column := fairy.Col().Gap(12)
	if v.maker.shot() != nil {
		column.Child(v.shotInspector(t))
	} else {
		column.Child(makerCard(t,
			makerHeading(t, T(sequencePrefix+"no_selection")),
			fairy.Text(T(sequencePrefix+"no_selection_hint")).FontSize(12).LineHeight(16).
				Color(t.MutedForeground).Wrap(),
		))
	}
	if v.showsToggleControls(variables) {
		column.Child(v.togglesSection(t, variables, width))
	}
	return fairy.Col(column).ScrollY().MinW(0).MinH(0)
}

func makerCard(t kit.Theme, children ...*fairy.El) *fairy.El {
	return fairy.Col(children...).Gap(8).Pad(12).Shrink(0).Radius(6).Fill(t.Muted.Alpha(0.3))
}

func makerHeading(t kit.Theme, text string) *fairy.El {
	return fairy.Text(text).FontSize(14).LineHeight(20).Medium().Color(t.Foreground).Ellipsis().Grow(1).MinW(0)
}

func makerLabel(t kit.Theme, text string) *fairy.El {
	return fairy.Text(text).FontSize(12).LineHeight(16).Color(t.MutedForeground)
}

func (v *ModelViewer) shotInspector(t kit.Theme) *fairy.El {
	m := v.maker
	i, shots := m.selected, m.draft.Shots
	shot := &shots[i]
	icon := func(glyph *svg.Vector, label string, onClick func(), disabled bool) *fairy.El {
		return kit.IconButton(glyph, onClick, kit.Ghost, kit.Small, kit.Disabled(disabled), kit.Tooltip(label)).
			Label(label)
	}
	card := makerCard(t,
		fairy.Row(
			makerHeading(t, T(sequencePrefix+"shot_of", "index", itoa(i+1), "count", itoa(len(shots)))),
			icon(lucide.ChevronLeft, T(sequencePrefix+"move_earlier"), func() { v.moveShot(i, -1) }, i == 0),
			icon(lucide.ChevronRight, T(sequencePrefix+"move_later"), func() { v.moveShot(i, 1) },
				i == len(shots)-1),
			icon(lucide.Copy, T(sequencePrefix+"duplicate"), func() { v.duplicateShot(i) }, false),
			icon(lucide.Trash, T("g.delete"), func() { v.deleteShot(i) }, false),
		).Gap(2).Align(fairy.Center),
		fairy.Text(T(sequencePrefix+"live_hint")).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap(),
	)

	if picker := v.shotClipPicker(trackMotion, shot.Motion, T(sequencePrefix+"motion")); picker != nil {
		card.Child(makerLabel(t, T(sequencePrefix+"motion")))
		card.Child(picker)
	}
	if len(v.trackClips(trackMod)) > 1 {
		if picker := v.shotClipPicker(trackMod, shot.Animation, T(sequencePrefix+"mod_clip")); picker != nil {
			card.Child(makerLabel(t, T(sequencePrefix+"mod_clip")))
			card.Child(picker)
		}
	}

	countable := v.shotCountable(*shot)
	mode := shotLengthSeconds
	if shot.Loops > 0 && countable {
		mode = shotLengthLoops
	}
	card.Child(makerLabel(t, T(sequencePrefix+"length")))
	card.Child(kit.Segmented(mode, []kit.Item{
		{Value: shotLengthLoops, Label: T(sequencePrefix + "loops"), Disabled: !countable},
		{Value: shotLengthSeconds, Label: T(sequencePrefix + "seconds")},
	}, func(next string) {
		m.numberDraft = nil
		if next == shotLengthLoops {
			shot.Loops = 1
		} else {
			shot.Loops = 0
			if shot.Seconds <= 0 {
				shot.Seconds = defaultShotSeconds
			}
		}
		m.dirty = true
	}))
	card.Child(v.shotLengthField(shot, mode))

	transition := shot.transition().Seconds()
	from := T(sequencePrefix+"transition_from", "index", itoa((i+len(shots)-1)%len(shots)+1))
	card.Child(fairy.Row(
		makerLabel(t, T(sequencePrefix+"transition")).Grow(1).MinW(0),
		makerLabel(t, T(sequencePrefix+"seconds_value", "value", formatViewerNumber(transition))).Shrink(0),
	).Gap(8))
	card.Child(kit.Slider(transition, 0, maxShotTransition, shotTransitionStep, func(next float64) {
		shot.Transition = math.Round(next*10) / 10
		m.dirty = true
	}, kit.Label(T(sequencePrefix+"transition"))).Key("model-viewer.sequence.transition").WFull())
	card.Child(fairy.Text(from).FontSize(11).LineHeight(14).Color(t.MutedForeground).Wrap())
	return card.Key("model-viewer.sequence.inspector")
}

// shotClipPicker lists the clips of track for the selected shot; picking one plays it, which the shot records. It
// is nil while track has no clips.
func (v *ModelViewer) shotClipPicker(track viewerTrack, current *wallpaperTrack, label string) *fairy.El {
	clips := v.trackClips(track)
	if len(clips) == 0 {
		return nil
	}
	items := make([]kit.Item, 0, len(clips))
	for _, clip := range clips {
		items = append(items, kit.Item{Value: clip.ID, Label: cmp.Or(clip.Label, clip.ID)})
	}
	selected := ""
	if current != nil {
		selected = current.Clip
	}
	return kit.Select("model-viewer.sequence.clip:"+track.key(), selected, items, func(id string) {
		v.startTrack(track, id)
	}, kit.Small, kit.Label(label)).WFull()
}

// shotLengthField sets the selected shot's loops or seconds, typed or stepped.
func (v *ModelViewer) shotLengthField(shot *WallpaperShot, mode string) *fairy.El {
	m := v.maker
	value, step := shot.duration().Seconds(), shotSecondsStep
	if mode == shotLengthLoops {
		value, step = float64(shot.Loops), 1
	}
	set := func(next float64) {
		m.numberDraft = nil
		if math.IsNaN(next) || math.IsInf(next, 0) {
			return
		}
		if mode == shotLengthLoops {
			shot.Loops = min(max(int(math.Round(next)), 1), maxShotLoops)
		} else {
			shot.Seconds = min(max(math.Round(next/shotSecondsStep)*shotSecondsStep, minShotSeconds), maxShotSeconds)
		}
		m.dirty = true
	}
	text := formatViewerNumber(value)
	if m.numberDraft != nil {
		text = *m.numberDraft
	}
	commit := func() {
		if m.numberDraft == nil {
			return
		}
		next, err := strconv.ParseFloat(strings.TrimSpace(*m.numberDraft), 64)
		if err != nil {
			m.numberDraft = nil
			return
		}
		set(next)
	}
	label := T(sequencePrefix + "seconds")
	if mode == shotLengthLoops {
		label = T(sequencePrefix + "loops")
	}
	return fairy.Row(
		kit.Button("-", func() { set(value - step) }, kit.Outline, kit.Small).Shrink(0),
		kit.TextField(text, func(next string) { m.numberDraft = &next }, kit.Label(label)).
			Key("model-viewer.sequence.length:"+mode).OnBlur(commit).OnSubmit(func(string) { commit() }).
			Grow(1).MinW(0),
		kit.Button("+", func() { set(value + step) }, kit.Outline, kit.Small).Shrink(0),
	).Gap(4).Align(fairy.Center)
}

func (v *ModelViewer) monitorSelect() *fairy.El {
	m := v.maker
	items := make([]kit.Item, 0, len(m.monitors))
	selected := ""
	for i, monitor := range m.monitors {
		key := "page.tools.model_viewer.menu.wallpaper_monitor"
		if monitor.Primary {
			key += "_primary"
		}
		items = append(items, kit.Item{Value: monitor.Device, Label: T(key, "index", itoa(i+1),
			"width", itoa(monitor.Bounds.Dx()), "height", itoa(monitor.Bounds.Dy()))})
		if monitor.Device == m.monitor || (selected == "" && monitor.Primary && m.monitor == "") {
			selected = monitor.Device
		}
	}
	if !slices.ContainsFunc(items, func(item kit.Item) bool { return item.Value == selected }) && len(items) > 0 {
		selected = items[0].Value
	}
	return kit.Select("model-viewer.sequence.monitor", selected, items, func(device string) { m.monitor = device },
		kit.Small, kit.Label(T(sequencePrefix+"monitor")))
}
