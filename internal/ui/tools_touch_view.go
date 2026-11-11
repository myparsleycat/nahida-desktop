package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"
	"github.com/samber/lo"

	touchprofile "nahida.live/desktop/internal/tools/touch_profile"
)

const touchChannelCount = 12

var touchChannelLabels = func() []string {
	labels := []string{"L Breast", "R Breast", "L Butt/Thigh", "R Butt/Thigh"}
	for channel := len(labels); channel < touchChannelCount; channel++ {
		labels = append(labels, fmt.Sprintf("Ch %d", channel))
	}
	return labels
}()

func (s *Shell) touchProfileToolPage(cx *fairy.Cx) *fairy.El {
	if s.tools.touch.key == "" {
		s.tools.touch = newTouchProfile("tools.touch")
	}
	return s.touchProfileView(cx, &s.tools.touch)
}

// touchProfileDialog shows the Touch Profile dialog that openTouchProfileDialog opened.
func (s *Shell) touchProfileDialog(cx *fairy.Cx) {
	tp := s.tools.touchDialog
	if tp == nil {
		return
	}
	t := kit.T()
	title := tp.modName
	if title == "" {
		title = T(touchPrefix+"title") + " (" + T("g.beta") + ")"
	}
	header := fairy.Row(
		fairy.Text(title).FontSize(16).LineHeight(24).SemiBold().Ellipsis().Grow(1).MinW(0),
		kit.IconButton(lucide.X, s.closeTouchProfileDialog, kit.Ghost, kit.Small).Label(T("g.close")),
	).Gap(8).Align(fairy.Center).Shrink(0)
	panel := fairy.Col(header, s.touchProfileView(cx, tp)).Gap(12).Pad(12).
		Fill(t.Background).Radius(t.RadiusXl()).Border(1, t.Border).Clip().Shadow(0, 20, 50, 0, shadowColor(t)).
		Role(fairy.RoleDialog).Label(title)
	showWindowDialog(cx, "mod.touch-profile", panel, s.closeTouchProfileDialog)
}

func (s *Shell) touchProfileView(cx *fairy.Cx, tp *touchProfile) *fairy.El {
	s.touchEffects(tp)
	t := kit.T()
	width, _ := cx.Size()

	header := fairy.Col(
		fairy.Text(T(touchPrefix+"title") + " (" + T("g.beta") + ")").FontSize(14).LineHeight(20).Medium(),
	)
	if tp.modName != "" {
		header.Child(fairy.Text(tp.modName).FontSize(12).LineHeight(16).Color(t.MutedForeground).Ellipsis())
	}
	content := fairy.Col().Gap(16).Pad(16)
	if tp.fixedPath == "" {
		busy := tp.loading || tp.applying || tp.rollingBack
		content.Child(kit.Field(T(touchPrefix+"mod_path"), fairy.Row(
			kit.TextField(tp.modPath, func(value string) { tp.modPath = value },
				kit.Placeholder(T(touchPrefix+"mod_path_placeholder")),
				kit.OnSubmit(func(value string) {
					if !busy {
						s.touchLoad(tp, value)
					}
				})).Grow(1).MinW(0),
			kit.IconButton(lucide.FolderOpen, func() { s.touchSelectFolder(tp) }, kit.Outline,
				kit.Disabled(tp.applying || tp.rollingBack)).Label(T(touchPrefix+"mod_path")),
		).Gap(8)))
	}
	if tp.phase != touchSelect {
		content.Child(kit.Button(T(touchPrefix+"discard"), func() { s.touchDiscard(tp) }, kit.Outline,
			kit.Disabled(tp.applied != nil || tp.applying)))
	}
	if tp.inputError != "" {
		prefix := touchPrefix + "input_error." + tp.inputError
		content.Child(touchNotice(t.Destructive,
			fairy.Text(T(prefix+".title")).Medium(), fairy.Text(T(prefix+".description")).Wrap()))
	}
	if tp.phase == touchSelect && tp.inspection != nil {
		content.Child(s.touchSelection(tp)...)
	}
	if tp.draft != nil {
		content.Child(s.touchReview(tp)...)
	}
	if tp.applied != nil {
		content.Child(s.touchResult(tp))
	}
	panel := fairy.Col(
		header.Gap(2).PadXY(16, 12).BorderEdges(0, 0, 1, 0, t.Border),
		fairy.Col(content).Grow(1).MinH(0).ScrollY(),
	).MinH(0).Radius(6).Fill(t.Muted.Alpha(0.3)).Clip()

	body := toolsViewportSplit(s.touchViewport(tp), panel, width < toolsWideViewport)
	view := fairy.Col().Gap(12).Grow(1).MinH(0).WFull()
	if (tp.phase == touchSelect && tp.meshPreview != nil) || (tp.phase == touchReview && tp.activePreview() != nil) {
		view.Child(s.touchMenuBar(tp))
	}
	return view.Child(body)
}

