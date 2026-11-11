package modelviewer

import (
	"errors"
	"fmt"
	"math"
)

// bindCyclicPackedKernel binds packed object vertices skinned by 3x4 bone matrices, interpolated between two
// pose frames. The shader works in Blender's axes, so vertices turn into them and back.
func bindCyclicPackedKernel(
	deformer ModelViewerComputeDeformerTransport,
	sources *computeSources,
) (computeKernel, error) {
	base, err := sources.packedVertices("cyclic packed base", deformer.Base, deformer.VertexCount)
	if err != nil {
		return nil, err
	}
	pose := deformer.Pose
	if pose == nil {
		return nil, errors.New("cyclic packed pose buffers are missing")
	}
	blend, frames, err := sources.loadPose("cyclic packed", pose, deformer.VertexCount, computeCyclicPoseStride)
	if err != nil {
		return nil, err
	}
	boneCount, frameCount := pose.BoneCount, pose.FrameCount
	for index, weight := range blend.weights {
		if bone := blend.bones[index]; weight != 0 && (bone < 0 || int(bone) >= boneCount) {
			return nil, fmt.Errorf("cyclic packed bone index %d is outside the pose buffer", bone)
		}
	}

	frame := newComputeFrame(deformer.VertexCount, false)
	return func(poseFrame, _ float64, _ []float64) (ComputeFrame, error) {
		if math.IsNaN(poseFrame) {
			return ComputeFrame{}, fmt.Errorf("invalid cyclic packed frame: %v", poseFrame)
		}
		frame0 := int(min(max(math.Floor(poseFrame), 0), float64(frameCount-1)))
		frame1 := min(frame0+1, frameCount-1)
		inter := 0.0
		if frame0 != frame1 {
			inter = poseFrame - float64(frame0)
		}
		for vertex := range deformer.VertexCount {
			source := base[vertex*computePackedVertexFloats:]
			x, y, z, w := float64(source[0]), -float64(source[2]), float64(source[1]), float64(source[3])
			nx, ny, nz := float64(source[4]), -float64(source[6]), float64(source[5])
			var rows [12]float64
			for influence := range 4 {
				weight := float64(blend.weights[vertex*4+influence])
				if weight == 0 {
					continue
				}
				bone := int(blend.bones[vertex*4+influence])
				// A sample without weight is not read: a NaN in an unused frame would spread through NaN * 0.
				for sample, factor := range [2]float64{weight * (1 - inter), weight * inter} {
					if factor == 0 {
						continue
					}
					matrix := frames[((frame0+sample*(frame1-frame0))*boneCount+bone)*12:]
					for i := range rows {
						rows[i] += float64(matrix[i]) * factor
					}
				}
			}
			at := vertex * 3
			frame.Positions[at] = float32(rows[0]*x + rows[1]*y + rows[2]*z + rows[3]*w)
			frame.Positions[at+1] = float32(rows[8]*x + rows[9]*y + rows[10]*z + rows[11]*w)
			frame.Positions[at+2] = float32(-(rows[4]*x + rows[5]*y + rows[6]*z + rows[7]*w))
			frame.Normals[at] = float32(rows[0]*nx + rows[1]*ny + rows[2]*nz)
			frame.Normals[at+1] = float32(rows[8]*nx + rows[9]*ny + rows[10]*nz)
			frame.Normals[at+2] = float32(-(rows[4]*nx + rows[5]*ny + rows[6]*nz))
		}
		normalizeComputeVectors(frame.Normals)
		return frame, nil
	}, nil
}

type computeShape struct {
	pass   ModelViewerComputeShapePass
	target []float32
}

