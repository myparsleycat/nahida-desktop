package tools

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type modelViewerShapeKeyDimension struct {
	VariableID      string `json:"variableId"`
	Mode            string `json:"mode,omitempty"`
	SmallerPath     string `json:"smallerPath"`
	BiggerPath      string `json:"biggerPath"`
	Sparse          bool   `json:"sparse,omitempty"`
	BufferShapeID   int    `json:"bufferShapeId,omitempty"`
	SparseOffset    int    `json:"sparseOffset,omitempty"`
	OffsetPath      string `json:"offsetPath,omitempty"`
	VertexIDPath    string `json:"vertexIdPath,omitempty"`
	VertexDeltaPath string `json:"vertexDeltaPath,omitempty"`
}

type modelViewerShapeKey struct {
	ShaderPath         string                         `json:"shaderPath"`
	TargetMeshPrefixes []string                       `json:"targetMeshPrefixes"`
	BasePath           string                         `json:"basePath"`
	VertexStride       int                            `json:"vertexStride"`
	PositionOffset     int                            `json:"positionOffset"`
	NormalOffset       int                            `json:"normalOffset"`
	TangentOffset      int                            `json:"tangentOffset"`
	Dimensions         []modelViewerShapeKeyDimension `json:"dimensions"`
}

var (
	modelViewerShapeOutputRE = regexp.MustCompile(`(?i)^([^=]+?)\s*=\s*copy\s+ref\s+cs-u5\s*$`)
	modelViewerShapeBaseRE   = regexp.MustCompile(`(?i)^cs-u5\s*=\s*copy\s+(.+)$`)
	modelViewerVariableRE    = regexp.MustCompile(`\$([\w.]+)`)
)

func collectModelViewerShapeKeys(sections []modINISection, resources []modelViewerResource, modDir string) []modelViewerShapeKey {
	sectionLookup := make(map[string]modINISection)
	for _, section := range sections {
		sectionLookup[modelViewerNormalizeKey(section.Header+section.Name)] = section
	}
	resourceMap := make(map[string]modelViewerResource)
	for _, resource := range resources {
		resourceMap[modelViewerNormalizeKey(resource.Name)] = resource
	}
	var output []modelViewerShapeKey
	for _, shader := range sections {
		shaderPath := sectionValue(shader.Lines, "cs")
		if !strings.EqualFold(shader.Header, "CustomShader") || !strings.EqualFold(filepath.Base(shaderPath), "shapekey.hlsl") {
			continue
		}
		outputName, baseName := "", ""
		for _, line := range shader.Lines {
			if match := modelViewerShapeOutputRE.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
				outputName = modelViewerTrimResourcePrefix(match[1])
			}
			if match := modelViewerShapeBaseRE.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
				baseName = modelViewerTrimResourcePrefix(match[1])
			}
		}
		outputResource, outputOK := resourceMap[modelViewerNormalizeKey(outputName)]
		baseResource, baseOK := resourceMap[modelViewerNormalizeKey(baseName)]
		if !outputOK || !baseOK || baseResource.Filename == "" {
			continue
		}
		target := ""
		if typed := parseModelViewerMihoyoResourceName(outputResource.Name); typed != nil {
			target = typed.Key
		}
		if target == "" {
			if typed := parseModelViewerWwmiResourceName(outputResource.Name); typed != nil {
				target = typed.Key
			}
		}
		if target == "" {
			continue
		}
		shaderFullName := shader.Header + shader.Name
		dimensions := make(map[string]modelViewerShapeKeyDimension)
		for _, caller := range sections {
			calls := false
			for _, line := range caller.Lines {
				if modelViewerNormalizeKey(line) == modelViewerNormalizeKey("run = "+shaderFullName) {
					calls = true
					break
				}
			}
			if !calls {
				continue
			}
			assignments := resolveModelViewerAssignments(caller, []string{"x88", "x89", "cs-t51", "cs-t52", "cs-t53", "cs-t54"}, sectionLookup, nil, make(map[string]bool))
			addDimension := func(variableValue, smallerValue, biggerValue string) {
				match := modelViewerVariableRE.FindStringSubmatch(variableValue)
				if match == nil {
					return
				}
				variable := modelViewerNormalizeKey(match[1])
				smallerName := modelViewerTrimResourcePrefix(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(smallerValue), "copy "), "ref ")))
				biggerName := modelViewerTrimResourcePrefix(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(biggerValue), "copy "), "ref ")))
				smaller, smallerOK := resourceMap[modelViewerNormalizeKey(smallerName)]
				bigger, biggerOK := resourceMap[modelViewerNormalizeKey(biggerName)]
				if smallerOK && biggerOK && smaller.Filename != "" && bigger.Filename != "" {
					dimensions[variable] = modelViewerShapeKeyDimension{VariableID: variable, Mode: "midpoint_pair", SmallerPath: filepath.Join(modDir, filepath.FromSlash(smaller.Filename)), BiggerPath: filepath.Join(modDir, filepath.FromSlash(bigger.Filename))}
				}
			}
			addDimension(assignments["x88"], assignments["cst52"], assignments["cst51"])
			addDimension(assignments["x89"], assignments["cst54"], assignments["cst53"])
		}
		if len(dimensions) == 0 {
			continue
		}
		keys := make([]string, 0, len(dimensions))
		for key := range dimensions {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		shape := modelViewerShapeKey{ShaderPath: filepath.Join(modDir, filepath.FromSlash(shaderPath)), TargetMeshPrefixes: []string{target}, BasePath: filepath.Join(modDir, filepath.FromSlash(baseResource.Filename)), VertexStride: baseResource.Stride, PositionOffset: 0, NormalOffset: 12, TangentOffset: 24}
		if shape.VertexStride == 0 {
			shape.VertexStride = 40
		}
		for _, key := range keys {
			shape.Dimensions = append(shape.Dimensions, dimensions[key])
		}
		output = append(output, shape)
	}
	return append(output, collectAdditionalModelViewerShapeKeys(sections, resourceMap, modDir)...)
}

