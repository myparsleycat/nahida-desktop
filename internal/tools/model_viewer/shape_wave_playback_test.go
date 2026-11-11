package modelviewer

import (
	"math"
	"testing"
)

func TestShapeWavePlayback(t *testing.T) {
	t.Parallel()

	meshes := []ModelViewerMeshTransport{{ShapeTargets: []ModelViewerShapeTarget{{
		Var: "freq",
		Wave: &ModelViewerShapeWave{
			SpeedVar: "speed", Rate: 1, AngularScale: 30, Amplitude: 0.5, Bias: 0.5,
			When: ModelViewerDNF{{{Var: "on", Value: "1"}}},
		},
	}}}}
	phase := func(t *testing.T, values map[string]any, want float64) {
		t.Helper()
		if got, _ := values["freq"].(float64); math.Abs(got-want) > 1e-9 {
			t.Fatalf("phase = %v, want %v", got, want)
		}
	}

	t.Run("advances only while the wave is on, at the speed set then", func(t *testing.T) {
		t.Parallel()
		playback := NewShapeWavePlayback(meshes)
		playback.Advance("clip", 0, 10, map[string]any{"on": 1, "speed": 0.5})
		phase(t, playback.Advance("clip", 1, 10, map[string]any{"on": 1, "speed": 0.5}), 0.5)
		phase(t, playback.Advance("clip", 2, 10, map[string]any{"on": 0, "speed": 0.5}), 0.5)
		phase(t, playback.Advance("clip", 3, 10, map[string]any{"on": 1, "speed": 0.25}), 0.75)
	})

	t.Run("keeps the phase when the clip loops", func(t *testing.T) {
		t.Parallel()
		playback := NewShapeWavePlayback(meshes)
		state := map[string]any{"on": 1, "speed": 1}
		playback.Advance("clip", 9.5, 10, state)
		phase(t, playback.Advance("clip", 0.5, 10, state), 1)
	})

	t.Run("returns to the same phase when scrubbed back", func(t *testing.T) {
		t.Parallel()
		playback := NewShapeWavePlayback(meshes)
		state := map[string]any{"on": 1, "speed": 1}
		playback.Advance("clip", 1, 10, state)
		playback.Advance("clip", 3, 10, state)
		phase(t, playback.Advance("clip", 1, 10, state), 0)
	})
}
