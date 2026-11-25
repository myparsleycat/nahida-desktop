package ui

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit"
	"golang.org/x/image/draw"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

const (
	sequencePrefix      = "page.tools.model_viewer.sequence."
	maxShotLoops        = 99
	minShotSeconds      = 0.5
	maxShotSeconds      = 600.0
	shotSecondsStep     = 0.5
	shotTransitionStep  = 0.1
	shotLengthLoops     = "loops"
	shotLengthSeconds   = "seconds"
	sequenceNameMaxRune = 64
	// shotThumbWidth and shotThumbHeight size a shot's thumbnail, which is taken once the view has rested for
	// shotThumbRest after a change.
	shotThumbWidth  = 192
	shotThumbHeight = 108
	shotThumbRest   = 400 * time.Millisecond
	// previewTick is how often the timeline redraws a preview's progress.
	previewTick = 100 * time.Millisecond
)

// wallpaperMaker is the Wallpaper Maker's state: the mod's saved sequences and the one being edited.
type wallpaperMaker struct {
	sequences []WallpaperSequence
	draft     WallpaperSequence
	// selected is the index of the shot the viewer shows and edits, or -1.
	selected int
	dirty    bool
	loading  bool
	saving   bool
	nextKey  int
	monitor  string
	// numberDraft is the shot length being typed, until it is committed.
	numberDraft *string
	naming      *sequenceNaming
	confirm     *sequenceConfirm
	// thumbKey is the shot whose thumbnail is taken once the view rests at thumbAt, or 0; thumbBusy is set while
	// one is read back and encoded.
	thumbKey  int
	thumbAt   time.Time
	thumbBusy bool
	// thumbTimer is set while a timer waits for thumbAt.
	thumbTimer bool
	// ticking is set while the timeline redraws a preview's progress.
	ticking bool
	// monitors are the displays the wallpaper can go on, listed when the Wallpaper Maker opened.
	monitors []platform.Monitor
}

// sequenceNaming is the dialog that names a new sequence or renames the one being edited.
type sequenceNaming struct {
	name   string
	rename bool
}

type sequenceConfirm struct {
	title, description, action string
	destructive                bool
	run                        func()
}

func (m *wallpaperMaker) edit(sequence WallpaperSequence) {
	m.draft = WallpaperSequence{ID: sequence.ID, Name: sequence.Name, Shots: m.keyed(sequence.Shots)}
	m.selected, m.dirty, m.numberDraft, m.thumbKey = -1, false, nil, 0
}

// keyed copies shots with new card keys.
func (m *wallpaperMaker) keyed(shots []WallpaperShot) []WallpaperShot {
	copied := cloneShots(shots)
	for i := range copied {
		m.nextKey++
		copied[i].key = m.nextKey
	}
	return copied
}

func (m *wallpaperMaker) saved(id string) bool {
	return slices.ContainsFunc(m.sequences, func(s WallpaperSequence) bool { return s.ID == id })
}

func (m *wallpaperMaker) shot() *WallpaperShot {
	if m.selected < 0 || m.selected >= len(m.draft.Shots) {
		return nil
	}
	return &m.draft.Shots[m.selected]
}

func (m *wallpaperMaker) shotByKey(key int) *WallpaperShot {
	if i := slices.IndexFunc(m.draft.Shots, func(s WallpaperShot) bool { return s.key == key }); i >= 0 {
		return &m.draft.Shots[i]
	}
	return nil
}

func cloneShots(shots []WallpaperShot) []WallpaperShot {
	copied := slices.Clone(shots)
	for i := range copied {
		copied[i].State = maps.Clone(copied[i].State)
		if t := copied[i].Animation; t != nil {
			track := *t
			copied[i].Animation = &track
		}
		if t := copied[i].Motion; t != nil {
			track := *t
			copied[i].Motion = &track
		}
	}
	return copied
}

func newSequenceID() string { return strconv.FormatInt(time.Now().UnixNano(), 36) }

func (v *ModelViewer) openWallpaperMaker() {
	m := &wallpaperMaker{selected: -1, loading: true}
	m.edit(WallpaperSequence{ID: newSequenceID(), Name: T(sequencePrefix + "default_name")})
	monitors, err := platform.Monitors()
	if err != nil {
		v.log.Warn("Model viewer could not list the monitors: "+err.Error(), "ModelViewer.sequence")
	}
	m.monitors = monitors
	v.maker = m
	v.loadWallpaperSequences(m)
}

