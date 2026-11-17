package ui

import (
	"context"
	"maps"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

const (
	viewerMinFPS = 1
	viewerMaxFPS = 1000
)

// viewerAnimation is the playback of the transport's animation clips.
type viewerAnimation struct {
	clipID  string
	frame   int
	playing bool
	// fps replaces the clip's rate; zero keeps it.
	fps float64
	// values are what the current frame sets over the toggles.
	values map[string]any
	waves  *modelviewer.ShapeWavePlayback
	// clock numbers the running timer, so an earlier one stops when playback changes.
	clock uint64
	// applied is the time the frame last advanced at.
	applied time.Time
	// resume holds whether playback continues once a drag of the frame slider ends.
	resume *bool
	// fpsDraft is the rate being typed while its popover is open.
	fpsDraft *string
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
	if v.scene == nil {
		return nil
	}
	clips := v.scene.transport.Animations
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
	if v.anim.fps > 0 {
		return v.anim.fps
	}
	return normalizeAnimationFPS(clip.FPS)
}

// advanceAnimationFrame steps a clip of frameCount frames from current by the whole frames that fit in
// elapsed. It returns the time those frames took, and whether a clip that does not loop reached its end.
func advanceAnimationFrame(
	current int,
	elapsed time.Duration,
	fps float64,
	frameCount int,
	loop bool,
) (frame int, consumed time.Duration, complete bool) {
	if frameCount <= 1 {
		return 0, 0, true
	}
	frameDuration := time.Duration(float64(time.Second) / normalizeAnimationFPS(fps))
	steps := int(max(elapsed, 0) / frameDuration)
	if steps == 0 {
		return current, 0, false
	}
	consumed = time.Duration(steps) * frameDuration
	if loop {
		return (current + steps) % frameCount, consumed, false
	}
	frame = min(current+steps, frameCount-1)
	return frame, consumed, frame == frameCount-1
}

// startAnimation selects a clip of a newly installed scene, or of the user's choice, and plays it from its
// first frame when it has more than one.
func (v *ModelViewer) startAnimation(clipID string) {
	v.anim.clipID = clipID
	v.anim.frame, v.anim.fps, v.anim.resume, v.anim.fpsDraft = 0, 0, nil, nil
	clip := v.animationClip()
	v.anim.playing = clip != nil && len(clip.Frames) > 1
	v.syncCompute()
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
	frame, consumed, complete := advanceAnimationFrame(
		v.anim.frame, time.Since(v.anim.applied), fps, len(clip.Frames), clip.Loop,
	)
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
	if clip == nil || len(clip.Frames) < 2 || v.anim.waves != nil {
		return nil
	}
	var upcoming [][]modelviewer.ViewerMeshState
	for offset := 1; offset <= 2; offset++ {
		index := v.anim.frame + offset
		if index >= len(clip.Frames) {
			if !clip.Loop {
				break
			}
			index %= len(clip.Frames)
		}
		state := maps.Clone(v.resolved)
		maps.Copy(state, clip.Frames[index].Values)
		_, states := modelviewer.EvaluateViewerState(v.scene.transport, state, form)
		upcoming = append(upcoming, states)
	}
	return upcoming
}

func clampViewerFPS(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return viewerMinFPS
	}
	return min(max(math.Round(value), viewerMinFPS), viewerMaxFPS)
}