// touchNotice is a bordered message box tinted with ink.
func touchNotice(ink fairy.Color, children ...*fairy.El) *fairy.El {
	return fairy.Col(children...).Gap(4).PadXY(12, 8).Radius(6).FontSize(12).Color(ink).
		Fill(ink.Alpha(0.1)).Border(1, ink.Alpha(0.4))
}

func touchCard(children ...*fairy.El) *fairy.El {
	return fairy.Col(children...).Gap(12).Pad(12).Radius(6).Fill(kit.T().Background.Alpha(0.6))
}

func touchCount(n int) string {
	digits := strconv.Itoa(n)
	var out strings.Builder
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 && digits[i-1] != '-' {
			out.WriteByte(',')
		}
		out.WriteRune(digit)
	}
	return out.String()
}

func touchPercent(value float64) string { return strconv.Itoa(int(math.Round(value*100))) + "%" }

func formatTouchSetting(value float64) string {
	text := strconv.FormatFloat(value, 'f', 3, 64)
	return strings.TrimRight(strings.TrimRight(text, "0"), ".")
}

func (s *Shell) touchSelection(tp *touchProfile) []*fairy.El {
	t := kit.T()
	inspection := tp.inspection
	selectWhere := func(keep func(candidate bool) bool) func() {
		return func() {
			tp.selected = map[string]bool{}
			for _, component := range inspection.Components {
				if keep(component.InteractiveCandidate) {
					tp.selected[component.ID] = true
				}
			}
		}
	}
	summary := fairy.Col(
		fairy.Text(T(touchPrefix+"mesh_select_title")).Medium(),
		fairy.Row(
			fairy.Text(T(touchPrefix+"support_grade")+": "+inspection.SupportGrade),
			fairy.Text(T(touchPrefix+"mesh_select_count", "selected", len(tp.selected),
				"total", len(inspection.Components))),
		).Gap(12).FontSize(12).Color(t.MutedForeground),
	).Gap(8)
	bulk := fairy.Row(
		kit.Button(T(touchPrefix+"mesh_select_all"), selectWhere(func(bool) bool { return true }), kit.Outline, kit.XS),
		kit.Button(T(touchPrefix+"mesh_select_candidates"), selectWhere(func(candidate bool) bool { return candidate }),
			kit.Outline, kit.XS),
		kit.Button(T(touchPrefix+"mesh_select_none"), selectWhere(func(bool) bool { return false }), kit.Outline,
			kit.XS),
	).Gap(8).Wrap()

	meshes := fairy.Col().Gap(8)
	for _, component := range inspection.Components {
		name := fairy.Row(fairy.Text(component.Name).Medium().Ellipsis().MinW(0)).Gap(4).Align(fairy.Center)
		if component.VariantKey != nil {
			name.Child(fairy.Text("(v" + *component.VariantKey + ")").FontSize(12).Color(t.MutedForeground).Shrink(0))
		}
		details := fairy.Row(
			kit.Badge(component.Kind, kit.Outline),
			kit.Badge(component.SupportGrade, kit.Outline),
			fairy.Text(touchCount(component.VertexCount)+" v · "+touchCount(component.IndexCount)+" i"),
		).Gap(6).Align(fairy.Center).Wrap().FontSize(12).Color(t.MutedForeground)
		if !component.InteractiveCandidate {
			details.Child(kit.Badge(T(touchPrefix+"mesh_non_candidate"), kit.Secondary))
		}
		id := component.ID
		row := fairy.Row(
			kit.Checkbox(tp.selected[id], func(on bool) {
				if on {
					tp.selected[id] = true
				} else {
					delete(tp.selected, id)
				}
			}).Label(component.Name),
			fairy.Col(name, details).Gap(4).Grow(1).MinW(0).Cursor(fairy.CursorPointer).OnClick(func() {
				if tp.focusMesh == id {
					tp.focusMesh = ""
				} else {
					tp.focusMesh = id
				}
			}),
		).Key("touch.mesh."+id).Gap(8).Align(fairy.Center).PadXY(12, 8).Radius(6).Animate(motion.Fast)
		if tp.focusMesh == id {
			row.Fill(t.Primary.Alpha(0.1))
		} else {
			row.HoverFill(t.Muted.Alpha(0.6))
		}
		meshes.Child(row)
	}

	next := T(touchPrefix + "mesh_next")
	if tp.loading {
		next = T(touchPrefix + "analyzing")
	}
	return []*fairy.El{
		summary, bulk, meshes, s.touchBoneSelection(tp),
		kit.Button(next, func() { s.touchAnalyze(tp) }, kit.Loading(tp.loading), kit.Disabled(!tp.touchCanAnalyze())),
	}
}

