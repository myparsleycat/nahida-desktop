package modelviewer

import (
	"context"
	"image"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/platform"
)

const (
	maxWwmiAtlasVertices = 65536
	maxWwmiAtlasSeams    = 4096
)

type wwmiAtlasSamples struct {
	uvs   [][2]float32
	seams [][2]int
}

// Shared dump names identify users of an atlas, not its shader role. Among
// otherwise equivalent atlases, UV seams provide evidence beyond share count.
func refineWwmiDumpAtlases(
	ctx context.Context,
	modDir string,
	meshes []modelViewerDirectMesh,
	resources []modelViewerResource,
) error {
	groups := make(map[string][]*modelViewerDirectMesh)
	for index := range meshes {
		mesh := &meshes[index]
		component := wwmiComponentIndex(mesh.component)
		if component == "" || !mesh.textureDumpInferred || mesh.textureAuthored ||
			wwmiDumpShareCount(mesh.textureDefaultFile) <= 1 {
			continue
		}
		if slices.ContainsFunc(mesh.textureAssignments, func(assignment modelViewerDirectTextureAssignment) bool {
			return assignment.role == "diffuse" &&
				(assignment.file != mesh.textureDefaultFile || !modelViewerDNFIsTrue(assignment.conditions))
		}) {
			continue
		}
		groups[component+"|"+mesh.textureDefaultFile] = append(groups[component+"|"+mesh.textureDefaultFile], mesh)
	}

	type atlas struct {
		hint   *wwmiTextureHint
		pixels *image.NRGBA
	}
	cache := make(map[string]atlas)
	remaining := int64(maxWwmiRecoveryBytes)
	inspect := func(file string) atlas {
		if found, ok := cache[file]; ok {
			return found
		}
		if len(cache) >= maxWwmiRecoveryFiles || !strings.EqualFold(filepath.Ext(file), ".dds") {
			return atlas{}
		}
		cache[file] = atlas{}
		path, err := resolveModelViewerResourcePath(modDir, modDir, file)
		if err != nil {
			return atlas{}
		}
		release, err := diskio.Acquire(ctx, path)
		if err != nil {
			return atlas{}
		}
		defer release()
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > remaining/2 || info.Size() > maxHintDecodeBytes {
			return atlas{}
		}
		remaining -= info.Size() * 2
		hint := inspectWwmiTextureHint(path)
		if hint == nil || hint.Area > maxHintDecodeArea || !isLikelyWwmiDiffuse(*hint) {
			return atlas{}
		}
		pixels, err := decodeModelViewerDDSHint(path, hint.Bytes)
		if err != nil {
			return atlas{}
		}
		found := atlas{hint: hint, pixels: pixels}
		cache[file] = found
		return found
	}
	for _, key := range slices.Sorted(maps.Keys(groups)) {
		group := groups[key]
		if err := ctx.Err(); err != nil {
			return err
		}
		currentFile := group[0].textureDefaultFile
		current := inspect(currentFile)
		if current.pixels == nil {
			continue
		}
		samples := collectWwmiAtlasSamples(group)
		bestScore, valid := samples.score(current.pixels)
		if !valid {
			continue
		}
		threshold := bestScore * 0.65
		bestFile, bestResource := currentFile, ""
		for _, resource := range resources {
			match := wwmiDumpTexRE.FindStringSubmatch(slashPath(resource.Filename))
			if match == nil || resource.Filename == currentFile || len(strings.Split(match[1], "-")) <= 1 ||
				!slices.Contains(strings.Split(match[1], "-"), wwmiComponentIndex(group[0].component)) ||
				slices.ContainsFunc(group, func(mesh *modelViewerDirectMesh) bool {
					return slices.Contains(mesh.nonDiffuseTextureFiles, resource.Filename) ||
						slices.ContainsFunc(
							mesh.textureAssignments,
							func(assignment modelViewerDirectTextureAssignment) bool {
								return assignment.role != "diffuse" &&
									platform.SamePathFold(assignment.file, resource.Filename)
							},
						)
				}) {
				continue
			}
			candidate := inspect(resource.Filename)
			if candidate.pixels == nil || candidate.hint.Area != current.hint.Area ||
				candidate.hint.SRGB != current.hint.SRGB {
				continue
			}
			if score, ok := samples.score(candidate.pixels); ok && score < threshold && score < bestScore {
				bestScore, bestFile, bestResource = score, resource.Filename, resource.Name
			}
		}
		if bestFile == currentFile {
			continue
		}
		for _, mesh := range group {
			mesh.textureDefaultFile = bestFile
			for index := range mesh.textureAssignments {
				assignment := &mesh.textureAssignments[index]
				if assignment.role == "diffuse" {
					assignment.file, assignment.resource = bestFile, bestResource
				}
			}
		}
	}
	return ctx.Err()
}

