package modelviewer

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

const (
	// Character shape and pose records pack a position, a normal and a float4 tangent. Object shape records
	// keep the position and normal and store a packed texcoord where the tangent would sit, so their tangent
	// stream stays as the mesh has it.
	computeShapePoseStride    = 40
	computeInlineObjectStride = 44
	// A blend record is four float32 weights followed by four int32 bones.
	computeBlendStride        = 32
	computePackedVertexStride = 20
	computePackedFloatStride  = 28
	// A decoded packed vertex is a float4 position and three normal components.
	computePackedVertexFloats = 7
	computeDualQuaternionSize = 56
	computeCyclicPoseStride   = 48
)

// ComputeFrame is the geometry a compute deformer gives its vertices: three floats for a position and for a
// normal, and four for a tangent. Tangents is nil for a kernel that writes none.
type ComputeFrame struct {
	Positions []float32
	Normals   []float32
	Tangents  []float32
}

type computeKernel func(poseFrame, phaseSeconds float64, groupWeights []float64) (ComputeFrame, error)

// ComputeDeformer runs the kernel of a compute deformer on the CPU, in place of the shaders the mod runs in
// game. Its frames share their buffers, so one goroutine uses it at a time.
type ComputeDeformer struct {
	vertexCount   int
	keepsTangents bool
	compute       computeKernel
}

// NewComputeDeformer reads the sources of deformer through read and binds the kernel of its kind.
func NewComputeDeformer(
	ctx context.Context,
	deformer ModelViewerComputeDeformerTransport,
	read func(context.Context, string) ([]byte, error),
) (*ComputeDeformer, error) {
	if deformer.VertexCount <= 0 {
		return nil, fmt.Errorf("compute deformer %s has no vertices", deformer.ID)
	}
	sources := &computeSources{ctx: ctx, read: read, cache: map[string][]byte{}}
	var (
		compute computeKernel
		err     error
	)
	switch deformer.Kind {
	case modelViewerMergedDeltaKind:
		compute, err = bindMergedDeltaKernel(deformer, sources)
	case modelViewerPackedShapeKind:
		compute, err = bindPackedShapeKernel(deformer, sources)
	case modelViewerPackedObjectKind:
		compute, err = bindCyclicPackedKernel(deformer, sources)
	case modelViewerPackedDualQuaternionKind:
		compute, err = bindPackedDualQuaternionKernel(deformer, sources)
	case modelViewerGIMIShapePoseKind:
		var kernel *shapePoseKernel
		if kernel, err = bindShapePoseKernel(deformer, sources); err == nil {
			compute = func(poseFrame, phaseSeconds float64, _ []float64) (ComputeFrame, error) {
				return kernel.compute(poseFrame, phaseSeconds)
			}
		}
	default:
		err = fmt.Errorf("compute deformer kind %q has no kernel", deformer.Kind)
	}
	if err != nil {
		return nil, fmt.Errorf("compute deformer %s: %w", deformer.ID, err)
	}
	return &ComputeDeformer{
		vertexCount:   deformer.VertexCount,
		keepsTangents: deformer.Kind == modelViewerPackedDualQuaternionKind,
		compute:       compute,
	}, nil
}

// VertexCount is the number of vertices a frame holds.
func (d *ComputeDeformer) VertexCount() int { return d.vertexCount }

// KeepsTangents reports a kernel that leaves the tangents of its meshes as they are. The other kernels that
// write no tangents replace a stream whose tangents no longer fit the frame.
func (d *ComputeDeformer) KeepsTangents() bool { return d.keepsTangents }

// Frame computes pose frame poseFrame with the shape passes at phaseSeconds and the merged shape groups at
// groupWeights. The frame is valid until the next call.
func (d *ComputeDeformer) Frame(poseFrame int, phaseSeconds float64, groupWeights []float64) (ComputeFrame, error) {
	return d.compute(float64(poseFrame), phaseSeconds, groupWeights)
}

// computeSources reads each source of a deformer once.
type computeSources struct {
	ctx   context.Context
	read  func(context.Context, string) ([]byte, error)
	cache map[string][]byte
}

func (s *computeSources) bytes(url string) ([]byte, error) {
	if data, ok := s.cache[url]; ok {
		return data, nil
	}
	data, err := s.read(s.ctx, url)
	if err != nil {
		return nil, err
	}
	s.cache[url] = data
	return data, nil
}