func (s *Shell) touchBoneSelection(tp *touchProfile) *fairy.El {
	t := kit.T()
	threshold := tp.threshold
	card := touchCard(
		fairy.Col(
			fairy.Text(T(touchPrefix+"bone_select_title")).Medium(),
			fairy.Text(T(touchPrefix+"bone_select_hint")).FontSize(12).Color(t.MutedForeground).Wrap(),
		).Gap(4),
		fairy.Col(
			fairy.Text(T(touchPrefix+"weight_threshold")).Medium(),
			fairy.Row(
				kit.RangeSlider(tp.key+".threshold", threshold, 0, 1, .005, func(value [2]float64) {
					tp.threshold = value
				}, kit.Label(T(touchPrefix+"weight_threshold"))).Grow(1),
				fairy.Text(fmt.Sprintf("%.3f ~ %.3f", threshold[0], threshold[1])).FontSize(12).W(80).Shrink(0).
					TextAlign(fairy.TextEnd),
			).Gap(12).Align(fairy.Center),
			fairy.Text(T(touchPrefix+"weight_threshold_hint")).FontSize(12).Color(t.MutedForeground).Wrap(),
		).Gap(4),
	)
	for _, component := range tp.inspection.Components {
		if !tp.selected[component.ID] {
			continue
		}
		head := fairy.Row(fairy.Text(component.Name).Medium().Ellipsis().MinW(0)).Gap(8).Align(fairy.Center)
		if !component.HasBlend {
			head.Child(kit.Badge(T(touchPrefix+"no_blend"), kit.Destructive))
			card.Child(fairy.Col(head, fairy.Text(T(touchPrefix+"no_blend_hint")).FontSize(12).
				Color(t.MutedForeground).Wrap()).Gap(8))
			continue
		}
		head.Child(kit.Badge(fmt.Sprintf("%d %s", len(component.Bones), T(touchPrefix+"bones_count")), kit.Outline))

		id := component.ID
		assignments := tp.assignments[id]
		vertexCounts := map[uint32]int{}
		items := make([]kit.Item, len(component.Bones))
		for i, bone := range component.Bones {
			vertexCounts[bone.ID] = bone.VertexCount
			items[i] = kit.Item{
				Value: strconv.FormatUint(uint64(bone.ID), 10),
				Label: T(touchPrefix+"bone_label", "id", bone.ID, "count", bone.VertexCount),
			}
		}
		values := lo.Map(assignments, func(a touchprofile.TouchBoneZoneSelection, _ int) string {
			return strconv.FormatUint(uint64(a.BoneID), 10)
		})
		picker := kit.MultiCombobox(tp.key+".bones."+id, values, items, func(next []string) {
			tp.setBoneAssignments(id, next)
		},
			kit.Placeholder(T(touchPrefix+"bone_select_placeholder")),
			kit.EmptyText(T(touchPrefix+"no_bones")),
			kit.OnHighlight(func(value string) {
				tp.browseBone = -1
				if bone, err := strconv.ParseUint(value, 10, 32); err == nil {
					tp.browseBone = int64(bone)
				}
			}))

		rows := fairy.Col().Gap(6)
		for index, assignment := range assignments {
			rows.Child(s.touchAssignmentRow(tp, id, index, assignment, assignments, vertexCounts[assignment.BoneID]))
		}
		card.Child(fairy.Col(head, picker, rows).Gap(8))
	}
	return card
}

