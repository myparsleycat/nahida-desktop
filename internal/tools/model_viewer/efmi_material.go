package modelviewer

import (
	"encoding/binary"
	"image"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/samber/lo"
)

const (
	efmiLightMapRole  = "light_map"
	efmiNormalMapRole = "normal_map"

	// EFMI reads a material map as the face's _SDFMask.
	efmiFaceMaskRole      = "material_map"
	efmiFaceHighlightRole = modelViewerHighlightMapRole

	// A shader binds its diffuse, light, and normal maps in consecutive slots,
	// or only the diffuse and normal maps, so its maps sit within this distance.
	efmiMaterialMapSlotReach = 2
	efmiMinMaterialMapSide   = 16

	// A dump drawn by more components than one material plausibly spans is shared
	// detail noise, even when its channels pass as a normal map.
	efmiMaxSharedMapComponents = 4

	efmiNormalDiskFraction = 0.85
	efmiNormalMaxRadius    = 0.5
	efmiMapSampleCount     = 4096
	efmiGrayChannelSpread  = 8
	efmiGrayFraction       = 0.98

	efmiFaceMaskCenterMaxGreen    = 24
	efmiFaceMaskMinEdgeFraction   = 0.05
	efmiFaceMaskMaxMeanAlpha      = 96
	efmiFaceHighlightMaxMean      = 8
	efmiFaceHighlightMinPeak      = 32
	efmiFaceHighlightOpaqueBeyond = 250

	// _DiffRampMap is sampled along one row and its alpha is the lit coverage. Mods also
	// name _SpecRampMap files RampMap; those are 2D, and their alpha is opaque.
	efmiDiffRampMinWidth     = 64
	efmiDiffRampMaxHeight    = 8
	efmiDiffRampMaxDarkAlpha = 64
	efmiDiffRampMinLitAlpha  = 200
)

var (
	efmiNonMaterialMapRE = regexp.MustCompile(`(?i)high\s*light|ramp|sdf|matcap|mask|emissi|glow`)
	efmiNamedNormalMapRE = regexp.MustCompile(`(?i)normal`)
	efmiNamedLightMapRE  = regexp.MustCompile(`(?i)light\s*map`)
	efmiNamedRampMapRE   = regexp.MustCompile(`(?i)ramp`)
)

// attachEFMIMaterialMaps recovers the light and normal maps that unlabeled EFMI
// mods bind beside their diffuse, either at the slots after it or as hash
// replacements that dumped the same component list. Roles a name already
// settled stay as they are.
func attachEFMIMaterialMaps(meshes []modelViewerDirectMesh, resources []modelViewerResource, modDir string) {
	cache := make(map[string]string)
	classify := func(resource, file string) string {
		key := slashPath(file)
		role, cached := cache[key]
		if !cached {
			role = classifyEFMIMaterialMap(resource, file, modDir)
			cache[key] = role
		}
		return role
	}
	for index := range meshes {
		mesh := &meshes[index]

		// RabbitFX templates bind the face's mask in the light map slot, where it would
		// read as a metallic map.
		mesh.textureAssignments = lo.Reject(
			mesh.textureAssignments,
			func(assignment modelViewerDirectTextureAssignment, _ int) bool {
				if assignment.role != efmiLightMapRole || assignment.file == "" {
					return false
				}
				role := classify(assignment.resource, assignment.file)
				return role == efmiFaceMaskRole || role == efmiFaceHighlightRole
			},
		)
		attachEFMISlotMaps(mesh, classify)
		attachEFMIRampMap(mesh, classify)
		attachEFMIDumpMaps(mesh, resources, modDir, classify)
	}
}

func attachEFMISlotMaps(mesh *modelViewerDirectMesh, classify func(resource, file string) string) {
	bySlot := make(map[int][]modelViewerDirectTextureAssignment)
	for _, candidate := range mesh.slotTextureCandidates {
		if slot, ok := modelViewerSlotTextureNumber(candidate.role); ok && candidate.file != "" {
			bySlot[slot] = append(bySlot[slot], candidate)
		}
	}
	if len(bySlot) == 0 {
		return
	}
	slots := lo.Keys(bySlot)
	sort.Ints(slots)

	// A diffuse bound by name or at ps-t0 is no candidate. The lowest slot then
	// holds the first map after it.
	first, last := slots[0], slots[0]+efmiMaterialMapSlotReach-1
	for _, slot := range slots {
		if slices.ContainsFunc(bySlot[slot], func(candidate modelViewerDirectTextureAssignment) bool {
			return meshBindsDiffuseFile(mesh, candidate.file)
		}) {
			first, last = slot+1, slot+efmiMaterialMapSlotReach
			break
		}
	}
	for _, slot := range slots {
		if slot < first || slot > last {
			continue
		}
		roles := lo.GroupBy(bySlot[slot], func(candidate modelViewerDirectTextureAssignment) string {
			if meshBindsDiffuseFile(mesh, candidate.file) {
				return ""
			}
			return classify(candidate.resource, candidate.file)
		})
		for _, role := range []string{efmiLightMapRole, efmiNormalMapRole} {
			if len(roles[role]) == 0 || meshHasRole(mesh, role) {
				continue
			}
			for _, candidate := range roles[role] {
				candidate.role = role
				mesh.textureAssignments = mergeModelViewerTextureAssignment(mesh.textureAssignments, candidate)
			}
			break
		}
	}
}