// load reads a source and checks it against the size and stride its descriptor declares.
func (s *computeSources) load(label string, source ModelViewerComputeBinarySource) ([]byte, error) {
	if source.Stride <= 0 || source.ByteLength <= 0 || source.ByteLength%int64(source.Stride) != 0 {
		return nil, fmt.Errorf("%s descriptor is invalid", label)
	}
	data, err := s.bytes(source.URL)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	if int64(len(data)) != source.ByteLength {
		return nil, fmt.Errorf("%s size changed: expected %d, received %d", label, source.ByteLength, len(data))
	}
	return data, nil
}

// packedVertices loads a packed vertex source as seven floats per vertex. The backend serves most of them
// decoded; the rest still hold half-float positions and signed byte normals.
func (s *computeSources) packedVertices(
	label string,
	source ModelViewerComputeBinarySource,
	vertexCount int,
) ([]float32, error) {
	data, err := s.load(label, source)
	if err != nil {
		return nil, err
	}
	decoded := source.Encoding == modelViewerPackedFloatEncoding
	stride := computePackedVertexStride
	if decoded {
		stride = computePackedFloatStride
	}
	if source.Stride != stride || len(data) != vertexCount*stride {
		return nil, fmt.Errorf("%s must use a %d-byte vertex stride with the declared vertex count", label, stride)
	}
	if decoded {
		return computeFloats(data), nil
	}
	values := make([]float32, vertexCount*computePackedVertexFloats)
	for vertex := range vertexCount {
		record := data[vertex*stride:]
		at := values[vertex*computePackedVertexFloats:]
		for axis := range 4 {
			at[axis] = modelViewerHalfToFloat(binary.LittleEndian.Uint16(record[axis*2:]))
		}
		for axis := range 3 {
			at[4+axis] = float32(int8(record[8+axis]))
		}
	}
	return values, nil
}

