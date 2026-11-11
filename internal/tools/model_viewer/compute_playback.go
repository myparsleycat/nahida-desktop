package modelviewer

import "math"

const computeGameFramesPerSecond = 60

// ComputePlayback turns the frames of a clip into the times a compute deformer runs at. The shape passes of
// an EFMI deformer and the groups of a merged shape deformer run on their own timers in game: the clip clock
// only tells how much time has passed, and a clip restarting does not rewind them.
type ComputePlayback struct {
	// continuous marks shape passes that keep their phase across the restarts of a pose clip.
	continuous  bool
	clip        string
	clipTime    float64
	phaseOffset float64

	// groups play as the mod's lists do, one step per game frame, each at its own speed, so no clip length
	// loops them all. A seek moves them by its distance in either direction.
	groups     []ModelViewerComputeShapeGroup
	frames     []float64
	directions []float64
	// seen holds the weight each group's slider last had; NaN before the first.
	seen         []float64
	groupTime    float64
	started      bool
	pendingTicks float64
}

// NewComputePlayback returns the playback of deformer at the start of its timers.
func NewComputePlayback(deformer ModelViewerComputeDeformerTransport) *ComputePlayback {
	playback := &ComputePlayback{}
	if pose := deformer.Pose; pose != nil {
		playback.continuous = pose.DualQuaternionVariant == modelViewerEFMIArmatureVariant ||
			pose.DualQuaternionVariant == modelViewerEFMICompactVariant
	}
	if merged := deformer.MergedDeltas; merged != nil {
		playback.groups = merged.Groups
		playback.frames = make([]float64, len(merged.Groups))
		playback.directions = make([]float64, len(merged.Groups))
		playback.seen = make([]float64, len(merged.Groups))
		for i := range merged.Groups {
			playback.directions[i], playback.seen[i] = 1, math.NaN()
		}
	}
	return playback
}

// FollowsState reports a deformer whose frame changes with the viewer state alone, so a paused clip is
// computed again when the state changes.
func (p *ComputePlayback) FollowsState() bool { return len(p.groups) > 0 }

// Observe takes the weights the viewer state gives the groups. A group that is not playing moves to a weight
// its slider newly set; pausing keeps the pose until then.
func (p *ComputePlayback) Observe(state map[string]any) {
	for index, group := range p.groups {
		weight := computeStateNumber(state, group.WeightVar)
		// A weight that is not a number never equals the one seen before, and sets nothing.
		changed := weight != p.seen[index]
		p.seen[index] = weight
		if changed && computeStateNumber(state, group.AutoPlayVar) != 1 && !math.IsNaN(weight) &&
			!math.IsInf(weight, 0) {
			p.frames[index] = min(max(weight, 0), 1) * group.FrameEnd
		}
	}
}

// Advance moves to time seconds into clip, which lasts duration seconds. It returns the time the shape passes
// run at and the weight of each group.
func (p *ComputePlayback) Advance(
	clip string,
	time, duration float64,
	state map[string]any,
) (phaseSeconds float64, groupWeights []float64) {
	phaseSeconds = time
	if p.continuous {
		// A step back of under half the clip is a seek rather than the clip wrapping around its end.
		if clip == p.clip && p.clipTime-time >= duration/2 {
			p.phaseOffset += duration
		}
		p.clip, p.clipTime = clip, time
		phaseSeconds += p.phaseOffset
	}
	if len(p.groups) == 0 {
		return phaseSeconds, nil
	}

	p.Observe(state)
	elapsed := 0.0
	if p.started {
		elapsed = time - p.groupTime
	}
	if elapsed < -duration/2 {
		elapsed += duration
	} else if elapsed > duration/2 {
		elapsed -= duration
	}
	p.groupTime, p.started = time, true
	p.pendingTicks += math.Abs(elapsed) * computeGameFramesPerSecond
	// A clip frame is one tick, which the frame times only reach within rounding.
	ticks := math.Floor(p.pendingTicks + 1e-6)
	p.pendingTicks = max(p.pendingTicks-ticks, 0)

	groupWeights = make([]float64, len(p.groups))
	for index, group := range p.groups {
		if computeStateNumber(state, group.AutoPlayVar) == 1 {
			speed := computeStateNumber(state, group.SpeedVar)
			if math.IsNaN(speed) {
				speed = 0
			}
			for range int(ticks) {
				p.step(index, speed)
			}
		}
		groupWeights[index] = p.frames[index] / group.FrameEnd
	}
	return phaseSeconds, groupWeights
}

func (p *ComputePlayback) step(index int, speed float64) {
	group := p.groups[index]
	weight := p.frames[index] / group.FrameEnd
	step := 1.0
	for _, entry := range group.Steps {
		if weight < entry.Until {
			step = entry.Step
			break
		}
	}
	next := p.frames[index] + p.directions[index]*min(step*speed/100, group.StepLimit)
	if group.Wrap {
		if next > group.FrameEnd {
			next = 0
		}
		p.frames[index] = next
		return
	}
	if next >= group.FrameEnd {
		p.directions[index] = -1
	} else if next <= 0 {
		p.directions[index] = 1
	}
	p.frames[index] = min(max(next, 0), group.FrameEnd)
}

// computeStateNumber reads a state variable as a number, with NaN for a missing or non-numeric one.
func computeStateNumber(state map[string]any, variable string) float64 {
	current, ok := viewerEvalLookup(state, variable)
	if !ok {
		return math.NaN()
	}
	value, ok := anyToFloat(current)
	if !ok {
		return math.NaN()
	}
	return value
}
