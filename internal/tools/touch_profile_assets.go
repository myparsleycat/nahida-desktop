package tools

import (
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const touchVisionSeedInfluence = .32

type TouchGeneratedObjectMap struct {
	Label        string `json:"label"`
	RelativePath string `json:"relativePath"`
	AbsolutePath string `json:"absolutePath"`
}

type TouchGeneratedAssets struct {
	ComponentID         string                    `json:"componentId"`
	AssetPrefix         string                    `json:"assetPrefix"`
	RelativeDir         string                    `json:"relativeDir"`
	MaskPaths           []string                  `json:"maskPaths"`
	ObjectMapPaths      []TouchGeneratedObjectMap `json:"objectMapPaths"`
	ParamsRelativePath  string                    `json:"paramsRelativePath"`
	ParamsAbsolutePath  string                    `json:"paramsAbsolutePath"`
	PreviewRelativePath string                    `json:"previewRelativePath"`
	PreviewAbsolutePath string                    `json:"previewAbsolutePath"`
	Masks               []float32                 `json:"masks"`
}

type touchSeedCell struct{ X, Y, Z int }
type touchSeedGrid struct {
	Inverse float64
	Buckets map[touchSeedCell][]int
}

func buildTouchVertexMasks(vertexCount int, positions []float32, indices []uint32, component TouchComponentAnalysis, zones []TouchZoneSpec) []float32 {
	masks, _ := buildTouchVertexMasksContext(context.Background(), vertexCount, positions, indices, component, zones)
	return masks
}

func buildTouchVertexMasksContext(ctx context.Context, vertexCount int, positions []float32, indices []uint32, component TouchComponentAnalysis, zones []TouchZoneSpec) ([]float32, error) {
	masks := make([]float32, vertexCount*touchZoneChannels)
	allowed := touchAllowedVertices(component, indices, vertexCount)
	minX, maxX := math.Inf(1), math.Inf(-1)
	for vertex, yes := range allowed {
		if yes {
			x := float64(positions[vertex*3])
			minX = math.Min(minX, x)
			maxX = math.Max(maxX, x)
		}
	}
	if math.IsInf(minX, 0) {
		minX, maxX = -1, 1
	}
	midX, spanX := (minX+maxX)*.5, math.Max(maxX-minX, .001)
	clipStart, clipEnd := spanX*.03, spanX*.08
	for _, zone := range zones {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if zone.Channel < 0 || zone.Channel >= touchZoneChannels {
			continue
		}
		strength := clampFinite(zone.Settings.MaskStrength, 0, 2, 1)
		curve := clampFinite(zone.Settings.MaskCurve, 0, 2, 1)
		radiusScale := clampFinite(zone.Settings.MaskRadiusScale, .1, 2, 1)
		core := touchCoreAttenuation(radiusScale, zone.Settings.MaskCoreAttenuation)
		seeds := make([]int, 0, len(zone.Seeds))
		for _, seed := range zone.Seeds {
			if seed >= 0 && seed < vertexCount {
				seeds = append(seeds, seed)
			}
		}
		seedInfluence := 0.0
		if len(seeds) > 0 {
			minRadius := math.Min(zone.Radius[0], math.Min(zone.Radius[1], zone.Radius[2]))
			maxRadius := math.Max(zone.Radius[0], math.Max(zone.Radius[1], zone.Radius[2]))
			seedInfluence = math.Max(math.Max(minRadius*touchVisionSeedInfluence, maxRadius*.2), .02) * radiusScale
		}
		seedInfluence2 := seedInfluence * seedInfluence
		cutoff := 1.0
		fade := .3 * .55 / .55
		var grid *touchSeedGrid
		if len(seeds) >= 64 && seedInfluence > 1e-6 {
			grid = buildTouchSeedGrid(positions, seeds, seedInfluence)
		}
		for vertex := range vertexCount {
			if vertex&1023 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			if !allowed[vertex] {
				continue
			}
			px, py, pz := float64(positions[vertex*3]), float64(positions[vertex*3+1]), float64(positions[vertex*3+2])
			weight := 0.0
			if len(seeds) > 0 {
				nearest := math.Inf(1)
				if grid != nil {
					nearest = nearestTouchSeed2(positions, *grid, px, py, pz, cutoff)
				} else {
					for _, seed := range seeds {
						dx := float64(positions[seed*3]) - px
						dy := float64(positions[seed*3+1]) - py
						dz := float64(positions[seed*3+2]) - pz
						nearest = math.Min(nearest, dx*dx+dy*dy+dz*dz)
					}
				}
				if nearest >= seedInfluence2*cutoff {
					continue
				}
				v := nearest / seedInfluence2
				weight = math.Pow(1-v/cutoff, curve) * (1 - touchSmoothstep(cutoff-fade, cutoff, v)) * core
			} else {
				dx := (px - zone.Center[0]) / (zone.Radius[0] * radiusScale)
				dy := (py - zone.Center[1]) / (zone.Radius[1] * radiusScale)
				dz := (pz - zone.Center[2]) / (zone.Radius[2] * radiusScale)
				d2 := dx*dx + dy*dy + dz*dz
				if d2 >= 2.25 {
					continue
				}
				weight = math.Pow(math.Exp(-1.35*d2), curve) * (1 - touchSmoothstep(1.7, 2.25, d2))
			}
			lowerID := strings.ToLower(zone.ID)
			if strings.Contains(lowerID, "left") && px > midX+clipStart {
				weight *= 1 - touchSmoothstep(0, clipEnd, px-(midX+clipStart))
			}
			if strings.Contains(lowerID, "right") && px < midX-clipStart {
				weight *= 1 - touchSmoothstep(0, clipEnd, midX-clipStart-px)
			}
			offset := vertex*touchZoneChannels + zone.Channel
			masks[offset] = max(masks[offset], float32(weight*strength))
		}
	}
	if err := smoothTouchMasksContext(ctx, masks, vertexCount, indices); err != nil {
		return nil, err
	}
	for i, value := range masks {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) || value < 0 {
			masks[i] = 0
		} else if value > 1 {
			masks[i] = 1
		}
	}
	return masks, nil
}