func computeFloats(data []byte) []float32 {
	values := make([]float32, len(data)/4)
	for i := range values {
		values[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return values
}

// computeBlend holds four weights and four bones per vertex.
type computeBlend struct {
	weights []float32
	bones   []int32
}

func decodeComputeBlend(data []byte) computeBlend {
	vertices := len(data) / computeBlendStride
	blend := computeBlend{weights: make([]float32, vertices*4), bones: make([]int32, vertices*4)}
	for vertex := range vertices {
		record := data[vertex*computeBlendStride:]
		for i := range 4 {
			blend.weights[vertex*4+i] = math.Float32frombits(binary.LittleEndian.Uint32(record[i*4:]))
			blend.bones[vertex*4+i] = int32(binary.LittleEndian.Uint32(record[16+i*4:]))
		}
	}
	return blend
}

// loadPose loads the blend records and the pose frames of stride frameStride bytes a bone.
func (s *computeSources) loadPose(
	label string,
	pose *ModelViewerComputePoseSource,
	vertexCount, frameStride int,
) (computeBlend, []float32, error) {
	if pose.BoneCount <= 0 || pose.FrameCount <= 0 {
		return computeBlend{}, nil, fmt.Errorf("%s frame buffer dimensions are invalid", label)
	}
	blend, err := s.load(label+" pose blend", pose.Blend)
	if err != nil {
		return computeBlend{}, nil, err
	}
	if pose.Blend.Stride != computeBlendStride || len(blend) != vertexCount*computeBlendStride {
		return computeBlend{}, nil, fmt.Errorf("%s blend buffer must use a 32-byte vertex stride", label)
	}
	frames, err := s.load(label+" pose frames", pose.Frames)
	if err != nil {
		return computeBlend{}, nil, err
	}
	if pose.Frames.Stride != frameStride ||
		int64(len(frames)) != int64(pose.FrameCount)*int64(pose.BoneCount)*int64(frameStride) {
		return computeBlend{}, nil, fmt.Errorf("%s frame buffer dimensions are invalid", label)
	}
	return decodeComputeBlend(blend), computeFloats(frames), nil
}

func newComputeFrame(vertexCount int, tangents bool) ComputeFrame {
	frame := ComputeFrame{Positions: make([]float32, vertexCount*3), Normals: make([]float32, vertexCount*3)}
	if tangents {
		frame.Tangents = make([]float32, vertexCount*4)
	}
	return frame
}

func normalizeComputeVectors(vectors []float32) {
	for at := 0; at+2 < len(vectors); at += 3 {
		x, y, z := float64(vectors[at]), float64(vectors[at+1]), float64(vectors[at+2])
		if length := math.Sqrt(x*x + y*y + z*z); length > 1e-8 {
			vectors[at], vectors[at+1], vectors[at+2] = float32(x/length), float32(y/length), float32(z/length)
		}
	}
}

// computeSign is JavaScript's Math.sign, which the kernels were written against: zero stays zero.
func computeSign(value float64) float64 {
	switch {
	case value > 0:
		return 1
	case value < 0:
		return -1
	default:
		return value
	}
}

func computeShapePassWeight(pass ModelViewerComputeShapePass, phaseSeconds float64) float64 {
	phase := max(phaseSeconds, 0) * pass.PhaseRate
	if pass.WrapAt != 0 && pass.WrapAt > pass.PhaseStart {
		phase = math.Mod(phase, pass.WrapAt-pass.PhaseStart)
	}
	return pass.Amplitude*math.Sin((pass.PhaseStart+phase+pass.PhaseOffset)*pass.AngularScale) + pass.Bias
}

// dualQuaternionTransform returns the rotation rows and the translation of a unit dual quaternion with real
// part q and dual part d.
func dualQuaternionTransform(q, d [4]float64) (m [9]float64, t [3]float64) {
	qx, qy, qz, qw := q[0], q[1], q[2], q[3]
	dx, dy, dz, dw := d[0], d[1], d[2], d[3]
	m = [9]float64{
		1 - 2*qy*qy - 2*qz*qz, 2 * (qx*qy - qw*qz), 2 * (qx*qz + qw*qy),
		2 * (qx*qy + qw*qz), 1 - 2*qx*qx - 2*qz*qz, 2 * (qy*qz - qw*qx),
		2 * (qx*qz - qw*qy), 2 * (qy*qz + qw*qx), 1 - 2*qx*qx - 2*qy*qy,
	}
	t = [3]float64{
		2 * (-dw*qx + dx*qw - dy*qz + dz*qy),
		2 * (-dw*qy + dx*qz + dy*qw - dz*qx),
		2 * (-dw*qz - dx*qy + dy*qx + dz*qw),
	}
	return m, t
}

func rotateCompute(m [9]float64, x, y, z float64) (float64, float64, float64) {
	return m[0]*x + m[1]*y + m[2]*z, m[3]*x + m[4]*y + m[5]*z, m[6]*x + m[7]*y + m[8]*z
}

// shapePoseKernel blends shape targets over a base and skins the result with dual quaternions.
type shapePoseKernel struct {
	vertexCount  int
	strideFloats int
	base         []float32
	passes       []ModelViewerComputeShapePass
	targets      [][]float32
	// pose is nil for a deformer that only blends shapes.
	pose       []float32
	blend      computeBlend
	boneCount  int
	frameCount int
	frame      ComputeFrame
}

func bindShapePoseKernel(
	deformer ModelViewerComputeDeformerTransport,
	sources *computeSources,
) (*shapePoseKernel, error) {
	base, err := sources.load("shape/pose base", deformer.Base)
	if err != nil {
		return nil, err
	}
	stride := deformer.Base.Stride
	if (stride != computeShapePoseStride && stride != computeInlineObjectStride) ||
		len(base) != deformer.VertexCount*stride {
		return nil, errors.New("shape/pose base buffer must use a 40-byte or 44-byte vertex stride")
	}
	if deformer.Pose != nil && stride != computeShapePoseStride {
		return nil, errors.New("shape/pose buffers with pose data must use a 40-byte vertex stride")
	}
	kernel := &shapePoseKernel{
		vertexCount:  deformer.VertexCount,
		strideFloats: stride / 4,
		base:         computeFloats(base),
		passes:       deformer.ShapePasses,
		frame:        newComputeFrame(deformer.VertexCount, stride == computeShapePoseStride),
	}
	for index, pass := range deformer.ShapePasses {
		label := fmt.Sprintf("shape/pose target %d", index)
		target, err := sources.load(label, pass.Target)
		if err != nil {
			return nil, err
		}
		if pass.Target.Stride != stride || len(target) != len(base) {
			return nil, fmt.Errorf("%s is incompatible with the base buffer", label)
		}
		kernel.targets = append(kernel.targets, computeFloats(target))
	}
	if deformer.Pose == nil {
		return kernel, nil
	}

	kernel.blend, kernel.pose, err = sources.loadPose(
		"shape/pose", deformer.Pose, deformer.VertexCount, computeDualQuaternionSize,
	)
	if err != nil {
		return nil, err
	}
	kernel.boneCount, kernel.frameCount = deformer.Pose.BoneCount, deformer.Pose.FrameCount
	for _, bone := range kernel.blend.bones {
		if bone < 0 || int(bone) >= kernel.boneCount {
			return nil, fmt.Errorf("shape/pose bone index %d is outside the pose buffer", bone)
		}
	}
	return kernel, nil
}

func (k *shapePoseKernel) compute(poseFrame, phaseSeconds float64) (ComputeFrame, error) {
	if poseFrame < 0 || poseFrame != math.Trunc(poseFrame) || math.IsInf(poseFrame, 0) {
		return ComputeFrame{}, fmt.Errorf("invalid shape/pose frame: %v", poseFrame)
	}
	positions, normals, tangents := k.frame.Positions, k.frame.Normals, k.frame.Tangents
	for vertex := range k.vertexCount {
		source := k.base[vertex*k.strideFloats:]
		copy(positions[vertex*3:vertex*3+3], source[:3])
		copy(normals[vertex*3:vertex*3+3], source[3:6])
		if tangents != nil {
			copy(tangents[vertex*4:vertex*4+4], source[6:10])
		}
	}

	for index, pass := range k.passes {
		weight := computeShapePassWeight(pass, phaseSeconds)
		target := k.targets[index]
		for vertex := range k.vertexCount {
			source := vertex * k.strideFloats
			delta := func(offset int) float64 {
				return (float64(target[source+offset]) - float64(k.base[source+offset])) * weight
			}
			for axis := range 3 {
				positions[vertex*3+axis] = float32(float64(positions[vertex*3+axis]) + delta(axis))
				normals[vertex*3+axis] = float32(float64(normals[vertex*3+axis]) + delta(3+axis))
			}
			if tangents != nil {
				for axis := range 4 {
					tangents[vertex*4+axis] = float32(float64(tangents[vertex*4+axis]) + delta(6+axis))
				}
			}
		}
	}

	if k.pose == nil || tangents == nil {
		normalizeComputeVectors(normals)
		return k.frame, nil
	}
	k.applyPose(min(int(poseFrame), k.frameCount-1))
	return k.frame, nil
}

// applyPose skins every vertex with the blend of its four bones at frame. A pose record is a scale, a bias
// and the real and dual parts of a quaternion.
func (k *shapePoseKernel) applyPose(frame int) {
	positions, normals, tangents := k.frame.Positions, k.frame.Normals, k.frame.Tangents
	frameOffset := frame * k.boneCount
	for vertex := range k.vertexCount {
		weights, bones := k.blend.weights[vertex*4:vertex*4+4], k.blend.bones[vertex*4:vertex*4+4]
		reference := k.pose[(frameOffset+int(bones[0]))*14+6:]
		var scale, bias [3]float64
		var q, d [4]float64
		for influence := range 4 {
			record := k.pose[(frameOffset+int(bones[influence]))*14:]
			weight := float64(weights[influence])
			for axis := range 3 {
				scale[axis] += float64(record[axis]) * weight
				bias[axis] += float64(record[3+axis]) * weight
			}
			// The later bones take the hemisphere of the first, so opposite quaternions do not cancel.
			if influence != 0 {
				weight *= computeSign(
					float64(reference[0])*float64(record[6]) + float64(reference[1])*float64(record[7]) +
						float64(reference[2])*float64(record[8]) + float64(reference[3])*float64(record[9]),
				)
			}
			for axis := range 4 {
				q[axis] += float64(record[6+axis]) * weight
				d[axis] += float64(record[10+axis]) * weight
			}
		}
		length := max(math.Sqrt(q[0]*q[0]+q[1]*q[1]+q[2]*q[2]+q[3]*q[3]), 1e-6)
		for axis := range 4 {
			q[axis] /= length
			d[axis] /= length
		}
		m, translation := dualQuaternionTransform(q, d)

		at := vertex * 3
		x, y, z := rotateCompute(
			m,
			float64(positions[at])*scale[0]+bias[0],
			float64(positions[at+1])*scale[1]+bias[1],
			float64(positions[at+2])*scale[2]+bias[2],
		)
		positions[at] = float32(x + translation[0])
		positions[at+1] = float32(y + translation[1])
		positions[at+2] = float32(z + translation[2])

		nx, ny, nz := rotateCompute(m, float64(normals[at]), float64(normals[at+1]), float64(normals[at+2]))
		divisor := 1.0
		if normalLength := math.Sqrt(nx*nx + ny*ny + nz*nz); normalLength > 1e-8 {
			divisor = normalLength
		}
		normals[at], normals[at+1], normals[at+2] = float32(nx/divisor), float32(ny/divisor), float32(nz/divisor)

		tangent := vertex * 4
		tx, ty, tz := rotateCompute(
			m, float64(tangents[tangent]), float64(tangents[tangent+1]), float64(tangents[tangent+2]),
		)
		tangents[tangent], tangents[tangent+1], tangents[tangent+2] = float32(tx), float32(ty), float32(tz)
	}
}

// bindMergedDeltaKernel binds shape keys merged into one delta buffer: a (first record, record count) pair
// per key, then records of a vertex and its position delta. Only positions change, as in the shader.
func bindMergedDeltaKernel(
	deformer ModelViewerComputeDeformerTransport,
	sources *computeSources,
) (computeKernel, error) {
	merged := deformer.MergedDeltas
	if merged == nil {
		return nil, errors.New("merged shape delta deformer is invalid")
	}
	if deformer.Base.Stride != computeShapePoseStride {
		return nil, errors.New("merged shape delta base buffer must use a 40-byte vertex stride")
	}
	rest := deformer
	rest.ShapePasses, rest.Pose = nil, nil
	baseKernel, err := bindShapePoseKernel(rest, sources)
	if err != nil {
		return nil, err
	}
	base, err := baseKernel.compute(0, 0)
	if err != nil {
		return nil, err
	}

	data, err := sources.bytes(merged.URL)
	if err != nil {
		return nil, fmt.Errorf("merged shape deltas: %w", err)
	}
	const recordWords = 4
	words := len(data) / 4
	header := len(merged.Keys) * 2
	mismatch := errors.New("merged shape delta buffer does not match its key table")
	if len(data)%4 != 0 || words < header {
		return nil, mismatch
	}
	word := func(index int) uint32 { return binary.LittleEndian.Uint32(data[index*4:]) }
	records := (words - header) / recordWords
	total := uint64(0)
	for key := range merged.Keys {
		first, count := uint64(word(key*2)), uint64(word(key*2+1))
		if first+count > uint64(records) {
			return nil, mismatch
		}
		total += count
	}
	if uint64(words) != uint64(header)+total*recordWords {
		return nil, mismatch
	}

	positions := make([]float32, len(base.Positions))
	vertexCount := uint32(deformer.VertexCount)
	return func(_, _ float64, groupWeights []float64) (ComputeFrame, error) {
		copy(positions, base.Positions)
		for key, entry := range merged.Keys {
			weight := mergedDeltaKeyWeight(entry, groupWeights)
			if weight == 0 {
				continue
			}
			first := header + int(word(key*2))*recordWords
			end := first + int(word(key*2+1))*recordWords
			for record := first; record < end; record += recordWords {
				vertex := word(record)
				if vertex >= vertexCount {
					continue
				}
				for axis := range 3 {
					at := int(vertex)*3 + axis
					delta := float64(math.Float32frombits(word(record + 1 + axis)))
					positions[at] = float32(float64(positions[at]) + delta*weight)
				}
			}
		}
		return ComputeFrame{Positions: positions, Normals: base.Normals, Tangents: base.Tangents}, nil
	}, nil
}

func mergedDeltaKeyWeight(key ModelViewerComputeDeltaKey, groupWeights []float64) float64 {
	if key.Group < 0 {
		return key.Weight
	}
	group := 0.0
	if key.Group < len(groupWeights) {
		group = groupWeights[key.Group]
	}
	return min(max(group*key.Scale-key.Offset, 0), 1)
}
