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