func (v *ModelViewer) closeWallpaperMaker() {
	v.discardThen(func() {
		v.stopSequence()
		v.maker = nil
	})
}

func (v *ModelViewer) loadWallpaperSequences(m *wallpaperMaker) {
	path, toasts := v.path, v.toasts
	v.shell.background(func(ctx context.Context) {
		raw, err := v.shell.deps.Mod.GetWallpaperSequences(ctx, path)
		var sequences []WallpaperSequence
		if err == nil && len(raw) > 0 {
			if err = json.Unmarshal(raw, &sequences); err != nil {
				err = infra.ReportError(
					v.log,
					fmt.Errorf("decode wallpaper sequences: %w", err),
					"ModelViewer.sequence",
					infra.Diagnostic{Operation: "load", Fields: map[string]any{"modPath": path}},
				)
			}
		}
		fairy.Go(func() {
			m.loading = false
			if err != nil {
				toasts.Show(kit.ToastError, T(sequencePrefix+"toast.load_error"), err.Error())
				return
			}
			m.sequences = sequences
			if v.maker == m && len(sequences) > 0 && !m.dirty && len(m.draft.Shots) == 0 {
				m.edit(sequences[0])
			}
		})
	})
}

// saveWallpaperSequences writes sequences to the mod's nhd.json and runs done once they are written.
func (v *ModelViewer) saveWallpaperSequences(m *wallpaperMaker, sequences []WallpaperSequence, done func()) {
	if m.saving {
		return
	}
	data, err := json.Marshal(sequences)
	if err != nil {
		v.toasts.Show(kit.ToastError, T(sequencePrefix+"toast.save_error"), err.Error())
		return
	}
	m.saving = true
	path, toasts := v.path, v.toasts
	v.shell.background(func(ctx context.Context) {
		err := v.shell.deps.Mod.SetWallpaperSequences(ctx, path, data)
		fairy.Go(func() {
			m.saving = false
			if err != nil {
				toasts.Show(kit.ToastError, T(sequencePrefix+"toast.save_error"), err.Error())
				return
			}
			m.sequences = sequences
			done()
		})
	})
}

func (v *ModelViewer) saveDraft() {
	m := v.maker
	sequences := slices.Clone(m.sequences)
	draft := WallpaperSequence{ID: m.draft.ID, Name: m.draft.Name, Shots: cloneShots(m.draft.Shots)}
	if i := slices.IndexFunc(sequences, func(s WallpaperSequence) bool { return s.ID == draft.ID }); i >= 0 {
		sequences[i] = draft
	} else {
		sequences = append(sequences, draft)
	}
	v.saveWallpaperSequences(m, sequences, func() {
		m.dirty = false
		v.toasts.Show(kit.ToastSuccess, T(sequencePrefix+"toast.saved"), "")
	})
}

func (v *ModelViewer) deleteDraft() {
	m := v.maker
	id := m.draft.ID
	sequences := slices.DeleteFunc(slices.Clone(m.sequences), func(s WallpaperSequence) bool { return s.ID == id })
	v.saveWallpaperSequences(m, sequences, func() {
		v.stopSequence()
		if len(sequences) > 0 {
			m.edit(sequences[0])
			return
		}
		m.edit(WallpaperSequence{ID: newSequenceID(), Name: T(sequencePrefix + "default_name")})
	})
}

func (v *ModelViewer) switchSequence(id string) {
	m := v.maker
	i := slices.IndexFunc(m.sequences, func(s WallpaperSequence) bool { return s.ID == id })
	if i < 0 || id == m.draft.ID {
		return
	}
	v.discardThen(func() {
		v.stopSequence()
		m.edit(m.sequences[i])
	})
}

// ConfirmClose reports whether the viewer can close now. With unsaved Wallpaper Maker changes it asks the user
// first and runs close once they agree to drop them.
func (v *ModelViewer) ConfirmClose(close func()) bool {
	m := v.maker
	if m == nil || !m.dirty {
		return true
	}
	v.discardThen(func() {
		m.dirty = false
		close()
	})
	return false
}

