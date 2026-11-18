package ui

import (
	"context"
	"maps"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/kit"

	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

// viewerSpeeds are the playback rates the speed menu offers, as multiples of the clip's rate.
var viewerSpeeds = []float64{0.25, 0.5, 1, 1.5, 2}

// viewerAnimation is the playback of the transport's animation clips.
type viewerAnimation struct {
	clipID  string
	frame   int
	playing bool
	// speed multiplies the clip's rate; zero plays it at its own rate.
	speed float64
	// scrubWidth is the measured width of the frame scrubber.
	scrubWidth float32
	// barAt is where the bar was moved to, as fractions of the viewer's width and height; nil keeps it centered at
	// the bottom. barDrag is the bar's position when a drag of it started, and barDragFrom the barAt a canceled
	// drag restores.
	barAt       *[2]float32
	barDrag     [2]float32
	barDragFrom *[2]float32
	// values are what the current frame sets over the toggles.
	values map[string]any
	waves  *modelviewer.ShapeWavePlayback
	// clock numbers the running timer, so an earlier one stops when playback changes.
	clock uint64
	// applied is the time the frame last advanced at.
	applied time.Time
	// resume holds whether playback continues once a drag of the frame slider ends.
	resume *bool
	// warmSince is set while playback waits, from that time, for the frames ahead to load before its clock runs.
	warmSince *time.Time
	// scheduleKey is what the scene's schedule is built, or being built, for; scheduleRevision numbers the builds,
	// so an earlier one that finishes late is dropped. scheduleCancel stops the build in progress and is nil
	// when none is.
	scheduleKey      viewerScheduleKey
	scheduleRevision uint64
	scheduleCancel   context.CancelFunc
}

// viewerWarmLead is how much of a clip ahead of the frame shown loads before playback starts, and viewerWarmLimit
// how long playback waits for it. Past that the clock runs and frames that are not loaded in time are skipped.
const (
	viewerWarmLead  = 500 * time.Millisecond
	viewerWarmLimit = 3 * time.Second
)

// animationClip returns the clip being played, or nil for a model without animations.
func (v *ModelViewer) animationClip() *modelviewer.ModelViewerAnimationClip {
	clips := v.animationClips()
	for i := range clips {
		if clips[i].ID == v.anim.clipID {
			return &clips[i]
		}
	}
	if len(clips) > 0 {
		return &clips[0]
	}
	return nil
}

func normalizeAnimationFPS(fps float64) float64 {
	if math.IsNaN(fps) || math.IsInf(fps, 0) || fps <= 0 {
		return 1
	}
	return fps
}

// animationFPS is the rate the clip plays at.
func (v *ModelViewer) animationFPS(clip *modelviewer.ModelViewerAnimationClip) float64 {
	return normalizeAnimationFPS(clip.FPS) * v.animationSpeed()
}

func (v *ModelViewer) animationSpeed() float64 {
	if v.anim.speed > 0 {
		return v.anim.speed
	}
	return 1
}

func (v *ModelViewer) setAnimationSpeed(speed float64) {
	v.anim.speed = speed
	v.runAnimationClock()
}

// advanceAnimationFrame steps a clip from current by the whole frames that fit in elapsed. It returns the time
// those frames took, and whether a clip that does not loop reached its end.
func advanceAnimationFrame(
	current int,
	elapsed time.Duration,
	fps float64,
	clip *modelviewer.ModelViewerAnimationClip,
) (frame int, consumed time.Duration, complete bool) {
	frameCount := len(clip.Frames)
	if frameCount <= 1 {
		return 0, 0, true
	}
	frameDuration := time.Duration(float64(time.Second) / normalizeAnimationFPS(fps))
	steps := int(max(elapsed, 0) / frameDuration)
	if steps == 0 {
		return current, 0, false
	}
	consumed = time.Duration(steps) * frameDuration
	if clip.Loop {
		return loopAnimationFrame(clip, current+steps), consumed, false
	}
	frame = min(current+steps, frameCount-1)
	return frame, consumed, frame == frameCount-1
}

// loopAnimationFrame wraps a frame index past the end of a looping clip back into its loop section.
func loopAnimationFrame(clip *modelviewer.ModelViewerAnimationClip, index int) int {
	frameCount := len(clip.Frames)
	if index < frameCount {
		return index
	}
	start := min(max(clip.LoopStart, 0), frameCount-1)
	return start + (index-start)%(frameCount-start)
}

// startAnimation selects a clip of a newly installed scene, or of the user's choice, and plays it from its
// first frame when it has more than one.
func (v *ModelViewer) startAnimation(clipID string) {
	v.anim.clipID = clipID
	v.anim.frame, v.anim.resume = 0, nil
	clip := v.animationClip()
	v.anim.playing = clip != nil && len(clip.Frames) > 1
	v.syncCompute()
	v.syncMotion()
	v.syncSchedule()
	v.setAnimationFrame(0)
	v.runAnimationClock()
}

func (v *ModelViewer) setAnimationPlaying(playing bool) {
	clip := v.animationClip()
	v.anim.playing = playing && clip != nil && len(clip.Frames) > 1
	if !v.anim.playing && v.untrimmed > 0 {
		v.trimDevice()
	}
	v.runAnimationClock()
}

// runAnimationClock starts the timer of the current playback and stops any earlier one.
func (v *ModelViewer) runAnimationClock() {
	v.anim.clock++
	v.anim.warmSince = nil
	clip := v.animationClip()
	if !v.anim.playing || clip == nil {
		return
	}
	now := time.Now()
	v.anim.applied, v.anim.warmSince = now, &now
	v.scheduleAnimationTick(v.anim.clock, time.Duration(float64(time.Second)/v.animationFPS(clip)))
}

// animationWarm reports whether the frame shown and the frames of the next viewerWarmLead are loaded, so playback
// can run in real time from here.
func (v *ModelViewer) animationWarm(clip *modelviewer.ModelViewerAnimationClip) bool {
	scene := v.scene
	if scene == nil {
		return true
	}
	if scene.waiting {
		return false
	}
	schedule := scene.schedule
	if schedule == nil {
		// Without a schedule the frames ahead are unknown; while one builds, wait for it.
		return v.anim.scheduleCancel == nil
	}
	lead := min(len(schedule.frames)-1, int(math.Ceil(viewerWarmLead.Seconds()*v.animationFPS(clip))))
	for step := 1; step <= lead; step++ {
		frame := scene.frame + step
		if frame >= len(schedule.frames) {
			if !schedule.loop {
				break
			}
			frame -= len(schedule.frames)
		}
		for _, item := range schedule.frames[frame] {
			mesh := scene.meshes[item.mesh]
			if item.variant < 0 {
				if mesh.unloaded && !mesh.failed {
					return false
				}
			} else if mesh.variants[item.variant] == nil && !mesh.unreadable[item.variant] &&
				mesh.geometryKey != variantGeometryKey(item.variant) {
				return false
			}
		}
	}
	return true
}

func (v *ModelViewer) scheduleAnimationTick(clock uint64, delay time.Duration) {
	post := v.poster()
	time.AfterFunc(delay, func() { post(func() { v.animationTick(clock) }) })
}

func (v *ModelViewer) animationTick(clock uint64) {
	clip := v.animationClip()
	if v.closed || clock != v.anim.clock || !v.anim.playing || clip == nil {
		return
	}
	fps := v.animationFPS(clip)
	// Playback starts once the frames ahead are loaded, then keeps real time: a frame whose meshes or position
	// variants are not loaded when its time comes is skipped, and the frame before stays on screen.
	if since := v.anim.warmSince; since != nil {
		if !v.animationWarm(clip) && time.Since(*since) < viewerWarmLimit {
			v.anim.applied = time.Now()
			v.scheduleAnimationTick(clock, time.Duration(float64(time.Second)/fps))
			return
		}
		v.anim.warmSince, v.anim.applied = nil, time.Now()
	}
	frame, consumed, complete := advanceAnimationFrame(v.anim.frame, time.Since(v.anim.applied), fps, clip)
	v.anim.applied = v.anim.applied.Add(consumed)
	if frame != v.anim.frame {
		v.setAnimationFrame(frame)
	}
	if complete {
		v.anim.playing = false
		return
	}
	next := time.Duration(float64(time.Second)/fps) - time.Since(v.anim.applied)
	v.scheduleAnimationTick(clock, max(next, time.Millisecond))
}

// setAnimationFrame shows frame index of the clip: its values, with the phases of the wave shape targets
// advanced to its time, go over the toggles, and its compute deformer computes the frame.
func (v *ModelViewer) setAnimationFrame(index int) {
	clip := v.animationClip()
	if clip == nil || len(clip.Frames) == 0 {
		v.anim.frame = 0
		if len(v.anim.values) > 0 {
			v.anim.values = nil
			v.present()
		}
		return
	}
	v.anim.frame = min(max(index, 0), len(clip.Frames)-1)
	if v.scene != nil {
		v.scene.frame = v.anim.frame
	}
	v.requestCompute(clip, v.anim.frame)
	v.requestMotion(clip, v.anim.frame)
	frame := clip.Frames[v.anim.frame]
	values := frame.Values
	if v.anim.waves != nil {
		state := maps.Clone(v.resolved)
		if state == nil {
			state = map[string]any{}
		}
		maps.Copy(state, frame.Values)
		values = maps.Clone(frame.Values)
		if values == nil {
			values = map[string]any{}
		}
		maps.Copy(
			values,
			v.anim.waves.Advance(clip.ID, frame.Time, float64(len(clip.Frames))/v.animationFPS(clip), state),
		)
	}
	// A wave clip plays even while every wave is off, which leaves nothing to redraw.
	if reflect.DeepEqual(values, v.anim.values) {
		return
	}
	v.anim.values = values
	v.present()
}

// upcomingAnimationStates returns the mesh states of the next two frames, whose position variants load ahead
// of them, paused or playing. A wave phase depends on the frames before it, so a clip with waves has none.
func (v *ModelViewer) upcomingAnimationStates(form string) [][]modelviewer.ViewerMeshState {
	clip := v.animationClip()
	if clip == nil || len(clip.Frames) < 2 || v.anim.waves != nil || isMotionClip(clip) {
		return nil
	}
	var upcoming [][]modelviewer.ViewerMeshState
	for offset := 1; offset <= 2; offset++ {
		index := v.anim.frame + offset
		if index >= len(clip.Frames) {
			if !clip.Loop {
				break
			}
			index = loopAnimationFrame(clip, index)
		}
		state := maps.Clone(v.resolved)
		maps.Copy(state, clip.Frames[index].Values)
		_, states := modelviewer.EvaluateViewerState(v.scene.transport, state, form)
		upcoming = append(upcoming, states)
	}
	return upcoming
}

// viewerAnimationBarHeight and viewerAnimationBarMaxWidth size the playback controller, viewerAnimationBarMargin
// is its default distance from the viewer's edges, and viewerScrubInset how far the scrubber's first and last frames
// sit from its ends.
const (
	viewerAnimationBarHeight   = 36
	viewerAnimationBarMaxWidth = 560
	viewerAnimationBarMargin   = 12
	viewerScrubInset           = 6
	// viewerAnimationBarKeyStep is how far an arrow key moves the bar.
	viewerAnimationBarKeyStep = 16
	// A bar narrower than viewerAnimationBarFullWidth leaves out the jumps to the clip's ends, one narrower than
	// viewerAnimationBarCompactWidth the time as well, and one narrower than viewerAnimationBarMinWidth is not shown.
	viewerAnimationBarFullWidth    = 360
	viewerAnimationBarCompactWidth = 300
	viewerAnimationBarMinWidth     = 232
)

// animationBarPlace is where the animation bar sits in the viewer's measured size, and false while the viewer is
// too narrow for it.
func (v *ModelViewer) animationBarPlace() (left, top, width float32, ok bool) {
	width = min(v.width-2*viewerAnimationBarMargin, viewerAnimationBarMaxWidth)
	if width < viewerAnimationBarMinWidth {
		return 0, 0, 0, false
	}
	if at := v.anim.barAt; at != nil {
		left, top = v.clampAnimationBar(at[0]*v.width, at[1]*v.height, width)
		return left, top, width, true
	}
	return (v.width - width) / 2, v.height - viewerAnimationBarMargin - viewerAnimationBarHeight, width, true
}

// clampAnimationBar keeps a bar of width at left, top inside the viewer.
func (v *ModelViewer) clampAnimationBar(left, top, width float32) (float32, float32) {
	return min(max(left, 0), max(v.width-width, 0)), min(max(top, 0), max(v.height-viewerAnimationBarHeight, 0))
}

// moveAnimationBar places a bar of width at left, top, kept inside the viewer.
func (v *ModelViewer) moveAnimationBar(left, top, width float32) {
	left, top = v.clampAnimationBar(left, top, width)
	v.anim.barAt = &[2]float32{left / max(v.width, 1), top / max(v.height, 1)}
}

// animationBar is the playback controller floating over the viewer: a grip that moves it, the play button, the
// clip, a frame scrubber between jumps to the clip's ends, the time and the speed. It is centered at the bottom
// until moved.
func (v *ModelViewer) animationBar(t kit.Theme) *fairy.El {
	clip := v.animationClip()
	left, top, width, ok := v.animationBarPlace()
	if clip == nil || !ok {
		return nil
	}
	move := T("page.tools.model_viewer.animation_move")
	grip := fairy.Box(fairy.Icon(lucide.GripVertical).Size(14, 14)).Size(16, 28).Center().Shrink(0).Radius(4).
		Color(t.MutedForeground).HoverColor(t.Foreground).Cursor(fairy.CursorMove).Label(move).Tooltip(move).
		Focusable().FocusRing(3, t.Ring.Alpha(0.5)).
		OnDoubleClick(func() { v.anim.barAt = nil }).
		OnDrag(func(d fairy.DragEvent) {
			if d.Phase == fairy.DragStart {
				v.anim.barDrag, v.anim.barDragFrom = [2]float32{left, top}, v.anim.barAt
			}
			if d.Canceled {
				v.anim.barAt = v.anim.barDragFrom
				return
			}
			v.moveAnimationBar(v.anim.barDrag[0]+d.DX, v.anim.barDrag[1]+d.DY, width)
		}).
		OnKey(func(k keyboard.KeyEvent) bool {
			if k.Mods != 0 {
				return false
			}
			const step = viewerAnimationBarKeyStep
			switch k.Key {
			case keyboard.KeyLeft:
				v.moveAnimationBar(left-step, top, width)
			case keyboard.KeyRight:
				v.moveAnimationBar(left+step, top, width)
			case keyboard.KeyUp:
				v.moveAnimationBar(left, top-step, width)
			case keyboard.KeyDown:
				v.moveAnimationBar(left, top+step, width)
			case keyboard.KeyHome:
				v.anim.barAt = nil
			default:
				return false
			}
			return true
		})

	last := max(len(clip.Frames)-1, 0)
	still := len(clip.Frames) <= 1

	icon, label := lucide.Play, T("page.tools.model_viewer.animation_play")
	if v.anim.playing {
		icon, label = lucide.Pause, T("page.tools.model_viewer.animation_pause")
	}
	full := width >= viewerAnimationBarFullWidth
	pill := fairy.Row(
		grip,
		kit.IconButton(icon, func() { v.setAnimationPlaying(!v.anim.playing) }, kit.Ghost, kit.Small,
			kit.Disabled(still)).Label(label),
		v.animationClipMenu(t, clip),
	).Key("model-viewer.animation").Gap(2).Align(fairy.Center).H(viewerAnimationBarHeight).PadXY(4, 0).
		Radius(viewerAnimationBarHeight/2).Color(t.PopoverForeground).Border(1, t.Border).
		Shadow(0, 8, 24, 0, shadowColor(&t)).Absolute().Left(left).Top(top).W(width)
	if full {
		start := T("page.tools.model_viewer.animation_start")
		pill.Child(kit.IconButton(lucide.SkipBack, func() { v.setAnimationFrame(0) }, kit.Ghost, kit.Small,
			kit.Disabled(still), kit.Tooltip(start)).Label(start))
	}
	pill.Child(v.animationScrubber(t, clip, last))
	if full {
		end := T("page.tools.model_viewer.animation_end")
		pill.Child(kit.IconButton(lucide.SkipForward, func() { v.setAnimationFrame(last) }, kit.Ghost, kit.Small,
			kit.Disabled(still), kit.Tooltip(end)).Label(end))
	}
	if width >= viewerAnimationBarCompactWidth {
		pill.Child(v.animationTime(t, clip))
	}
	pill.Child(v.animationSpeedMenu(t))
	return glass(pill, opacity(t.Popover, t.SurfaceAlpha), t.Popover, t.SurfaceBlur)
}

// animationClipMenu names the clip shown and, when there are others, opens a menu to pick one.
func (v *ModelViewer) animationClipMenu(t kit.Theme, clip *modelviewer.ModelViewerAnimationClip) *fairy.El {
	icon := lucide.Clapperboard
	if strings.HasPrefix(clip.ID, motionClipPrefix) {
		icon = lucide.PersonStanding
	}
	trigger := fairy.Row(
		fairy.Icon(icon).Size(14, 14).Shrink(0),
		fairy.Text(clip.Label).FontSize(12).LineHeight(16).Medium().Ellipsis().MinW(0),
	).Gap(5).Align(fairy.Center).H(28).PadXY(8, 0).MinW(0).MaxW(160).Radius(14)
	clips := v.animationClips()
	if len(clips) <= 1 {
		return trigger
	}

	trigger.Child(fairy.Icon(lucide.ChevronUp).Size(12, 12).Shrink(0).Color(t.MutedForeground)).
		HoverFill(hoverFill(&t)).Cursor(fairy.CursorPointer).Role(fairy.RoleButton).Focusable().
		FocusRing(3, t.Ring.Alpha(0.5)).
		Label(T("page.tools.model_viewer.animation_clip")).Key("model-viewer.animation-clip-trigger")
	items := func() []kit.MenuItem {
		var mod, motion []kit.MenuItem
		for _, entry := range clips {
			item := kit.MenuItem{
				Label: entry.Label, Radio: true, Checked: entry.ID == clip.ID,
				OnClick: func() { v.startAnimation(entry.ID) },
			}
			if strings.HasPrefix(entry.ID, motionClipPrefix) {
				motion = append(motion, item)
			} else {
				mod = append(mod, item)
			}
		}
		if len(mod) == 0 || len(motion) == 0 {
			return slices.Concat(mod, motion)
		}
		return slices.Concat(
			[]kit.MenuItem{{Heading: true, Label: T("page.tools.model_viewer.animation_mod_clips")}},
			mod,
			[]kit.MenuItem{
				{Separator: true},
				{Heading: true, Label: T("page.tools.model_viewer.menu.character_motion")},
			},
			motion,
		)
	}
	return kit.DropdownMenuFunc("model-viewer.animation-clip", trigger, items, kit.MaxHeight(320))
}

// animationScrubber shows the clip's frames as ticks with the frame shown as a playhead. A press or drag seeks,
// and playback pauses while it is dragged and continues from where it is let go.
func (v *ModelViewer) animationScrubber(t kit.Theme, clip *modelviewer.ModelViewerAnimationClip, last int) *fairy.El {
	const height = 22
	usable := v.anim.scrubWidth - 2*viewerScrubInset
	at := func(frame int) float32 {
		if last == 0 || usable <= 0 {
			return viewerScrubInset
		}
		return viewerScrubInset + float32(frame)/float32(last)*usable
	}
	seek := func(x, width float32) {
		fraction := min(max(float64((x-viewerScrubInset)/max(width-2*viewerScrubInset, 1)), 0), 1)
		v.setAnimationFrame(int(math.Round(fraction * float64(last))))
	}

	scrub := fairy.Box().Key("model-viewer.animation-frame").Grow(1).MinW(80).H(height).
		OnLayout(func(w, _ float32) bool {
			if v.anim.scrubWidth == w {
				return false
			}
			v.anim.scrubWidth = w
			return true
		})
	if usable > 0 && last > 0 {
		type tickVersion struct {
			last   int
			usable float32
			color  fairy.Color
		}
		color := t.MutedForeground.Alpha(0.5)
		scrub.Child(fairy.Memo("model-viewer.animation-ticks", tickVersion{last, usable, color}, func() *fairy.El {
			ticks := fairy.Box().Absolute().Inset(0).PassThrough()
			step := animationTickStep(last, usable)
			for i := 0; i*step <= last; i++ {
				size := float32(5)
				if i%5 == 0 {
					size = 10
				}
				ticks.Child(fairy.Box().Absolute().Left(at(i*step)-0.5).Top((height-size)/2).Size(1, size).
					Fill(color).PassThrough())
			}
			return ticks
		}))
	}
	scrub.Child(fairy.Box().Absolute().Left(at(v.anim.frame)-1.5).Top(4).Size(3, height-8).Radius(1.5).
		Fill(t.Primary).FocusRing(3, t.Ring.Alpha(0.5)).FocusFromParent().PassThrough())

	current := clip.FrameStart
	if v.anim.frame < len(clip.Frames) {
		current = clip.Frames[v.anim.frame].Index
	}
	return scrub.Role(fairy.RoleSlider).
		RangeValue(float64(v.anim.frame), 0, float64(last), 1, func(frame float64) {
			v.setAnimationFrame(int(math.Round(frame)))
		}).
		Label(T("page.tools.model_viewer.animation_frame_slider")).
		Tooltip(T("page.tools.model_viewer.animation_frame", "current", current, "end", clip.FrameEnd)).
		Cursor(fairy.CursorPointer).
		Focusable().
		OnPointerDown(func(p fairy.PointerEvent) { seek(p.LocalX, p.W) }).
		OnDrag(func(d fairy.DragEvent) {
			if d.Phase == fairy.DragStart && v.anim.resume == nil {
				playing := v.anim.playing
				v.anim.resume = &playing
				v.setAnimationPlaying(false)
			}
			seek(d.LocalX, d.W)
			if d.Phase == fairy.DragEnd && v.anim.resume != nil {
				resume := *v.anim.resume
				v.anim.resume = nil
				v.setAnimationPlaying(resume)
			}
		}).
		OnKey(func(k keyboard.KeyEvent) bool {
			if k.Mods != 0 {
				return false
			}
			switch k.Key {
			case keyboard.KeyLeft, keyboard.KeyDown:
				v.setAnimationFrame(v.anim.frame - 1)
			case keyboard.KeyRight, keyboard.KeyUp:
				v.setAnimationFrame(v.anim.frame + 1)
			case keyboard.KeyHome:
				v.setAnimationFrame(0)
			case keyboard.KeyEnd:
				v.setAnimationFrame(last)
			default:
				return false
			}
			return true
		})
}

// animationTickStep is the number of frames between the scrubber's ticks: the smallest of 1, 2, 5, 10, 20, 50 and
// so on that keeps ticks at least 6 pixels apart over width.
func animationTickStep(last int, width float32) int {
	for scale := 1; ; scale *= 10 {
		for _, step := range []int{scale, 2 * scale, 5 * scale} {
			if step >= last || float32(step)/float32(last)*width >= 6 {
				return step
			}
		}
	}
}

// animationTime shows the time of the frame shown and the clip's length at the clip's own rate.
func (v *ModelViewer) animationTime(t kit.Theme, clip *modelviewer.ModelViewerAnimationClip) *fairy.El {
	fps := normalizeAnimationFPS(clip.FPS)
	seconds := func(frames int) string { return strconv.FormatFloat(float64(frames)/fps, 'f', 2, 64) }
	total := seconds(len(clip.Frames))
	timeText := func(value string) *fairy.El {
		return fairy.Text(T("page.tools.model_viewer.animation_time", "seconds", value)).
			FontSize(12).LineHeight(14).Medium()
	}
	// The hidden text widest for the clip's length holds the column's width, so the scrubber beside it keeps its
	// size while the time gains digits.
	widest := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return '0'
		}
		return r
	}, total)
	return fairy.Col(
		fairy.Box(
			timeText(widest).Opacity(0).Role(fairy.RoleNone).PassThrough(),
			timeText(seconds(v.anim.frame)).Color(t.Primary).Absolute().Right(0).Top(0),
		),
		fairy.Text(T("page.tools.model_viewer.animation_duration", "total", total)).
			FontSize(10).LineHeight(12).Color(t.MutedForeground),
	).Align(fairy.End).MinW(48).Shrink(0).PadXY(4, 0)
}

// animationSpeedMenu shows the playback speed and opens a menu to change it.
func (v *ModelViewer) animationSpeedMenu(t kit.Theme) *fairy.El {
	speed := v.animationSpeed()
	trigger := fairy.Box(fairy.Text(formatViewerNumber(speed)+"×").FontSize(12).LineHeight(16).Medium()).
		H(28).MinW(36).PadXY(8, 0).Center().Radius(14).Shrink(0).
		HoverFill(hoverFill(&t)).Cursor(fairy.CursorPointer).Role(fairy.RoleButton).Focusable().
		FocusRing(3, t.Ring.Alpha(0.5)).
		Label(T("page.tools.model_viewer.animation_speed")).Key("model-viewer.animation-speed-trigger")
	items := make([]kit.MenuItem, len(viewerSpeeds))
	for i, option := range viewerSpeeds {
		items[i] = kit.MenuItem{
			Label: formatViewerNumber(option) + "×", Radio: true, Checked: option == speed,
			OnClick: func() { v.setAnimationSpeed(option) },
		}
	}
	return kit.DropdownMenu("model-viewer.animation-speed", trigger, items, kit.AlignEnd)
}
