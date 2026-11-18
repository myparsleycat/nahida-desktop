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
