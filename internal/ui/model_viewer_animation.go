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

// viewerTrack names one of the viewer's independent playbacks: the mod's animation clips, or the game motions of the
// mod's character, which pose the geometry the mod's clip shows.
type viewerTrack int

const (
	trackMod viewerTrack = iota
	trackMotion
)

func (t viewerTrack) key() string {
	if t == trackMotion {
		return "motion"
	}
	return "mod"
}

// viewerPlayback is the playback of one track's clips.
type viewerPlayback struct {
	clipID  string
	frame   int
	playing bool
	// speed multiplies the clip's rate; zero plays it at its own rate.
	speed float64
	// stepped shows whole frames while playing, instead of drawing between them.
	stepped bool
	// clock numbers the running timer, so an earlier one stops when playback changes.
	clock uint64
	// applied is the time the frame last advanced at.
	applied time.Time
	// resume holds whether playback continues once a drag of the frame slider ends.
	resume *bool
	// scrubWidth is the measured width of the frame scrubber.
	scrubWidth float32
}

func (p *viewerPlayback) rate() float64 {
	if p.speed > 0 {
		return p.speed
	}
	return 1
}

// fps is the rate the clip plays at.
func (p *viewerPlayback) fps(clip *modelviewer.ModelViewerAnimationClip) float64 {
	return normalizeAnimationFPS(clip.FPS) * p.rate()
}

// phase is the share of the frame shown that has played at now, from 0 to 1.
func (p *viewerPlayback) phase(clip *modelviewer.ModelViewerAnimationClip, now time.Time) float64 {
	return min(max(now.Sub(p.applied).Seconds()*p.fps(clip), 0), 1)
}

