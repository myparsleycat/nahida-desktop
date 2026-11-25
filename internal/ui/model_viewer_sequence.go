package ui

import (
	"maps"
	"math"
	"reflect"
	"slices"
	"time"

	"github.com/myparsleycat/fairy"

	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

const (
	// defaultShotSeconds is how long a shot lasts whose length is not counted in loops of a playing clip.
	defaultShotSeconds = 5.0
	// defaultShotTransition is the camera ease and motion blend into a new shot, in seconds.
	defaultShotTransition = 0.6
	maxShotTransition     = 3.0
)

// WallpaperShot is one view of a wallpaper sequence: where the camera looks, the toggles and form shown, and the
// clips that play, for a number of loops of its timing clip or a number of seconds.
type WallpaperShot struct {
	Camera    wallpaperCamera `json:"camera"`
	Form      string          `json:"form,omitempty"`
	State     map[string]any  `json:"state,omitempty"`
	Animation *wallpaperTrack `json:"animation,omitempty"`
	Motion    *wallpaperTrack `json:"motion,omitempty"`
	// Loops counts plays of the timing clip, the motion's or else the mod's; zero, or a shot whose timing clip does
	// not play, lasts Seconds.
	Loops   int     `json:"loops,omitempty"`
	Seconds float64 `json:"seconds,omitempty"`
	// Transition is how many seconds the camera eases and the motion blends from the shot before.
	Transition float64 `json:"transition"`
	// Thumbnail is a small PNG of the shot, base64 encoded, that the editor's timeline shows.
	Thumbnail string `json:"thumbnail,omitempty"`

	// key identifies the shot's card in the editor while shots move; thumb is Thumbnail decoded.
	key   int
	thumb *fairy.Image
}

// WallpaperSequence is a named, looping list of shots of one mod, stored in the mod's nhd.json.
type WallpaperSequence struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Shots []WallpaperShot `json:"shots"`
}

func (s WallpaperShot) duration() time.Duration {
	seconds := s.Seconds
	if seconds <= 0 || math.IsNaN(seconds) {
		seconds = defaultShotSeconds
	}
	return time.Duration(min(seconds, maxShotSeconds) * float64(time.Second))
}

func (s WallpaperShot) transition() time.Duration {
	seconds := min(max(s.Transition, 0), maxShotTransition)
	if math.IsNaN(seconds) {
		seconds = 0
	}
	return time.Duration(seconds * float64(time.Second))
}

func (s WallpaperShot) timingTrack() (viewerTrack, *wallpaperTrack) {
	if s.Motion != nil {
		return trackMotion, s.Motion
	}
	return trackMod, s.Animation
}

// sequencer plays the shots of a sequence in a loop on its own clock, which stops while the wallpaper is paused.
type sequencer struct {
	shots []WallpaperShot
	index int
	// start is when the shot shown began, moved on by the time spent paused; pausedAt is set while paused.
	start    time.Time
	pausedAt time.Time
	// from and to are the cameras the shot's transition eases between over ease; settled is set once the camera
	// reached to and the sequence stops moving it.
	from, to orbitCamera
	ease     time.Duration
	settled  bool
	// cycleBase is the timing playback's count of finished plays when the shot began.
	cycleBase int
	// wake numbers the timer that ends a shot timed in seconds, so an earlier one does nothing.
	wake uint64
	// missing holds the clips shots name that the mod or motion lacks, which are logged once.
	missing map[string]bool
}

func (s *sequencer) elapsed(now time.Time) time.Duration {
	if !s.pausedAt.IsZero() {
		now = s.pausedAt
	}
	return max(now.Sub(s.start), 0)
}

func (s *sequencer) pause(now time.Time) {
	if s.pausedAt.IsZero() {
		s.pausedAt = now
	}
}

func (s *sequencer) resume(now time.Time) {
	if s.pausedAt.IsZero() {
		return
	}
	s.start = s.start.Add(now.Sub(s.pausedAt))
	s.pausedAt = time.Time{}
}

func (s *sequencer) enter(index int, now time.Time, from, to orbitCamera, ease time.Duration, cycleBase int) {
	s.index, s.start = index, now
	if !s.pausedAt.IsZero() {
		s.pausedAt = now
	}
	s.from, s.to, s.ease, s.settled = from, to, ease, false
	s.cycleBase = cycleBase
}

func (s *sequencer) next() int { return (s.index + 1) % len(s.shots) }

// due reports whether the shot shown has run its length: its loops of the timing clip when counted, its seconds
// otherwise.
func (s *sequencer) due(now time.Time, cycles int, counted bool) bool {
	shot := s.shots[s.index]
	if shot.Loops > 0 && counted {
		return cycles-s.cycleBase >= shot.Loops
	}
	return s.elapsed(now) >= shot.duration()
}

