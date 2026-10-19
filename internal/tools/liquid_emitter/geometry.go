package liquidemitter

import (
	"fmt"
	"math"

	"nahida.live/desktop/internal/infra"
)

type vec3 [3]float64

func (a vec3) sub(b vec3) vec3    { return vec3{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
func (a vec3) dot(b vec3) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
func (a vec3) scale(s float64) vec3 {
	return vec3{a[0] * s, a[1] * s, a[2] * s}
}

func (a vec3) cross(b vec3) vec3 {
	return vec3{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}

func (a vec3) unit() (vec3, bool) {
	length := math.Sqrt(a.dot(a))
	if length < 1e-10 || math.IsNaN(length) || math.IsInf(length, 0) {
		return vec3{}, false
	}
	return a.scale(1 / length), true
}

// surfaceProfile is one runtime profile: the 12-float GPU row and the offset
// in the emitter's right/up/forward frame.
type surfaceProfile struct {
	row    [12]float64
	offset vec3
}

// anchorTriangle is a picked face reordered to index buffer order, the order
// in which the runtime captures the live triangle.
type anchorTriangle struct {
	positions   [3]vec3
	barycentric [3]float64
}

// orderTriangle maps the viewer's corner order onto the index buffer order.
func orderTriangle(input ApplyInput, indexOrder [3]uint32) (anchorTriangle, error) {
	if len(input.FaceVertices) != 3 || len(input.Barycentric) != 3 || len(input.TrianglePositions) != 9 {
		return anchorTriangle{}, infra.ContractError(ErrorInvalidAnchor + ":shape")
	}
	if !finite(input.Barycentric...) || !finite(input.TrianglePositions...) {
		return anchorTriangle{}, infra.ContractError(ErrorInvalidAnchor + ":non-finite")
	}
	var sum float64
	for _, weight := range input.Barycentric {
		if weight < -1e-4 || weight > 1+1e-4 {
			return anchorTriangle{}, infra.ContractError(ErrorInvalidAnchor + ":barycentric")
		}
		sum += weight
	}
	if math.Abs(sum-1) > 1e-3 {
		return anchorTriangle{}, infra.ContractError(ErrorInvalidAnchor + ":barycentric")
	}

	var out anchorTriangle
	used := [3]bool{}
	for corner, vertex := range indexOrder {
		match := -1
		for candidate, picked := range input.FaceVertices {
			if picked == vertex && !used[candidate] {
				match = candidate
				break
			}
		}
		if match < 0 {
			return anchorTriangle{}, infra.ContractError(
				fmt.Sprintf("%s:%v!=%v", ErrorDrawMismatch, input.FaceVertices, indexOrder),
			)
		}
		used[match] = true
		out.barycentric[corner] = math.Max(0, input.Barycentric[match])
		out.positions[corner] = vec3{
			input.TrianglePositions[match*3],
			input.TrianglePositions[match*3+1],
			input.TrianglePositions[match*3+2],
		}
	}

	// The GPU loader rejects rows whose weights do not sum to 1 within 1e-3.
	total := out.barycentric[0] + out.barycentric[1] + out.barycentric[2]
	for corner := range out.barycentric {
		out.barycentric[corner] /= total
	}
	return out, nil
}

// buildSurfaceProfile ports AttachmentEditor apply_anchor: the emission axis
// becomes the profile's local down axis, expressed in the triangle's
// tangent/bitangent/normal frame so it follows the skinned surface.
func buildSurfaceProfile(triangle anchorTriangle, direction, offset vec3, priority float64) (surfaceProfile, error) {
	a, b, c := triangle.positions[0], triangle.positions[1], triangle.positions[2]
	tangent, okTangent := b.sub(a).unit()
	normal, okNormal := b.sub(a).cross(c.sub(a)).unit()
	if !okTangent || !okNormal {
		return surfaceProfile{}, infra.ContractError(ErrorInvalidAnchor + ":degenerate")
	}
	bitangent := normal.cross(tangent)

	down, ok := direction.unit()
	if !ok {
		return surfaceProfile{}, infra.ContractError(ErrorInvalidAnchor + ":direction")
	}
	up := down.scale(-1)
	reference := vec3{0, -1, 0}
	if math.Abs(up.dot(reference)) > .95 {
		reference = vec3{1, 0, 0}
	}
	right, _ := up.cross(reference).unit()
	forward, _ := right.cross(up).unit()

	if !finite(offset[:]...) || math.Sqrt(offset.dot(offset)) > maxOffsetMeters+1e-9 {
		return surfaceProfile{}, infra.ContractError(ErrorOffsetLimit)
	}

	var profile surfaceProfile
	profile.row[0] = priority
	copy(profile.row[1:4], triangle.barycentric[:])
	for axis, basis := range [3]vec3{tangent, bitangent, normal} {
		profile.row[4+axis] = forward.dot(basis)
		profile.row[8+axis] = up.dot(basis)
	}
	profile.offset = vec3{offset.dot(right), offset.dot(up), offset.dot(forward)}
	return profile, nil
}

func finite(values ...float64) bool {
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}

func toVec3(values []float64) (vec3, bool) {
	if len(values) != 3 || !finite(values...) {
		return vec3{}, false
	}
	return vec3{values[0], values[1], values[2]}, true
}