// viewerAnimation is the playback of the transport's animation clips, and where the playback controller sits.
type viewerAnimation struct {
	viewerPlayback
	// barAt is where the bar was moved to, as fractions of the viewer's width and height; nil keeps it centered at
	// the bottom. barDrag is the bar's position when a drag of it started, and barDragFrom the barAt a canceled
	// drag restores.
	barAt       *[2]float32
	barDrag     [2]float32
	barDragFrom *[2]float32
	// values are what the current frame sets over the toggles.
	values map[string]any
	waves  *modelviewer.ShapeWavePlayback
	// warmSince is set while playback waits, from that time, for the frames ahead to load before its clock runs.
	warmSince *time.Time
	// nextStates are the mesh states of the frame after the one shown that the meshes move towards, or nil.
	nextStates []modelviewer.ViewerMeshState
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

// playback returns the playback of track, or nil for a motion track while no motion is loaded.
func (v *ModelViewer) playback(track viewerTrack) *viewerPlayback {
	if track == trackMotion {
		if m := v.activeMotion(); m != nil {
			return &m.play
		}
		return nil
	}
	return &v.anim.viewerPlayback
}

func (v *ModelViewer) trackClips(track viewerTrack) []modelviewer.ModelViewerAnimationClip {
	if track == trackMotion {
		if m := v.activeMotion(); m != nil {
			return m.clips
		}
		return nil
	}
	if v.scene == nil {
		return nil
	}
	return v.scene.transport.Animations
}

// trackClip returns the clip track plays, or nil for a track without clips.
func (v *ModelViewer) trackClip(track viewerTrack) *modelviewer.ModelViewerAnimationClip {
	p := v.playback(track)
	if p == nil {
		return nil
	}
	clips := v.trackClips(track)
	for i := range clips {
		if clips[i].ID == p.clipID {
			return &clips[i]
		}
	}
	if len(clips) > 0 {
		return &clips[0]
	}
	return nil
}

// animationClip returns the mod's clip being played, or nil for a model without animations.
func (v *ModelViewer) animationClip() *modelviewer.ModelViewerAnimationClip {
	return v.trackClip(trackMod)
}

func normalizeAnimationFPS(fps float64) float64 {
	if math.IsNaN(fps) || math.IsInf(fps, 0) || fps <= 0 {
		return 1
	}
	return fps
}

// animationFPS is the rate the mod's clip plays at.
func (v *ModelViewer) animationFPS(clip *modelviewer.ModelViewerAnimationClip) float64 {
	return v.anim.fps(clip)
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

// startTrack selects a clip of track and plays it from its first frame when it has more than one.
func (v *ModelViewer) startTrack(track viewerTrack, clipID string) {
	if track == trackMotion {
		v.startMotion(clipID)
		return
	}
	v.startAnimation(clipID)
}

// startAnimation selects a mod clip of a newly installed scene, or of the user's choice, and plays it from its
// first frame when it has more than one.
func (v *ModelViewer) startAnimation(clipID string) {
	v.anim.clipID = clipID
	v.anim.frame, v.anim.resume = 0, nil
	clip := v.animationClip()
	v.anim.playing = clip != nil && len(clip.Frames) > 1
	v.syncCompute()
	v.syncSchedule()
	v.setAnimationFrame(0)
	v.runTrackClock(trackMod)
}

func (v *ModelViewer) setTrackFrame(track viewerTrack, index int) {
	if track == trackMotion {
		v.setMotionFrame(index)
		return
	}
	v.setAnimationFrame(index)
}

func (v *ModelViewer) setTrackPlaying(track viewerTrack, playing bool) {
	p := v.playback(track)
	if p == nil {
		return
	}
	clip := v.trackClip(track)
	p.playing = playing && clip != nil && len(clip.Frames) > 1
	if track == trackMod && !p.playing && v.untrimmed > 0 {
		v.trimDevice()
	}
	v.runTrackClock(track)
	if track == trackMod {
		v.syncNextFrame()
	}
}

func (v *ModelViewer) setTrackStepped(track viewerTrack, stepped bool) {
	if p := v.playback(track); p != nil {
		p.stepped = stepped
		if track == trackMod {
			v.syncNextFrame()
		}
		v.revision++
	}
}

// nextFrame is the frame track plays after the one shown, and false when playback stops there.
func (v *ModelViewer) nextFrame(track viewerTrack) (int, bool) {
	p, clip := v.playback(track), v.trackClip(track)
	if p == nil || clip == nil || len(clip.Frames) < 2 {
		return 0, false
	}
	if p.frame+1 < len(clip.Frames) {
		return p.frame + 1, true
	}
	if !clip.Loop {
		return 0, false
	}
	return loopAnimationFrame(clip, p.frame+1), true
}

// interpolates reports whether track draws between its frames: while it plays and has a frame after the one shown.
// A tool picks and edits the geometry the viewer shows, so a viewer with one shows whole frames.
func (v *ModelViewer) interpolates(track viewerTrack) bool {
	p := v.playback(track)
	if p == nil || !p.playing || p.stepped || v.tool != nil {
		return false
	}
	_, ok := v.nextFrame(track)
	return ok
}

// interpolation is the share of the way from the frame track shows to the next that playback has reached at now,
// or 0 while it shows whole frames. The mod's playback shows whole frames while it waits for frames to load.
func (v *ModelViewer) interpolation(track viewerTrack, now time.Time) float64 {
	if !v.interpolates(track) {
		return 0
	}
	if track == trackMod && !v.modClockRunning() {
		return 0
	}
	return v.playback(track).phase(v.trackClip(track), now)
}

// interpolating reports a playback that draws between frames, which redraws the viewer on every frame. The mod's
// playback does only while its clock runs and a mesh has a next frame's geometry to move towards; the motion's also
// while it blends into a newly selected clip.
func (v *ModelViewer) interpolating() bool {
	if v.interpolates(trackMotion) || v.activeMotion().fading() {
		return true
	}
	if !v.interpolates(trackMod) || !v.modClockRunning() {
		return false
	}
	return slices.ContainsFunc(v.scene.meshes, func(m *viewerMesh) bool { return m.next != nil })
}

// modClockRunning reports whether the mod's playback runs in real time, neither waiting for frames ahead to load
// nor for the frame shown.
func (v *ModelViewer) modClockRunning() bool {
	return v.anim.warmSince == nil && v.scene != nil && !v.scene.waiting
}

// syncNextFrame gives the meshes the geometry of the mod clip's next frame to move towards, or none while the clip
// shows whole frames.
func (v *ModelViewer) syncNextFrame() {
	v.anim.nextStates = nil
	if v.scene != nil {
		v.anim.nextStates = v.nextAnimationStates()
		v.scene.applyNext(v.anim.nextStates)
	}
}

// retryNextFrame gives the meshes the next frame's geometry again once position variant index of mesh i, which the
// next frame shows, is loaded.
func (v *ModelViewer) retryNextFrame(i, index int) {
	states := v.anim.nextStates
	if v.scene != nil && i < len(states) && states[i].Visible && states[i].PositionVariant == index {
		v.scene.applyNext(states)
		v.revision++
	}
}

// nextAnimationStates returns the mesh states of the mod clip's next frame, or nil while it shows whole frames. The
// phases of the wave shape targets are those the next frame advances them to.
func (v *ModelViewer) nextAnimationStates() []modelviewer.ViewerMeshState {
	clip := v.animationClip()
	next, ok := v.nextFrame(trackMod)
	if clip == nil || !ok || !v.interpolates(trackMod) {
		return nil
	}
	frame := clip.Frames[next]
	state := v.animationFrameState(clip, next)
	if v.anim.waves != nil {
		duration := float64(len(clip.Frames)) / v.animationFPS(clip)
		maps.Copy(state, v.anim.waves.Peek(clip.ID, frame.Time, duration, state))
	}
	_, states := modelviewer.EvaluateViewerState(v.scene.transport, state, v.shownForm())
	return states
}

func (v *ModelViewer) setAnimationPlaying(playing bool) {
	v.setTrackPlaying(trackMod, playing)
}

// setTrackSpeed changes track's speed. Playing, it keeps the share of the frame shown already played, so a clip drawn
// between frames goes on from where it is, and a mod clip waiting for frames to load keeps waiting. A motion fading
// into its clip keeps the share of the fade played.
func (v *ModelViewer) setTrackSpeed(track viewerTrack, speed float64) {
	p, clip := v.playback(track), v.trackClip(track)
	if p == nil {
		return
	}
	now := time.Now()
	var phase float64
	if clip != nil {
		phase = p.phase(clip, now)
	}
	var fading *viewerMotion
	var faded float64
	if m := v.activeMotion(); track == trackMotion && m.fading() {
		fading, faded = m, m.fadeProgress(now)
	}
	p.speed = speed
	p.clock++
	if fading != nil {
		fading.fadeStart = now.Add(-time.Duration(faded * float64(viewerMotionFade) / p.rate()))
	}
	if !p.playing || clip == nil {
		return
	}
	frame := time.Duration(float64(time.Second) / p.fps(clip))
	played := time.Duration(phase * float64(frame))
	p.applied = now.Add(-played)
	v.scheduleTrackTick(track, p, p.clock, max(frame-played, time.Millisecond))
}

// runTrackClock starts the timer of track's playback and stops any earlier one.
func (v *ModelViewer) runTrackClock(track viewerTrack) {
	p := v.playback(track)
	if p == nil {
		return
	}
	p.clock++
	if track == trackMod {
		v.anim.warmSince = nil
	}
	clip := v.trackClip(track)
	if !p.playing || clip == nil {
		return
	}
	now := time.Now()
	p.applied = now
	if track == trackMod {
		v.anim.warmSince = &now
	}
	v.scheduleTrackTick(track, p, p.clock, time.Duration(float64(time.Second)/p.fps(clip)))
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

func (v *ModelViewer) scheduleTrackTick(track viewerTrack, p *viewerPlayback, clock uint64, delay time.Duration) {
	post := v.poster()
	time.AfterFunc(delay, func() { post(func() { v.trackTick(track, p, clock) }) })
}

// trackTick advances the playback p of track, unless p was replaced or its clock restarted since the tick was
// scheduled.
func (v *ModelViewer) trackTick(track viewerTrack, p *viewerPlayback, clock uint64) {
	clip := v.trackClip(track)
	if v.closed || v.playback(track) != p || clock != p.clock || !p.playing || clip == nil {
		return
	}
	fps := p.fps(clip)
	// The mod's playback starts once the frames ahead are loaded, then keeps real time: a frame whose meshes or
	// position variants are not loaded when its time comes is skipped, and the frame before stays on screen.
	if since := v.anim.warmSince; track == trackMod && since != nil {
		if !v.animationWarm(clip) && time.Since(*since) < viewerWarmLimit {
			p.applied = time.Now()
			v.scheduleTrackTick(track, p, clock, time.Duration(float64(time.Second)/fps))
			return
		}
		v.anim.warmSince, p.applied = nil, time.Now()
	}
	frame, consumed, complete := advanceAnimationFrame(p.frame, time.Since(p.applied), fps, clip)
	p.applied = p.applied.Add(consumed)
	if frame != p.frame {
		v.setTrackFrame(track, frame)
	}
	if complete {
		p.playing = false
		return
	}
	next := time.Duration(float64(time.Second)/fps) - time.Since(p.applied)
	v.scheduleTrackTick(track, p, clock, max(next, time.Millisecond))
}

// setAnimationFrame shows frame index of the mod's clip: its values, with the phases of the wave shape targets
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
		v.syncNextFrame()
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
			index = loopAnimationFrame(clip, index)
		}
		_, states := modelviewer.EvaluateViewerState(v.scene.transport, v.animationFrameState(clip, index), form)
		upcoming = append(upcoming, states)
	}
	return upcoming
}

