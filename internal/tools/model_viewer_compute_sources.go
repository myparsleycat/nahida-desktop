package tools

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"

	"nahida.live/desktop/internal/infra"
)

const modelViewerPackedFloatEncoding = "packed_f32_v1"

func (t *Tools) prepareModelViewerComputeSources(ctx context.Context, sessionID string, transport *ModelViewerTransport, meshes []modelViewerMeshPayload, cache *modelViewerPositionCache) error {
	if len(meshes) != len(transport.Meshes) {
		return fmt.Errorf("model viewer payload mesh count mismatch")
	}
	byID := make(map[string]modelViewerMeshPayload, len(meshes))
	for index, mesh := range transport.Meshes {
		byID[mesh.ID] = meshes[index]
	}
	for index := range transport.ComputeDeformers {
		if err := ctx.Err(); err != nil {
			return err
		}
		deformer := &transport.ComputeDeformers[index]
		if deformer.VertexCount <= 0 {
			return fmt.Errorf("invalid compute vertex count")
		}
		used := make([]bool, deformer.VertexCount)
		usedCount := 0
		for _, id := range deformer.MeshIDs {
			mesh, ok := byID[id]
			if !ok {
				return fmt.Errorf("compute deformer %s references missing mesh %s", deformer.ID, id)
			}
			for vertex := range len(mesh.Positions) / 3 {
				source := uint32(vertex)
				if mesh.SourceIndices != nil {
					if len(mesh.SourceIndices) != len(mesh.Positions)/3 {
						return fmt.Errorf("compute mesh %s source index count mismatch", id)
					}
					source = mesh.SourceIndices[vertex]
				}
				if uint64(source) >= uint64(deformer.VertexCount) {
					return fmt.Errorf("compute mesh %s index %d exceeds %d vertices", id, source, deformer.VertexCount)
				}
				if !used[source] {
					used[source] = true
					usedCount++
				}
			}
		}
		sources := make([]uint32, 0, usedCount)
		for source, included := range used {
			if included {
				sources = append(sources, uint32(source))
			}
		}
		originalCount := deformer.VertexCount
		if len(sources) < originalCount {
			lookup := make([]uint32, originalCount)
			for vertex, source := range sources {
				lookup[source] = uint32(vertex)
			}
			deformer.MeshSourceIndices = make(map[string]string, len(deformer.MeshIDs))
			for _, id := range deformer.MeshIDs {
				mesh := byID[id]
				remapped := make([]uint32, len(mesh.Positions)/3)
				for vertex := range remapped {
					source := uint32(vertex)
					if mesh.SourceIndices != nil {
						source = mesh.SourceIndices[vertex]
					}
					remapped[vertex] = lookup[source]
				}
				url, err := t.protocol.StoreMemoryBuffer(sessionID, deformer.ID+".indices."+id, modelViewerUint32Bytes(remapped), "application/octet-stream")
				if err != nil {
					return err
				}
				deformer.MeshSourceIndices[id] = url
			}
			deformer.VertexCount = len(sources)
		} else {
			sources = nil
		}
		registered := make(map[string]ModelViewerComputeBinarySource)
		register := func(source *ModelViewerComputeBinarySource, packed bool) error {
			original := *source
			if original.Stride <= 0 || original.ByteLength != int64(originalCount)*int64(original.Stride) {
				return fmt.Errorf("compute source dimensions changed: %s", original.sourcePath)
			}
			key := fmt.Sprintf("%s|%d|%t", original.sourcePath, original.Stride, packed)
			if hit, ok := registered[key]; ok {
				*source = hit
				return nil
			}
			if sources == nil && !packed {
				source.URL = t.protocol.LocalFileURL(original.sourcePath, true)
				registered[key] = *source
				return nil
			}
			stride := original.Stride
			if packed {
				stride = 28
				source.Encoding = modelViewerPackedFloatEncoding
			}
			source.Stride, source.ByteLength = stride, int64(deformer.VertexCount)*int64(stride)
			bufferID := fmt.Sprintf("%s.source.%d", deformer.ID, len(registered))
			vertexCount := deformer.VertexCount
			url, err := t.protocol.StoreMemoryLoader(sessionID, bufferID, func(ctx context.Context) (data []byte, err error) {
				defer func() {
					if err != nil && ctx.Err() == nil {
						err = infra.ReportError(t.log, err, "Tools.ModelViewerComputeSource", infra.Diagnostic{Operation: "prepare-compute-source", Stage: "decode", Fields: map[string]any{"memorySessionId": sessionID, "sourcePath": original.sourcePath, "sourceBytes": original.ByteLength, "bufferId": bufferID}})
					}
				}()
				return cache.load(ctx, bufferID, func(ctx context.Context) ([]byte, error) {
					return readModelViewerComputeRecords(ctx, original, sources, vertexCount, packed)
				})
			})
			if err != nil {
				return err
			}
			source.URL = url
			registered[key] = *source
			return nil
		}
		packed := deformer.Kind != "gimi_shape_pose_v1"
		if err := register(&deformer.Base, packed); err != nil {
			return err
		}
		for pass := range deformer.ShapePasses {
			if err := register(&deformer.ShapePasses[pass].Target, false); err != nil {
				return err
			}
		}
		for stage := range deformer.ShapeStages {
			if err := register(&deformer.ShapeStages[stage].Base, true); err != nil {
				return err
			}
			if err := register(&deformer.ShapeStages[stage].Target, true); err != nil {
				return err
			}
		}
		if deformer.Pose != nil {
			if err := register(&deformer.Pose.Blend, false); err != nil {
				return err
			}
			deformer.Pose.Frames.URL = t.protocol.LocalFileURL(deformer.Pose.Frames.sourcePath, true)
		}
		if t.log != nil {
			t.log.Info(fmt.Sprintf("Prepared compute inputs: deformer=%s vertices=%d compactVertices=%d packedDecoded=%t", deformer.ID, originalCount, deformer.VertexCount, packed), "Tools.ModelViewerComputeSource")
		}
	}
	return ctx.Err()
}

