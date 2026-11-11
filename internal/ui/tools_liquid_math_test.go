package ui

import (
	"math"
	"testing"
)

var (
	liquidYDown = vec3{0, -1, 0}
	liquidZDown = vec3{0, 0, -1}
)

func near(a, b vec3, tolerance float64) bool {
	return a.sub(b).length() < tolerance
}

func TestLiquidDirectionAnglesRoundTrip(t *testing.T) {
	for _, down := range []vec3{liquidYDown, liquidZDown} {
		for _, angles := range []liquidAngles{{30, 0}, {90, 135}, {150, 300}} {
			read := liquidDirectionToAngles(liquidAnglesToDirection(angles, down), down)
			if math.Abs(read.tilt-angles.tilt) > 1e-9 || math.Abs(read.azimuth-angles.azimuth) > 1e-9 {
				t.Errorf("down %v: %+v read back as %+v", down, angles, read)
			}
		}
	}
}

// Saved anchors store vectors, so the angles must measure from the game's down axis and turn the same way the
// React editor did.
func TestLiquidDirectionAnglesConventions(t *testing.T) {
	if tilt := liquidDirectionToAngles(vec3{0, -180, 0}, liquidYDown).tilt; tilt != 0 {
		t.Errorf("Y down tilt = %v, want 0", tilt)
	}
	if tilt := liquidDirectionToAngles(vec3{0, 0, -5}, liquidZDown).tilt; tilt != 0 {
		t.Errorf("Z down tilt = %v, want 0", tilt)
	}
	if tilt := liquidDirectionToAngles(vec3{0, 0, -1}, liquidYDown).tilt; math.Abs(tilt-90) > 1e-9 {
		t.Errorf("horizontal tilt = %v, want 90", tilt)
	}
	if quarter := liquidAnglesToDirection(liquidAngles{90, 90}, liquidZDown); math.Abs(quarter[1]-1) > 1e-9 {
		t.Errorf("Z down quarter turn = %v, want +Y", quarter)
	}
	if quarter := liquidAnglesToDirection(liquidAngles{90, 90}, liquidYDown); math.Abs(quarter[2]+1) > 1e-9 {
		t.Errorf("Y down quarter turn = %v, want -Z", quarter)
	}
}

// The expected points come from the React flowTrajectory and trajectoryPointAt this preview replaces.
func TestLiquidFlowTrajectoryMatchesReact(t *testing.T) {
	for _, tc := range []struct {
		force     float64
		jet       bool
		end, half vec3
	}{
		{
			force: 1,
			end:   vec3{0.3455185874840167, 0.14842359903108024, 0.05},
			half:  vec3{0.16588699782821437, 1.1513304788513923, 0.05},
		},
		{
			force: 2, jet: true,
			end:  vec3{2.100646912978448, 0.4810371864984058, 0.05},
			half: vec3{0.617101238174075, 1.2397287265375334, 0.05},
		},
	} {
		path := liquidFlowTrajectory(vec3{0.1, 1.2, 0.05}, vec3{0, 0.01, 0}, vec3{1, 0.2, 0}, liquidYDown, tc.force,
			tc.jet)
		if len(path.points) != 117 || !near(path.points[len(path.points)-1], tc.end, 1e-9) {
			t.Errorf("force %v jet %v: %d points ending at %v, want 117 ending at %v", tc.force, tc.jet,
				len(path.points), path.points[len(path.points)-1], tc.end)
		}
		if at := path.pointAt(0.1234); !near(at, tc.half, 1e-9) {
			t.Errorf("force %v jet %v: point at 0.1234 s = %v, want %v", tc.force, tc.jet, at, tc.half)
		}
	}
}
