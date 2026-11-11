package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"

	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/modname"
)

// mergeDraft is the merge dialog: the selected mods' classification and the plan the user arranges from it.
type mergeDraft struct {
	groupPath string
	paths     []string
	loading   bool
	result    mod.ClassifyMergePacksResult
	plan      *mod.MergePlanNode
	packName  string
	placement string
	busy      bool
	// editingKey is "forward" or "back" while the key recorder for that binding is open.
	editingKey string
	recording  keyRecording
}

func (s *Shell) canMerge() bool {
	game := s.gameConfig()
	return game != nil && mod.IsThreedmigotoImporter(deref(game.Importer))
}

func (s *Shell) setMergeMode(on bool) {
	m := &s.mods
	m.mergeMode = on
	clear(m.mergeSelected)
	m.mergeDraft = nil
}

// mergeSelection lists the selected mods that are still in the folder, in listing order.
func (s *Shell) mergeSelection() []string {
	m := &s.mods
	if m.listing == nil {
		return nil
	}
	var paths []string
	for _, info := range m.listing.Mods {
		if m.mergeSelected[info.Path] {
			paths = append(paths, info.Path)
		}
	}
	return paths
}

func (s *Shell) mergeHeader() *fairy.El {
	t := kit.T()
	count := len(s.mergeSelection())
	return fairy.Row(
		fairy.Text(T("page.mod.merge.selected_count", "count", count)).FontSize(13).Color(t.MutedForeground).NoWrap(),
		kit.Button(T("g.cancel"), func() { s.setMergeMode(false) }, kit.Outline, kit.Small),
		kit.Button(T("page.mod.merge.next"), s.openMergeDialog, kit.Small, kit.Disabled(count < 2)),
	).Gap(8).Align(fairy.Center).Shrink(0)
}

func (s *Shell) openMergeDialog() {
	m := &s.mods
	paths := s.mergeSelection()
	if len(paths) < 2 || m.group == nil {
		return
	}
	d := &mergeDraft{groupPath: m.group.Path, paths: paths, loading: true, placement: "new_folder"}
	m.mergeDraft = d
	s.background(func(ctx context.Context) {
		result, err := s.deps.Mod.ClassifyMergePacks(ctx, paths)
		fairy.Go(func() {
			if m.mergeDraft != d {
				return
			}
			d.loading = false
			if err != nil {
				s.modError(err, "classify-merge-packs", map[string]any{"paths": paths})
				m.mergeDraft = nil
				message := err.Error()
				if message == "" {
					message = T("page.mod.merge.classify_failed")
				}
				s.toasts.Show(kit.ToastError, message, "")
				return
			}
			d.result = result
			d.packName = "Merged"
			if len(result.Packs) > 0 {
				if name := strings.Join(strings.Fields(result.Packs[0].Name), ""); name != "" {
					d.packName = name
				}
			}
			d.plan = mod.DefaultMergePlan(result, d.packName)
		})
	})
}

// mergeRoot is the plan as it is sent: the root takes the pack name without spaces.
func (d *mergeDraft) mergeRoot() *mod.MergePlanNode {
	if d.plan == nil {
		return nil
	}
	root := *d.plan
	if name := strings.Join(strings.Fields(d.packName), ""); name != "" {
		root.Name = name
	}
	return &root
}

func (d *mergeDraft) packs() map[string]mod.MergePackClassification {
	packs := make(map[string]mod.MergePackClassification, len(d.result.Packs))
	for _, pack := range d.result.Packs {
		packs[pack.Path] = pack
	}
	return packs
}