func (s *Shell) touchAssignmentRow(
	tp *touchProfile,
	componentID string,
	index int,
	assignment touchprofile.TouchBoneZoneSelection,
	assignments []touchprofile.TouchBoneZoneSelection,
	vertexCount int,
) *fairy.El {
	t := kit.T()
	update := func(edit func(*touchprofile.TouchBoneZoneSelection)) {
		if current := tp.assignments[componentID]; index < len(current) {
			edit(&current[index])
		}
	}
	channels := make([]kit.Item, touchChannelCount)
	for channel := range touchChannelCount {
		label := touchChannelLabels[channel]
		for other, a := range assignments {
			if other != index && a.Channel != nil && *a.Channel == channel {
				label += " (" + T(touchPrefix+"bone_label_short", "id", a.BoneID) + ")"
				break
			}
		}
		channels[channel] = kit.Item{Value: strconv.Itoa(channel), Label: label}
	}
	channel := ""
	if assignment.Channel != nil {
		channel = strconv.Itoa(*assignment.Channel)
	}
	key := fmt.Sprintf("%s.assign.%s.%d", tp.key, componentID, assignment.BoneID)
	bone := int64(assignment.BoneID)
	return fairy.Row(
		fairy.Text(T(touchPrefix+"bone_label", "id", assignment.BoneID, "count", vertexCount)).FontSize(12).Medium().
			Ellipsis().Grow(1).MinW(0),
		kit.Select(key+".channel", channel, channels, func(value string) {
			if number, err := strconv.Atoi(value); err == nil {
				update(func(a *touchprofile.TouchBoneZoneSelection) { a.Channel = &number })
			}
		},
			kit.Placeholder(T(touchPrefix+"channel_select_placeholder")), kit.Small,
			kit.OnOpenChange(func(open bool) {
				switch {
				case open:
					tp.channelBone = bone
				case tp.channelBone == bone:
					tp.channelBone = -1
				}
			})).W(112).Shrink(0),
		kit.TextField(deref(assignment.Label), func(value string) {
			update(func(a *touchprofile.TouchBoneZoneSelection) {
				a.Label = nil
				if value != "" {
					a.Label = &value
				}
			})
		}, kit.Placeholder(T(touchPrefix+"zone_label_placeholder")), kit.Small).W(112).Grow(0).Shrink(0),
	).Key(key).Gap(8).Align(fairy.Center).PadXY(8, 6).Radius(6).Fill(t.Background.Alpha(0.6)).
		OnHover(func(in bool) {
			switch {
			case in:
				tp.hoverBone = bone
			case tp.hoverBone == bone:
				tp.hoverBone = -1
			}
		})
}