// discardThen runs next at once, or after the user agrees to drop the draft's unsaved changes.
func (v *ModelViewer) discardThen(next func()) {
	m := v.maker
	if m == nil || !m.dirty {
		next()
		return
	}
	m.confirm = &sequenceConfirm{
		title:       T(sequencePrefix + "dialog.discard.title"),
		description: T(sequencePrefix+"dialog.discard.description", "name", m.draft.Name),
		action:      T(sequencePrefix + "dialog.discard.confirm"),
		destructive: true,
		run:         next,
	}
}

// captureView records what the view shows as a shot without a length: the camera, form, toggles and clips.
func (v *ModelViewer) captureView() WallpaperShot {
	shot := WallpaperShot{
		Camera:    cameraSnapshot(v.camera),
		Form:      v.form,
		Animation: v.wallpaperTrack(trackMod),
		Motion:    v.wallpaperTrack(trackMotion),
	}
	if v.scene != nil {
		shot.State = toggleState(v.state, &v.scene.transport)
	}
	// A viewer that never picked a mod clip plays the first; the shot names it so its loops can be counted.
	if clip := v.animationClip(); shot.Animation == nil && clip != nil && len(clip.Frames) > 1 {
		shot.Animation = &wallpaperTrack{Clip: clip.ID, Paused: !v.anim.playing, Frame: v.anim.frame,
			Speed: v.anim.speed, Stepped: v.anim.stepped}
	}
	// A playing clip's frame moves on; only a paused one's belongs to the shot.
	for _, t := range []*wallpaperTrack{shot.Animation, shot.Motion} {
		if t != nil && !t.Paused {
			t.Frame = 0
		}
	}
	return shot
}

// captureShot records the view shown as a new shot, timed by one loop of its clip when it plays one.
func (v *ModelViewer) captureShot() WallpaperShot {
	shot := v.captureView()
	shot.Transition = defaultShotTransition
	if v.shotCountable(shot) {
		shot.Loops = 1
	} else {
		shot.Seconds = defaultShotSeconds
	}
	return shot
}

// shotCountable reports whether shot's timing clip plays, so its length can be counted in loops.
func (v *ModelViewer) shotCountable(shot WallpaperShot) bool {
	track, t := shot.timingTrack()
	if t == nil || t.Paused {
		return false
	}
	clip := v.findClip(track, t.Clip)
	return clip != nil && len(clip.Frames) > 1
}

func (v *ModelViewer) findClip(track viewerTrack, id string) *modelviewer.ModelViewerAnimationClip {
	clips := v.trackClips(track)
	if i := slices.IndexFunc(clips, func(c modelviewer.ModelViewerAnimationClip) bool { return c.ID == id }); i >= 0 {
		return &clips[i]
	}
	return nil
}

// shotLength is how long shot plays: its loops of its timing clip at the clip's rate, or its seconds.
func (v *ModelViewer) shotLength(shot WallpaperShot) time.Duration {
	if shot.Loops <= 0 || !v.shotCountable(shot) {
		return shot.duration()
	}
	track, t := shot.timingTrack()
	clip := v.findClip(track, t.Clip)
	rate := t.Speed
	if rate <= 0 {
		rate = 1
	}
	seconds := float64(shot.Loops*len(clip.Frames)) / (normalizeAnimationFPS(clip.FPS) * rate)
	return time.Duration(seconds * float64(time.Second))
}

// syncSelectedShot records a change the user made in the viewer, to the camera, form, toggles or clips, into the
// selected shot. Call it on each build.
func (v *ModelViewer) syncSelectedShot() {
	m := v.maker
	if m == nil || v.sequence != nil || v.scene == nil || v.loading {
		return
	}
	shot := m.shot()
	if shot == nil {
		return
	}
	current := v.captureView()
	// Motions still loading, or turned off here, leave the shot's motion as it is.
	if v.activeMotion() == nil {
		current.Motion = shot.Motion
	}
	if shotViewEqual(*shot, current) {
		return
	}
	shot.Camera, shot.Form, shot.State = current.Camera, current.Form, current.State
	shot.Animation, shot.Motion = current.Animation, current.Motion
	if shot.Loops > 0 && !v.shotCountable(*shot) {
		shot.Loops = 0
		if shot.Seconds <= 0 {
			shot.Seconds = defaultShotSeconds
		}
	}
	m.dirty = true
	v.queueThumbnail(shot.key, shotThumbRest)
}

