package modelviewer

import (
	"context"
	"fmt"
	"math"
	"slices"

	"nahida.live/desktop/internal/infra"
)

type modelViewerGeometryRecoveryStats struct {
	invalidVertices          int
	firstInvalidSourceVertex int
	removedTriangles         int
	discardedIndices         int
	omittedNormals           bool
	omittedTangents          bool
}

type modelViewerGeometryRecovery struct {
	geometry      *modelViewerGeometry
	stats         modelViewerGeometryRecoveryStats
	iniPaths      []string
	positionPaths []string
	meshIDs       []string
}

func recoverModelViewerGeometry(
	ctx context.Context,
	geometry *modelViewerGeometry,
) (*modelViewerGeometry, modelViewerGeometryRecoveryStats, error) {
	stats := modelViewerGeometryRecoveryStats{firstInvalidSourceVertex: -1}
	if geometry == nil || geometry.VertexCount < 0 || len(geometry.Position)%3 != 0 {
		return nil, stats, fmt.Errorf("invalid position attribute length")
	}
	count := len(geometry.Position) / 3
	if geometry.VertexCount != count ||
		(geometry.SourceIndices != nil && len(geometry.SourceIndices) != count) ||
		(geometry.sourceFaces != nil && len(geometry.sourceFaces) != len(geometry.Indices)/3) {
		return nil, stats, fmt.Errorf("invalid geometry attribute counts")
	}
	stats.discardedIndices = len(geometry.Indices) % 3
	for _, attribute := range []struct {
		name   string
		values []float32
		width  int
	}{
		{name: "normal", values: geometry.Normal, width: 3},
		{name: "tangent", values: geometry.Tangent, width: 4},
		{name: "texcoord", values: geometry.Texcoord0, width: 2},
		{name: "color", values: geometry.Color0, width: 4},
	} {
		if len(attribute.values) != 0 && len(attribute.values)/attribute.width != count ||
			len(attribute.values)%attribute.width != 0 {
			return nil, stats, fmt.Errorf("invalid %s attribute length", attribute.name)
		}
	}

	var invalid []bool
	for vertex := range count {
		if vertex%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, stats, err
			}
		}
		for _, value := range geometry.Position[vertex*3 : vertex*3+3] {
			if !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0) {
				continue
			}
			if invalid == nil {
				invalid = make([]bool, count)
				stats.firstInvalidSourceVertex = vertex
				if geometry.SourceIndices != nil {
					stats.firstInvalidSourceVertex = int(geometry.SourceIndices[vertex])
				}
			}
			invalid[vertex] = true
			stats.invalidVertices++
			break
		}
	}

	completeIndices := geometry.Indices[:len(geometry.Indices)-stats.discardedIndices]
	indices := completeIndices
	var sourceFaces []uint32
	for offset := 0; offset < len(completeIndices); offset += 3 {
		if offset%12288 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, stats, err
			}
		}
		triangle := completeIndices[offset : offset+3]
		remove := false
		for _, vertex := range triangle {
			if uint64(vertex) >= uint64(count) {
				return nil, stats, fmt.Errorf("triangle index exceeds vertex count")
			}
			remove = remove || invalid != nil && invalid[vertex]
		}
		if remove {
			if sourceFaces == nil {
				indices = slices.Clone(geometry.Indices[:offset])
				sourceFaces = make([]uint32, offset/3)
				for face := range sourceFaces {
					sourceFaces[face] = uint32(face)
					if geometry.sourceFaces != nil {
						sourceFaces[face] = geometry.sourceFaces[face]
					}
				}
			}
			stats.removedTriangles++
			continue
		}
		if sourceFaces != nil {
			indices = append(indices, triangle...)
			face := uint32(offset / 3)
			if geometry.sourceFaces != nil {
				face = geometry.sourceFaces[offset/3]
			}
			sourceFaces = append(sourceFaces, face)
		}
	}
	if len(indices) == 0 {
		return nil, stats, ctx.Err()
	}

	result := *geometry
	result.Indices = indices
	if invalid != nil || stats.discardedIndices != 0 {
		remapped, sources, ok := modelViewerCompactIndices(indices, count, nil)
		if !ok {
			return nil, stats, fmt.Errorf("cannot compact recovered geometry")
		}
		compact := func(values []float32, width int) []float32 {
			if len(values) == 0 {
				return values
			}
			data := make([]float32, len(sources)*width)
			for vertex, source := range sources {
				copy(data[vertex*width:], values[int(source)*width:int(source)*width+width])
			}
			return data
		}
		result.Position = compact(geometry.Position, 3)
		result.Normal = compact(geometry.Normal, 3)
		result.Tangent = compact(geometry.Tangent, 4)
		result.Texcoord0 = compact(geometry.Texcoord0, 2)
		result.Color0 = compact(geometry.Color0, 4)
		result.Indices, result.VertexCount = remapped, len(sources)
		result.SourceIndices = make([]uint32, len(sources))
		for vertex, source := range sources {
			result.SourceIndices[vertex] = source
			if geometry.SourceIndices != nil {
				result.SourceIndices[vertex] = geometry.SourceIndices[source]
			}
		}
		if sourceFaces != nil {
			result.sourceFaces = sourceFaces
		}
	}
	for index, values := range [][]float32{result.Normal, result.Tangent} {
		for offset, value := range values {
			if offset%16384 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, stats, err
				}
			}
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				if index == 0 {
					stats.omittedNormals = true
				}
				stats.omittedTangents = len(result.Tangent) != 0
				break
			}
		}
	}
	if stats.omittedNormals {
		result.Normal = nil
	}
	if stats.omittedTangents {
		result.Tangent = nil
	}
	if err := ctx.Err(); err != nil {
		return nil, stats, err
	}
	if invalid == nil && stats.discardedIndices == 0 && !stats.omittedNormals && !stats.omittedTangents {
		return geometry, stats, nil
	}
	return &result, stats, nil
}

func reportModelViewerGeometryRecoveries(
	log *infra.Log,
	recoveries map[*modelViewerGeometry]*modelViewerGeometryRecovery,
) {
	for _, recovery := range recoveries {
		stats := recovery.stats
		if stats.invalidVertices == 0 && stats.discardedIndices == 0 && !stats.omittedNormals &&
			!stats.omittedTangents {
			continue
		}
		remaining := 0
		if recovery.geometry != nil {
			remaining = len(recovery.geometry.Indices) / 3
		}
		fields := map[string]any{
			"message":            "Skipped invalid model viewer geometry data",
			"stage":              "prepare-geometry",
			"iniPaths":           recovery.iniPaths,
			"positionPaths":      recovery.positionPaths,
			"meshIds":            recovery.meshIDs,
			"invalidVertices":    stats.invalidVertices,
			"removedTriangles":   stats.removedTriangles,
			"discardedIndices":   stats.discardedIndices,
			"remainingTriangles": remaining,
			"omittedNormals":     stats.omittedNormals,
			"omittedTangents":    stats.omittedTangents,
		}
		if stats.firstInvalidSourceVertex >= 0 {
			fields["firstInvalidSourceVertex"] = stats.firstInvalidSourceVertex
		}
		log.Warn(fields, "StaticGlb.loadForViewer")
	}
}
