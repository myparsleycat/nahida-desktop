package tools

import "testing"

func TestDetectModelViewerPresentAnimation(t *testing.T) {
	sections := parseModINI(`[Constants]
global $fps = 30
[Present]
$frame = (time * $fps) // 1
[TextureOverrideFrame]
if $frame == 0
drawindexed = 3, 0, 0
elif $frame == 1
drawindexed = 3, 3, 0
endif`)
	clips := detectModelViewerPresentAnimations(sections, collectModelViewerDefaultVariables(sections), nil)
	if len(clips) != 1 || clips[0].FPS != 30 || len(clips[0].Frames) != 2 || clips[0].Frames[1].Values["frame"] != float64(1) {
		t.Fatalf("clips = %#v", clips)
	}
}

func TestDetectModelViewerPresentAnimationRejectsExcessiveFrameSpan(t *testing.T) {
	sections := parseModINI(`[Constants]
global $fps = 30
[Present]
post $frame = time * $fps
[TextureOverrideFrame]
if $frame == 0
drawindexed = 3, 0, 0
elif $frame == 100000
drawindexed = 3, 3, 0
endif`)
	clips := detectModelViewerPresentAnimations(sections, collectModelViewerDefaultVariables(sections), nil)
	if len(clips) != 0 {
		t.Fatalf("clips = %#v", clips)
	}
}

func TestDetectModelViewerPresentAnimationTimeStartsAtZero(t *testing.T) {
	sections := parseModINI(`[Constants]
global $fps = 20
[Present]
$frame = time * $fps
[TextureOverrideFrame]
if $frame == 10
drawindexed = 3, 0, 0
elif $frame == 11
drawindexed = 3, 3, 0
endif`)
	clips := detectModelViewerPresentAnimations(sections, collectModelViewerDefaultVariables(sections), nil)
	if len(clips) != 1 || clips[0].Frames[0].Time != 0 || clips[0].Frames[1].Time != 0.05 || clips[0].Frames[0].Values["frame"] != float64(10) {
		t.Fatalf("clips = %#v", clips)
	}
}

func TestDetectModelViewerPresentAccumulatorAnimation(t *testing.T) {
	sections := parseModINI(`[Constants]
global $speed = 2
global $elapsed = 0
global $frame = 0
[Present]
if ($elapsed + (1 / $speed)) < 3
$elapsed = $elapsed + (1 / $speed)
else
$elapsed = 0
endif
$frame = $elapsed // 1
[TextureOverrideFrame]
if $frame == 0
drawindexed = 3, 0, 0
elif $frame == 2
drawindexed = 3, 3, 0
endif`)
	clips := detectModelViewerPresentAnimations(sections, collectModelViewerDefaultVariables(sections), nil)
	if len(clips) != 1 || clips[0].ID != "frame" || clips[0].FPS != 30 || clips[0].FrameStart != 0 || clips[0].FrameEnd != 3 || len(clips[0].Frames) != 4 {
		t.Fatalf("clips = %#v", clips)
	}
}

func TestDetectModelViewerIncrementalAnimationUsesFirstReset(t *testing.T) {
	sections := parseModINI(`[Constants]
global $speed = 2
global $tick = 0
global $frame = 0
[Present]
post $tick = $tick + 1
if $tick % $speed == 0
if $frame < 5
$frame = $frame + 1
$frame = 2
$frame = 0
endif
endif
[TextureOverrideFrame]
if $frame == 0
drawindexed = 3, 0, 0
elif $frame == 5
drawindexed = 3, 3, 0
endif`)
	clips := detectModelViewerIncrementalAnimations(sections, collectModelViewerDefaultVariables(sections), nil)
	if len(clips) != 1 || clips[0].FrameStart != 2 || clips[0].FrameEnd != 5 {
		t.Fatalf("clips = %#v", clips)
	}
}

func TestDetectModelViewerPresentAnimationKeepsFirstClipForID(t *testing.T) {
	sections := parseModINI(`[Constants]
global $fps = 20
global $speed = 2
global $tick = 0
global $frame = 0
[Present]
post $frame = time * $fps
post $tick = $tick + 1
if $tick % $speed == 0
if $frame < 5
$frame = $frame + 1
$frame = 2
endif
endif
[TextureOverrideFrame]
if $frame == 0
drawindexed = 3, 0, 0
elif $frame == 4
drawindexed = 3, 3, 0
endif`)
	clips := detectModelViewerPresentAnimations(sections, collectModelViewerDefaultVariables(sections), nil)
	if len(clips) != 1 || clips[0].FPS != 20 || clips[0].FrameStart != 0 || clips[0].FrameEnd != 4 {
		t.Fatalf("clips = %#v", clips)
	}
}

func TestModelViewerAnimationDomainExpandsFrameComparisonGates(t *testing.T) {
	sections := parseModINI(`[Constants]
global $fps = 30
global $frame = 0
[Present]
$frame = time * $fps
[TextureOverrideFrameGate]
if $frame < 3
drawindexed = 3, 0, 0
endif
[TextureOverrideFrameRange]
if $frame == 0
drawindexed = 3, 0, 0
elif $frame == 3
drawindexed = 3, 3, 0
endif`)
	variables := modelViewerDirectConditionVariables(sections, collectModelViewerDefaultVariables(sections))

	dnf := parseModelViewerConditionDNF("$frame < 3", nil, variables)
	if len(dnf) != 3 {
		t.Fatalf("frame gate DNF = %#v", dnf)
	}
	for index, group := range dnf {
		if len(group) != 1 || group[0].Var != "frame" || group[0].Value != modelViewerString(float64(index)) || group[0].Negate {
			t.Fatalf("frame gate DNF[%d] = %#v", index, group)
		}
	}
	if impossible := parseModelViewerConditionDNF("$frame > 3", nil, variables); len(impossible) != 0 {
		t.Fatalf("out-of-range frame gate = %#v", impossible)
	}
}