func buildTouchSeedGrid(positions []float32, seeds []int, size float64) *touchSeedGrid {
	grid := &touchSeedGrid{Inverse: 1 / size, Buckets: map[touchSeedCell][]int{}}
	for _, seed := range seeds {
		cell := touchSeedCell{int(math.Floor(float64(positions[seed*3]) * grid.Inverse)), int(math.Floor(float64(positions[seed*3+1]) * grid.Inverse)), int(math.Floor(float64(positions[seed*3+2]) * grid.Inverse))}
		grid.Buckets[cell] = append(grid.Buckets[cell], seed)
	}
	return grid
}
func nearestTouchSeed2(positions []float32, grid touchSeedGrid, px, py, pz, cutoff float64) float64 {
	radius := int(math.Ceil(math.Sqrt(cutoff)))
	cx, cy, cz := int(math.Floor(px*grid.Inverse)), int(math.Floor(py*grid.Inverse)), int(math.Floor(pz*grid.Inverse))
	nearest := math.Inf(1)
	for dx := -radius; dx <= radius; dx++ {
		for dy := -radius; dy <= radius; dy++ {
			for dz := -radius; dz <= radius; dz++ {
				for _, seed := range grid.Buckets[touchSeedCell{cx + dx, cy + dy, cz + dz}] {
					sx := float64(positions[seed*3]) - px
					sy := float64(positions[seed*3+1]) - py
					sz := float64(positions[seed*3+2]) - pz
					nearest = math.Min(nearest, sx*sx+sy*sy+sz*sz)
				}
			}
		}
	}
	return nearest
}

func touchAllowedVertices(component TouchComponentAnalysis, indices []uint32, count int) []bool {
	allowed := make([]bool, count)
	ranges := component.DrawRanges
	if len(ranges) == 0 {
		ranges = make([]TouchDrawRange, len(component.ObjectMaps))
		for i, entry := range component.ObjectMaps {
			ranges[i] = TouchDrawRange{FirstIndex: entry.FirstIndex, IndexCount: entry.IndexCount}
		}
	}
	for _, entry := range ranges {
		end := min(len(indices), entry.FirstIndex+entry.IndexCount)
		for i := max(0, entry.FirstIndex); i < end; i++ {
			if indices[i] < uint32(count) {
				allowed[indices[i]] = true
			}
		}
	}
	any := false
	for _, v := range allowed {
		any = any || v
	}
	if !any {
		for i := range allowed {
			allowed[i] = true
		}
	}
	return allowed
}

func smoothTouchMasksContext(ctx context.Context, masks []float32, vertexCount int, indices []uint32) error {
	adj := make([][]int, vertexCount)
	for i := 0; i+2 < len(indices); i += 3 {
		if i&4095 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		a, b, c := int(indices[i]), int(indices[i+1]), int(indices[i+2])
		if a < vertexCount && b < vertexCount {
			adj[a] = append(adj[a], b)
			adj[b] = append(adj[b], a)
		}
		if a < vertexCount && c < vertexCount {
			adj[a] = append(adj[a], c)
			adj[c] = append(adj[c], a)
		}
		if b < vertexCount && c < vertexCount {
			adj[b] = append(adj[b], c)
			adj[c] = append(adj[c], b)
		}
	}
	next := append([]float32(nil), masks...)
	for vertex, neighbors := range adj {
		if vertex&1023 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if len(neighbors) == 0 {
			continue
		}
		for channel := range touchZoneChannels {
			sum := masks[vertex*touchZoneChannels+channel]
			for _, neighbor := range neighbors {
				sum += masks[neighbor*touchZoneChannels+channel]
			}
			next[vertex*touchZoneChannels+channel] = sum / float32(len(neighbors)+1)
		}
	}
	copy(masks, next)
	return nil
}