// A ramp sits at no fixed distance from the diffuse: mods bind it at ps-t11 or ps-t12.
// The viewer draws one ramp per mesh, so a variant guard around the binding is dropped.
func attachEFMIRampMap(mesh *modelViewerDirectMesh, classify func(resource, file string) string) {
	for _, candidate := range mesh.slotTextureCandidates {
		if candidate.file == "" || classify(candidate.resource, candidate.file) != modelViewerRampMapRole {
			continue
		}
		candidate.role = modelViewerRampMapRole
		candidate.conditions = modelViewerDNFTrue()
		mesh.textureAssignments = mergeModelViewerTextureAssignment(mesh.textureAssignments, candidate)
	}
}

func meshBindsDiffuseFile(mesh *modelViewerDirectMesh, file string) bool {
	return slashPath(mesh.textureDefaultFile) == slashPath(file) ||
		slices.ContainsFunc(mesh.textureAssignments, func(assignment modelViewerDirectTextureAssignment) bool {
			return assignment.role == "diffuse" && slashPath(assignment.file) == slashPath(file)
		})
}

// attachEFMIDumpMaps pairs a dumped diffuse with the dumps of the component list
// it came from, because one material's textures are drawn by the same components.
// A map shared more widely must at least match the atlas resolution, except the
// face's mask and highlight, which the game authors at a fixed size.
func attachEFMIDumpMaps(
	mesh *modelViewerDirectMesh,
	resources []modelViewerResource,
	modDir string,
	classify func(resource, file string) string,
) {
	diffuse := wwmiDumpTexRE.FindStringSubmatch(slashPath(mesh.textureDefaultFile))
	if diffuse == nil {
		return
	}
	component := wwmiComponentIndex(mesh.component)
	diffuseArea := peekImageArea(mesh.textureDefaultFile, modDir)
	for _, role := range []string{efmiLightMapRole, efmiNormalMapRole, efmiFaceMaskRole, efmiFaceHighlightRole} {
		if meshHasRole(mesh, role) {
			continue
		}
		var best modelViewerResource
		bestExact, bestArea := false, -1
		for _, resource := range resources {
			match := wwmiDumpTexRE.FindStringSubmatch(slashPath(resource.Filename))
			if match == nil || meshBindsDiffuseFile(mesh, resource.Filename) ||
				classify(resource.Name, resource.Filename) != role ||
				meshTextureBelongsToOtherState(mesh, resource.Name) {
				continue
			}
			exact := match[1] == diffuse[1]
			area := peekImageArea(resource.Filename, modDir)
			components := strings.Split(match[1], "-")
			faceMap := role == efmiFaceMaskRole || role == efmiFaceHighlightRole
			if !exact && (len(components) > efmiMaxSharedMapComponents ||
				!slices.Contains(components, component) || !faceMap && area != diffuseArea) {
				continue
			}
			if exact && !bestExact || exact == bestExact &&
				(area > bestArea || area == bestArea && resource.Filename < best.Filename) {
				best, bestExact, bestArea = resource, exact, area
			}
		}
		if best.Filename != "" {
			mesh.textureAssignments = mergeModelViewerTextureAssignment(
				mesh.textureAssignments,
				modelViewerDirectTextureAssignment{
					role:       role,
					resource:   best.Name,
					file:       best.Filename,
					conditions: modelViewerDNFTrue(),
				},
			)
		}
	}
}

// classifyEFMIMaterialMap returns the role of a texture that may be a light or
// normal map, or "" for anything else. An unnamed map is told apart by its red
// and green channels: normal XY stay near the center of the unit disk, while the
// light map holds opposite extremes there (red near 0, green near 1).
func classifyEFMIMaterialMap(resource, file, modDir string) string {
	label := resource + " " + filepath.Base(slashPath(file))
	if efmiNamedRampMapRE.MatchString(label) {
		return classifyEFMIRampMap(file, modDir)
	}
	switch {
	case efmiNonMaterialMapRE.MatchString(label):
		return ""
	case efmiNamedNormalMapRE.MatchString(label):
		return efmiNormalMapRole
	case efmiNamedLightMapRE.MatchString(label):
		return efmiLightMapRole
	}
	path := viewerResourcePath(modDir, file)
	if path == "" || !strings.EqualFold(filepath.Ext(path), ".dds") {
		return ""
	}

	header := readFilePrefix(path, 148)
	if len(header) < 128 {
		return ""
	}
	if min(binary.LittleEndian.Uint32(header[12:16]), binary.LittleEndian.Uint32(header[16:20])) <
		efmiMinMaterialMapSide {
		return ""
	}
	srgb := parseDdsSrgbState(header)
	if srgb != nil && *srgb {
		return ""
	}
	fourCC := string(header[84:88])
	dxgi := uint32(0)
	if fourCC == "DX10" && len(header) >= 132 {
		dxgi = binary.LittleEndian.Uint32(header[128:132])
	}
	switch {
	case fourCC == "ATI1" || fourCC == "BC4U" || fourCC == "BC4S" || dxgi == 80 || dxgi == 81:
		return ""
	case ddsPackedFourCC[fourCC] || ddsPackedDXGI[dxgi]:
		return efmiNormalMapRole
	}

	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	decoded, err := decodeModelViewerDDSHint(path, info.Size())
	if err != nil {
		return ""
	}
	if role := efmiFaceMapRoleFromPixels(decoded); srgb != nil && role != "" {
		return role
	}
	role := efmiMapRoleFromPixels(decoded)

	// A container without a color space can hold a diffuse, whose channels also
	// pass as a light map. Only the narrow normal signature is safe to accept.
	if srgb == nil && role == efmiLightMapRole {
		return ""
	}
	return role
}

