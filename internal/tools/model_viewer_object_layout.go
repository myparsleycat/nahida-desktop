package tools

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	modelViewerPackedObjectStride   = 20
	modelViewerPackedObjectStride24 = 24
	modelViewerPackedObjectStride28 = 28
	modelViewerPackedObjectKind     = "gimi_cyclic_packed_v1"
	modelViewerPackedObjectWEpsilon = 0.05

	modelViewerDrawVertexMihoyo = "mihoyo"
	modelViewerDrawVertexWWMI   = "wwmi"
	modelViewerDrawVertexPacked = "packed"
)

var (
	modelViewerFilenamePositionRE     = regexp.MustCompile(`(?i)position`)
	modelViewerPackedPositionAbsLimit = 1e5
)

func isModelViewerPackedObjectStride(stride int) bool {
	return stride == modelViewerPackedObjectStride || stride == modelViewerPackedObjectStride24 ||
		stride == modelViewerPackedObjectStride28
}

func modelViewerPackedObjectLayout(indexFormat string, stride int) modelViewerFmtLayout {
	return modelViewerPackedObjectLayoutAt(indexFormat, stride, 12)
}

func modelViewerPackedObjectLayoutAt(indexFormat string, stride, texcoordOffset int) modelViewerFmtLayout {
	if indexFormat == "" {
		indexFormat = "DXGI_FORMAT_R32_UINT"
	}
	if !isModelViewerPackedObjectStride(stride) {
		stride = modelViewerPackedObjectStride
	}
	return modelViewerFmtLayout{
		Stride:      stride,
		Topology:    "trianglelist",
		IndexFormat: indexFormat,
		Elements: []modelViewerFmtElement{
			{
				SemanticName:      "POSITION",
				Format:            "DXGI_FORMAT_R16G16B16A16_FLOAT",
				AlignedByteOffset: 0,
				InputSlotClass:    "per-vertex",
			},
			{
				SemanticName:      "NORMAL",
				Format:            "DXGI_FORMAT_R8G8B8A8_SINT",
				AlignedByteOffset: 8,
				InputSlotClass:    "per-vertex",
			},
			{
				SemanticName:      "TEXCOORD",
				Format:            "DXGI_FORMAT_R16G16_FLOAT",
				AlignedByteOffset: texcoordOffset,
				InputSlotClass:    "per-vertex",
			},
		},
	}
}

func modelViewerPackedObjectShaderLayout(shader string) (stride, texcoordOffset int, known bool) {
	compact := compactModelViewerShader(shader)
	switch {
	case strings.Contains(compact, "structvertexattributes{uint2position;uintnormal;uinttexcoord;uinttangent;}"):
		return modelViewerPackedObjectStride, 12, true
	case strings.Contains(compact, "structvertexattributes{uint2position;uintnormal;uinttangent;uinttexcoord;uinttexcoord1;}"):
		return modelViewerPackedObjectStride24, 16, true
	case strings.Contains(
		compact,
		"structvertexattributes{uint2position;uintnormal;uinttangent;uintcolor;uinttexcoord;uinttexcoord1;}",
	):
		return modelViewerPackedObjectStride28, 20, true
	default:
		return 0, 0, false
	}
}

func isKnownModelViewerGIMICyclicPackedBoneShader(shader string) bool {
	if _, _, known := modelViewerPackedObjectShaderLayout(shader); !known {
		return false
	}
	compact := compactModelViewerShader(shader)
	required := []string{
		"structposeattributes{float4x;float4y;float4z;}",
		"structuredbuffer<vertexattributes>", "register(t50)",
		"structuredbuffer<blendattributes>", "register(t51)",
		"structuredbuffer<poseattributes>", "register(t52)",
		"f16tof32", "f32tof16",
		"frame*((int)vg_count)", "(frame+1)*((int)vg_count)",
		"dot(trans,pos)",
	}
	for _, signature := range required {
		if !strings.Contains(compact, signature) {
			return false
		}
	}
	return strings.Contains(compact, "int4indicies") || strings.Contains(compact, "int4indices")
}

