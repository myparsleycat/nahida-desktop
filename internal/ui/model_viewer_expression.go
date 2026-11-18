package ui

import (
	"math/rand/v2"
	"slices"
	"time"

	"github.com/myparsleycat/fairy/kit"

	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

// viewerExpression is how the viewer sets the shape keys of the original model's face: the preset the user picked,
// which overrides a motion clip's own expression, and whether the eyes blink on their own.
type viewerExpression struct {
	preset  string
	noBlink bool
	blink   viewerBlink
}

// viewerExpressionPreset sets shape keys over the part's defaults. Each key lists the names games give it, and the
// first one a part has is set. A preset that sets the eyes stops blinking.
type viewerExpressionPreset struct {
	id   string
	keys []viewerPresetKey
	eyes bool
	// closeEyes sets the part's blink keys.
	closeEyes bool
}

type viewerPresetKey struct {
	names  []string
	weight float32
}

func presetKey(weight float32, names ...string) viewerPresetKey {
	return viewerPresetKey{names: names, weight: weight}
}

// viewerExpressionPresets use the shape key names of Genshin's faces and of the Zenless faces that name theirs.
var viewerExpressionPresets = []viewerExpressionPreset{
	{id: "smile", keys: []viewerPresetKey{
		presetKey(1, "Mouth_Smile01", "Mouth_Smile1"),
		presetKey(1, "Brow_Smily_L"), presetKey(1, "Brow_Smily_R"),
		presetKey(1, "Eyebrow_Relax"), presetKey(0, "Eyebrow_Angry"), presetKey(0, "Eyebrow_↓"),
	}},
	{id: "angry", keys: []viewerPresetKey{
		presetKey(1, "Mouth_Angry01"),
		presetKey(1, "Brow_Angry_L"), presetKey(1, "Brow_Angry_R"), presetKey(1, "Eyebrow_Angry"),
		presetKey(1, "Eye_Hostility", "Eye_Angry"),
	}},
	{id: "troubled", keys: []viewerPresetKey{
		presetKey(1, "Mouth_Line01"),
		presetKey(1, "Brow_Trouble_L"), presetKey(1, "Brow_Trouble_R"), presetKey(1, "Eyebrow_困扰"),
		presetKey(1, "Eye_困扰"), presetKey(0, "Eyebrow_Angry"),
	}},
	{id: "surprised", keys: []viewerPresetKey{
		presetKey(1, "Mouth_Open01", "Mouth_Oo1"),
		presetKey(1, "Brow_Up_L"), presetKey(1, "Brow_Up_R"), presetKey(1, "Eyebrow_L↑"), presetKey(1, "Eyebrow_R↑"),
		presetKey(0, "Eyebrow_Angry"), presetKey(0, "Eyebrow_↓"),
	}},
	{id: "wink", eyes: true, keys: []viewerPresetKey{
		presetKey(1, "Eye_WinkB_L", "Eye_Wink_L"),
		presetKey(1, "Mouth_Smile01", "Mouth_Smile1"),
		presetKey(1, "Brow_Smily_L"), presetKey(1, "Brow_Smily_R"),
	}},
	{id: "closed", eyes: true, closeEyes: true},
}

func expressionPreset(id string) *viewerExpressionPreset {
	for i := range viewerExpressionPresets {
		if viewerExpressionPresets[i].id == id {
			return &viewerExpressionPresets[i]
		}
	}
	return nil
}

// set writes the preset's weights over a part's defaults in weights and reports whether it set any key that moves
// the part.
func (p *viewerExpressionPreset) set(part *viewerOriginalPart, weights []float32) bool {
	moved := false
	if p.closeEyes {
		for _, k := range part.blinkKeys {
			weights[k], moved = 1, true
		}
	}
	for _, key := range p.keys {
		for _, name := range key.names {
			if k, ok := part.targetIndex[name]; ok {
				weights[k] = key.weight
				moved = moved || key.weight != 0
				break
			}
		}
	}
	return moved
}

const (
	blinkClose = 70 * time.Millisecond
	blinkHold  = 40 * time.Millisecond
	blinkOpen  = 120 * time.Millisecond
	blinkSpan  = blinkClose + blinkHold + blinkOpen
	// blinkAgain separates the two blinks of a double blink.
	blinkAgain = 90 * time.Millisecond
	// A blink follows the last one after blinkMinGap plus up to blinkGapRange, and is a double blink at
	// blinkDoubleChance.
	blinkMinGap       = 1800 * time.Millisecond
	blinkGapRange     = 4200 * time.Millisecond
	blinkDoubleChance = 0.2
	// clipBlinkRange is how far a clip must move a blink key for its own blinking to replace the viewer's.
	clipBlinkRange = 0.3
)

// viewerBlink times the blinks: start is when the next or current one begins, and armed marks a timer that will
// redraw the viewer for it.
type viewerBlink struct {
	start  time.Time
	double bool
	armed  bool
}

// closed is how far the eyes are closed at now, and whether a blink is under way. A blink that is over schedules
// the next.
func (b *viewerBlink) closed(now time.Time) (float32, bool) {
	if b.start.IsZero() {
		b.schedule(now)
	}
	span := blinkSpan
	if b.double {
		span = 2*blinkSpan + blinkAgain
	}
	elapsed := now.Sub(b.start)
	if elapsed < 0 {
		return 0, false
	}
	if elapsed >= span {
		b.schedule(now)
		return 0, false
	}
	if b.double && elapsed >= blinkSpan+blinkAgain {
		elapsed -= blinkSpan + blinkAgain
	}
	switch {
	case elapsed < blinkClose:
		x := float64(elapsed) / float64(blinkClose)
		return float32(x * x), true
	case elapsed < blinkClose+blinkHold:
		return 1, true
	case elapsed < blinkSpan:
		x := 1 - float64(elapsed-blinkClose-blinkHold)/float64(blinkOpen)
		return float32(x * x * (3 - 2*x)), true
	}
	return 0, true
}

func (b *viewerBlink) schedule(after time.Time) {
	//nolint:gosec // Blink timing needs no cryptographic randomness.
	b.start = after.Add(blinkMinGap + time.Duration(rand.Float64()*float64(blinkGapRange)))
	//nolint:gosec // Blink timing needs no cryptographic randomness.
	b.double = rand.Float64() < blinkDoubleChance
}

// viewerOriginalMorph is what an original model keeps to read a motion's shape key weights: the motion it read
// them for and the pose they were evaluated at.
type viewerOriginalMorph struct {
	motion  *viewerMotion
	at      viewerMotionPoseAt
	valid   bool
	weights [][]float32
	driven  []bool
}

// expressOriginal sets the shape keys of the original model's shown parts for now and reports whether they keep
// changing, which keeps the viewer drawing. Idle, it arms a redraw for the next blink.
func (v *ModelViewer) expressOriginal(now time.Time) bool {
	o := (*viewerOriginal)(nil)
	if v.scene != nil {
		o = v.scene.original
	}
	if o == nil || !o.morphs {
		return false
	}

	m := v.activeMotion()
	if m != nil && v.trackClip(trackMotion) == nil {
		m = nil
	}
	o.readMotionMorphs(m)
	preset := v.shownPreset(o)

	clipBlinks := m != nil && preset == nil && o.clipBlinks(m, m.posedAt.clip)
	blinking := !v.expression.noBlink && (preset == nil || !preset.eyes) && !clipBlinks
	var closed float32
	animating, blinkable := false, false
	for _, part := range o.parts {
		if !part.shown || len(part.weights) == 0 {
			continue
		}
		want := part.want
		for k, target := range o.data.Parts[part.index].Targets {
			want[k] = target.Default
		}
		switch {
		case preset != nil:
			preset.set(part, want)
		case part.morph >= 0 && o.morph.driven[part.morph]:
			for j, k := range part.morphKeys {
				if k >= 0 {
					want[k] = o.morph.weights[part.morph][j]
				}
			}
		}
		if blinking && len(part.blinkKeys) > 0 {
			if !blinkable {
				closed, animating = v.expression.blink.closed(now)
				blinkable = true
			}
			for _, k := range part.blinkKeys {
				want[k] += (1 - want[k]) * closed
			}
		}
		if part.applied && slices.Equal(want, part.weights) {
			continue
		}
		copy(part.weights, want)
		part.applied = true
		part.morphGeometry(o.data.Parts[part.index].Targets)
	}

	if blinkable && !animating && !v.expression.blink.armed {
		b := &v.expression.blink
		b.armed = true
		post := v.poster()
		time.AfterFunc(max(b.start.Sub(now), time.Millisecond), func() {
			post(func() {
				b.armed = false
				v.revision++
			})
		})
	}
	return animating
}

// readMotionMorphs evaluates the shape key weights of the clip m shows unless they were evaluated at that pose, and
// maps the motion's shape keys to the parts' when m changed.
func (o *viewerOriginal) readMotionMorphs(m *viewerMotion) {
	if m != o.morph.motion {
		o.morph = viewerOriginalMorph{motion: m}
		for _, part := range o.parts {
			part.morph, part.morphKeys = -1, nil
		}
		if m != nil {
			o.mapMotionMorphs(m)
		}
	}
	if m == nil || len(o.morph.weights) == 0 {
		return
	}
	// The shape keys follow the clip alone, not the fade of the pose into it.
	at := m.posedAt
	at.hold = 0
	if o.morph.valid && o.morph.at == at {
		return
	}
	o.morph.at, o.morph.valid = at, true
	if err := m.motion.MorphWeights(at.clip, at.frame, at.next, at.t, o.morph.weights, o.morph.driven); err != nil {
		clear(o.morph.driven)
	}
}

func (o *viewerOriginal) mapMotionMorphs(m *viewerMotion) {
	morphs := m.motion.Morphs
	o.morph.weights = make([][]float32, len(morphs))
	o.morph.driven = make([]bool, len(morphs))
	for i, morph := range morphs {
		o.morph.weights[i] = make([]float32, len(morph.Targets))
	}
	for _, part := range o.parts {
		mesh := o.data.Parts[part.index].Mesh
		i := slices.IndexFunc(morphs, func(morph modelviewer.CharacterMotionMorph) bool { return morph.Mesh == mesh })
		if i < 0 || len(part.weights) == 0 {
			continue
		}
		part.morph = i
		part.morphKeys = make([]int, len(morphs[i].Targets))
		for j, name := range morphs[i].Targets {
			k, ok := part.targetIndex[name]
			if !ok {
				k = -1
			}
			part.morphKeys[j] = k
		}
	}
}

// clipBlinks reports whether clip moves the shown parts' blink keys itself.
func (o *viewerOriginal) clipBlinks(m *viewerMotion, clip int) bool {
	for _, part := range o.parts {
		if !part.shown || part.morph < 0 {
			continue
		}
		for j, k := range part.morphKeys {
			if k >= 0 && slices.Contains(part.blinkKeys, k) &&
				m.motion.MorphRange(clip, part.morph, j) >= clipBlinkRange {
				return true
			}
		}
	}
	return false
}

// prepareMorphs gives the parts with shape keys their weight buffers and name lookups.
func (o *viewerOriginal) prepareMorphs() {
	for _, part := range o.parts {
		source := o.data.Parts[part.index]
		if len(source.Targets) == 0 || len(part.mesh.current) != len(source.Positions)*2 {
			continue
		}
		o.morphs = true
		part.weights = make([]float32, len(source.Targets))
		part.want = make([]float32, len(source.Targets))
		part.targetIndex = make(map[string]int, len(source.Targets))
		for k, target := range source.Targets {
			if _, ok := part.targetIndex[target.Name]; !ok {
				part.targetIndex[target.Name] = k
			}
		}
		for _, name := range source.Blink {
			if k, ok := part.targetIndex[name]; ok {
				part.blinkKeys = append(part.blinkKeys, k)
			}
		}
		part.morph = -1
	}
}

// morphGeometry rebuilds the part's vertex positions under its weights.
func (part *viewerOriginalPart) morphGeometry(targets []modelviewer.CharacterOriginalTarget) {
	mesh := part.mesh
	current, positions := mesh.current, mesh.positions
	for v := 0; v*3+2 < len(positions); v++ {
		copy(current[v*6:v*6+3], positions[v*3:v*3+3])
	}
	for k, w := range part.weights {
		if w == 0 || k >= len(targets) || len(targets[k].Positions) != len(positions) {
			continue
		}
		offsets := targets[k].Positions
		for v := 0; v*3+2 < len(offsets); v++ {
			current[v*6] += w * offsets[v*3]
			current[v*6+1] += w * offsets[v*3+1]
			current[v*6+2] += w * offsets[v*3+2]
		}
	}
	mesh.dirty = true
}

// expressionMenuItems are the Model menu's entries for the original face's expression: its presets that move a
// shown part, and blinking when a shown part can blink.
func (v *ModelViewer) expressionMenuItems(o *viewerOriginal) []kit.MenuItem {
	if o == nil || !o.morphs {
		return nil
	}
	blinks := slices.ContainsFunc(o.parts, func(part *viewerOriginalPart) bool {
		return part.shown && len(part.blinkKeys) > 0
	})
	active := ""
	if preset := v.shownPreset(o); preset != nil {
		active = preset.id
	}
	var presets []kit.MenuItem
	for i := range viewerExpressionPresets {
		if preset := &viewerExpressionPresets[i]; o.presetMoves(preset) {
			presets = append(presets, v.expressionItem(preset.id, active))
		}
	}
	if len(presets) == 0 && !blinks {
		return nil
	}
	submenu := append([]kit.MenuItem{v.expressionItem("", active)}, presets...)
	if blinks {
		submenu = append(submenu, kit.MenuItem{Separator: true}, kit.MenuItem{
			Label: T("page.tools.model_viewer.menu.expression_blink"), Checkbox: true,
			Checked: !v.expression.noBlink,
			OnClick: func() {
				v.expression.noBlink = !v.expression.noBlink
				v.revision++
			},
		})
	}
	return []kit.MenuItem{{Label: T("page.tools.model_viewer.menu.expression"), Submenu: submenu}}
}

// shownPreset is the preset the user picked, or nil when it moves no shown part of o; the choice stays for faces
// that can show it.
func (v *ModelViewer) shownPreset(o *viewerOriginal) *viewerExpressionPreset {
	preset := expressionPreset(v.expression.preset)
	if preset == nil || !o.presetMoves(preset) {
		return nil
	}
	return preset
}

// presetMoves reports whether the preset moves a shown part of o.
func (o *viewerOriginal) presetMoves(preset *viewerExpressionPreset) bool {
	for _, part := range o.parts {
		if !part.shown || len(part.weights) == 0 {
			continue
		}
		if cap(o.presetScratch) < len(part.weights) {
			o.presetScratch = make([]float32, len(part.weights))
		}
		scratch := o.presetScratch[:len(part.weights)]
		if preset.set(part, scratch) {
			return true
		}
	}
	return false
}

// expressionItem is the menu entry of preset id, checked when it is active.
func (v *ModelViewer) expressionItem(id, active string) kit.MenuItem {
	label := "default"
	if id != "" {
		label = id
	}
	return kit.MenuItem{
		Label: T("page.tools.model_viewer.menu.expression_" + label), Radio: true,
		Checked: id == active,
		OnClick: func() {
			v.expression.preset = id
			v.revision++
		},
	}
}
