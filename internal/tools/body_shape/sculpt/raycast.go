package sculpt

import "math"

// RayHit is the nearest triangle a ray crosses, in the mesh space of the
// positions it was cast against.
type RayHit struct {
	Point    Vec3
	Distance float64
	// FaceIndex is the triangle number; A, B and C are its vertex indices.
	FaceIndex int
	A, B, C   uint32
	// FaceNormal is the unit geometric normal of the triangle in winding
	// order, not flipped toward the ray.
	FaceNormal Vec3
}

// Vertices returns the hit triangle's vertex indices.
func (h RayHit) Vertices() []uint32 {
	return []uint32{h.A, h.B, h.C}
}

// Raycast returns the nearest triangle hit in front of the ray origin, testing
// both faces as a double-sided material does. Without indices, consecutive
// vertex triples are triangles. The direction need not be normalized; Distance
// is measured in mesh units.
func Raycast(positions []float32, indices []uint32, origin, direction Vec3) (RayHit, bool) {
	vertexCount := len(positions) / 3
	var best RayHit
	found := false
	test := func(face int, a, b, c uint32) {
		if int(a) >= vertexCount || int(b) >= vertexCount || int(c) >= vertexCount {
			return
		}
		pa := vertexOffset(positions, int(a), Vec3{})
		pb := vertexOffset(positions, int(b), Vec3{})
		pc := vertexOffset(positions, int(c), Vec3{})
		point, ok := intersectTriangle(origin, direction, pa, pb, pc)
		if !ok {
			return
		}
		offset := Vec3{point[0] - origin[0], point[1] - origin[1], point[2] - origin[2]}
		distance := math.Sqrt(offset.dot(offset))
		if found && distance >= best.Distance {
			return
		}
		found = true
		best = RayHit{
			Point:      point,
			Distance:   distance,
			FaceIndex:  face,
			A:          a,
			B:          b,
			C:          c,
			FaceNormal: triangleNormal(pa, pb, pc),
		}
	}

	if indices != nil {
		for i := 0; i+2 < len(indices); i += 3 {
			test(i/3, indices[i], indices[i+1], indices[i+2])
		}
	} else {
		for i := 0; i+2 < vertexCount; i += 3 {
			test(i/3, uint32(i), uint32(i+1), uint32(i+2))
		}
	}
	return best, found
}

// intersectTriangle is the watertight ray/triangle test of Woop, Benthin and
// Wald (JCGT 2013) without backface culling, as three.js Ray.intersectTriangle:
// a ray through a shared edge always hits one of the two triangles.
func intersectTriangle(origin, direction, a, b, c Vec3) (Vec3, bool) {
	ao := Vec3{a[0] - origin[0], a[1] - origin[1], a[2] - origin[2]}
	bo := Vec3{b[0] - origin[0], b[1] - origin[1], b[2] - origin[2]}
	co := Vec3{c[0] - origin[0], c[1] - origin[1], c[2] - origin[2]}

	// Project along the dominant direction axis kz; kx and ky swap when that
	// component is negative to preserve the winding.
	adx, ady, adz := math.Abs(direction[0]), math.Abs(direction[1]), math.Abs(direction[2])
	var kx, ky, kz int
	switch {
	case adx >= ady && adx >= adz:
		kz = 0
		kx, ky = 1, 2
		if direction[0] < 0 {
			kx, ky = 2, 1
		}
	case ady >= adz:
		kz = 1
		kx, ky = 2, 0
		if direction[1] < 0 {
			kx, ky = 0, 2
		}
	default:
		kz = 2
		kx, ky = 0, 1
		if direction[2] < 0 {
			kx, ky = 1, 0
		}
	}

	dkz := direction[kz]
	if dkz == 0 {
		return Vec3{}, false
	}
	sx, sy, sz := direction[kx]/dkz, direction[ky]/dkz, 1/dkz

	ax, ay := ao[kx]-sx*ao[kz], ao[ky]-sy*ao[kz]
	bx, by := bo[kx]-sx*bo[kz], bo[ky]-sy*bo[kz]
	cx, cy := co[kx]-sx*co[kz], co[ky]-sy*co[kz]

	u := cx*by - cy*bx
	v := ax*cy - ay*cx
	w := bx*ay - by*ax
	if (u < 0 || v < 0 || w < 0) && (u > 0 || v > 0 || w > 0) {
		return Vec3{}, false
	}

	det := u + v + w
	if det == 0 {
		return Vec3{}, false
	}
	tScaled := sz * (u*ao[kz] + v*bo[kz] + w*co[kz])
	if (det > 0 && tScaled < 0) || (det <= 0 && tScaled > 0) {
		return Vec3{}, false
	}
	t := tScaled / det
	return Vec3{origin[0] + direction[0]*t, origin[1] + direction[1]*t, origin[2] + direction[2]*t}, true
}

// triangleNormal is (c - b) × (a - b), normalized, as three.js Triangle.getNormal.
func triangleNormal(a, b, c Vec3) Vec3 {
	cb := Vec3{c[0] - b[0], c[1] - b[1], c[2] - b[2]}
	ab := Vec3{a[0] - b[0], a[1] - b[1], a[2] - b[2]}
	normal := Vec3{
		cb[1]*ab[2] - cb[2]*ab[1],
		cb[2]*ab[0] - cb[0]*ab[2],
		cb[0]*ab[1] - cb[1]*ab[0],
	}
	if lengthSq := normal.dot(normal); lengthSq > 0 {
		return normal.scaled(1 / math.Sqrt(lengthSq))
	}
	return Vec3{}
}

// NearestVertex returns the candidate closest to point, skipping candidates
// outside the positions; it picks the brush seed from a hit triangle.
func NearestVertex(positions []float32, candidates []uint32, point Vec3) (uint32, bool) {
	var nearest uint32
	nearestDistance := 0.0
	found := false
	for _, candidate := range candidates {
		if int(candidate)*3+2 >= len(positions) {
			continue
		}
		offset := vertexOffset(positions, int(candidate), point)
		distance := offset.dot(offset)
		if !found || distance < nearestDistance {
			nearest, nearestDistance, found = candidate, distance, true
		}
	}
	return nearest, found
}

// ModelSpaceUp is the mesh-space direction the viewport shows as up, for a
// model turned by orientation = (roll, pitch, yaw) in degrees, applied as a
// three.js Euler in YXZ order. The anatomy models use it as the direction of
// gravity.
func ModelSpaceUp(orientation Vec3) Vec3 {
	// The inverse rotation of +Y is the second row of the rotation matrix, which
	// yaw does not affect.
	rollRad := orientation[0] * math.Pi / 180
	pitchRad := orientation[1] * math.Pi / 180
	return Vec3{
		math.Cos(pitchRad) * math.Sin(rollRad),
		math.Cos(pitchRad) * math.Cos(rollRad),
		-math.Sin(pitchRad),
	}
}
