package modelviewer

import "math"

// ShapeWavePlayback advances the phases of wave shape targets as the mod's accumulators do. They grow only
// while their wave's condition holds, at the speed the state sets then, so a phase depends on its history:
// the clip clock only tells how much time has passed, and a clip restarting does not rewind them. Scrubbing
// back runs them back.
type ShapeWavePlayback struct {
	vars    []string
	waves   map[string]ModelViewerShapeWave
	phases  map[string]float64
	clip    string
	time    float64
	started bool
}

// NewShapeWavePlayback returns the playback of the wave shape targets of meshes, or nil when they have none.
func NewShapeWavePlayback(meshes []ModelViewerMeshTransport) *ShapeWavePlayback {
	playback := &ShapeWavePlayback{waves: map[string]ModelViewerShapeWave{}, phases: map[string]float64{}}
	for _, mesh := range meshes {
		for _, target := range mesh.ShapeTargets {
			if _, seen := playback.waves[target.Var]; target.Wave == nil || seen {
				continue
			}
			playback.waves[target.Var] = *target.Wave
			playback.vars = append(playback.vars, target.Var)
		}
	}
	if len(playback.vars) == 0 {
		return nil
	}
	return playback
}

// Advance moves the phases to time seconds into clip, which lasts duration seconds, and returns them by phase
// variable. state is the viewer state the wave conditions and speeds are read from.
func (p *ShapeWavePlayback) Advance(clip string, time, duration float64, state map[string]any) map[string]any {
	elapsed := 0.0
	if p.started && clip == p.clip {
		elapsed = time - p.time
	}
	// A step of more than half the clip is the clock wrapping around its end.
	if elapsed < -duration/2 {
		elapsed += duration
	} else if elapsed > duration/2 {
		elapsed -= duration
	}
	p.clip, p.time, p.started = clip, time, true

	values := make(map[string]any, len(p.vars))
	for _, phaseVar := range p.vars {
		wave := p.waves[phaseVar]
		phase, known := p.phases[phaseVar]
		if !known {
			phase = viewerStateNumber(state, phaseVar)
		}
		if viewerEvalDNFSatisfied(wave.When, state) {
			speed := 1.0
			if wave.SpeedVar != "" {
				speed = viewerStateNumber(state, wave.SpeedVar)
			}
			phase += elapsed * wave.Rate * speed
		}
		p.phases[phaseVar] = phase
		values[phaseVar] = phase
	}
	return values
}

// viewerStateNumber reads a state variable as a number, with zero for a missing or non-numeric one.
func viewerStateNumber(state map[string]any, variable string) float64 {
	current, _ := viewerEvalLookup(state, variable)
	value, ok := anyToFloat(current)
	if !ok || math.IsNaN(value) {
		return 0
	}
	return value
}