func collectWwmiAtlasSamples(meshes []*modelViewerDirectMesh) wwmiAtlasSamples {
	result := wwmiAtlasSamples{}
	byPosition := make(map[[3]int64][]int)
	seen := make(map[*modelViewerGeometry]bool)
	for _, mesh := range meshes {
		geometry := mesh.geometry
		if geometry == nil || seen[geometry] || len(geometry.Position) != geometry.VertexCount*3 ||
			len(geometry.Texcoord0) != geometry.VertexCount*2 {
			continue
		}
		seen[geometry] = true
		for vertex := range geometry.VertexCount {
			if len(result.uvs) >= maxWwmiAtlasVertices {
				return result
			}
			var key [3]int64
			for axis := range key {
				key[axis] = int64(math.Round(float64(geometry.Position[vertex*3+axis]) * 10000))
			}
			uv := [2]float32{geometry.Texcoord0[vertex*2], 1 - geometry.Texcoord0[vertex*2+1]}
			if !(uv[0] >= 0 && uv[0] <= 1 && uv[1] >= 0 && uv[1] <= 1) {
				continue
			}
			index := len(result.uvs)
			result.uvs = append(result.uvs, uv)
			for _, other := range byPosition[key] {
				previous := result.uvs[other]
				if math.Abs(float64(previous[0]-uv[0]))+math.Abs(float64(previous[1]-uv[1])) > 0.01 &&
					len(result.seams) < maxWwmiAtlasSeams {
					result.seams = append(result.seams, [2]int{other, index})
				}
			}
			if len(byPosition[key]) < 8 {
				byPosition[key] = append(byPosition[key], index)
			}
		}
	}
	return result
}

func (s wwmiAtlasSamples) score(pixels *image.NRGBA) (float64, bool) {
	if len(s.seams) < 32 || len(s.uvs) == 0 {
		return 0, false
	}
	var sum, square [3]float64
	errorSum := 0.0
	for _, seam := range s.seams {
		var colors [2][3]float64
		for corner, index := range seam {
			uv := s.uvs[index]
			pixel := pixels.NRGBAAt(
				int(float32(pixels.Bounds().Dx()-1)*uv[0]),
				int(float32(pixels.Bounds().Dy()-1)*uv[1]),
			)
			colors[corner] = [3]float64{float64(pixel.R), float64(pixel.G), float64(pixel.B)}
			for channel, value := range colors[corner] {
				sum[channel] += value
				square[channel] += value * value
			}
		}
		for channel := range 3 {
			difference := colors[0][channel] - colors[1][channel]
			errorSum += difference * difference
		}
	}
	count := float64(len(s.seams) * 2)
	variance := 0.0
	for channel := range sum {
		mean := sum[channel] / count
		variance += square[channel]/count - mean*mean
	}

	// Blank seam regions can look continuous by accident. Only the seam samples
	// provide contrast evidence; variation elsewhere must not validate them.
	if variance < 75 {
		return 0, false
	}
	return errorSum / float64(len(s.seams)) / variance, true
}