// animationFrameState is the toggle state frame index of the mod's clip sets, without its wave phases.
func (v *ModelViewer) animationFrameState(clip *modelviewer.ModelViewerAnimationClip, index int) map[string]any {
	state := maps.Clone(v.resolved)
	if state == nil {
		state = map[string]any{}
	}
	maps.Copy(state, clip.Frames[index].Values)
	return state
}

// viewerAnimationBarHeight is the height of one lane of the playback controller, viewerAnimationBarMaxWidth its
// widest, viewerAnimationBarMargin its default distance from the viewer's edges, and viewerScrubInset how far the
// scrubber's first and last frames sit from its ends.
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
	// viewerAnimationClipWidth is the width of the clip names while two lanes show, so their scrubbers line up.
	viewerAnimationClipWidth = 136
)

// animationTracks lists the tracks the controller shows, top to bottom. The motion lane goes above the mod's, so
// while the controller sits in its default place the mod's lane stays put when motions are turned on or off.
func (v *ModelViewer) animationTracks() []viewerTrack {
	var tracks []viewerTrack
	for _, track := range []viewerTrack{trackMotion, trackMod} {
		if v.trackClip(track) != nil {
			tracks = append(tracks, track)
		}
	}
	return tracks
}

func (v *ModelViewer) animationBarHeight() float32 {
	return float32(len(v.animationTracks())) * viewerAnimationBarHeight
}

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
	return (v.width - width) / 2, v.height - viewerAnimationBarMargin - v.animationBarHeight(), width, true
}