// shotViewEqual reports whether a and b show the same view, whatever their lengths.
func shotViewEqual(a, b WallpaperShot) bool {
	return cameraEqual(a.Camera, b.Camera) && a.Form == b.Form && trackEqual(a.Animation, b.Animation) &&
		trackEqual(a.Motion, b.Motion) && stateEqual(a.State, b.State)
}

// cameraEqual compares two cameras seen through one lens, as the viewer dollies a stored camera to its own.
func cameraEqual(a, b wallpaperCamera) bool {
	ca, okA := a.orbit()
	cb, okB := b.orbit()
	if !okA || !okB {
		return a == b
	}
	cb = cb.withLens(ca.lens())
	near := func(x, y float64) bool { return math.Abs(x-y) <= 1e-9*max(1, math.Abs(x), math.Abs(y)) }
	for i := range 3 {
		if !near(ca.target[i], cb.target[i]) || !near(ca.center[i], cb.center[i]) {
			return false
		}
	}
	return near(ca.distance, cb.distance) && near(ca.yaw, cb.yaw) && near(ca.pitch, cb.pitch) &&
		near(ca.radius, cb.radius)
}

func trackEqual(a, b *wallpaperTrack) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Clip == b.Clip && a.Paused == b.Paused && a.Speed == b.Speed && a.Stepped == b.Stepped &&
		(!a.Paused || a.Frame == b.Frame)
}

// stateEqual compares toggle states by their JSON, so a value read back from JSON as float64 equals the int it was.
func stateEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(ja, jb)
}

// selectShot shows shot i in the viewer, where changes to the view go into it.
func (v *ModelViewer) selectShot(i int) {
	m := v.maker
	v.stopSequence()
	m.selected, m.numberDraft = i, nil
	v.showShot(i)
}

func (v *ModelViewer) showShot(i int) {
	shot := v.maker.draft.Shots[i]
	v.applyShot(shot, viewerMotionFade)
	if camera, ok := shot.Camera.orbit(); ok {
		v.camera = camera.withLens(v.camera.lens())
		v.motion.stop()
	}
	v.revision++
}

// addShot records the view shown as a new shot after the selected one, or at the end, and selects it.
func (v *ModelViewer) addShot() {
	m := v.maker
	v.stopPreview()
	shot := v.captureShot()
	m.nextKey++
	shot.key = m.nextKey
	at := len(m.draft.Shots)
	if m.shot() != nil {
		at = m.selected + 1
	}
	m.draft.Shots = slices.Insert(m.draft.Shots, at, shot)
	m.selected, m.dirty, m.numberDraft = at, true, nil
	v.queueThumbnail(shot.key, 0)
}

func (v *ModelViewer) duplicateShot(i int) {
	m := v.maker
	v.stopPreview()
	shot := cloneShots(m.draft.Shots[i : i+1])[0]
	m.nextKey++
	shot.key = m.nextKey
	m.draft.Shots = slices.Insert(m.draft.Shots, i+1, shot)
	m.dirty = true
	v.selectShot(i + 1)
}

func (v *ModelViewer) moveShot(i, direction int) {
	m := v.maker
	v.stopPreview()
	shots := m.draft.Shots
	shots[i], shots[i+direction] = shots[i+direction], shots[i]
	switch m.selected {
	case i:
		m.selected = i + direction
	case i + direction:
		m.selected = i
	}
	m.dirty = true
}

func (v *ModelViewer) deleteShot(i int) {
	m := v.maker
	v.stopSequence()
	m.draft.Shots = slices.Delete(m.draft.Shots, i, i+1)
	switch {
	case m.selected == i:
		m.selected, m.numberDraft = -1, nil
	case m.selected > i:
		m.selected--
	}
	m.dirty = true
}

func (v *ModelViewer) togglePreview() {
	if v.sequence != nil {
		v.stopPreview()
		return
	}
	m := v.maker
	if m == nil || len(m.draft.Shots) == 0 {
		return
	}
	v.startSequence(cloneShots(m.draft.Shots), false)
	v.tickPreview(m)
}