// setBoneAssignments makes the component's assignments follow the bones chosen in its picker, in their order,
// keeping the channel and label of bones that stay.
func (tp *touchProfile) setBoneAssignments(componentID string, values []string) {
	current := tp.assignments[componentID]
	next := make([]touchprofile.TouchBoneZoneSelection, 0, len(values))
	for _, value := range values {
		bone, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			continue
		}
		assignment, ok := lo.Find(current, func(a touchprofile.TouchBoneZoneSelection) bool {
			return a.BoneID == uint32(bone)
		})
		if !ok {
			assignment = touchprofile.TouchBoneZoneSelection{BoneID: uint32(bone)}
		}
		next = append(next, assignment)
	}
	tp.assignments[componentID] = next
	hovered := func(a touchprofile.TouchBoneZoneSelection) bool { return int64(a.BoneID) == tp.hoverBone }
	if !lo.ContainsBy(next, hovered) {
		tp.hoverBone = -1
	}
}

func (s *Shell) touchReview(tp *touchProfile) []*fairy.El {
	t := kit.T()
	draft := tp.draft
	interactive := tp.interactiveComponents()
	component := tp.selectedComponent()
	active := tp.activePreview()
	zoneID := tp.activeZoneID()

	yesNo := T(touchPrefix + "no")
	if draft.CanAutoApply {
		yesNo = T(touchPrefix + "yes")
	}
	out := []*fairy.El{fairy.Col(
		fairy.Text(T(touchPrefix+"preview_title")).Medium(),
		fairy.Row(
			fairy.Text(T(touchPrefix+"support_grade")+": "+draft.Analysis.SupportGrade),
			fairy.Text(T(touchPrefix+"auto_apply")+": "+yesNo),
		).Gap(12).FontSize(12).Color(t.MutedForeground),
	).Gap(8)}

	componentItems := lo.Map(interactive, func(c *touchprofile.TouchComponentDraft, _ int) kit.Item {
		return kit.Item{Value: c.ComponentID, Label: fmt.Sprintf("%s (%d)", c.ComponentID, len(c.Zones))}
	})
	componentValue := ""
	if component != nil {
		componentValue = component.ComponentID
	}
	out = append(out, kit.Field(T(touchPrefix+"component"),
		kit.Select(tp.key+".component", componentValue, componentItems, func(value string) {
			tp.componentID, tp.zoneID = value, allTouchZones
		}, kit.Placeholder(T(touchPrefix+"component_placeholder")),
			kit.Disabled(len(interactive) == 0 || tp.previewLoading))))

	zoneItems := []kit.Item{{Value: allTouchZones, Label: T(touchPrefix + "all_zones")}}
	if active != nil {
		for _, zone := range active.preview.Zones {
			zoneItems = append(zoneItems, kit.Item{Value: zone.ID, Label: lo.CoalesceOrEmpty(zone.Label, zone.ID)})
		}
	}
	out = append(out, kit.Field(T(touchPrefix+"zone"),
		kit.Select(tp.key+".zone", zoneID, zoneItems, func(value string) { tp.zoneID = value },
			kit.Placeholder(T(touchPrefix+"zone_placeholder")), kit.Disabled(active == nil || tp.previewLoading))))

	if component == nil {
		out = append(out, touchCard(fairy.Text(T(touchPrefix+"no_interactive_components")).FontSize(12).
			Color(t.MutedForeground)))
	} else {
		out = append(out, s.touchComponentCard(tp, component, zoneID))
		if !tp.linked(component.ComponentID) && zoneID != allTouchZones {
			for i := range component.Zones {
				if component.Zones[i].ID == zoneID {
					out = append(out, touchAdvancedCard(tp, component, &component.Zones[i]))
				}
			}
		}
	}

	if tp.previewError != "" {
		message := T(touchPrefix+"preview_failed") + ": " + tp.previewError
		out = append(out, touchNotice(t.Destructive, fairy.Text(message).Wrap()))
	}
	if component != nil && len(component.Warnings) > 0 {
		out = append(out, touchNotice(t.Warning, fairy.Text(strings.Join(component.Warnings, " · ")).Wrap()))
	}
	if len(draft.Warnings) > 0 {
		out = append(out, touchNotice(t.Warning, fairy.Text(strings.Join(draft.Warnings, " · ")).Wrap()))
	}

	apply := T(touchPrefix + "force_create")
	switch {
	case tp.applied != nil:
		apply = T(touchPrefix + "regenerate")
	case draft.CanAutoApply:
		apply = T(touchPrefix + "create")
	}
	return append(out,
		kit.Button(T(touchPrefix+"mesh_back"), tp.backToSelect, kit.Outline, kit.Disabled(tp.applying)),
		kit.Button(apply, func() { s.touchApply(tp, !draft.CanAutoApply) }, kit.Loading(tp.applying),
			kit.Disabled(tp.applying || tp.savePending || len(interactive) == 0)),
	)
}