func (s *Shell) runMerge(d *mergeDraft) {
	m := &s.mods
	root := d.mergeRoot()
	if d.busy || root == nil {
		return
	}
	d.busy = true
	request := mod.MergeModsRequest{GroupPath: d.groupPath, Placement: d.placement, PackName: d.packName, Root: *root}
	s.background(func(ctx context.Context) {
		_, err := s.deps.Mod.MergeMods(ctx, request)
		fairy.Go(func() {
			d.busy = false
			if err != nil {
				if strings.Contains(err.Error(), "MOD_DOWNLOAD_IN_PROGRESS") {
					s.toasts.Show(kit.ToastWarning, T("page.mod.download_action_blocked"), "")
					return
				}
				s.modError(err, "merge-mods", map[string]any{
					"groupPath": request.GroupPath, "placement": request.Placement, "packName": request.PackName,
					"paths": d.paths,
				})
				message := err.Error()
				if message == "" {
					message = T("page.mod.merge.failed")
				}
				s.toasts.Show(kit.ToastError, message, "")
				return
			}
			s.toasts.Show(kit.ToastSuccess, T("page.mod.merge.success"), "")
			if m.mergeDraft == d {
				s.setMergeMode(false)
			}
			s.refreshListing()
			s.refreshFolders()
		})
	})
}

func (s *Shell) mergeDialog(cx *fairy.Cx) {
	m := &s.mods
	d := m.mergeDraft
	if d == nil {
		return
	}
	t := kit.T()
	windowWidth, windowHeight := cx.Size()
	dialogWidth := min(windowWidth-32, 768)
	dialogHeight := min(windowHeight*0.9, 800, windowHeight-titleBarHeight-32)
	const prefix = "page.mod.merge."
	close := func() {
		if !d.busy {
			m.mergeDraft = nil
		}
	}

	var body *fairy.El
	canSubmit := false
	switch {
	case d.loading:
		body = fairy.Row(kit.Spinner(16), fairy.Text(T(prefix+"classifying")).Color(t.MutedForeground)).Gap(8).
			Align(fairy.Center).Justify(fairy.JustifyCenter).PadXY(0, 40)
	case d.plan == nil:
		body = fairy.Col(xxmiAlert(lucide.CircleAlert, true, "", T(prefix+"no_usable_mods"))).
			Justify(fairy.JustifyCenter).PadXY(0, 40)
	default:
		packs := d.packs()
		plan := d.plan
		classicLocked := mod.MergePlanHasClassicViolation(*plan, packs)
		nameValid := strings.TrimSpace(d.packName) == "" || mod.SafeMergeName(d.packName)
		root := d.mergeRoot()
		canSubmit = nameValid && m.group != nil && mod.MergePlanValid(*root) && !classicLocked

		body = fairy.Col().Gap(16)
		if !d.result.HashOverlap {
			body.Child(xxmiAlert(lucide.Info, false, "", T(prefix+"hash_mismatch")))
		}
		if classicLocked {
			body.Child(xxmiAlert(lucide.CircleAlert, true, "", T(prefix+"classic_locked")))
		}

		nameLabel := T(prefix + "pack_name")
		nameField := kit.Field(nameLabel, kit.TextField(d.packName, func(v string) { d.packName = v },
			kit.Invalid(!nameValid)), kit.Invalid(!nameValid), kit.Description(errorText(!nameValid,
			T(prefix+"invalid_name"))))
		keys := fairy.Row(
			s.mergeKeyField(d, "forward", T(prefix+"forward_key"), plan.ForwardKey).Grow(1).MinW(0),
			s.mergeKeyField(d, "back", T(prefix+"back_key"), plan.BackKey).Grow(1).MinW(0),
		).Gap(12)
		inputs := fairy.Col(nameField, keys).Gap(12)
		if windowWidth >= 640 {
			inputs = fairy.Row(nameField.Grow(1).Basis(0).MinW(0), keys.Grow(1).Basis(0).MinW(0)).
				Gap(12).
				Align(fairy.Start)
		}

		choice := func(label string, selected bool, onClick func()) *fairy.El {
			variant := kit.Outline
			if selected {
				variant = kit.Default
			}
			return kit.Button(T(prefix+label), onClick, variant, kit.Small)
		}
		placement := mergeOption(T(prefix+"placement"), fairy.Row(
			choice("placement_in_place", d.placement == "in_place", func() { d.placement = "in_place" }),
			choice("placement_new_folder", d.placement == "new_folder", func() { d.placement = "new_folder" }),
		).Gap(8))
		engine := mergeOption(T(prefix+"engine"), fairy.Row(
			choice(
				"engine_classic",
				plan.Engine == "classic",
				func() { plan.Engine, plan.IncludeVanilla = "classic", false },
			),
			choice(
				"engine_namespace",
				plan.Engine == "namespace",
				func() { plan.Engine, plan.IncludeVanilla = "namespace", true },
			),
		).Gap(4))
		vanilla := mergeOption(T(prefix+"include_vanilla"), fairy.Row(kit.Switch(plan.IncludeVanilla,
			func(on bool) { plan.IncludeVanilla = on }, kit.Disabled(plan.Engine != "namespace")).
			Label(T(prefix+"include_vanilla"))).H(32).Align(fairy.Center))
		options := fairy.Col(placement, engine, vanilla).Gap(12)
		if windowWidth >= 640 {
			options = fairy.Row(placement.Grow(1).Basis(0), engine.Grow(1).Basis(0), vanilla.Grow(1).Basis(0)).Gap(12)
		}
		body.Child(fairy.Col(inputs, options).Gap(12).Shrink(0))

		body.Child(
			fairy.Col(s.mergeGroupEditor(plan, packs, true)).Grow(1).MinH(0).ScrollY().Pad(12).Radius(t.RadiusLg()).
				Fill(t.Muted.Alpha(0.5)),
		)
	}

	kit.ShowDialog(cx, kit.DialogProps{
		Title: T(prefix+"dialog_title") + " (" + T("g.beta") + ")",
		Width: dialogWidth,
		Body: fairy.Col(body.Grow(1).MinH(0).Clip(), fairy.Row(
			kit.Button(T("g.cancel"), close, kit.Outline, kit.Disabled(d.busy)).AutoFocus(),
			kit.Button(T(prefix+"confirm"), func() { s.runMerge(d) }, kit.Loading(d.busy),
				kit.Disabled(!canSubmit)),
		).Gap(8).Justify(fairy.JustifyEnd).Shrink(0)).H(max(dialogHeight-80, 0)).Gap(16),
		OnClose: close,
		Key:     "mod.merge",
	})
	s.mergeKeyDialog(cx, d)
}

