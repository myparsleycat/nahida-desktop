package tools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type modelViewerResource struct {
	Name     string
	Filename string
	Stride   int
	Format   string
	Values   map[string]string
}

type modelViewerBufferGroup struct {
	Key         string
	VBFilename  string
	VB          []byte
	Stride      int
	SourceFiles []string
}

type modelViewerTypedResource struct {
	Key  string
	Kind string
}

var (
	modelViewerMihoyoTypedRE    = regexp.MustCompile(`(?i)^(.*?)(Position|Blend|Texcoord)(\.\d+)?$`)
	modelViewerMihoyoBaseRE     = regexp.MustCompile(`(?i)^(.*?)PositionBase(\.\d+)?$`)
	modelViewerWwmiTypedRE      = regexp.MustCompile(`(?i)^(.*?)(Position|Vector|Blend|Color|TexCoord)Buffer(\.\d+)?$`)
	modelViewerPositionSuffixRE = regexp.MustCompile(`^[\w.-]+$`)
)

func collectModelViewerResources(sections []modINISection) []modelViewerResource {
	resources := make([]modelViewerResource, 0)
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "Resource") {
			continue
		}
		stride, _ := strconv.Atoi(modelViewerSectionValue(section, "stride"))
		resources = append(resources, modelViewerResource{Name: section.Name, Filename: modelViewerSectionValue(section, "filename"), Stride: stride, Format: modelViewerSectionValue(section, "format"), Values: section.Values})
	}
	return resources
}

func detectModelViewerLayout(sections []modINISection, resources []modelViewerResource) string {
	for _, section := range sections {
		if strings.EqualFold(section.Header, "Constants") {
			for _, line := range section.Lines {
				if strings.Contains(strings.ToLower(line), "$required_wwmi_version") {
					return "wwmi"
				}
			}
		}
	}
	names := make(map[string]bool)
	for _, resource := range resources {
		names[modelViewerNormalizeKey(resource.Name)] = true
	}
	if names[modelViewerNormalizeKey("IndexBuffer")] && names[modelViewerNormalizeKey("PositionBuffer")] && names[modelViewerNormalizeKey("VectorBuffer")] && names[modelViewerNormalizeKey("TexCoordBuffer")] {
		return "wwmi"
	}
	return "mihoyo"
}

func parseModelViewerMihoyoResourceName(name string) *modelViewerTypedResource {
	if match := modelViewerMihoyoTypedRE.FindStringSubmatch(name); match != nil {
		return &modelViewerTypedResource{Key: match[1] + match[3], Kind: strings.ToLower(match[2])}
	}
	if match := modelViewerMihoyoBaseRE.FindStringSubmatch(name); match != nil {
		return &modelViewerTypedResource{Key: match[1] + match[2], Kind: "position"}
	}
	return nil
}

func parseModelViewerWwmiResourceName(name string) *modelViewerTypedResource {
	match := modelViewerWwmiTypedRE.FindStringSubmatch(name)
	if match == nil {
		return nil
	}
	return &modelViewerTypedResource{Key: match[1] + "IndexBuffer" + match[3], Kind: strings.ToLower(match[2])}
}

func collectModelViewerBufferGroups(modDir, layout string, resources []modelViewerResource, cache *modelViewerBufferCache, warn func(string)) ([]modelViewerBufferGroup, error) {
	if layout == "wwmi" {
		return collectModelViewerWwmiGroups(modDir, resources, cache, warn)
	}
	return collectModelViewerMihoyoGroups(modDir, resources, cache, warn)
}