// stopPreview ends the Wallpaper Maker's preview and shows the selected shot again, so changes to the view keep
// going into it. A wallpaper's sequence plays on.
func (v *ModelViewer) stopPreview() {
	if v.wallpaper != nil || v.sequence == nil {
		return
	}
	v.stopSequence()
	if m := v.maker; m != nil && m.shot() != nil {
		v.showShot(m.selected)
	}
}

// tickPreview rebuilds the viewer while the preview plays, so the timeline shows its progress.
func (v *ModelViewer) tickPreview(m *wallpaperMaker) {
	if m.ticking {
		return
	}
	m.ticking = true
	s, post := v.sequence, v.poster()
	var tick func()
	tick = func() {
		time.AfterFunc(previewTick, func() {
			post(func() {
				if v.closed || v.maker != m || v.sequence != s {
					m.ticking = false
					return
				}
				tick()
			})
		})
	}
	tick()
}

// queueThumbnail takes the thumbnail of shot key once the view has rested for delay.
func (v *ModelViewer) queueThumbnail(key int, delay time.Duration) {
	m := v.maker
	m.thumbKey, m.thumbAt = key, time.Now().Add(delay)
	v.waitThumbnail(m)
}

// waitThumbnail takes the queued thumbnail at thumbAt, with one timer however often the view changes until then.
func (v *ModelViewer) waitThumbnail(m *wallpaperMaker) {
	if m.thumbTimer {
		return
	}
	m.thumbTimer = true
	post := v.poster()
	time.AfterFunc(max(time.Until(m.thumbAt), 0)+10*time.Millisecond, func() {
		post(func() {
			m.thumbTimer = false
			v.takeThumbnail(m)
		})
	})
}

func (v *ModelViewer) takeThumbnail(m *wallpaperMaker) {
	if v.closed || v.maker != m || m.thumbKey == 0 || m.thumbBusy {
		return
	}
	if time.Now().Before(m.thumbAt) {
		v.waitThumbnail(m)
		return
	}
	key := m.thumbKey
	shot := m.shot()
	if shot == nil || shot.key != key || v.sequence != nil {
		// The view no longer shows the shot.
		m.thumbKey = 0
		return
	}
	if v.motion.running {
		v.queueThumbnail(key, shotThumbRest)
		return
	}
	frame, err := v.capture(1)
	if errors.Is(err, errViewerCaptureNotReady) {
		v.queueThumbnail(key, shotThumbRest)
		return
	}
	if err != nil {
		m.thumbKey = 0
		_ = infra.ReportError(v.log, err, "ModelViewer.sequence", infra.Diagnostic{
			Operation: "shot-thumbnail", Stage: "render", Fields: map[string]any{"modPath": v.path},
		})
		return
	}
	m.thumbKey, m.thumbBusy = 0, true
	v.shell.background(func(context.Context) {
		pixels, err := frame.NRGBA()
		frame.Release()
		var small *image.NRGBA
		var encoded string
		if err == nil {
			small, encoded, err = shotThumbnail(pixels)
		}
		fairy.Go(func() {
			m.thumbBusy = false
			if err != nil {
				_ = infra.ReportError(v.log, err, "ModelViewer.sequence", infra.Diagnostic{
					Operation: "shot-thumbnail", Stage: "encode", Fields: map[string]any{"modPath": v.path},
				})
			} else if target := m.shotByKey(key); target != nil {
				target.Thumbnail, target.thumb = encoded, fairy.NewImage(small)
			}
			if m.thumbKey != 0 {
				v.takeThumbnail(m)
			}
		})
	})
}

// shotThumbnail scales the centre of src that has the thumbnail's aspect down to the thumbnail's size, and returns
// it with its base64 PNG.
func shotThumbnail(src *image.NRGBA) (*image.NRGBA, string, error) {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width*shotThumbHeight > height*shotThumbWidth {
		width = height * shotThumbWidth / shotThumbHeight
	} else {
		height = width * shotThumbHeight / shotThumbWidth
	}
	x, y := bounds.Min.X+(bounds.Dx()-width)/2, bounds.Min.Y+(bounds.Dy()-height)/2
	small := image.NewNRGBA(image.Rect(0, 0, shotThumbWidth, shotThumbHeight))
	draw.CatmullRom.Scale(small, small.Bounds(), src, image.Rect(x, y, x+width, y+height), draw.Src, nil)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, small); err != nil {
		return nil, "", fmt.Errorf("encode shot thumbnail: %w", err)
	}
	return small, base64.StdEncoding.EncodeToString(encoded.Bytes()), nil
}