func mergeOption(label string, control *fairy.El) *fairy.El {
	return fairy.Col(fairy.Text(label).FontSize(14).LineHeight(17.5).Medium(), control).Gap(8)
}

func (s *Shell) mergeKeyField(d *mergeDraft, which, label, value string) *fairy.El {
	t := kit.T()
	keys := fairy.Row().Gap(4).Align(fairy.Center)
	for _, key := range keyLabelsOf(value) {
		keys.Child(kit.Kbd(key))
	}
	if len(keyLabelsOf(value)) == 0 {
		keys.Child(fairy.Text(T("page.mod.merge.press_key")).Color(t.MutedForeground))
	}
	open := func() {
		other := d.plan.BackKey
		if which == "back" {
			other = d.plan.ForwardKey
		}
		d.editingKey = which
		d.recording = keyRecording{value: value}
		if strings.TrimSpace(other) != "" {
			d.recording.otherKeys = []string{other}
		}
	}
	button := keys.H(32).PadXY(8, 0).Radius(t.RadiusMd()).Border(1, t.Input).HoverFill(hoverFill(t)).
		Animate(motion.Fast).Cursor(fairy.CursorPointer).Focusable().Role(fairy.RoleButton).OnClick(open)
	return kit.Field(label, button)
}

func (s *Shell) mergeKeyDialog(cx *fairy.Cx, d *mergeDraft) {
	if d.editingKey == "" || d.plan == nil {
		return
	}
	title := T("page.mod.merge.forward_key")
	if d.editingKey == "back" {
		title = T("page.mod.merge.back_key")
	}
	dismiss := func() { d.editingKey = "" }
	save := func() {
		if d.editingKey == "back" {
			d.plan.BackKey = d.recording.value
		} else {
			d.plan.ForwardKey = d.recording.value
		}
		d.editingKey = ""
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title: title,
		Body:  keyRecorder(&d.recording, title, "mod.merge-key-manual", save),
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), dismiss, kit.Outline),
			kit.Button(T("g.save"), save),
		},
		OnClose: dismiss,
		Key:     "mod.merge-key",
	})
}