func readModelViewerComputeRecords(ctx context.Context, source ModelViewerComputeBinarySource, sources []uint32, vertexCount int, packed bool) ([]byte, error) {
	if source.Stride <= 0 || source.Stride > 64<<10 || (packed && source.Stride != 20) || vertexCount < 0 {
		return nil, fmt.Errorf("invalid compute source stride")
	}
	file, err := os.Open(source.sourcePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() != source.ByteLength {
		return nil, fmt.Errorf("compute source size changed: expected %d, received %d", source.ByteLength, info.Size())
	}
	stride := source.Stride
	if packed {
		stride = 28
	}
	output := make([]byte, vertexCount*stride)
	chunk := make([]byte, 64<<10)
	start, end := int64(-1), int64(-1)
	for vertex := range vertexCount {
		if vertex%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		index := uint64(vertex)
		if sources != nil {
			index = uint64(sources[vertex])
		}
		if index >= uint64(source.ByteLength/int64(source.Stride)) {
			return nil, fmt.Errorf("compute source index %d exceeds available records", index)
		}
		offset := int64(index) * int64(source.Stride)
		if offset < start || offset+int64(source.Stride) > end {
			n, readErr := file.ReadAt(chunk, offset)
			if readErr != nil && readErr != io.EOF {
				return nil, readErr
			}
			start, end = offset, offset+int64(n)
			if n < source.Stride {
				return nil, io.ErrUnexpectedEOF
			}
		}
		record := chunk[int(offset-start) : int(offset-start)+source.Stride]
		if !packed {
			copy(output[vertex*stride:], record)
			continue
		}
		for axis := range 7 {
			var value float32
			if axis < 4 {
				value = modelViewerHalfToFloat(binary.LittleEndian.Uint16(record[axis*2:]))
			} else {
				value = float32(int8(record[8+axis-4]))
			}
			binary.LittleEndian.PutUint32(output[vertex*stride+axis*4:], math.Float32bits(value))
		}
	}
	return output, ctx.Err()
}