// bindPackedDualQuaternionKernel binds packed vertices skinned by dual quaternions interpolated between two
// pose frames. The variants differ in the axes their shaders work in and in what they write back.
func bindPackedDualQuaternionKernel(
	deformer ModelViewerComputeDeformerTransport,
	sources *computeSources,
) (computeKernel, error) {
	pose := deformer.Pose
	invalid := errors.New("packed dual-quaternion descriptor is invalid")
	if pose == nil {
		return nil, invalid
	}
	variant := pose.DualQuaternionVariant
	object := variant == modelViewerObjectPoseVariant
	direct := variant == modelViewerObjectDirectPoseVariant
	efmiCompact := variant == modelViewerEFMICompactVariant
	// EFMI stores float positions and unit normals, so nothing is quantized to the 16-bit and 8-bit records
	// the other variants write back.
	efmi := variant == modelViewerEFMIArmatureVariant || efmiCompact
	if (variant != "" && !object && !direct && !efmi) || pose.BoneCount <= 0 || pose.FrameCount < 2 ||
		(!efmi && len(deformer.ShapePasses) != 0) || len(deformer.ShapeStages) != 0 {
		return nil, invalid
	}
	base, err := sources.packedVertices("packed dual-quaternion base", deformer.Base, deformer.VertexCount)
	if err != nil {
		return nil, err
	}
	shapes := make([]computeShape, len(deformer.ShapePasses))
	for index, pass := range deformer.ShapePasses {
		label := fmt.Sprintf("packed dual-quaternion shape target %d", index)
		target, err := sources.packedVertices(label, pass.Target, deformer.VertexCount)
		if err != nil {
			return nil, err
		}
		shapes[index] = computeShape{pass: pass, target: target}
	}
	blend, palette, err := sources.loadPose(
		"packed dual-quaternion", pose, deformer.VertexCount, computeDualQuaternionSize,
	)
	if err != nil {
		return nil, err
	}
	boneCount, frameCount := pose.BoneCount, pose.FrameCount
	// The shader fetches all four bones, including those without weight.
	for index, bone := range blend.bones {
		weight := float64(blend.weights[index])
		if bone < 0 || int(bone) >= boneCount || math.IsNaN(weight) || math.IsInf(weight, 0) {
			return nil, fmt.Errorf("invalid packed dual-quaternion influence at vertex %d: bone=%d", index/4, bone)
		}
	}

	// The direct and EFMI shaders seed the blend with their first sample as it is; the others turn every
	// sample into the hemisphere of the first.
	unsignedSeed := direct || efmi
	frame := newComputeFrame(deformer.VertexCount, false)
	weights := make([]float64, len(shapes))
	return func(poseFrame, phaseSeconds float64, _ []float64) (ComputeFrame, error) {
		if math.IsNaN(poseFrame) || math.IsInf(poseFrame, 0) || poseFrame < 0 {
			return ComputeFrame{}, fmt.Errorf("invalid packed dual-quaternion frame: %v", poseFrame)
		}
		for index, shape := range shapes {
			weights[index] = computeShapePassWeight(shape.pass, phaseSeconds)
		}
		at := min(poseFrame, float64(frameCount-1))
		frame0 := int(math.Floor(at))
		frame1 := min(frame0+1, frameCount-1)
		inter := at - float64(frame0)

		for vertex := range deformer.VertexCount {
			var sum [14]float64
			bones := blend.bones[vertex*4 : vertex*4+4]
			reference := palette[(frame0*boneCount+int(bones[0]))*14+6:]
			for influence, bone := range bones {
				weight := float64(blend.weights[vertex*4+influence])
				for sample, factor := range [2]float64{weight * (1 - inter), weight * inter} {
					record := palette[((frame0+sample*(frame1-frame0))*boneCount+int(bone))*14:]
					dot := float64(reference[0])*float64(record[6]) + float64(reference[1])*float64(record[7]) +
						float64(reference[2])*float64(record[8]) + float64(reference[3])*float64(record[9])
					// Legacy exports keep orthogonal samples; the others multiply by sign(dot), which drops
					// them when the dot is zero.
					signed := factor
					switch {
					case object || (unsignedSeed && (influence != 0 || sample != 0)):
						signed = factor * computeSign(dot)
					case dot < 0:
						signed = -factor
					}
					for component := range 6 {
						sum[component] += float64(record[component]) * factor
					}
					for component := 6; component < 14; component++ {
						sum[component] += float64(record[component]) * signed
					}
				}
			}
			length := math.Sqrt(sum[6]*sum[6] + sum[7]*sum[7] + sum[8]*sum[8] + sum[9]*sum[9])
			finite := true
			for _, value := range sum {
				finite = finite && !math.IsNaN(value) && !math.IsInf(value, 0)
			}
			if (!unsignedSeed && length < 1e-8) || !finite {
				return ComputeFrame{}, fmt.Errorf("invalid packed dual-quaternion pose at vertex %d", vertex)
			}
			if unsignedSeed {
				length = max(1e-6, length)
			}
			var q, d [4]float64
			for axis := range 4 {
				q[axis], d[axis] = sum[6+axis]/length, sum[10+axis]/length
			}
			m, translation := dualQuaternionTransform(q, d)

			source := base[vertex*computePackedVertexFloats:]
			baseX, baseY, baseZ := float64(source[0]), float64(source[1]), float64(source[2])
			for index, shape := range shapes {
				target := shape.target[vertex*computePackedVertexFloats:]
				baseX += (float64(target[0]) - float64(source[0])) * weights[index]
				baseY += (float64(target[1]) - float64(source[1])) * weights[index]
				baseZ += (float64(target[2]) - float64(source[2])) * weights[index]
			}
			// Direct exports keep the game's position axes but still convert the normals they read. EFMI
			// turns positions the opposite way around X from the legacy shaders.
			y, z := -baseZ, baseY
			switch {
			case direct:
				y, z = baseY, baseZ
			case efmi:
				y, z = baseZ, -baseY
			}
			posedX, posedY, posedZ := rotateCompute(m, baseX*sum[0]+sum[3], y*sum[1]+sum[4], z*sum[2]+sum[5])
			posedX, posedY, posedZ = posedX+translation[0], posedY+translation[1], posedZ+translation[2]
			// Compact EFMI shaders turn the normals with the positions; the older ones do not.
			ny, nz := -float64(source[6]), float64(source[5])
			if efmiCompact {
				ny, nz = float64(source[6]), -float64(source[5])
			}
			normalX, normalY, normalZ := rotateCompute(m, float64(source[4]), ny, nz)

			outY, outZ := posedZ, -posedY
			switch {
			case direct:
				outY, outZ = posedY, posedZ
			case efmi:
				outY, outZ = -posedZ, posedY
			}
			outNormalY, outNormalZ := normalZ, -normalY
			switch {
			case direct:
				outNormalY, outNormalZ = normalY, normalZ
			case efmiCompact:
				outNormalY, outNormalZ = -normalZ, normalY
			}
			dest := vertex * 3
			for axis, value := range [3]float64{posedX, outY, outZ} {
				if !efmi {
					rounded, err := roundPackedPosition(value)
					if err != nil {
						return ComputeFrame{}, err
					}
					value = rounded
				}
				frame.Positions[dest+axis] = float32(value)
			}
			for axis, value := range [3]float64{normalX, outNormalY, outNormalZ} {
				if !efmi {
					value = max(-128, min(127, math.Trunc(value)))
				}
				frame.Normals[dest+axis] = float32(value)
			}
		}
		normalizeComputeVectors(frame.Normals)
		return frame, nil
	}, nil
}