func collectModelViewerPackedObjectResources(root, shaderBaseDir string, sections []modINISection) map[string]int {
	packed := map[string]int{}
	reachable := collectModelViewerReachableComputeSections(sections)
	add := func(name string, texcoordOffset int) {
		if key := modelViewerNormalizeKey(name); key != "" {
			packed[key] = texcoordOffset
		}
	}
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "CustomShader") ||
			!reachable[modelViewerNormalizeKey(section.Header+section.Name)] {
			continue
		}
		for _, pass := range collectModelViewerComputePasses(section) {
			shader, ok := readModelViewerComputeShader(root, shaderBaseDir, pass.shader)
			if !ok {
				continue
			}
			_, texcoordOffset, known := modelViewerPackedObjectShaderLayout(shader)
			if isKnownModelViewerPackedDualQuaternionShader(shader) {
				texcoordOffset = 16
			} else if !known {
				continue
			}
			add(pass.outputName, texcoordOffset)
			add(pass.t50, texcoordOffset)
		}
	}
	return packed
}

func modelViewerUsePackedObjectLayout(resource modelViewerResource, data []byte, shaderPacked bool) (int, bool) {
	stride, ok := modelViewerPackedObjectStrideOf(resource.Stride, data)
	if !ok {
		return 0, false
	}
	if shaderPacked || modelViewerPositionLooksPackedObject(data, stride) {
		return stride, true
	}
	return 0, false
}

func modelViewerPackedObjectStrideOf(declared int, data []byte) (int, bool) {
	if isModelViewerPackedObjectStride(declared) {
		if len(data) < declared || len(data)%declared != 0 {
			return 0, false
		}
		return declared, true
	}
	if declared != 0 {
		return 0, false
	}
	if len(data) >= modelViewerPackedObjectStride24 && len(data)%modelViewerPackedObjectStride24 == 0 &&
		modelViewerPositionLooksPackedObject(data, modelViewerPackedObjectStride24) {
		return modelViewerPackedObjectStride24, true
	}
	if len(data) >= modelViewerPackedObjectStride && len(data)%modelViewerPackedObjectStride == 0 &&
		modelViewerPositionLooksPackedObject(data, modelViewerPackedObjectStride) {
		return modelViewerPackedObjectStride, true
	}
	if len(data) >= modelViewerPackedObjectStride28 && len(data)%modelViewerPackedObjectStride28 == 0 &&
		modelViewerPositionLooksPackedObject(data, modelViewerPackedObjectStride28) {
		return modelViewerPackedObjectStride28, true
	}
	return 0, false
}

func modelViewerPositionLooksPackedObject(data []byte, stride int) bool {
	if !isModelViewerPackedObjectStride(stride) || len(data) < stride || len(data)%stride != 0 {
		return false
	}
	vertexCount := len(data) / stride
	sampleCount := min(vertexCount, 256)
	step := max(1, vertexCount/sampleCount)
	sampled, valid := 0, 0
	requireW := stride >= modelViewerPackedObjectStride24
	for vertex := 0; vertex < vertexCount && sampled < sampleCount; vertex += step {
		offset := vertex * stride
		x := modelViewerHalfToFloat(binary.LittleEndian.Uint16(data[offset:]))
		y := modelViewerHalfToFloat(binary.LittleEndian.Uint16(data[offset+2:]))
		z := modelViewerHalfToFloat(binary.LittleEndian.Uint16(data[offset+4:]))
		sampled++
		if !modelViewerPackedPositionSampleOK(x, y, z) {
			continue
		}
		if requireW {
			w := modelViewerHalfToFloat(binary.LittleEndian.Uint16(data[offset+6:]))
			if math.IsNaN(float64(w)) || math.IsInf(float64(w), 0) ||
				math.Abs(float64(w-1)) > modelViewerPackedObjectWEpsilon {
				continue
			}
		}
		valid++
	}
	return sampled > 0 && float64(valid)/float64(sampled) >= 0.9
}

