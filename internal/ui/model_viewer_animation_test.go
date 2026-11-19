package ui

import (
	"testing"
	"time"

	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

func TestAdvanceAnimationFrame(t *testing.T) {
	clip := func(loop bool, loopStart int) *modelviewer.ModelViewerAnimationClip {
		return &modelviewer.ModelViewerAnimationClip{
			Loop: loop, LoopStart: loopStart, Frames: make([]modelviewer.ModelViewerAnimationFrame, 10),
		}
	}
	tests := []struct {
		name         string
		clip         *modelviewer.ModelViewerAnimationClip
		current      int
		steps        int
		wantFrame    int
		wantComplete bool
	}{
		{name: "loop wraps to the first frame", clip: clip(true, 0), current: 8, steps: 3, wantFrame: 1},
		{name: "intro plays before the loop section", clip: clip(true, 6), current: 2, steps: 3, wantFrame: 5},
		{name: "loop section wraps past the intro", clip: clip(true, 6), current: 8, steps: 3, wantFrame: 7},
		{name: "loop section wraps more than once", clip: clip(true, 6), current: 9, steps: 9, wantFrame: 6},
		{name: "once stops at the last frame", clip: clip(false, 0), current: 8, steps: 3, wantFrame: 9,
			wantComplete: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame, _, complete := advanceAnimationFrame(tt.current, time.Duration(tt.steps)*time.Second, 1, tt.clip)
			if frame != tt.wantFrame || complete != tt.wantComplete {
				t.Errorf("got %d, %v; want %d, %v", frame, complete, tt.wantFrame, tt.wantComplete)
			}
		})
	}
}

// A speed change keeps the share of the frame already played, so a clip drawn between frames goes on from where it
// is, and a mod clip waiting for frames to load keeps waiting rather than starting over.
func TestSetTrackSpeedKeepsFramePhase(t *testing.T) {
	clip := modelviewer.ModelViewerAnimationClip{
		ID: "clip", FPS: 10, Loop: true, Frames: make([]modelviewer.ModelViewerAnimationFrame, 10),
	}
	v := &ModelViewer{scene: &viewerScene{
		transport: modelviewer.ModelViewerTransport{Animations: []modelviewer.ModelViewerAnimationClip{clip}},
	}}
	warming := time.Now()
	v.anim.clipID, v.anim.playing, v.anim.warmSince = clip.ID, true, &warming
	v.anim.applied = time.Now().Add(-50 * time.Millisecond)

	v.setTrackSpeed(trackMod, 0.5)

	if phase := v.anim.phase(v.animationClip(), time.Now()); phase < 0.45 || phase > 0.6 {
		t.Errorf("phase after the speed change = %v, want about 0.5", phase)
	}
	if v.anim.warmSince != &warming {
		t.Error("the speed change restarted the wait for frames to load")
	}
}

func TestSetTrackPlayingRestartsFinishedOnceClip(t *testing.T) {
	t.Run("mod track", func(t *testing.T) {
		clip := modelviewer.ModelViewerAnimationClip{
			ID: "once", FPS: 30, Loop: false, Frames: make([]modelviewer.ModelViewerAnimationFrame, 3),
		}
		v := &ModelViewer{scene: &viewerScene{
			transport: modelviewer.ModelViewerTransport{Animations: []modelviewer.ModelViewerAnimationClip{clip}},
		}}
		v.anim.clipID, v.anim.frame, v.anim.playing = clip.ID, 2, false

		v.setTrackPlaying(trackMod, true)
		if !v.anim.playing || v.anim.frame != 0 || v.revision == 0 {
			t.Errorf("playing=%v frame=%d revision=%d; want true, 0, >0", v.anim.playing, v.anim.frame, v.revision)
		}
	})

	t.Run("motion track", func(t *testing.T) {
		motion := &modelviewer.CharacterMotion{
			FPS:   30,
			Clips: []modelviewer.CharacterMotionClip{{Name: "Skill", Frames: 4, Loop: false}},
		}
		scene := &viewerScene{}
		m := &viewerMotion{
			scene:  scene,
			motion: motion,
			pose:   motion.NewPose(),
			clips: []modelviewer.ModelViewerAnimationClip{{
				ID: "0", FPS: 30, Loop: false, Frames: make([]modelviewer.ModelViewerAnimationFrame, 4),
			}},
		}
		m.play.clipID, m.play.frame, m.play.playing = "0", 3, false
		v := &ModelViewer{scene: scene, charMotion: m}

		v.setTrackPlaying(trackMotion, true)
		if !m.play.playing || m.play.frame != 0 || v.revision == 0 {
			t.Errorf("playing=%v frame=%d revision=%d; want true, 0, >0", m.play.playing, m.play.frame, v.revision)
		}
	})
}