// image returns the shot's thumbnail, decoding it on first use, or nil when it has none.
func (s *WallpaperShot) image() *fairy.Image {
	if s.thumb != nil || s.Thumbnail == "" {
		return s.thumb
	}
	data, err := base64.StdEncoding.DecodeString(s.Thumbnail)
	if err == nil {
		var img image.Image
		if img, _, err = infra.DecodeImage(bytes.NewReader(data)); err == nil {
			s.thumb = fairy.NewImage(img)
			return s.thumb
		}
	}
	// An unreadable thumbnail is dropped; the next change to the shot takes a new one.
	s.Thumbnail = ""
	return nil
}

// sequenceWallpaper is the wallpaper that plays sequence on monitor, starting from its first shot.
func (v *ModelViewer) sequenceWallpaper(sequence WallpaperSequence, monitor string) ViewerWallpaper {
	w := v.wallpaperSnapshot(monitor)
	first := sequence.Shots[0]
	w.Camera, w.Form, w.Animation, w.Motion = first.Camera, first.Form, first.Animation, first.Motion
	state := maps.Clone(w.State)
	if state == nil {
		state = map[string]any{}
	}
	maps.Copy(state, first.State)
	w.State = state
	usesMotion := slices.ContainsFunc(sequence.Shots, func(s WallpaperShot) bool { return s.Motion != nil })
	w.MotionOff = v.motionDismissed && !usesMotion
	w.Sequence, w.Shots = sequence.Name, cloneShots(sequence.Shots)
	for i := range w.Shots {
		w.Shots[i].Thumbnail, w.Shots[i].thumb = "", nil
	}
	return w
}

func (v *ModelViewer) setSequenceWallpaper() {
	m := v.maker
	host := v.shell.deps.Wallpaper
	if m == nil || host == nil || len(m.draft.Shots) == 0 {
		return
	}
	if err := host.Set(v.sequenceWallpaper(m.draft, m.monitor)); err != nil {
		v.toasts.Show(kit.ToastError, T("page.tools.model_viewer.toast.wallpaper_error"), err.Error())
	}
}

// wallpaperMakerDialogs shows the Wallpaper Maker's naming and confirmation dialogs.
func (v *ModelViewer) wallpaperMakerDialogs(cx *fairy.Cx) {
	m := v.maker
	if m == nil {
		return
	}
	if c := m.confirm; c != nil {
		close := func() { m.confirm = nil }
		var opts []kit.Option
		if c.destructive {
			opts = append(opts, kit.Destructive)
		}
		confirm := kit.Button(c.action, func() {
			close()
			c.run()
		}, opts...)
		kit.ShowDialog(cx, kit.DialogProps{
			Title:       c.title,
			Description: c.description,
			Alert:       true,
			Actions:     []*fairy.El{kit.Button(T("g.cancel"), close, kit.Outline), confirm},
			OnClose:     close,
			Key:         "model-viewer.sequence.confirm",
		})
		return
	}
	n := m.naming
	if n == nil {
		return
	}
	close := func() { m.naming = nil }
	name := strings.TrimSpace(n.name)
	submit := func() {
		if name == "" {
			return
		}
		close()
		if n.rename {
			m.draft.Name = name
			m.dirty = true
			return
		}
		v.stopSequence()
		m.edit(WallpaperSequence{ID: newSequenceID(), Name: name})
	}
	title := T(sequencePrefix + "dialog.new_title")
	if n.rename {
		title = T(sequencePrefix + "dialog.rename_title")
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title: title,
		Body: kit.TextField(n.name, func(value string) { n.name = limitRunes(value, sequenceNameMaxRune) },
			kit.Label(T("g.name"))).AutoFocus().OnSubmit(func(string) { submit() }),
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), close, kit.Outline),
			kit.Button(T("g.save"), submit, kit.Disabled(name == "")),
		},
		OnClose: close,
		Key:     "model-viewer.sequence.name",
	})
}