func modelViewerPackedPositionSampleOK(x, y, z float32) bool {
	for _, value := range []float32{x, y, z} {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) ||
			math.Abs(float64(value)) > modelViewerPackedPositionAbsLimit {
			return false
		}
	}
	return true
}

func lookupModelViewerFamilyTexcoord(
	modDir string,
	position modelViewerResource,
	resources []modelViewerResource,
) (modelViewerResource, bool) {
	posCount, _, ok := modelViewerResourceVertexCount(modDir, position, 40)
	if !ok || posCount == 0 {
		return modelViewerResource{}, false
	}
	if typed := parseModelViewerMihoyoResourceName(position.Name); typed != nil && typed.Kind == "position" {
		for _, resource := range resources {
			other := parseModelViewerMihoyoResourceName(resource.Name)
			if other == nil || other.Kind != "texcoord" ||
				modelViewerNormalizeKey(other.Key) != modelViewerNormalizeKey(typed.Key) ||
				resource.Filename == "" {
				continue
			}
			if tcCount, _, tcOK := modelViewerResourceVertexCount(
				modDir,
				resource,
				resource.Stride,
			); tcOK &&
				tcCount == posCount {
				return resource, true
			}
		}
	}
	for _, filename := range modelViewerSiblingTexcoordFilenames(position) {
		path, err := resolveModelViewerResourcePath(modDir, modDir, filename)
		if err != nil || !modelViewerPathWithin(modDir, path) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size()%int64(posCount) != 0 {
			continue
		}
		stride := int(info.Size() / int64(posCount))
		if stride < 4 || stride > 64 {
			continue
		}
		return modelViewerResource{
			Name:     strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)),
			Filename: filename,
			Stride:   stride,
		}, true
	}
	return modelViewerResource{}, false
}