func (s *Shell) touchComponentCard(
	tp *touchProfile,
	component *touchprofile.TouchComponentDraft,
	zoneID string,
) *fairy.El {
	t := kit.T()
	linked := tp.linked(component.ComponentID)
	card := touchCard(fairy.Row(
		fairy.Text(component.ComponentID).Medium().Ellipsis().Grow(1).MinW(0),
		kit.Badge(T(touchPrefix+"confidence")+" "+touchPercent(component.Confidence), kit.Outline),
		fairy.Text(T(touchPrefix+"link_settings")).FontSize(12).Color(t.MutedForeground),
		kit.Switch(linked, func(on bool) { tp.setLinked(component, on) }).Label(T(touchPrefix+"link_settings")),
	).Gap(8).Align(fairy.Center).Wrap()).Gap(8)

	for index := range component.Zones {
		if linked && index > 0 {
			break
		}
		zone := &component.Zones[index]
		block := fairy.Col().Gap(12).PadXY(12, 8).Radius(6).Animate(motion.Fast)
		if linked {
			labels := lo.Map(component.Zones, func(zone touchprofile.TouchZoneSpec, _ int) string {
				return lo.CoalesceOrEmpty(zone.Label, zone.ID)
			})
			block.Child(fairy.Text(strings.Join(labels, " · ")).FontSize(12).Color(t.MutedForeground).Wrap())
		} else {
			selected := zoneID == zone.ID
			id := zone.ID
			block.Child(fairy.Col(
				fairy.Row(
					fairy.Text(lo.CoalesceOrEmpty(zone.Label, zone.ID)).Medium().Grow(1).MinW(0).Ellipsis(),
					kit.Badge(T(touchPrefix+"mode_bone"), kit.Outline),
				).Gap(8).Align(fairy.Center),
				fairy.Text(T(touchPrefix+"confidence")+" "+touchPercent(zone.Confidence)).FontSize(12).
					Color(t.MutedForeground),
			).Gap(4).Cursor(fairy.CursorPointer).Role(fairy.RoleButton).OnClick(func() {
				if selected {
					tp.zoneID = allTouchZones
				} else {
					tp.zoneID = id
				}
			}))
			if selected {
				block.Fill(t.Primary.Alpha(0.1))
			} else {
				block.HoverFill(t.Muted.Alpha(0.6))
			}
		}
		block.Child(touchZoneControls(tp, component, zone)...)
		card.Child(block.Key("touch.zone." + zone.ID))
	}
	return card
}