// camera is the camera of the transition at now, seen through lens; ok is false once the camera settled, when the
// sequence leaves it alone.
func (s *sequencer) camera(now time.Time, lens float64) (orbitCamera, bool) {
	if s.settled {
		return orbitCamera{}, false
	}
	from, to := s.from.withLens(lens), s.to.withLens(lens)
	t := 1.0
	if s.ease > 0 {
		t = s.elapsed(now).Seconds() / s.ease.Seconds()
	}
	if t >= 1 {
		s.settled = true
		return to, true
	}
	return lerpCamera(from, to, smoothstep(0, 1, t)), true
}

// withLens dollies c to the field of view fov, so the model keeps its size on screen.
func (c orbitCamera) withLens(fov float64) orbitCamera {
	if current := c.lens(); current != fov {
		c.distance *= math.Tan(current/2) / math.Tan(fov/2)
		c.fov = fov
	}
	return c
}

// lerpCamera is the camera the share t of the way from a to b, turning the short way round. Both use one lens.
func lerpCamera(a, b orbitCamera, t float64) orbitCamera {
	lerp := func(x, y float64) float64 { return x + (y-x)*t }
	yaw := math.Remainder(b.yaw-a.yaw, 2*math.Pi)
	return orbitCamera{
		target:   a.target.add(b.target.sub(a.target).scale(t)),
		distance: lerp(a.distance, b.distance),
		yaw:      a.yaw + yaw*t,
		pitch:    lerp(a.pitch, b.pitch),
		radius:   lerp(a.radius, b.radius),
		center:   a.center.add(b.center.sub(a.center).scale(t)),
		fov:      b.fov,
	}
}

func (c wallpaperCamera) orbit() (orbitCamera, bool) {
	if c.Distance <= 0 || c.Radius <= 0 {
		return orbitCamera{}, false
	}
	return orbitCamera{
		target: c.Target, distance: c.Distance, yaw: c.Yaw, pitch: c.Pitch, radius: c.Radius, center: c.Center,
		fov: c.FOV,
	}, true
}

func cameraSnapshot(c orbitCamera) wallpaperCamera {
	return wallpaperCamera{
		Target: c.target, Distance: c.distance, Yaw: c.yaw, Pitch: c.pitch, Radius: c.radius, Center: c.center,
		FOV: c.fov,
	}
}

// startSequence plays shots in a loop from the first. With cut the first shot shows at once; otherwise the camera
// and motion move into it from the view shown.
func (v *ModelViewer) startSequence(shots []WallpaperShot, cut bool) {
	v.stopSequence()
	if len(shots) == 0 || v.scene == nil {
		return
	}
	v.sequence = &sequencer{shots: shots}
	if v.wallpaperPaused {
		v.sequence.pause(time.Now())
	}
	v.enterShot(0, time.Now(), cut)
}

func (v *ModelViewer) stopSequence() {
	if s := v.sequence; s != nil {
		s.wake++
		v.sequence = nil
	}
}

// enterShot shows shot index of the sequence: its form and toggles at once, its mod clip from the start, its motion
// blended in and its camera eased in over the shot's transition, unless cut.
func (v *ModelViewer) enterShot(index int, now time.Time, cut bool) {
	s := v.sequence
	shot := s.shots[index]
	ease := shot.transition()
	if cut {
		ease = 0
	}
	v.applyShot(shot, ease)

	to, ok := shot.Camera.orbit()
	if !ok {
		to = v.camera
	}
	cycles, _ := v.shotCycles(shot)
	s.enter(index, now, v.camera, to, ease, cycles)
	v.motion.stop()
	v.scheduleSequenceWake()
	v.revision++
}

// applyShot shows the form and toggles of shot and plays its clips from their start, blending the motion in over
// fade.
func (v *ModelViewer) applyShot(shot WallpaperShot, fade time.Duration) {
	if shot.Form != v.form {
		v.setForm(shot.Form)
	}
	if len(shot.State) > 0 {
		next := maps.Clone(v.state)
		maps.Copy(next, shot.State)
		if !reflect.DeepEqual(next, v.state) {
			v.setToggleState(next)
		}
	}
	v.playShotTrack(trackMod, shot.Animation, 0)
	v.playShotTrack(trackMotion, shot.Motion, fade)
}