func collectModelViewerMihoyoGroups(modDir string, resources []modelViewerResource, cache *modelViewerBufferCache, warn func(string)) ([]modelViewerBufferGroup, error) {
	type group struct{ position, blend, texcoord, single *modelViewerResource }
	byKey := make(map[string]*group)
	order := make([]string, 0)
	get := func(key string) *group {
		if byKey[key] == nil {
			byKey[key] = &group{}
			order = append(order, key)
		}
		return byKey[key]
	}
	for i := range resources {
		resource := &resources[i]
		if resource.Filename == "" || resource.Stride == 0 {
			continue
		}
		if typed := parseModelViewerMihoyoResourceName(resource.Name); typed != nil {
			switch typed.Kind {
			case "position":
				get(typed.Key).position = resource
			case "blend":
				get(typed.Key).blend = resource
			case "texcoord":
				get(typed.Key).texcoord = resource
			}
		} else if !isModelViewerShapePositionVariant(resource.Name) && (strings.EqualFold(filepath.Ext(resource.Filename), ".buf") || strings.EqualFold(filepath.Ext(resource.Filename), ".vb")) {
			get(resource.Name).single = resource
		}
	}
	groups := make([]modelViewerBufferGroup, 0)
	for _, key := range order {
		entry := byKey[key]
		if entry.single != nil {
			singlePath := filepath.Join(modDir, filepath.FromSlash(entry.single.Filename))
			bytes, err := cache.read(singlePath)
			if os.IsNotExist(err) {
				if warn != nil {
					warn("Missing vertex buffer file: " + filepath.Join(modDir, entry.single.Filename))
				}
				continue
			}
			if err != nil {
				return nil, err
			}
			groups = append(groups, modelViewerBufferGroup{Key: key, VBFilename: entry.single.Filename, VB: bytes, Stride: entry.single.Stride, SourceFiles: []string{entry.single.Filename}})
			continue
		}
		if entry.position == nil || entry.blend == nil || entry.texcoord == nil {
			continue
		}
		sourceFiles := []string{entry.position.Filename, entry.blend.Filename, entry.texcoord.Filename}
		strides := []int{entry.position.Stride, entry.blend.Stride, entry.texcoord.Stride}
		interleaveKey := fmt.Sprintf("%s|%d|%d|%d", strings.Join(sourceFiles, "|"), strides[0], strides[1], strides[2])
		vb, stride, err := cache.interleavedBuffers(interleaveKey, func() ([]byte, int, error) {
			parts, err := readModelViewerResourceSet(modDir, []*modelViewerResource{entry.position, entry.blend, entry.texcoord}, cache)
			if err != nil {
				return nil, 0, err
			}
			vb, stride, vertexCount, err := interleaveModelViewerBuffers(parts, strides)
			if err != nil {
				return nil, 0, err
			}
			if len(vb) != vertexCount*stride {
				return nil, 0, fmt.Errorf("unexpected interleaved buffer length for %s", key)
			}
			return vb, stride, nil
		})
		if err != nil {
			if isModelViewerInterleaveValidationError(err) {
				continue
			}
			return nil, err
		}
		groups = append(groups, modelViewerBufferGroup{Key: key, VBFilename: key + ".vb", VB: vb, Stride: stride, SourceFiles: sourceFiles})
	}
	return groups, nil
}

func isModelViewerShapePositionVariant(name string) bool {
	lower := strings.ToLower(name)
	position := strings.LastIndex(lower, "position")
	if position < 0 {
		return false
	}
	suffix := lower[position+len("position"):]
	return suffix != "" && suffix != "base" && !strings.HasPrefix(suffix, "base.") && modelViewerPositionSuffixRE.MatchString(suffix)
}

func collectModelViewerWwmiGroups(modDir string, resources []modelViewerResource, cache *modelViewerBufferCache, warn func(string)) ([]modelViewerBufferGroup, error) {
	type group struct{ position, vector, blend, color, texcoord *modelViewerResource }
	byKey := make(map[string]*group)
	order := make([]string, 0)
	get := func(key string) *group {
		if byKey[key] == nil {
			byKey[key] = &group{}
			order = append(order, key)
		}
		return byKey[key]
	}
	for i := range resources {
		resource := &resources[i]
		if resource.Filename == "" || resource.Stride == 0 {
			continue
		}
		typed := parseModelViewerWwmiResourceName(resource.Name)
		if typed == nil {
			continue
		}
		entry := get(typed.Key)
		switch typed.Kind {
		case "position":
			entry.position = resource
		case "vector":
			entry.vector = resource
		case "blend":
			entry.blend = resource
		case "color":
			entry.color = resource
		case "texcoord":
			entry.texcoord = resource
		}
	}
	groups := make([]modelViewerBufferGroup, 0)
	for _, key := range order {
		entry := byKey[key]
		set := []*modelViewerResource{entry.position, entry.vector, entry.blend, entry.color, entry.texcoord}
		if entry.position == nil || entry.vector == nil || entry.blend == nil || entry.color == nil || entry.texcoord == nil {
			if warn != nil {
				warn("Skipping incomplete WWMI buffer group: " + key)
			}
			continue
		}
		sourceFiles := make([]string, len(set))
		strides := make([]int, len(set))
		for i, resource := range set {
			sourceFiles[i] = resource.Filename
			strides[i] = resource.Stride
		}
		interleaveKey := strings.Join(sourceFiles, "|") + "#" + fmt.Sprint(strides)
		vb, stride, err := cache.interleavedBuffers(interleaveKey, func() ([]byte, int, error) {
			parts, err := readModelViewerResourceSet(modDir, set, cache)
			if err != nil {
				return nil, 0, err
			}
			vb, stride, _, err := interleaveModelViewerBuffers(parts, strides)
			if err != nil {
				return nil, 0, err
			}
			return vb, stride, nil
		})
		if err != nil {
			return nil, err
		}
		groups = append(groups, modelViewerBufferGroup{Key: key, VBFilename: key + ".vb", VB: vb, Stride: stride, SourceFiles: sourceFiles})
	}
	return groups, nil
}