func modelViewerResourceVertexCount(modDir string, resource modelViewerResource, defaultStride int) (int, int64, bool) {
	stride := resource.Stride
	if stride <= 0 {
		stride = defaultStride
	}
	if stride <= 0 || resource.Filename == "" {
		return 0, 0, false
	}
	path, err := resolveModelViewerResourcePath(modDir, modDir, resource.Filename)
	if err != nil || !modelViewerPathWithin(modDir, path) {
		return 0, 0, false
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size()%int64(stride) != 0 {
		return 0, 0, false
	}
	return int(info.Size() / int64(stride)), info.Size(), true
}

type modelViewerDrawVertexSource struct {
	kind                           string
	packedTexcoordOffset           int
	packedTexcoordDeclared         bool
	ib, position, texcoord, vector modelViewerResource
	packed                         []byte
	packedStride                   int
	missingTexcoord                bool
}

func resolveModelViewerDrawVertexSource(
	modDir, layoutName string,
	state modelViewerDirectBufferState,
	resourceMap map[string]modelViewerResource,
	resources []modelViewerResource,
	cache *modelViewerBufferCache,
	packedResources map[string]int,
) modelViewerDrawVertexSource {
	ib, ibOK := resourceMap[modelViewerNormalizeKey(state.ib)]
	position, posOK := resourceMap[modelViewerNormalizeKey(state.vb0)]
	if !ibOK || !posOK || ib.Filename == "" || position.Filename == "" {
		return modelViewerDrawVertexSource{}
	}
	source := modelViewerDrawVertexSource{ib: ib, position: position}
	if layoutName == "wwmi" {
		vector, vectorOK := resourceMap[modelViewerNormalizeKey(state.vb1)]
		texcoord, tcOK := resourceMap[modelViewerNormalizeKey(state.vb2)]
		if !vectorOK || vector.Filename == "" || !tcOK || texcoord.Filename == "" {
			return source
		}
		source.kind = modelViewerDrawVertexWWMI
		source.vector = vector
		source.texcoord = texcoord
		return source
	}
	source.packedTexcoordOffset = packedResources[modelViewerNormalizeKey(state.vb0)]
	if source.packedTexcoordOffset == 0 {
		source.packedTexcoordOffset = packedResources[modelViewerNormalizeKey(position.Name)]
	}
	source.packedTexcoordDeclared = source.packedTexcoordOffset != 0
	if !source.packedTexcoordDeclared {
		source.packedTexcoordOffset = 12
	}
	shaderPacked := source.packedTexcoordDeclared
	texcoord, tcOK := resourceMap[modelViewerNormalizeKey(state.vb1)]
	if !tcOK || texcoord.Filename == "" {
		if raw, packedStride, packed := readModelViewerPackedObjectBuffer(
			modDir,
			position,
			cache,
			shaderPacked,
		); packed {
			source.kind = modelViewerDrawVertexPacked
			source.packed = raw
			source.packedStride = packedStride
			return source
		}
		if sibling, ok := lookupModelViewerFamilyTexcoord(modDir, position, resources); ok {
			source.kind = modelViewerDrawVertexMihoyo
			source.texcoord = sibling
			return source
		}
		source.missingTexcoord = true
		return source
	}
	if shaderPacked {
		if raw, packedStride, packed := readModelViewerPackedObjectBuffer(modDir, position, cache, true); packed {
			source.kind = modelViewerDrawVertexPacked
			source.packed = raw
			source.packedStride = packedStride
			return source
		}
	}
	source.kind = modelViewerDrawVertexMihoyo
	source.texcoord = texcoord
	return source
}

func readModelViewerPackedObjectBuffer(
	modDir string,
	position modelViewerResource,
	cache *modelViewerBufferCache,
	shaderPacked bool,
) ([]byte, int, bool) {
	raw, err := cache.read(filepath.Join(modDir, filepath.FromSlash(position.Filename)))
	if err != nil {
		return nil, 0, false
	}
	stride, packed := modelViewerUsePackedObjectLayout(position, raw, shaderPacked)
	return raw, stride, packed
}

type modelViewerDrawVertexBuffers struct {
	combined  []byte
	stride    int
	layout    modelViewerFmtLayout
	posStride int
	hasFrame  bool
}

func loadModelViewerDrawVertexBuffers(
	modDir string,
	source modelViewerDrawVertexSource,
	cache *modelViewerBufferCache,
) (modelViewerDrawVertexBuffers, bool, error) {
	position, vector, texcoord := source.position, source.vector, source.texcoord
	posStride := position.Stride
	if posStride <= 0 {
		posStride = 40
	}
	tcStride := texcoord.Stride
	if tcStride <= 0 {
		tcStride = 20
	}
	switch source.kind {
	case modelViewerDrawVertexPacked:
		stride := source.packedStride
		if stride <= 0 {
			stride = modelViewerPackedObjectStride
		}
		texcoordOffset := source.packedTexcoordOffset
		if stride == modelViewerPackedObjectStride24 && !source.packedTexcoordDeclared {
			texcoordOffset = detectModelViewerPackedTexcoordOffset(source.packed, stride)
		}
		if texcoordOffset == 16 {
			texcoordOffset = resolveModelViewerPackedTexcoordOffset(modDir, position, source.packed, stride, cache)
		}
		return modelViewerDrawVertexBuffers{
			combined:  source.packed,
			stride:    stride,
			posStride: stride,
			layout:    modelViewerPackedObjectLayoutAt(source.ib.Format, stride, texcoordOffset),
		}, true, nil
	case modelViewerDrawVertexWWMI:
		vectorStride := vector.Stride
		if vectorStride <= 0 {
			vectorStride = 8
		}
		parts, readErr := readModelViewerResourceSet(
			modDir,
			[]*modelViewerResource{&position, &vector, &texcoord},
			cache,
		)
		if readErr == nil {
			strides := []int{posStride, vectorStride, tcStride}
			key := strings.ToLower(
				strings.Join([]string{position.Filename, vector.Filename, texcoord.Filename}, "|"),
			) + "#" + fmt.Sprint(
				strides,
			)
			combined, stride, buffersErr := cache.interleavedBuffers(key, func() ([]byte, int, error) {
				bytes, combinedStride, _, combineErr := interleaveModelViewerBuffers(parts, strides)
				return bytes, combinedStride, combineErr
			})
			if buffersErr == nil {
				uvOffset, uvFormat := detectModelViewerUVBest(combined, stride, posStride+vectorStride, tcStride)
				vectorFormat := firstModelViewerString(vector.Format, "DXGI_FORMAT_R8G8B8A8_SNORM")
				return modelViewerDrawVertexBuffers{
					combined:  combined,
					stride:    stride,
					posStride: posStride,
					layout: modelViewerFmtLayout{
						Stride:      stride,
						Topology:    "trianglelist",
						IndexFormat: source.ib.Format,
						Elements: []modelViewerFmtElement{
							{
								SemanticName:      "POSITION",
								Format:            "DXGI_FORMAT_R32G32B32_FLOAT",
								AlignedByteOffset: 0,
								InputSlotClass:    "per-vertex",
							},
							{
								SemanticName:      "NORMAL",
								Format:            vectorFormat,
								AlignedByteOffset: posStride,
								InputSlotClass:    "per-vertex",
							},
							{
								SemanticName:      "TEXCOORD",
								Format:            uvFormat,
								AlignedByteOffset: uvOffset,
								InputSlotClass:    "per-vertex",
							},
						},
					},
				}, true, nil
			}
		}
		return modelViewerDrawVertexBuffers{}, false, nil
	case modelViewerDrawVertexMihoyo:
		buffers, buffersErr := cache.paired(
			filepath.Join(modDir, filepath.FromSlash(position.Filename)),
			posStride,
			filepath.Join(modDir, filepath.FromSlash(texcoord.Filename)),
			tcStride,
		)
		if buffersErr != nil {
			if isModelViewerInterleaveValidationError(buffersErr) {
				return modelViewerDrawVertexBuffers{}, false, nil
			}
			return modelViewerDrawVertexBuffers{}, false, buffersErr
		}
		return modelViewerDrawVertexBuffers{
			combined:  buffers.combined,
			stride:    buffers.stride,
			posStride: posStride,
			hasFrame:  buffers.hasFrame,
			layout: modelViewerFmtLayout{
				Stride:      buffers.stride,
				Topology:    "trianglelist",
				IndexFormat: source.ib.Format,
				Elements: []modelViewerFmtElement{
					{
						SemanticName:      "POSITION",
						Format:            "DXGI_FORMAT_R32G32B32_FLOAT",
						AlignedByteOffset: 0,
						InputSlotClass:    "per-vertex",
					},
					{
						SemanticName:      "TEXCOORD",
						Format:            buffers.uvFormat,
						AlignedByteOffset: buffers.uvOffset,
						InputSlotClass:    "per-vertex",
					},
				},
			},
		}, true, nil
	default:
		return modelViewerDrawVertexBuffers{}, false, nil
	}
}

// detectModelViewerPackedTexcoordOffset picks the UV0 word for a packed object
// buffer whose layout no compute shader declares; callers must not use it to
// second-guess a declared layout. 24-byte dumps are not uniform: the cyclic
// animation layout stores tangent before the two UV sets (UV0 at byte 16),
// while older frame-swap dumps keep UV0 in the tangent slot (byte 12). Judge
// from the data so both keep working.
func detectModelViewerPackedTexcoordOffset(data []byte, stride int) int {
	if stride != modelViewerPackedObjectStride24 {
		return 12
	}
	if modelViewerPackedUVScore(data, stride, 16) > modelViewerPackedUVScore(data, stride, 12) {
		return 16
	}
	return 12
}

// modelViewerPackedUVScore reports how much the 16-bit pair at a candidate
// offset looks like a live UV stream. Non-finite and out-of-range pairs count
// against the score, and a stream that never varies scores zero.
func modelViewerPackedUVScore(data []byte, stride, offset int) float64 {
	if stride <= 0 || offset+4 > stride {
		return 0
	}
	vertexCount := len(data) / stride
	if vertexCount == 0 {
		return 0
	}
	step := max(1, vertexCount/1024)
	sampled, inRange := 0, 0
	minU, maxU, minV, maxV := float32(0), float32(0), float32(0), float32(0)
	first := true
	for vertex := 0; vertex < vertexCount; vertex += step {
		base := vertex*stride + offset
		if base+4 > len(data) {
			break
		}
		u := modelViewerHalfToFloat(binary.LittleEndian.Uint16(data[base:]))
		v := modelViewerHalfToFloat(binary.LittleEndian.Uint16(data[base+2:]))
		sampled++
		if math.IsNaN(float64(u)) || math.IsInf(float64(u), 0) ||
			math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			continue
		}
		if first {
			minU, maxU, minV, maxV = u, u, v, v
			first = false
		}
		minU, maxU = min(minU, u), max(maxU, u)
		minV, maxV = min(minV, v), max(maxV, v)
		if u >= -0.01 && u <= 2 && v >= -0.01 && v <= 2 {
			inRange++
		}
	}
	if sampled == 0 || (maxU-minU)+(maxV-minV) < 1e-4 {
		return 0
	}
	return float64(inRange) / float64(sampled)
}