// mergeGroupEditor lists the children of node with buttons to reorder them, group a pack with the next one, and
// dissolve a nested group. Nested groups are edited in place.
func (s *Shell) mergeGroupEditor(node *mod.MergePlanNode, packs map[string]mod.MergePackClassification,
	allowGrouping bool,
) *fairy.El {
	t := kit.T()
	list := fairy.Col().Gap(12)
	for index := range node.Children {
		child := &node.Children[index]
		move := func(to int) {
			if to < 0 || to >= len(node.Children) {
				return
			}
			children := slices.Clone(node.Children)
			children[index], children[to] = children[to], children[index]
			node.Children = children
		}
		buttons := fairy.Col(
			kit.IconButton(lucide.ArrowUp, func() { move(index - 1) }, kit.Outline, kit.Small,
				kit.Tooltip(T("g.move_up")), kit.Disabled(index == 0)).Label(T("g.move_up")),
			kit.IconButton(lucide.ArrowDown, func() { move(index + 1) }, kit.Outline, kit.Small,
				kit.Tooltip(T("g.move_down")), kit.Disabled(index == len(node.Children)-1)).Label(T("g.move_down")),
		).Gap(4).Justify(fairy.JustifyCenter)
		if allowGrouping && child.Kind == "leaf" && index+1 < len(node.Children) &&
			node.Children[index+1].Kind == "leaf" {
			label := T("page.mod.merge.group_next")
			buttons.Child(kit.IconButton(lucide.Group, func() {
				group := mod.MergePlanNode{
					Kind:       "group",
					ID:         fmt.Sprintf("group-%d-%d", time.Now().UnixMilli(), index),
					Engine:     node.Engine,
					Name:       fmt.Sprintf("%sPart%d", node.Name, index+1),
					ForwardKey: node.ForwardKey,
					BackKey:    node.BackKey,
					Children:   slices.Clone(node.Children[index : index+2]),
				}
				node.Children = slices.Concat(node.Children[:index], []mod.MergePlanNode{group},
					node.Children[index+2:])
			}, kit.Outline, kit.Small, kit.Tooltip(label)).Label(label))
		}
		if child.Kind == "group" {
			label := T("page.mod.merge.ungroup")
			buttons.Child(kit.IconButton(lucide.Ungroup, func() {
				node.Children = slices.Concat(node.Children[:index], child.Children, node.Children[index+1:])
			}, kit.Outline, kit.Small, kit.Tooltip(label)).Label(label))
		}

		var content *fairy.El
		if child.Kind == "leaf" {
			content = mergeLeafRow(child.Path, packs)
		} else {
			content = fairy.Col(s.mergeGroupEditor(child, packs, false)).Pad(12).Radius(8).
				Fill(t.Muted.Alpha(0.5))
		}
		list.Child(fairy.Row(buttons.Shrink(0), content.Grow(1).MinW(0)).Gap(8).Align(fairy.Stretch))
	}
	return list
}

func mergeLeafRow(path string, packs map[string]mod.MergePackClassification) *fairy.El {
	t := kit.T()
	pack, ok := packs[path]
	name, family := modname.StripDisabled(filepath.Base(path)), "ordinary"
	if ok {
		name, family = pack.Name, pack.Family
	}
	detail := T("page.mod.merge.family." + family)
	if ok && !pack.AllowsClassic {
		detail += " · " + T("page.mod.merge.namespace_only")
	}
	return fairy.Col(
		fairy.Text(name).Medium().Ellipsis().MinW(0).TooltipWhenTruncated(),
		fairy.Text(detail).FontSize(12).Color(t.MutedForeground),
	).Gap(2).PadXY(12, 8).Justify(fairy.JustifyCenter).Radius(6).Fill(t.Background.Alpha(0.6))
}