func readModelViewerResourceSet(modDir string, resources []*modelViewerResource, cache *modelViewerBufferCache) ([][]byte, error) {
	parts := make([][]byte, len(resources))
	for i, resource := range resources {
		bytes, err := cache.read(filepath.Join(modDir, filepath.FromSlash(resource.Filename)))
		if err != nil {
			return nil, fmt.Errorf("read resource %s: %w", resource.Filename, err)
		}
		parts[i] = bytes
	}
	return parts, nil
}

type modelViewerInterleaveValidationError struct {
	msg string
}

func (e *modelViewerInterleaveValidationError) Error() string {
	return e.msg
}

func isModelViewerInterleaveValidationError(err error) bool {
	var target *modelViewerInterleaveValidationError
	return errors.As(err, &target)
}

func interleaveModelViewerBuffers(parts [][]byte, strides []int) ([]byte, int, int, error) {
	if len(parts) == 0 || len(parts) != len(strides) {
		return nil, 0, 0, fmt.Errorf("invalid vertex buffer set")
	}
	stride, vertexCount := 0, -1
	for i, partStride := range strides {
		if partStride <= 0 {
			return nil, 0, 0, fmt.Errorf("vertex buffer stride must be greater than zero")
		}
		if len(parts[i])%partStride != 0 {
			return nil, 0, 0, &modelViewerInterleaveValidationError{
				msg: fmt.Sprintf("vertex buffer length %d is not divisible by stride %d", len(parts[i]), partStride),
			}
		}
		stride += partStride
		count := len(parts[i]) / partStride
		if vertexCount >= 0 && count != vertexCount {
			return nil, 0, 0, &modelViewerInterleaveValidationError{
				msg: fmt.Sprintf("vertex buffer count mismatch: %d != %d", count, vertexCount),
			}
		}
		vertexCount = count
	}
	out := make([]byte, vertexCount*stride)
	for vertex := range vertexCount {
		target := vertex * stride
		for i, part := range parts {
			source := vertex * strides[i]
			copy(out[target:target+strides[i]], part[source:source+strides[i]])
			target += strides[i]
		}
	}
	return out, stride, vertexCount, nil
}

// modelViewerBufferCache mirrors Electron mesh-builder.ts rawBufCache/bufCache:
// every buffer file is read at most once per viewer load, and the interleaved
// position+texcoord view with its detected UV layout is reused across draws
// that share the same buffer pair. Methods are safe for concurrent use; heavy
// builds run outside the lock and may race benignly (last store wins).
type modelViewerFmtCacheEntry struct {
	layout modelViewerFmtLayout
	err    error
}

type modelViewerInterleavedBuffers struct {
	vb     []byte
	stride int
}

type modelViewerBufferCache struct {
	mu          sync.Mutex
	files       map[string][]byte
	indices     map[string][]uint32
	geometries  map[string]*modelViewerGeometry
	pairs       map[string]modelViewerPairedBuffers
	fmts        map[string]modelViewerFmtCacheEntry
	interleaved map[string]modelViewerInterleavedBuffers
}

type modelViewerPairedBuffers struct {
	combined []byte
	stride   int
	uvOffset int
	uvFormat string
	hasFrame bool
}

func newModelViewerBufferCache() *modelViewerBufferCache {
	return &modelViewerBufferCache{files: make(map[string][]byte), indices: make(map[string][]uint32), geometries: make(map[string]*modelViewerGeometry), pairs: make(map[string]modelViewerPairedBuffers), fmts: make(map[string]modelViewerFmtCacheEntry), interleaved: make(map[string]modelViewerInterleavedBuffers)}
}

func (c *modelViewerBufferCache) releaseGeometryScratch() {
	if c == nil {
		return
	}
	c.mu.Lock()
	clear(c.indices)
	clear(c.geometries)
	clear(c.pairs)
	clear(c.fmts)
	clear(c.interleaved)
	c.mu.Unlock()
}

func (c *modelViewerBufferCache) releaseAll() {
	if c == nil {
		return
	}
	c.releaseGeometryScratch()
	c.mu.Lock()
	clear(c.files)
	c.mu.Unlock()
}