// clampAnimationBar keeps a bar of width at left, top inside the viewer.
func (v *ModelViewer) clampAnimationBar(left, top, width float32) (float32, float32) {
	return min(max(left, 0), max(v.width-width, 0)), min(max(top, 0), max(v.height-v.animationBarHeight(), 0))
}

// moveAnimationBar places a bar of width at left, top, kept inside the viewer.
func (v *ModelViewer) moveAnimationBar(left, top, width float32) {
	left, top = v.clampAnimationBar(left, top, width)
	v.anim.barAt = &[2]float32{left / max(v.width, 1), top / max(v.height, 1)}
}

// animationBar is the playback controller floating over the viewer: a grip that moves it, and a lane for each track
// with clips. It is centered at the bottom until moved.
func (v *ModelViewer) animationBar(t kit.Theme) *fairy.El {
	tracks := v.animationTracks()
	left, top, width, ok := v.animationBarPlace()
	if len(tracks) == 0 || !ok {
		return nil
	}
	move := T("page.tools.model_viewer.animation_move")
	grip := fairy.Box(fairy.Icon(lucide.GripVertical).Size(14, 14)).Size(16, 28).Center().Shrink(0).Radius(4).
		Color(t.MutedForeground).HoverColor(t.Foreground).Cursor(fairy.CursorMove).Label(move).Tooltip(move).
		Focusable().FocusRing(3, t.Ring.Alpha(0.5)).
		OnDoubleClick(func() { v.anim.barAt = nil }).
		OnDrag(func(d fairy.DragEvent) bool {
			if d.Phase == fairy.DragStart {
				v.anim.barDrag, v.anim.barDragFrom = [2]float32{left, top}, v.anim.barAt
			}
			if d.Canceled {
				v.anim.barAt = v.anim.barDragFrom
				return true
			}
			v.moveAnimationBar(v.anim.barDrag[0]+d.DX, v.anim.barDrag[1]+d.DY, width)
			return true
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

	lanes := fairy.Col().Grow(1).MinW(0)
	for _, track := range tracks {
		lanes.Child(v.animationLane(t, track, width, len(tracks) > 1))
	}
	bar := fairy.Row(grip, lanes).Key("model-viewer.animation").Gap(2).Align(fairy.Center).
		H(float32(len(tracks))*viewerAnimationBarHeight).PadXY(4, 0).
		Radius(viewerAnimationBarHeight/2).Color(t.PopoverForeground).Border(1, t.Border).
		Shadow(0, 8, 24, 0, shadowColor(&t)).Absolute().Left(left).Top(top).W(width)
	return glass(bar, opacity(t.Popover, t.SurfaceAlpha), t.Popover, t.SurfaceBlur)
}

// animationLane is one track's row of a controller of width: the play button, the clip, a frame scrubber between
// jumps to the clip's ends, the time and the speed. The motion lane ends with a button that turns the motions off,
// and the mod's lane leaves that room empty beside it, so the two scrubbers line up.
func (v *ModelViewer) animationLane(t kit.Theme, track viewerTrack, width float32, paired bool) *fairy.El {
	p, clip := v.playback(track), v.trackClip(track)
	last := max(len(clip.Frames)-1, 0)
	still := len(clip.Frames) <= 1

	icon, label := lucide.Play, T("page.tools.model_viewer.animation_play")
	if p.playing {
		icon, label = lucide.Pause, T("page.tools.model_viewer.animation_pause")
	}
	full := width >= viewerAnimationBarFullWidth
	lane := fairy.Row(
		kit.IconButton(icon, func() { v.setTrackPlaying(track, !p.playing) }, kit.Ghost, kit.Small,
			kit.Disabled(still)).Label(label),
		v.animationClipMenu(t, track, clip, paired && full),
	).Key("model-viewer.animation-lane." + track.key()).Gap(2).Align(fairy.Center).H(viewerAnimationBarHeight)
	if full {
		start := T("page.tools.model_viewer.animation_start")
		lane.Child(kit.IconButton(lucide.SkipBack, func() { v.setTrackFrame(track, 0) }, kit.Ghost, kit.Small,
			kit.Disabled(still), kit.Tooltip(start)).Label(start))
	}
	lane.Child(v.animationScrubber(t, track, p, clip, last))
	if full {
		end := T("page.tools.model_viewer.animation_end")
		lane.Child(kit.IconButton(lucide.SkipForward, func() { v.setTrackFrame(track, last) }, kit.Ghost, kit.Small,
			kit.Disabled(still), kit.Tooltip(end)).Label(end))
	}
	if width >= viewerAnimationBarCompactWidth {
		lane.Child(v.animationTime(t, p, clip))
	}
	lane.Child(v.animationSpeedMenu(t, track, p))
	switch {
	case track == trackMotion:
		off := T("page.tools.model_viewer.animation_motion_off")
		lane.Child(kit.IconButton(lucide.X, v.dismissMotion, kit.Ghost, kit.Small,
			kit.Tooltip(off)).Label(off))
	case paired:
		lane.Child(fairy.Box().Size(28, 28).Shrink(0))
	}
	return lane
}

// animationClipMenu names the clip track shows and, when it has others, opens a menu to pick one. Its icon tells
// the mod's clips from the character's motions.
func (v *ModelViewer) animationClipMenu(
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

	trigger.Child(fairy.Icon(lucide.ChevronUp).Size(12, 12).Shrink(0).Color(t.MutedForeground)).
		HoverFill(hoverFill(&t)).Cursor(fairy.CursorPointer).Role(fairy.RoleButton).Focusable().
		FocusRing(3, t.Ring.Alpha(0.5)).
		Label(T("page.tools.model_viewer.animation_clip")).Key("model-viewer.animation-clip-trigger." + track.key())
	items := func() []kit.MenuItem {
		items := make([]kit.MenuItem, len(clips))
		for i, entry := range clips {
			items[i] = kit.MenuItem{
				Label: entry.Label, Radio: true, Checked: entry.ID == clip.ID,
				OnClick: func() { v.startTrack(track, entry.ID) },
			}
		}
		return items
	}
	return kit.DropdownMenuFunc("model-viewer.animation-clip."+track.key(), trigger, items, kit.MaxHeight(320))
}

// animationScrubber shows the clip's frames as ticks with the frame shown as a playhead. A press or drag seeks,
// and playback pauses while it is dragged and continues from where it is let go.
func (v *ModelViewer) animationScrubber(
	t kit.Theme,
	track viewerTrack,
	p *viewerPlayback,
	clip *modelviewer.ModelViewerAnimationClip,
	last int,
) *fairy.El {
	const height = 22
	usable := p.scrubWidth - 2*viewerScrubInset
	at := func(frame int) float32 {
		if last == 0 || usable <= 0 {
			return viewerScrubInset
		}
		return viewerScrubInset + float32(frame)/float32(last)*usable
	}
	seek := func(x, width float32) {
		fraction := min(max(float64((x-viewerScrubInset)/max(width-2*viewerScrubInset, 1)), 0), 1)
		v.setTrackFrame(track, int(math.Round(fraction*float64(last))))
	}

	scrub := fairy.Box().Key("model-viewer.animation-frame." + track.key()).Grow(1).MinW(80).H(height).
		OnLayout(func(w, _ float32) bool {
			if p.scrubWidth == w {
				return false
			}
			p.scrubWidth = w
			return true
		})
	if usable > 0 && last > 0 {
		type tickVersion struct {
			last   int
			usable float32
			color  fairy.Color
		}
		color := t.MutedForeground.Alpha(0.5)
		scrub.Child(fairy.Memo("model-viewer.animation-ticks."+track.key(), tickVersion{last, usable, color},
			func() *fairy.El {
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
	scrub.Child(fairy.Box().Absolute().Left(at(p.frame)-1.5).Top(4).Size(3, height-8).Radius(1.5).
		Fill(t.Primary).FocusRing(3, t.Ring.Alpha(0.5)).FocusFromParent().PassThrough())

	current := clip.FrameStart
	if p.frame < len(clip.Frames) {
		current = clip.Frames[p.frame].Index
	}
	return scrub.Role(fairy.RoleSlider).
		RangeValue(float64(p.frame), 0, float64(last), 1, func(frame float64) {
			v.setTrackFrame(track, int(math.Round(frame)))
		}).
		Label(T("page.tools.model_viewer.animation_frame_slider")).
		Tooltip(T("page.tools.model_viewer.animation_frame", "current", current, "end", clip.FrameEnd)).
		Cursor(fairy.CursorPointer).
		Focusable().
		OnPointerDown(func(e fairy.PointerEvent) { seek(e.LocalX, e.W) }).
		OnDrag(func(d fairy.DragEvent) bool {
			if d.Phase == fairy.DragStart && p.resume == nil {
				playing := p.playing
				p.resume = &playing
				v.setTrackPlaying(track, false)
			}
			seek(d.LocalX, d.W)
			if d.Phase == fairy.DragEnd && p.resume != nil {
				resume := *p.resume
				p.resume = nil
				v.setTrackPlaying(track, resume)
			}
			return true
		}).
		OnKey(func(k keyboard.KeyEvent) bool {
			if k.Mods != 0 {
				return false
			}
			switch k.Key {
			case keyboard.KeyLeft, keyboard.KeyDown:
				v.setTrackFrame(track, p.frame-1)
			case keyboard.KeyRight, keyboard.KeyUp:
				v.setTrackFrame(track, p.frame+1)
			case keyboard.KeyHome:
				v.setTrackFrame(track, 0)
			case keyboard.KeyEnd:
				v.setTrackFrame(track, last)
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
func (v *ModelViewer) animationTime(
	t kit.Theme,
	p *viewerPlayback,
	clip *modelviewer.ModelViewerAnimationClip,
) *fairy.El {
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
			timeText(seconds(p.frame)).Color(t.Primary).Absolute().Right(0).Top(0),
		),
		fairy.Text(T("page.tools.model_viewer.animation_duration", "total", total)).
			FontSize(10).LineHeight(12).Color(t.MutedForeground),
	).Align(fairy.End).MinW(48).Shrink(0).PadXY(4, 0)
}

// animationSpeedMenu shows track's playback speed and opens a menu to change it.
func (v *ModelViewer) animationSpeedMenu(t kit.Theme, track viewerTrack, p *viewerPlayback) *fairy.El {
	speed := p.rate()
	trigger := fairy.Box(fairy.Text(formatViewerNumber(speed)+"×").FontSize(12).LineHeight(16).Medium()).
		H(28).MinW(36).PadXY(8, 0).Center().Radius(14).Shrink(0).
		HoverFill(hoverFill(&t)).Cursor(fairy.CursorPointer).Role(fairy.RoleButton).Focusable().
		FocusRing(3, t.Ring.Alpha(0.5)).
		Label(T("page.tools.model_viewer.animation_speed")).Key("model-viewer.animation-speed-trigger." + track.key())
	items := make([]kit.MenuItem, len(viewerSpeeds), len(viewerSpeeds)+2)
	for i, option := range viewerSpeeds {
		items[i] = kit.MenuItem{
			Label: formatViewerNumber(option) + "×", Radio: true, Checked: option == speed,
			OnClick: func() { v.setTrackSpeed(track, option) },
		}
	}
	items = append(items, kit.MenuItem{Separator: true}, kit.MenuItem{
		Label: T("page.tools.model_viewer.animation_interpolate"), Checkbox: true, Checked: !p.stepped,
		OnClick: func() { v.setTrackStepped(track, !p.stepped) },
	})
	return kit.DropdownMenu("model-viewer.animation-speed."+track.key(), trigger, items, kit.AlignEnd)
}