func TestSetTrackPlayingUpdatesPoseAndRevision(t *testing.T) {
	motion := &modelviewer.CharacterMotion{
		FPS:   1,
		Clips: []modelviewer.CharacterMotionClip{{Name: "idle", Frames: 4, Loop: true}},
	}
	scene := &viewerScene{}
	m := &viewerMotion{
		scene:  scene,
		motion: motion,
		pose:   motion.NewPose(),
		clips: []modelviewer.ModelViewerAnimationClip{{
			ID: "0", FPS: 1, Loop: true, Frames: make([]modelviewer.ModelViewerAnimationFrame, 4),
		}},
	}
	m.play.clipID, m.play.frame, m.play.playing = "0", 1, true
	m.play.applied = time.Now().Add(-500 * time.Millisecond)
	v := &ModelViewer{scene: scene, charMotion: m}

	if err := v.poseMotion(time.Now()); err != nil {
		t.Fatal(err)
	}
	if m.posedAt.t <= 0 {
		t.Fatalf("posedAt.t = %v, want mid-frame interpolation > 0 before pause", m.posedAt.t)
	}

	revBeforePause := v.revision
	v.setTrackPlaying(trackMotion, false)
	if m.play.playing || m.posedAt.t != 0 || v.revision <= revBeforePause {
		t.Errorf("after pause: playing=%v t=%v revision=%d (was %d); want false, 0, >%d",
			m.play.playing, m.posedAt.t, v.revision, revBeforePause, revBeforePause)
	}

	revBeforeResume := v.revision
	v.setTrackPlaying(trackMotion, true)
	if !m.play.playing || v.revision <= revBeforeResume {
		t.Errorf("after resume: playing=%v revision=%d (was %d); want true, >%d",
			m.play.playing, v.revision, revBeforeResume, revBeforeResume)
	}
}

func TestSeekTrackFrameClearsMotionFade(t *testing.T) {
	motion := &modelviewer.CharacterMotion{
		FPS:   60,
		Clips: []modelviewer.CharacterMotionClip{{Name: "idle", Frames: 4, Loop: true}},
	}
	scene := &viewerScene{}
	m := &viewerMotion{
		scene:  scene,
		motion: motion,
		pose:   motion.NewPose(),
		clips: []modelviewer.ModelViewerAnimationClip{{
			ID: "0", FPS: 60, Loop: true, Frames: make([]modelviewer.ModelViewerAnimationFrame, 4),
		}},
	}
	m.play.clipID = "0"
	v := &ModelViewer{scene: scene, charMotion: m}

	if err := v.poseMotion(time.Now()); err != nil {
		t.Fatal(err)
	}
	if !m.pose.Hold() {
		t.Fatal("Hold() returned false after pose evaluation")
	}
	m.fadeStart = time.Now()
	m.fadeStowed = []bool{true}

	v.setMotionFrame(1)
	if m.fadeStart.IsZero() || m.fadeStowed == nil || m.posedAt.hold <= 0 {
		t.Errorf("setMotionFrame cleared active clip fade: fadeStart=%v stowed=%v hold=%v",
			m.fadeStart, m.fadeStowed, m.posedAt.hold)
	}

	v.seekTrackFrame(trackMotion, 2)
	if !m.fadeStart.IsZero() || m.fadeStowed != nil || m.play.frame != 2 || m.posedAt.hold != 0 {
		t.Errorf("seekTrackFrame did not clear fade: fadeStart=%v stowed=%v frame=%d hold=%v",
			m.fadeStart, m.fadeStowed, m.play.frame, m.posedAt.hold)
	}
}