func touchZoneControls(
	tp *touchProfile,
	component *touchprofile.TouchComponentDraft,
	zone *touchprofile.TouchZoneSpec,
) []*fairy.El {
	t := kit.T()
	settings := zone.Settings
	targets := tp.zoneTargets(component, zone.ID)
	update := func(next touchprofile.TouchZoneSettings, refresh bool) {
		tp.touchUpdateZones(component.ComponentID, targets, next, refresh)
	}
	key := tp.key + ".zone." + zone.ID
	options := func(values ...string) []kit.Item {
		return lo.Map(values, func(value string, _ int) kit.Item {
			return kit.Item{Value: value, Label: T(touchPrefix + value)}
		})
	}

	strength := kit.Select(key+".strength", "strength_"+settings.StrengthPreset,
		options("strength_light", "strength_normal", "strength_strong"), func(value string) {
			next := settings
			next.StrengthPreset = strings.TrimPrefix(value, "strength_")
			update(next, false)
		})
	physicsValues := []string{"physics_soft", "physics_normal", "physics_firm"}
	if settings.PhysicsPreset == "custom" {
		physicsValues = append(physicsValues, "physics_custom")
	}
	physics := kit.Select(key+".physics", "physics_"+settings.PhysicsPreset, options(physicsValues...),
		func(value string) {
			if next, ok := touchprofile.WithPhysicsPreset(settings, strings.TrimPrefix(value, "physics_")); ok {
				update(next, false)
			}
		})

	busy := tp.applying || tp.rollingBack
	stepRadius := func(delta float64) func() {
		return func() {
			if next, changed := touchprofile.StepMaskRadiusScale(settings.MaskRadiusScale, delta); changed {
				updated := settings
				updated.MaskRadiusScale = next
				update(updated, true)
			}
		}
	}
	step := touchprofile.MaskRadiusScaleRange.Step
	radius := fairy.Col(
		fairy.Row(
			fairy.Text(T(touchPrefix+"mask_radius")).Medium().Grow(1),
			fairy.Text(touchPercent(settings.MaskRadiusScale)).FontSize(12).Color(t.MutedForeground),
		).Align(fairy.Center),
		fairy.Row(
			kit.Button(T(touchPrefix+"mask_radius_shrink"), stepRadius(-step), kit.Outline, kit.Small,
				kit.Icon(lucide.Minimize2),
				kit.Disabled(busy || settings.MaskRadiusScale <= touchprofile.MaskRadiusScaleRange.Min)).
				Grow(1).Basis(0),
			kit.Button(T(touchPrefix+"mask_radius_grow"), stepRadius(step), kit.Outline, kit.Small,
				kit.Icon(lucide.Maximize2),
				kit.Disabled(busy || settings.MaskRadiusScale >= touchprofile.MaskRadiusScaleRange.Max)).
				Grow(1).Basis(0),
		).Gap(8),
	).Gap(6)

	core := kit.Select(key+".core", "mask_core_attenuation_"+settings.MaskCoreAttenuation,
		options("mask_core_attenuation_off", "mask_core_attenuation_linear", "mask_core_attenuation_sqrt",
			"mask_core_attenuation_pow"),
		func(value string) {
			next := settings
			next.MaskCoreAttenuation = strings.TrimPrefix(value, "mask_core_attenuation_")
			update(next, true)
		})

	maskSlider := func(label, reset, hint, value string, current float64, r touchprofile.SettingRange,
		set func(*touchprofile.TouchZoneSettings, float64),
	) *fairy.El {
		apply := func(v float64) {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return
			}
			next := settings
			set(&next, r.Clamp(v))
			update(next, true)
		}
		col := fairy.Col(fairy.Row(
			fairy.Text(T(touchPrefix+label)).Medium().Grow(1),
			fairy.Text(value).FontSize(12).Color(t.MutedForeground),
			kit.IconButton(lucide.RotateCcw, func() { apply(1) }, kit.Ghost, kit.XS,
				kit.Tooltip(T(touchPrefix+reset))).Label(T(touchPrefix+reset)),
		).Gap(4).Align(fairy.Center)).Gap(8)
		if hint != "" {
			col.Child(fairy.Text(T(touchPrefix + hint)).FontSize(12).Color(t.MutedForeground).Wrap())
		}
		return col.Child(kit.Slider(current, r.Min, r.Max, r.Step, apply).Label(T(touchPrefix + label)))
	}

	return []*fairy.El{
		fairy.Row(
			kit.Field(T(touchPrefix+"touch_strength"), strength).Grow(1).Basis(0).MinW(0),
			kit.Field(T(touchPrefix+"physics_preset"), physics).Grow(1).Basis(0).MinW(0),
		).Gap(8),
		radius,
		kit.Field(T(touchPrefix+"mask_core_attenuation"), core,
			kit.Description(T(touchPrefix+"mask_core_attenuation_hint"))),
		maskSlider("mask_strength", "mask_strength_reset", "", touchPercent(settings.MaskStrength),
			settings.MaskStrength, touchprofile.MaskStrengthRange,
			func(s *touchprofile.TouchZoneSettings, v float64) { s.MaskStrength = v }),
		maskSlider("mask_curve", "mask_curve_reset", "mask_curve_hint", formatTouchSetting(settings.MaskCurve)+"x",
			settings.MaskCurve, touchprofile.MaskCurveRange,
			func(s *touchprofile.TouchZoneSettings, v float64) { s.MaskCurve = v }),
	}
}