// animationBar is the playback row under the viewer: the clip, its rate and frame, a frame slider and the
// play button.
func (v *ModelViewer) animationBar(t kit.Theme) *fairy.El {
	clip := v.animationClip()
	if clip == nil {
		return nil
	}
	clips := v.scene.transport.Animations
	last := max(len(clip.Frames)-1, 0)
	current := clip.FrameStart
	if v.anim.frame < len(clip.Frames) {
		current = clip.Frames[v.anim.frame].Index
	}

	var title *fairy.El
	if len(clips) > 1 {
		items := make([]kit.Item, len(clips))
		for i, entry := range clips {
			items[i] = kit.Item{Value: entry.ID, Label: entry.Label}
		}
		title = kit.Select("model-viewer.animation-clip", clip.ID, items, v.startAnimation,
			kit.Small, kit.Label(T("page.tools.model_viewer.animation_clip"))).WFull()
	} else {
		title = fairy.Text(clip.Label).FontSize(14).LineHeight(20).Medium().Ellipsis()
	}
	info := fairy.Row(
		v.fpsControl(t, clip),
		fairy.Text(" · "+T("page.tools.model_viewer.animation_frame", "current", current, "end", clip.FrameEnd)).
			FontSize(12).LineHeight(16),
	).FontSize(12).Color(t.MutedForeground).Align(fairy.Center)

	seek := func(x, width float32) {
		const thumb = 12
		fraction := min(max(float64((x-thumb/2)/max(width-thumb, 1)), 0), 1)
		v.setAnimationFrame(int(math.Round(fraction * float64(last))))
	}
	slider := kit.Slider(float64(v.anim.frame), 0, float64(last), 1, func(frame float64) {
		v.setAnimationFrame(int(frame))
	}, kit.Label(T("page.tools.model_viewer.animation_frame_slider"))).Key("model-viewer.animation-frame").
		Grow(1).MinW(0).
		OnPointerDown(func(p fairy.PointerEvent) { seek(p.LocalX, p.W) }).
		// Playback pauses while the slider is dragged and continues from where it is let go.
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
		})

	icon, label := lucide.Play, T("page.tools.model_viewer.animation_play")
	if v.anim.playing {
		icon, label = lucide.Pause, T("page.tools.model_viewer.animation_pause")
	}
	return fairy.Row(
		fairy.Col(title, info).W(144).MinW(0).Shrink(0),
		fairy.Row(
			fairy.Text(strconv.Itoa(clip.FrameStart)).FontSize(12).LineHeight(16).Color(t.MutedForeground),
			slider,
			fairy.Text(strconv.Itoa(clip.FrameEnd)).FontSize(12).LineHeight(16).Color(t.MutedForeground),
		).Gap(12).Align(fairy.Center).Grow(1).MinW(0),
		kit.IconButton(icon, func() { v.setAnimationPlaying(!v.anim.playing) },
			kit.Ghost, kit.Disabled(len(clip.Frames) <= 1)).Label(label),
	).Key("model-viewer.animation").Gap(8).Align(fairy.Center).PadXY(8, 0).Shrink(0)
}

// fpsControl shows the playback rate and opens a field to change it.
func (v *ModelViewer) fpsControl(t kit.Theme, clip *modelviewer.ModelViewerAnimationClip) *fairy.El {
	fps := v.animationFPS(clip)
	text := formatViewerNumber(fps)
	trigger := fairy.Text(text + " FPS").
		FontSize(12).
		LineHeight(16).
		HoverColor(t.Foreground).
		Cursor(fairy.CursorPointer).
		Role(fairy.RoleButton).
		Label(T("page.tools.model_viewer.fps_control")).
		Focusable().
		OnClick(func() {
			if v.anim.fpsDraft != nil {
				v.anim.fpsDraft = nil
				return
			}
			v.anim.fpsDraft = &text
		})
	if v.anim.fpsDraft == nil {
		return trigger
	}

	closePopover := func() { v.anim.fpsDraft = nil }
	commit := func() {
		draft := strings.TrimSpace(*v.anim.fpsDraft)
		closePopover()
		value, err := strconv.ParseFloat(draft, 64)
		if draft == "" || err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return
		}
		v.anim.fps = clampViewerFPS(value)
		v.runAnimationClock()
	}
	panel := fairy.Col(
		fairy.Row(
			kit.TextField(*v.anim.fpsDraft, func(value string) { v.anim.fpsDraft = &value },
				kit.Label(T("page.tools.model_viewer.fps_control")), kit.OnSubmit(func(string) { commit() })).
				Key("model-viewer.fps").Grow(1).MinW(0),
			kit.Button(T("page.tools.model_viewer.fps_apply"), commit, kit.Small),
		).Gap(6).Align(fairy.Center),
		fairy.Text(T("page.tools.model_viewer.fps_default", "fps", formatViewerNumber(normalizeAnimationFPS(clip.FPS)))).
			FontSize(12).
			LineHeight(16).
			Color(t.MutedForeground),
	).Gap(8).Pad(10).W(176).Radius(t.RadiusLg()).Color(t.PopoverForeground).
		Border(1, t.Border).Shadow(0, 8, 24, 0, shadowColor(&t))
	t.Surface(panel, t.Popover)
	return trigger.Popover(panel, fairy.PopoverOptions{Align: fairy.AlignStart, Gap: 6, OnDismiss: closePopover})
}
