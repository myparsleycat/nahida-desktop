package touchprofile

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"nahida.live/desktop/internal/tools/modmesh"
)

// TouchMesh is a component's topology and blend data, read from the buffers a mesh descriptor names.
type TouchMesh struct {
	SessionID   string
	ComponentID string
	VertexCount int
	Positions   []float32
	Indices     []uint32
	Bones       []modmesh.BlendBoneInfo
	// BlendStride is 0 when the component has no blend buffer.
	BlendStride int
	BlendBytes  []byte
}

type TouchPreviewZone struct {
	TouchZoneSpec
	// Weights holds one mask weight per vertex; zones share one backing array.
	Weights []float32
}

type TouchPreview struct {
	SessionID   string
	ComponentID string
	VertexCount int
	Zones       []TouchPreviewZone
}

type bufferReader func(context.Context, string) ([]byte, error)

// TouchProfileMesh returns a component's topology for screens that render it in process.
func (t *Service) TouchProfileMesh(ctx context.Context, input TouchProfilePreviewInput) (TouchMesh, error) {
	descriptor, err := t.TouchProfileGetMeshDescriptor(ctx, input)
	if err != nil {
		return TouchMesh{}, err
	}
	return readTouchMesh(ctx, descriptor, t.protocol.Read)
}

// TouchProfilePreview returns the mask weights of a component's zones; mesh must come from the same session.
func (t *Service) TouchProfilePreview(
	ctx context.Context,
	input TouchProfilePreviewInput,
	mesh TouchMesh,
) (TouchPreview, error) {
	descriptor, err := t.TouchProfileGetPreviewDescriptor(ctx, input)
	if err != nil {
		return TouchPreview{}, err
	}
	return readTouchPreview(ctx, descriptor, mesh, t.protocol.Read)
}

func readTouchMesh(ctx context.Context, descriptor TouchMeshDescriptor, read bufferReader) (TouchMesh, error) {
	data, err := read(ctx, descriptor.PositionsURL)
	if err != nil {
		return TouchMesh{}, err
	}
	positions, err := modmesh.DecodeFloat32Bytes(data)
	if err != nil {
		return TouchMesh{}, err
	}
	if len(positions) != descriptor.PositionsCount {
		return TouchMesh{}, fmt.Errorf(
			"touch mesh positions hold %d values, want %d", len(positions), descriptor.PositionsCount,
		)
	}
	mesh := TouchMesh{
		SessionID:   descriptor.SessionID,
		ComponentID: descriptor.ComponentID,
		VertexCount: descriptor.VertexCount,
		Positions:   positions,
		Bones:       descriptor.Bones,
	}
	if mesh.Bones == nil {
		mesh.Bones = []modmesh.BlendBoneInfo{}
	}

	if descriptor.IndicesURL != nil {
		if data, err = read(ctx, *descriptor.IndicesURL); err != nil {
			return TouchMesh{}, err
		}
		if len(data) != descriptor.IndexCount*4 {
			return TouchMesh{}, fmt.Errorf(
				"touch mesh indices hold %d bytes, want %d", len(data), descriptor.IndexCount*4,
			)
		}
		mesh.Indices = make([]uint32, descriptor.IndexCount)
		for index := range mesh.Indices {
			mesh.Indices[index] = binary.LittleEndian.Uint32(data[index*4:])
		}
	}

	if descriptor.BlendURL != nil {
		if mesh.BlendBytes, err = read(ctx, *descriptor.BlendURL); err != nil {
			return TouchMesh{}, err
		}
		if len(mesh.BlendBytes) != descriptor.BlendBytes {
			return TouchMesh{}, fmt.Errorf(
				"touch mesh blend holds %d bytes, want %d", len(mesh.BlendBytes), descriptor.BlendBytes,
			)
		}
		if descriptor.BlendStride != nil {
			mesh.BlendStride = *descriptor.BlendStride
		}
	}
	return mesh, nil
}

func readTouchPreview(
	ctx context.Context,
	descriptor TouchProfilePreviewDescriptor,
	mesh TouchMesh,
	read bufferReader,
) (TouchPreview, error) {
	if mesh.SessionID != descriptor.SessionID || mesh.ComponentID != descriptor.ComponentID {
		return TouchPreview{}, errors.New("touch preview descriptor does not match the loaded topology")
	}
	data, err := read(ctx, descriptor.WeightsURL)
	if err != nil {
		return TouchPreview{}, err
	}
	weights, err := modmesh.DecodeFloat32Bytes(data)
	if err != nil {
		return TouchPreview{}, err
	}
	if len(weights) != descriptor.WeightsCount {
		return TouchPreview{}, fmt.Errorf(
			"touch preview weights hold %d values, want %d", len(weights), descriptor.WeightsCount,
		)
	}

	preview := TouchPreview{
		SessionID:   descriptor.SessionID,
		ComponentID: descriptor.ComponentID,
		VertexCount: descriptor.VertexCount,
		Zones:       make([]TouchPreviewZone, 0, len(descriptor.Zones)),
	}
	for _, zone := range descriptor.Zones {
		end := zone.WeightOffset + descriptor.VertexCount
		if zone.WeightOffset < 0 || end > len(weights) {
			return TouchPreview{}, fmt.Errorf("touch preview zone %s weights are out of range", zone.ID)
		}
		preview.Zones = append(preview.Zones, TouchPreviewZone{
			TouchZoneSpec: zone.TouchZoneSpec,
			Weights:       weights[zone.WeightOffset:end:end],
		})
	}
	return preview, nil
}

// BoneWeights returns each vertex's strongest weight for bone, with weights outside threshold set to 0.
func BoneWeights(mesh TouchMesh, bone uint32, threshold [2]float64) []float32 {
	weights := make([]float32, mesh.VertexCount)
	if mesh.BlendStride <= 0 {
		return weights
	}
	limit := min(mesh.VertexCount, len(mesh.BlendBytes)/mesh.BlendStride)
	for vertex := range limit {
		var weight float32
		modmesh.VisitBlendInfluences(mesh.BlendBytes, vertex*mesh.BlendStride, mesh.BlendStride,
			func(id uint32, value float32) {
				if id == bone && value > weight {
					weight = value
				}
			})
		if float64(weight) < threshold[0] || float64(weight) > threshold[1] {
			weight = 0
		}
		weights[vertex] = weight
	}
	return weights
}

// InputErrorCode names the reason a mod cannot be converted: "already_touch", "suspected_touch", or "".
func InputErrorCode(err error) string {
	if err == nil {
		return ""
	}
	switch message := err.Error(); {
	case strings.Contains(message, "TOUCH_PROFILE_INPUT_ALREADY_TOUCH"):
		return "already_touch"
	case strings.Contains(message, "TOUCH_PROFILE_INPUT_SUSPECTED_TOUCH"):
		return "suspected_touch"
	}
	return ""
}