func readModelViewerSparseShapePositions(cache *modelViewerBufferCache, dimension modelViewerShapeKeyDimension, geometry *modelViewerGeometry) ([]float32, error) {
	offsets, err := cache.read(dimension.OffsetPath)
	if err != nil {
		return nil, err
	}
	vertexIDs, err := cache.read(dimension.VertexIDPath)
	if err != nil {
		return nil, err
	}
	deltas, err := cache.read(dimension.VertexDeltaPath)
	if err != nil {
		return nil, err
	}
	keyID := dimension.BufferShapeID
	if keyID < 0 || (keyID+2)*4 > len(offsets) {
		return nil, fmt.Errorf("sparse shape offset %d is out of range", keyID)
	}
	begin := int(binary.LittleEndian.Uint32(offsets[keyID*4:])) + dimension.SparseOffset
	end := int(binary.LittleEndian.Uint32(offsets[(keyID+1)*4:])) + dimension.SparseOffset
	limit := min(end, len(vertexIDs)/4, len(deltas)/12)
	if begin < 0 || begin > limit {
		return nil, fmt.Errorf("sparse shape range %d:%d is invalid", begin, limit)
	}
	type delta struct{ x, y, z float32 }
	values := make(map[uint32]delta, limit-begin)
	for index := begin; index < limit; index++ {
		vertexID := binary.LittleEndian.Uint32(vertexIDs[index*4:])
		prior := values[vertexID]
		prior.x += modelViewerHalfToFloat(binary.LittleEndian.Uint16(deltas[index*12:]))
		prior.y += modelViewerHalfToFloat(binary.LittleEndian.Uint16(deltas[index*12+2:]))
		prior.z += modelViewerHalfToFloat(binary.LittleEndian.Uint16(deltas[index*12+4:]))
		values[vertexID] = prior
	}
	positions := append([]float32(nil), geometry.Position...)
	sourceIndices := geometry.SourceIndices
	if len(sourceIndices) == 0 {
		sourceIndices = make([]uint32, geometry.VertexCount)
		for index := range sourceIndices {
			sourceIndices[index] = uint32(index)
		}
	}
	for outputIndex, sourceIndex := range sourceIndices {
		value, ok := values[sourceIndex]
		if !ok || outputIndex*3+2 >= len(positions) {
			continue
		}
		positions[outputIndex*3] += value.x
		positions[outputIndex*3+1] += value.y
		positions[outputIndex*3+2] += value.z
	}
	return positions, nil
}