// Some compute shaders name the untouched UV word "tangent". Only override
// their declared layout when a separate UV stream matches every vertex byte.
func resolveModelViewerPackedTexcoordOffset(
	modDir string,
	position modelViewerResource,
	packed []byte,
	stride int,
	cache *modelViewerBufferCache,
) int {
	if stride < 20 || len(packed) == 0 || len(packed)%stride != 0 {
		return 16
	}
	for _, filename := range modelViewerSiblingTexcoordFilenames(position) {
		resolved, err := resolveModelViewerResourcePath(modDir, modDir, filename)
		if err != nil || !modelViewerPathWithin(modDir, resolved) {
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(packed)/stride)*4 {
			continue
		}
		uv, err := cache.read(resolved)
		if err != nil || len(uv) != len(packed)/stride*4 {
			continue
		}
		for _, offset := range []int{16, 12} {
			matches := true
			for vertex := range len(uv) / 4 {
				if !bytes.Equal(uv[vertex*4:vertex*4+4], packed[vertex*stride+offset:vertex*stride+offset+4]) {
					matches = false
					break
				}
			}
			if matches {
				return offset
			}
		}
	}
	return 16
}

func normalizeModelViewerPackedObjectNormals(normals []float32) {
	for offset := 0; offset+2 < len(normals); offset += 3 {
		x, y, z := normals[offset], normals[offset+1], normals[offset+2]
		length := float32(math.Sqrt(float64(x*x + y*y + z*z)))
		if length > 1e-8 {
			normals[offset] = x / length
			normals[offset+1] = y / length
			normals[offset+2] = z / length
		}
	}
}

func modelViewerSiblingTexcoordFilenames(position modelViewerResource) []string {
	raw := strings.ReplaceAll(position.Filename, `\`, "/")
	dir, base := path.Dir(raw), path.Base(raw)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	join := func(name string) string {
		if dir == "." || dir == "" {
			return name
		}
		return dir + "/" + name
	}
	var names []string
	seen := map[string]bool{}
	add := func(name string) {
		key := modelViewerNormalizeKey(name)
		if name == "" || seen[key] {
			return
		}
		seen[key] = true
		names = append(names, name)
	}
	if replaced := modelViewerFilenamePositionRE.ReplaceAllString(
		stem,
		"Texcoord",
	); !strings.EqualFold(
		replaced,
		stem,
	) {
		add(join(replaced + ext))
	}
	add(join(stem + "Texcoord" + ext))
	if typed := parseModelViewerMihoyoResourceName(position.Name); typed != nil && typed.Key != "" {
		add(join(typed.Key + "Texcoord" + ext))
	}
	return names
}