func extractTouchMaskChannel(masks []float32, vertexCount, channel int) []float32 {
	weights := make([]float32, vertexCount)
	if channel < 0 || channel >= touchZoneChannels {
		return weights
	}
	for vertex := range vertexCount {
		weights[vertex] = masks[vertex*touchZoneChannels+channel]
	}
	return weights
}

func writeTouchComponentAssets(outputRoot string, component TouchComponentAnalysis, draft TouchComponentDraft, positions []float32, indices []uint32, prefix string) (TouchGeneratedAssets, error) {
	relativeDir := filepath.Join("Resources", "IM")
	absoluteDir := filepath.Join(outputRoot, relativeDir)
	if err := os.MkdirAll(absoluteDir, 0755); err != nil {
		return TouchGeneratedAssets{}, err
	}
	masks := buildTouchVertexMasks(component.VertexCount, positions, indices, component, draft.Zones)
	result := TouchGeneratedAssets{ComponentID: component.ID, AssetPrefix: prefix, RelativeDir: filepath.ToSlash(relativeDir), Masks: masks, MaskPaths: []string{}, ObjectMapPaths: []TouchGeneratedObjectMap{}}
	for band := range touchMaskBands {
		name := prefix + "JiggleMasks" + string(rune('0'+band)) + ".buf"
		absolute := filepath.Join(absoluteDir, name)
		values := make([]float32, component.VertexCount*4)
		for vertex := range component.VertexCount {
			copy(values[vertex*4:vertex*4+4], masks[vertex*touchZoneChannels+band*4:vertex*touchZoneChannels+band*4+4])
		}
		if err := writeTouchFloat32File(absolute, values); err != nil {
			return TouchGeneratedAssets{}, err
		}
		result.MaskPaths = append(result.MaskPaths, filepath.ToSlash(filepath.Join(relativeDir, name)))
	}
	maps := component.ObjectMaps
	if len(maps) == 0 {
		first, count := 0, component.IndexCount
		if len(component.DrawRanges) > 0 {
			first, count = component.DrawRanges[0].FirstIndex, component.DrawRanges[0].IndexCount
		}
		maps = []TouchObjectMapEntry{{FirstIndex: first, IndexCount: count, ObjectMode: touchObjectMode, ObjectID: draft.ObjectID, Label: "main"}}
	}
	for _, entry := range maps {
		entry.ObjectID = draft.ObjectID
		entry.ObjectMode = touchObjectMode
		labelName := entry.Label
		if labelName == "main" || labelName == "skin" {
			labelName = ""
		} else {
			labelName = strings.ToUpper(labelName[:1]) + labelName[1:]
		}
		name := prefix + labelName + "ObjectMap.buf"
		absolute := filepath.Join(absoluteDir, name)
		if err := writeTouchFloat32File(absolute, encodeTouchObjectMap([]TouchObjectMapEntry{entry})); err != nil {
			return TouchGeneratedAssets{}, err
		}
		result.ObjectMapPaths = append(result.ObjectMapPaths, TouchGeneratedObjectMap{Label: entry.Label, RelativePath: filepath.ToSlash(filepath.Join(relativeDir, name)), AbsolutePath: absolute})
	}
	paramsName := prefix + "JiggleParams.buf"
	paramsAbs := filepath.Join(absoluteDir, paramsName)
	settings := defaultTouchZoneSettings()
	if len(draft.Zones) > 0 {
		settings = draft.Zones[0].Settings
	}
	if err := writeTouchFloat32File(paramsAbs, encodeTouchJiggleParams(resolveTouchJiggleParams(settings, draft.ObjectID))); err != nil {
		return TouchGeneratedAssets{}, err
	}
	result.ParamsRelativePath = filepath.ToSlash(filepath.Join(relativeDir, paramsName))
	result.ParamsAbsolutePath = paramsAbs
	previewName := prefix + "TouchMaskPreview.png"
	previewAbs := filepath.Join(absoluteDir, previewName)
	if err := writeTouchMaskPreview(previewAbs, positions, masks); err != nil {
		return TouchGeneratedAssets{}, err
	}
	result.PreviewRelativePath = filepath.ToSlash(filepath.Join(relativeDir, previewName))
	result.PreviewAbsolutePath = previewAbs
	return result, nil
}