func touchAdvancedCard(
	tp *touchProfile,
	component *touchprofile.TouchComponentDraft,
	zone *touchprofile.TouchZoneSpec,
) *fairy.El {
	t := kit.T()
	card := touchCard(fairy.Col(
		fairy.Text(T(touchPrefix+"advanced_title")+": "+zone.Label).Medium(),
		fairy.Text(T(touchPrefix+"advanced_description")).FontSize(12).Color(t.MutedForeground).Wrap(),
	).Gap(4)).Gap(16)
	for _, setting := range touchprofile.AdvancedSettings {
		settings := zone.Settings
		value := *setting.Field(&settings.Advanced)
		label := T(touchPrefix + "advanced_" + setting.Key)
		card.Child(fairy.Col(
			fairy.Row(
				fairy.Text(label).Medium().Grow(1),
				fairy.Text(formatTouchSetting(value)).FontSize(12).Color(t.MutedForeground),
			).Align(fairy.Center),
			kit.Slider(value, setting.Range.Min, setting.Range.Max, setting.Range.Step, func(next float64) {
				updated := zone.Settings
				updated.PhysicsPreset = "custom"
				*setting.Field(&updated.Advanced) = next
				tp.touchUpdateZones(component.ComponentID, []string{zone.ID}, updated, false)
			}).Label(label),
		).Gap(8))
	}
	return card
}

func (s *Shell) touchResult(tp *touchProfile) *fairy.El {
	t := kit.T()
	rollback := T(touchPrefix + "rollback")
	if tp.rollingBack {
		rollback = T(touchPrefix + "rolling_back")
	}
	return fairy.Col(
		fairy.Text(T(touchPrefix+"result")).Medium(),
		fairy.Text(tp.applied.output).Color(t.MutedForeground).Wrap(),
		fairy.Text(T(touchPrefix+"source")+": "+tp.applied.source).Color(t.MutedForeground).Wrap(),
		fairy.Row(
			kit.Button(T(touchPrefix+"open_folder"), func() { s.openPath(tp.applied.output) }, kit.Outline,
				kit.Disabled(tp.rollingBack)),
			kit.Button(rollback, func() { s.touchRollback(tp) }, kit.Destructive, kit.Icon(lucide.Undo2),
				kit.Loading(tp.rollingBack), kit.Disabled(tp.rollingBack)),
		).Gap(8).Wrap().MarginEdges(4, 0, 0, 0),
	).Gap(4).PadXY(12, 8).Radius(6).FontSize(12).Fill(t.Background.Alpha(0.6))
}
