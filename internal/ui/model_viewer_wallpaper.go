package ui

import (
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/platform"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

// wallpaperPausedTick is how often a paused wallpaper's mod animation checks whether it may play again.
const wallpaperPausedTick = 250 * time.Millisecond

// ViewerWallpaper is a model viewer shown as the desktop wallpaper: the mod and the view the user set, stored as
// JSON in the modelViewer.wallpaper setting.
type ViewerWallpaper struct {
	Path string `json:"path"`
	// Monitor is the device name of the display, such as \\.\DISPLAY1; empty is the primary display.
	Monitor     string          `json:"monitor,omitempty"`
	Camera      wallpaperCamera `json:"camera"`
	Orientation [3]float64      `json:"orientation"`
	Form        string          `json:"form,omitempty"`
	State       map[string]any  `json:"state,omitempty"`
	NormalMaps  bool            `json:"normalMaps"`
	DoubleSided bool            `json:"doubleSided"`
	MotionOff   bool            `json:"motionOff,omitempty"`
	// Animation is the playback of the mod's clips, and Motion that of the character's game motions.
	Animation *wallpaperTrack `json:"animation,omitempty"`
	Motion    *wallpaperTrack `json:"motion,omitempty"`
	// Shots, when set, play in a loop from the view above, which is the first shot's; Sequence is their name.
	Sequence string          `json:"sequence,omitempty"`
	Shots    []WallpaperShot `json:"shots,omitempty"`
}

// wallpaperTrack is the playback of one track: the clip, and the frame it stopped at when it does not play.
type wallpaperTrack struct {
	Clip    string  `json:"clip"`
	Frame   int     `json:"frame,omitempty"`
	Paused  bool    `json:"paused,omitempty"`
	Speed   float64 `json:"speed,omitempty"`
	Stepped bool    `json:"stepped,omitempty"`
}

type wallpaperCamera struct {
	Target   [3]float64 `json:"target"`
	Distance float64    `json:"distance"`
	Yaw      float64    `json:"yaw"`
	Pitch    float64    `json:"pitch"`
	Radius   float64    `json:"radius"`
	Center   [3]float64 `json:"center"`
	FOV      float64    `json:"fov,omitempty"`
}

// WallpaperHost shows a model viewer behind the desktop icons. The app implements it; call its methods on the UI
// thread.
type WallpaperHost interface {
	Set(ViewerWallpaper) error
	Clear()
	Active() bool
}

func (v *ModelViewer) wallpaperSnapshot(monitor string) ViewerWallpaper {
	w := ViewerWallpaper{
		Path:        v.path,
		Monitor:     monitor,
		Camera:      cameraSnapshot(v.camera),
		Orientation: v.orientation,
		Form:        v.form,
		NormalMaps:  v.normalMaps,
		DoubleSided: v.doubleSided,
		MotionOff:   v.motionDismissed,
		Animation:   v.wallpaperTrack(trackMod),
		Motion:      v.wallpaperTrack(trackMotion),
	}
	if v.scene != nil {
		w.State = toggleState(v.state, &v.scene.transport)
	}
	return w
}

// restoreWallpaper shows the view of w over the scene being installed, before its state is evaluated.
func (v *ModelViewer) restoreWallpaper(w ViewerWallpaper) {
	maps.Copy(v.state, w.State)
	v.form = w.Form
	v.orientation = w.Orientation
	if camera, ok := w.Camera.orbit(); ok {
		v.camera = camera
	}
}

func (v *ModelViewer) wallpaperTrack(track viewerTrack) *wallpaperTrack {
	p := v.playback(track)
	if p == nil || p.clipID == "" {
		return nil
	}
	return &wallpaperTrack{Clip: p.clipID, Frame: p.frame, Paused: !p.playing, Speed: p.speed, Stepped: p.stepped}
}

// restoreWallpaperTrack plays the clip of t on track, once the track's clips are loaded. A clip the mod or the
// motions no longer have leaves the track as it started.
func (v *ModelViewer) restoreWallpaperTrack(track viewerTrack, t *wallpaperTrack) {
	if t == nil {
		return
	}
	index := slices.IndexFunc(v.trackClips(track), func(clip modelviewer.ModelViewerAnimationClip) bool {
		return clip.ID == t.Clip
	})
	if index < 0 {
		return
	}
	if p := v.playback(track); p == nil || p.clipID != t.Clip {
		v.startTrack(track, t.Clip)
	}
	if t.Speed > 0 {
		v.setTrackSpeed(track, t.Speed)
	}
	v.setTrackStepped(track, t.Stepped)
	if t.Paused {
		v.setTrackPlaying(track, false)
		frames := len(v.trackClips(track)[index].Frames)
		v.seekTrackFrame(track, min(max(t.Frame, 0), max(frames-1, 0)))
	}
}

// NewWallpaperViewer makes a viewer that draws the mod of w with its view, for a window behind the desktop icons.
// Call it on the UI thread.
func (s *Shell) NewWallpaperViewer(w ViewerWallpaper) *ModelViewer {
	v := s.newModelViewer(w.Path, nil)
	v.wallpaper = &w
	// Nothing draws these toasts: the viewer's errors are logged, and they would otherwise show in the main window.
	v.toasts = &v.windowToasts
	v.normalMaps, v.doubleSided = w.NormalMaps, w.DoubleSided
	v.motionDismissed = w.MotionOff
	return v
}

// WallpaperView builds a wallpaper window: the viewport alone, which takes no input. The viewport clears to
// transparent, so the desktop's own wallpaper shows around the model in a window made with Options.Transparent.
func (v *ModelViewer) WallpaperView(cx *fairy.Cx) *fairy.El {
	v.window = cx.Window()
	v.syncFieldOfView()
	v.runSequence(time.Now())
	v.syncOriginal()
	v.syncMotion()
	v.syncToonAssets(v.toonStyle())
	opts := v.drawOptions([4]float32{})
	return fairy.Box(v.viewportSurface(cx, v.scene, opts)).WFull().HFull()
}

// SetPaused stops or resumes the wallpaper's animation.
func (v *ModelViewer) SetPaused(paused bool) {
	if v.wallpaperPaused == paused {
		return
	}
	v.wallpaperPaused = paused
	v.pauseSequence(paused)
	if !paused && v.window != nil {
		v.window.InvalidateSurface(modelViewerSurfaceKey)
		v.window.Update()
	}
}

func (v *ModelViewer) wallpaperMenuItems() []kit.MenuItem {
	if v.shell == nil || v.shell.deps.Wallpaper == nil || v.blender != nil {
		return nil
	}
	host := v.shell.deps.Wallpaper
	monitors, err := platform.Monitors()
	if err != nil {
		v.log.Warn("Model viewer could not list the monitors: "+err.Error(), "ModelViewer.wallpaper")
	}
	choices := make([]kit.MenuItem, 0, len(monitors))
	for i, m := range monitors {
		key := "page.tools.model_viewer.menu.wallpaper_monitor"
		if m.Primary {
			key += "_primary"
		}
		device := m.Device
		choices = append(choices, kit.MenuItem{
			Label: T(key, "index", strconv.Itoa(i+1), "width", strconv.Itoa(m.Bounds.Dx()),
				"height", strconv.Itoa(m.Bounds.Dy())),
			OnClick: func() { v.setWallpaper(host, device) },
		})
	}
	items := []kit.MenuItem{
		{
			Label: T("page.tools.model_viewer.menu.set_wallpaper"), Icon: lucide.Wallpaper,
			Disabled: v.scene == nil || len(choices) == 0, Submenu: choices,
		},
		{
			Label: T("page.tools.model_viewer.menu.wallpaper_maker"), Icon: lucide.Clapperboard,
			Disabled: v.scene == nil || v.tool != nil || v.maker != nil || v.shell.deps.Mod == nil,
			OnClick:  v.openWallpaperMaker,
		},
	}
	if host.Active() {
		items = append(items, kit.MenuItem{
			Label: T("page.tools.model_viewer.menu.remove_wallpaper"), Icon: lucide.MonitorOff, OnClick: host.Clear,
		})
	}
	return items
}

func (v *ModelViewer) setWallpaper(host WallpaperHost, monitor string) {
	if err := host.Set(v.wallpaperSnapshot(monitor)); err != nil {
		v.toasts.Show(kit.ToastError, T("page.tools.model_viewer.toast.wallpaper_error"), err.Error())
	}
}