func classifyEFMIRampMap(file, modDir string) string {
	path := viewerResourcePath(modDir, file)
	if path == "" || !strings.EqualFold(filepath.Ext(path), ".dds") {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	decoded, err := decodeModelViewerDDSHint(path, info.Size())
	if err != nil || !efmiIsDiffRamp(decoded) {
		return ""
	}
	return modelViewerRampMapRole
}

func efmiIsDiffRamp(img *image.NRGBA) bool {
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	if width < efmiDiffRampMinWidth || height > efmiDiffRampMaxHeight {
		return false
	}
	row := img.Pix[(height/2)*img.Stride:]
	return row[3] <= efmiDiffRampMaxDarkAlpha && row[(width-1)*4+3] >= efmiDiffRampMinLitAlpha
}

// efmiFaceMapRoleFromPixels recognizes the shared face maps. The highlight map is
// opaque black apart from a faint gray nose highlight. The mask's green channel,
// which selects N.L over the SDF, is off across the face and on around its edge.
func efmiFaceMapRoleFromPixels(img *image.NRGBA) string {
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	stride := max(1, int(math.Sqrt(float64(width*height)/efmiMapSampleCount)))
	samples, centerSamples, edge, opaqueGray := 0, 0, 0, 0
	redSum, alphaSum, centerGreenSum, peak := 0, 0, 0, 0
	for y := 0; y < height; y += stride {
		for x := 0; x < width; x += stride {
			offset := y*img.Stride + x*4
			red, green, blue := int(img.Pix[offset]), int(img.Pix[offset+1]), int(img.Pix[offset+2])
			alpha := int(img.Pix[offset+3])
			samples++
			redSum += red
			alphaSum += alpha
			peak = max(peak, red)
			if alpha >= efmiFaceHighlightOpaqueBeyond &&
				max(red, green, blue)-min(red, green, blue) <= efmiGrayChannelSpread {
				opaqueGray++
			}
			if green >= 200 {
				edge++
			}
			if x*5 >= width*2 && x*5 < width*3 && y*5 >= height*2 && y*5 < height*4 {
				centerSamples++
				centerGreenSum += green
			}
		}
	}
	if samples == 0 || centerSamples == 0 {
		return ""
	}
	if opaqueGray == samples && redSum/samples < efmiFaceHighlightMaxMean && peak >= efmiFaceHighlightMinPeak {
		return efmiFaceHighlightRole
	}
	if centerGreenSum/centerSamples <= efmiFaceMaskCenterMaxGreen &&
		float64(edge) >= efmiFaceMaskMinEdgeFraction*float64(samples) &&
		alphaSum/samples <= efmiFaceMaskMaxMeanAlpha {
		return efmiFaceMaskRole
	}
	return ""
}

func efmiMapRoleFromPixels(img *image.NRGBA) string {
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	stride := max(1, int(math.Sqrt(float64(width*height)/efmiMapSampleCount)))
	samples, used, inDisk, gray := 0, 0, 0, 0
	radiusSum := 0.0
	for y := 0; y < height; y += stride {
		for x := 0; x < width; x += stride {
			offset := y*img.Stride + x*4
			red, green, blue := int(img.Pix[offset]), int(img.Pix[offset+1]), int(img.Pix[offset+2])
			samples++
			if max(red, green, blue)-min(red, green, blue) <= efmiGrayChannelSpread {
				gray++
			}

			// Atlas gaps are filled with solid black or white.
			if (red <= 5 && green <= 5) || (red >= 250 && green >= 250) {
				continue
			}
			used++
			normalX, normalY := float64(red)/127.5-1, float64(green)/127.5-1
			if radius := math.Hypot(normalX, normalY); radius <= 1.02 {
				inDisk++
				radiusSum += radius
			}
		}
	}
	if inDisk > 0 && float64(inDisk) >= efmiNormalDiskFraction*float64(used) &&
		radiusSum/float64(inDisk) <= efmiNormalMaxRadius && float64(gray) < efmiGrayFraction*float64(samples) {
		return efmiNormalMapRole
	}

	// Single-channel masks and shared dummies are stored as gray.
	if float64(gray) >= efmiGrayFraction*float64(samples) {
		return ""
	}
	return efmiLightMapRole
}
