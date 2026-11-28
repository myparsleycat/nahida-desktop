package ui

import (
	"slices"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

// viewerClipGroupOrder is the order of the motion groups the character data names; other groups follow in the
// order their first clip comes, and clips without a group go last.
var viewerClipGroupOrder = []string{"idle", "move", "combat", "story", "extras"}

var viewerClipGroupLabels = map[string]string{
	"idle":   "page.tools.model_viewer.animation_group_idle",
	"move":   "page.tools.model_viewer.animation_group_move",
	"combat": "page.tools.model_viewer.animation_group_combat",
	"story":  "page.tools.model_viewer.animation_group_story",
	"extras": "page.tools.model_viewer.animation_group_extras",
	"":       "page.tools.model_viewer.animation_group_other",
}

const (
	viewerClipTileWidth   = 72
	viewerClipTileHeight  = 44
	viewerClipTileGap     = 6
	viewerClipTileColumns = 5
	viewerClipPanelPad    = 10
)

// viewerClipGroup is one tab of the clip picker: a group name and the indexes of its clips.
type viewerClipGroup struct {
	name  string
	clips []int
}

// clipGroups groups clip indexes by names, the group of each clip, in viewerClipGroupOrder.
func clipGroups(names []string) []viewerClipGroup {
	var groups []viewerClipGroup
	index := map[string]int{}
	for i, name := range names {
		k, ok := index[name]
		if !ok {
			k = len(groups)
			index[name] = k
			groups = append(groups, viewerClipGroup{name: name})
		}
		groups[k].clips = append(groups[k].clips, i)
	}

	rank := func(name string) int {
		if name == "" {
			return len(viewerClipGroupOrder) + 1
		}
		if i := slices.Index(viewerClipGroupOrder, name); i >= 0 {
			return i
		}
		return len(viewerClipGroupOrder)
	}
	slices.SortStableFunc(groups, func(a, b viewerClipGroup) int { return rank(a.name) - rank(b.name) })
	return groups
}

func clipGroupLabel(name string) string {
	if key, ok := viewerClipGroupLabels[name]; ok {
		return T(key)
	}
	return name
}

// trackClipGroups returns the group of each of track's clips. The mod's clips have none.
func (v *ModelViewer) trackClipGroups(track viewerTrack, clips []modelviewer.ModelViewerAnimationClip) []string {
	names := make([]string, len(clips))
	m := v.activeMotion()
	if track != trackMotion || m == nil {
		return names
	}
	for i, clip := range clips {
		if n := m.clipNumber(clip.ID); n < len(m.motion.Clips) {
			names[i] = m.motion.Clips[n].Group
		}
	}
	return names
}

// closeClipPicker closes the clip picker when the lane it opened from is no longer among tracks.
func (v *ModelViewer) closeClipPicker(tracks []viewerTrack) {
	if open := v.anim.clipPicker; open != nil && !slices.Contains(tracks, *open) {
		v.anim.clipPicker = nil
	}
}

// animationClipPicker names the clip track shows and, when it has others, opens a panel above the bar with the
// clips as tiles, under a tab for each group when they have more than one. Its icon tells the mod's clips from the
// character's motions.
func (v *ModelViewer) animationClipPicker(
	t kit.Theme,
	track viewerTrack,
	clip *modelviewer.ModelViewerAnimationClip,
	paired bool,
) *fairy.El {
	icon := lucide.Clapperboard
	if track == trackMotion {
		icon = lucide.PersonStanding
	}
	trigger := fairy.Row(
		fairy.Icon(icon).Size(14, 14).Shrink(0),
		fairy.Text(clip.Label).FontSize(12).LineHeight(16).Medium().Ellipsis().MinW(0).Grow(1),
	).Gap(5).Align(fairy.Center).H(28).PadXY(8, 0).MinW(0).MaxW(160).Radius(14)
	if paired {
		trigger.W(viewerAnimationClipWidth)
	}
	clips := v.trackClips(track)
	if len(clips) <= 1 {
		return trigger
	}

	open := v.anim.clipPicker != nil && *v.anim.clipPicker == track
	trigger.Child(fairy.Icon(lucide.ChevronUp).Size(12, 12).Shrink(0).Color(t.MutedForeground)).
		HoverFill(hoverFill(&t)).Cursor(fairy.CursorPointer).Role(fairy.RoleButton).Focusable().
		FocusRing(3, t.Ring.Alpha(0.5)).Expanded(open).
		Label(T("page.tools.model_viewer.animation_clip")).Key("model-viewer.animation-clip-trigger." + track.key()).
		OnClick(func() {
			if open {
				v.anim.clipPicker = nil
				return
			}
			v.anim.clipPicker, v.anim.clipGroup = &track, ""
		})
	if !open {
		return trigger
	}
	trigger.Fill(hoverFill(&t))
	return trigger.Popover(v.clipPickerPanel(t, track, clip, clips), fairy.PopoverOptions{
		Side: fairy.SideTop, Align: fairy.AlignStart, Gap: 8,
		OnDismiss: func() { v.anim.clipPicker = nil },
	})
}

// clipPickerPanel shows the clips of track as tiles, the one playing selected. The tab shown is the one last picked,
// or the group of the clip playing.
func (v *ModelViewer) clipPickerPanel(
	t kit.Theme,
	track viewerTrack,
	clip *modelviewer.ModelViewerAnimationClip,
	clips []modelviewer.ModelViewerAnimationClip,
) *fairy.El {
	names := v.trackClipGroups(track, clips)
	groups := clipGroups(names)
	current := slices.IndexFunc(groups, func(g viewerClipGroup) bool { return g.name == v.anim.clipGroup })
	if current < 0 {
		var playing string
		if i := slices.IndexFunc(clips, func(c modelviewer.ModelViewerAnimationClip) bool {
			return c.ID == clip.ID
		}); i >= 0 {
			playing = names[i]
		}
		current = max(slices.IndexFunc(groups, func(g viewerClipGroup) bool { return g.name == playing }), 0)
	}
	group := groups[current]

	columns := viewerClipTileColumns
	if len(groups) == 1 {
		columns = min(len(clips), viewerClipTileColumns)
	}
	width := float32(columns)*viewerClipTileWidth + float32(columns-1)*viewerClipTileGap + 2*viewerClipPanelPad
	width = min(width, max(v.width-24, viewerClipTileWidth+2*viewerClipPanelPad))

	body := fairy.Col().Gap(8)
	if len(groups) > 1 {
		items := make([]kit.Item, len(groups))
		for i, g := range groups {
			items[i] = kit.Item{Value: g.name, Label: clipGroupLabel(g.name)}
		}
		body.Child(kit.Tabs(group.name, items, func(name string) { v.anim.clipGroup = name }, kit.Line).
			Key("model-viewer.animation-clip-groups." + track.key()))
	}
	// The tiles stay on one row that scrolls sideways, so the panel keeps its height on every tab.
	tiles := fairy.Row().Gap(viewerClipTileGap).WFull().ScrollX().FocusGroup().Role(fairy.RoleGroup).
		Key("model-viewer.animation-clip-tiles." + track.key() + "." + group.name)
	for _, i := range group.clips {
		tiles.Child(v.clipTile(t, track, clips[i], clips[i].ID == clip.ID))
	}
	body.Child(tiles)

	return t.Surface(body, t.Popover).W(width).Pad(viewerClipPanelPad).
		Radius(t.RadiusLg()).Ring(1, t.Foreground.Alpha(0.1)).Color(t.PopoverForeground).
		Label(T("page.tools.model_viewer.animation_clip"))
}

func (v *ModelViewer) clipTile(
	t kit.Theme,
	track viewerTrack,
	clip modelviewer.ModelViewerAnimationClip,
	selected bool,
) *fairy.El {
	label := fairy.Text(clip.Label).FontSize(11).LineHeight(14).Medium().Wrap().TextAlign(fairy.TextCenter).WFull()
	tile := fairy.Col(fairy.Box(label).WFull().MaxH(28).Clip()).
		Key(clip.ID).Size(viewerClipTileWidth, viewerClipTileHeight).Shrink(0).Center().PadXY(4, 0).
		Radius(t.RadiusMd()).Border(1, t.Border).HoverFill(hoverFill(&t)).
		Cursor(fairy.CursorPointer).Role(fairy.RoleRadio).Selected(selected).Label(clip.Label).
		Focusable().FocusRing(3, t.Ring.Alpha(0.5))
	if selected {
		return tile.Border(1, t.Primary).Fill(t.Primary.Alpha(0.12))
	}
	return tile.OnClick(func() { v.startTrack(track, clip.ID) })
}