// roundPackedPosition rounds a position as f32tof16 does, to nearest with ties to even, so the viewer shows
// no more precision than the half-float record the shader writes.
func roundPackedPosition(value float64) (float64, error) {
	magnitude := math.Abs(float64(float32(value)))
	if math.IsNaN(magnitude) || magnitude >= 65520 {
		return 0, errors.New("packed dual-quaternion position exceeds finite float16 range")
	}
	if magnitude == 0 {
		return value, nil
	}
	step := math.Pow(2, max(-24, math.Floor(math.Log2(magnitude))-10))
	scaled := magnitude / step
	lower := math.Floor(scaled)
	rounded := math.Floor(scaled + 0.5)
	if scaled-lower == 0.5 {
		rounded = lower + math.Mod(lower, 2)
	}
	return computeSign(value) * rounded * step, nil
}

type computeShapeStage struct {
	stage        ModelViewerComputeShapeStage
	base, target []float32
}

// bindPackedShapeKernel binds packed vertices blended between a base and a target, through stages that
// follow one another in time.
func bindPackedShapeKernel(
	deformer ModelViewerComputeDeformerTransport,
	sources *computeSources,
) (computeKernel, error) {
	if deformer.Pose != nil {
		return nil, errors.New("packed shape deformer must not include pose buffers")
	}
	if len(deformer.ShapeStages) == 0 {
		return nil, errors.New("packed shape deformer has no stages")
	}
	stages := make([]computeShapeStage, len(deformer.ShapeStages))
	for index, stage := range deformer.ShapeStages {
		label := fmt.Sprintf("packed shape stage %d", index)
		base, err := sources.packedVertices(label+" base", stage.Base, deformer.VertexCount)
		if err != nil {
			return nil, err
		}
		target, err := sources.packedVertices(label+" target", stage.Target, deformer.VertexCount)
		if err != nil {
			return nil, err
		}
		if stage.Duration <= 0 || math.IsNaN(stage.Duration) {
			return nil, fmt.Errorf("%s duration is invalid", label)
		}
		stages[index] = computeShapeStage{stage: stage, base: base, target: target}
	}

	frame := newComputeFrame(deformer.VertexCount, false)
	return func(_, phaseSeconds float64, _ []float64) (ComputeFrame, error) {
		index, localTime := packedShapeStageAt(deformer.ShapeStages, phaseSeconds)
		current := stages[index]
		stage := current.stage
		weight := stage.Amplitude*
			math.Sin((stage.PhaseStart+localTime*stage.PhaseRate+stage.PhaseOffset)*stage.AngularScale) + stage.Bias
		for vertex := range deformer.VertexCount {
			source := vertex * computePackedVertexFloats
			for axis := range 3 {
				from, to := float64(current.base[source+axis]), float64(current.target[source+axis])
				frame.Positions[vertex*3+axis] = float32(from + (to-from)*weight)
				from, to = float64(current.base[source+4+axis]), float64(current.target[source+4+axis])
				frame.Normals[vertex*3+axis] = float32(from + (to-from)*weight)
			}
		}
		normalizeComputeVectors(frame.Normals)
		return frame, nil
	}, nil
}

// packedShapeStageAt returns the stage playing at phaseSeconds and the time into it. The last stage holds
// its end.
func packedShapeStageAt(stages []ModelViewerComputeShapeStage, phaseSeconds float64) (index int, localTime float64) {
	remaining := max(phaseSeconds, 0)
	last := len(stages) - 1
	for index, stage := range stages {
		if index == last || remaining < stage.Duration {
			return index, min(remaining, stage.Duration)
		}
		remaining -= stage.Duration
	}
	return last, stages[last].Duration
}