func encodeTouchObjectMap(entries []TouchObjectMapEntry) []float32 {
	values := make([]float32, (1+len(entries))*4)
	values[0] = float32(len(entries))
	for i, entry := range entries {
		offset := (i + 1) * 4
		values[offset] = float32(entry.FirstIndex)
		values[offset+1] = float32(entry.IndexCount)
		values[offset+2] = float32(entry.ObjectMode)
		if entry.ObjectMode == 0 {
			values[offset+2] = touchObjectMode
		}
		values[offset+3] = float32(entry.ObjectID)
	}
	return values
}
func encodeTouchJiggleParams(p TouchJiggleParams) []float32 {
	return []float32{float32(p.ObjectID), float32(p.Radius), float32(p.Strength), float32(p.Falloff), float32(p.DragScale), float32(p.GrabDamping), float32(p.GrabSpring), float32(p.ReleaseDamping), float32(p.ReleaseSpring), float32(p.ReleaseKick), float32(p.MaxOffset), float32(p.TargetFollow), float32(p.MouseYDirection), float32(p.MouseXDirection), 0, 0}
}
func writeTouchFloat32File(path string, values []float32) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	writeErr := binary.Write(file, binary.LittleEndian, values)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func touchAssetPrefix(component TouchComponentAnalysis, namespace string) string {
	kind := regexp.MustCompile(`(?i)position`).ReplaceAllString(component.Name, "")
	kind = regexp.MustCompile(`[^a-zA-Z0-9]+`).ReplaceAllString(kind, "")
	if component.Kind == "body" {
		kind = "Body"
	} else if component.Kind == "legs" {
		kind = "Leg"
	} else if kind == "" {
		kind = "Mesh"
	}
	ns := regexp.MustCompile(`(?i)`+regexp.QuoteMeta(kind)+`$`).ReplaceAllString(namespace, "")
	if ns == "" {
		ns = "Nhd"
	}
	variant := ""
	if component.VariantKey != nil {
		variant = "V" + *component.VariantKey
	}
	return ns + kind + variant
}

func writeTouchMaskPreview(path string, positions, masks []float32) error {
	const size = 768
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			img.SetNRGBA(x, y, color.NRGBA{A: 255})
		}
	}
	minX, maxX, minZ, maxZ := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
	for i := range len(positions) / 3 {
		x, z := float64(positions[i*3]), float64(positions[i*3+2])
		minX = math.Min(minX, x)
		maxX = math.Max(maxX, x)
		minZ = math.Min(minZ, z)
		maxZ = math.Max(maxZ, z)
	}
	scale := math.Min(float64(size-40)/math.Max(maxX-minX, 1e-6), float64(size-40)/math.Max(maxZ-minZ, 1e-6))
	cx, cz := (minX+maxX)*.5, (minZ+maxZ)*.5
	palette := [][3]uint8{{255, 70, 70}, {70, 130, 255}, {255, 190, 50}, {180, 70, 255}}
	for i := range len(positions) / 3 {
		x := int(math.Round((float64(positions[i*3])-cx)*scale + size/2))
		y := int(math.Round(size/2 - (float64(positions[i*3+2])-cz)*scale))
		if x < 0 || y < 0 || x >= size || y >= size {
			continue
		}
		best, channel := float32(0), 0
		for c := range 4 {
			if masks[i*touchZoneChannels+c] > best {
				best = masks[i*touchZoneChannels+c]
				channel = c
			}
		}
		if best <= 0 {
			img.SetNRGBA(x, y, color.NRGBA{45, 45, 50, 255})
		} else {
			gain := .25 + .75*float64(best)
			p := palette[channel]
			img.SetNRGBA(x, y, color.NRGBA{uint8(math.Min(255, float64(p[0])*gain)), uint8(math.Min(255, float64(p[1])*gain)), uint8(math.Min(255, float64(p[2])*gain)), 255})
		}
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	encodeErr := png.Encode(file, img)
	closeErr := file.Close()
	if encodeErr != nil {
		return encodeErr
	}
	return closeErr
}

func clampFinite(value, low, high, fallback float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fallback
	}
	return math.Max(low, math.Min(high, value))
}
func touchSmoothstep(edge0, edge1, value float64) float64 {
	normalized := math.Max(0, math.Min(1, (value-edge0)/(edge1-edge0)))
	return normalized * normalized * (3 - 2*normalized)
}
func touchCoreAttenuation(scale float64, mode string) float64 {
	if mode == "off" || scale >= 1 {
		return 1
	}
	if mode == "linear" {
		return scale
	}
	if mode == "sqrt" {
		return math.Sqrt(scale)
	}
	return math.Pow(scale, .4)
}