// fmtLayout caches a resolved vertex layout per lookup key so IBs referenced
// by several ini files parse their .fmt once per viewer load.
func (c *modelViewerBufferCache) fmtLayout(key string, build func() (modelViewerFmtLayout, error)) (modelViewerFmtLayout, error) {
	c.mu.Lock()
	if entry, ok := c.fmts[key]; ok {
		c.mu.Unlock()
		return entry.layout, entry.err
	}
	c.mu.Unlock()
	layout, err := build()
	c.mu.Lock()
	c.fmts[key] = modelViewerFmtCacheEntry{layout: layout, err: err}
	c.mu.Unlock()
	return layout, err
}

// interleavedBuffers caches an interleaved vertex buffer per source file set
// so buffer groups shared across ini files interleave once per viewer load.
func (c *modelViewerBufferCache) interleavedBuffers(key string, build func() ([]byte, int, error)) ([]byte, int, error) {
	c.mu.Lock()
	if entry, ok := c.interleaved[key]; ok {
		c.mu.Unlock()
		return entry.vb, entry.stride, nil
	}
	c.mu.Unlock()
	vb, stride, err := build()
	if err != nil {
		return nil, 0, err
	}
	c.mu.Lock()
	c.interleaved[key] = modelViewerInterleavedBuffers{vb: vb, stride: stride}
	c.mu.Unlock()
	return vb, stride, nil
}

// geometry caches extracted vertex data per extraction spec so draws that
// resolve to the same buffers, layout, and index range are extracted once per
// viewer load. The cached geometry must stay read-only after the build
// function returns (including any UV post-processing done inside it).
func (c *modelViewerBufferCache) geometry(key string, build func() (*modelViewerGeometry, error)) (*modelViewerGeometry, error) {
	c.mu.Lock()
	if cached, ok := c.geometries[key]; ok {
		c.mu.Unlock()
		return cached, nil
	}
	c.mu.Unlock()
	geometry, err := build()
	if err != nil {
		return nil, err
	}
	if geometry != nil {
		c.mu.Lock()
		c.geometries[key] = geometry
		c.mu.Unlock()
	}
	return geometry, nil
}

func (c *modelViewerBufferCache) read(path string) ([]byte, error) {
	key := strings.ToLower(path)
	c.mu.Lock()
	if data, ok := c.files[key]; ok {
		c.mu.Unlock()
		return data, nil
	}
	c.mu.Unlock()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.files[key] = data
	c.mu.Unlock()
	return data, nil
}

// decodeIndices caches the decoded index buffer per file+format so draws that
// share an IB do not re-decode it. The returned slice must stay read-only.
func (c *modelViewerBufferCache) decodeIndices(path, format string, raw []byte) ([]uint32, error) {
	key := strings.ToLower(path) + "|" + strings.ToUpper(format)
	c.mu.Lock()
	if decoded, ok := c.indices[key]; ok {
		c.mu.Unlock()
		return decoded, nil
	}
	c.mu.Unlock()
	decoded, err := modelViewerDecodeIndices(raw, format)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.indices[key] = decoded
	c.mu.Unlock()
	return decoded, nil
}

func (c *modelViewerBufferCache) paired(posPath string, posStride int, tcPath string, tcStride int) (modelViewerPairedBuffers, error) {
	key := strings.ToLower(posPath) + "|" + strconv.Itoa(posStride) + "|" + strings.ToLower(tcPath) + "|" + strconv.Itoa(tcStride)
	c.mu.Lock()
	if entry, ok := c.pairs[key]; ok {
		c.mu.Unlock()
		return entry, nil
	}
	c.mu.Unlock()
	posRaw, err := c.read(posPath)
	if err != nil {
		return modelViewerPairedBuffers{}, err
	}
	tcRaw, err := c.read(tcPath)
	if err != nil {
		return modelViewerPairedBuffers{}, err
	}
	combined, stride, _, err := interleaveModelViewerBuffers([][]byte{posRaw, tcRaw}, []int{posStride, tcStride})
	if err != nil {
		return modelViewerPairedBuffers{}, err
	}
	uvOffset, uvFormat := detectModelViewerUVBest(combined, stride, posStride, tcStride)
	entry := modelViewerPairedBuffers{
		combined: combined,
		stride:   stride,
		uvOffset: uvOffset,
		uvFormat: uvFormat,
		hasFrame: detectModelViewerPositionFrame(combined, stride),
	}
	c.mu.Lock()
	c.pairs[key] = entry
	c.mu.Unlock()
	return entry, nil
}

func modelViewerNormalizeKey(value string) string {
	var builder strings.Builder
	for _, char := range strings.ToLower(value) {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			builder.WriteRune(char)
		}
	}
	return builder.String()
}

func sortedModelViewerGroupKeys(groups []modelViewerBufferGroup) []string {
	keys := make([]string, len(groups))
	for i := range groups {
		keys[i] = groups[i].Key
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	return keys
}