// playShotTrack plays the clip of t on track from its start, blending a motion in over fade. A track the shot
// leaves out, or whose clip is gone, keeps playing what it plays.
func (v *ModelViewer) playShotTrack(track viewerTrack, t *wallpaperTrack, fade time.Duration) {
	if t == nil {
		return
	}
	clips := v.trackClips(track)
	index := slices.IndexFunc(clips, func(clip modelviewer.ModelViewerAnimationClip) bool { return clip.ID == t.Clip })
	if index < 0 {
		// Motions load after the scene; the shot plays them again once they are in.
		if track == trackMotion && v.activeMotion() == nil {
			return
		}
		v.reportMissingShotClip(track, t.Clip)
		return
	}
	if track == trackMotion {
		v.startMotionFade(t.Clip, fade)
	} else {
		v.startTrack(track, t.Clip)
	}
	v.setTrackSpeed(track, t.Speed)
	v.setTrackStepped(track, t.Stepped)
	if t.Paused {
		v.setTrackPlaying(track, false)
		v.setTrackFrame(track, min(max(t.Frame, 0), max(len(clips[index].Frames)-1, 0)))
	}
}

func (v *ModelViewer) reportMissingShotClip(track viewerTrack, clip string) {
	s := v.sequence
	key := track.key() + ":" + clip
	if s == nil || s.missing[key] {
		return
	}
	if s.missing == nil {
		s.missing = map[string]bool{}
	}
	s.missing[key] = true
	v.log.Warn("Wallpaper sequence shot names a clip the "+track.key()+" track does not have: "+clip,
		"ModelViewer.sequence")
}

// shotCycles is the number of finished plays of shot's timing clip; counted is false while that clip does not play
// as the shot has it, which times the shot in seconds.
func (v *ModelViewer) shotCycles(shot WallpaperShot) (cycles int, counted bool) {
	track, t := shot.timingTrack()
	p, clip := v.playback(track), v.trackClip(track)
	if t == nil || t.Paused || p == nil || clip == nil || clip.ID != t.Clip || len(clip.Frames) < 2 {
		return 0, false
	}
	return p.cycles, true
}

// runSequence moves the sequence on at now: to the next shot once the one shown ran its length, and the camera
// along the shot's transition. Call it on each build, before the draw options are taken.
func (v *ModelViewer) runSequence(now time.Time) {
	s := v.sequence
	if s == nil || v.closed || v.scene == nil {
		return
	}
	if s.pausedAt.IsZero() {
		shot := s.shots[s.index]
		cycles, counted := v.shotCycles(shot)
		track, t := shot.timingTrack()
		// A clip that does not loop stops at its end; it plays again until the shot's loops are done.
		if p := v.playback(track); counted && shot.Loops > 0 && !p.playing && cycles-s.cycleBase < shot.Loops {
			v.playShotTrack(track, t, shot.transition())
		}
		if s.due(now, cycles, counted) {
			v.enterShot(s.next(), now, len(s.shots) == 1)
		}
	}
	if camera, ok := s.camera(now, v.camera.lens()); ok {
		v.camera = camera
	}
}

// sequenceMoving reports a camera transition in progress, which redraws the viewer on every frame.
func (v *ModelViewer) sequenceMoving() bool {
	return v.sequence != nil && !v.sequence.settled && v.sequence.pausedAt.IsZero()
}

// scheduleSequenceWake ends a shot timed in seconds when its time is up, as nothing else may rebuild the viewer
// then.
func (v *ModelViewer) scheduleSequenceWake() {
	s := v.sequence
	if s == nil || !s.pausedAt.IsZero() {
		return
	}
	s.wake++
	wake := s.wake
	shot := s.shots[s.index]
	if _, counted := v.shotCycles(shot); counted && shot.Loops > 0 {
		return
	}
	post := v.poster()
	time.AfterFunc(max(shot.duration()-s.elapsed(time.Now()), time.Millisecond), func() {
		post(func() {
			if v.closed || v.sequence != s || s.wake != wake {
				return
			}
			v.runSequence(time.Now())
			if v.window != nil {
				v.window.InvalidateSurface(modelViewerSurfaceKey)
			}
		})
	})
}

// pauseSequence stops or resumes the sequence's clock with the wallpaper.
func (v *ModelViewer) pauseSequence(paused bool) {
	s := v.sequence
	if s == nil {
		return
	}
	now := time.Now()
	if paused {
		s.pause(now)
		s.wake++
		return
	}
	s.resume(now)
	v.scheduleSequenceWake()
}

// restartShot shows the shot shown again from its start, as when the motions it plays finished loading.
func (v *ModelViewer) restartShot() {
	if s := v.sequence; s != nil {
		v.enterShot(s.index, time.Now(), true)
	}
}
