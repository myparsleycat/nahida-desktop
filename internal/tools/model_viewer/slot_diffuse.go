package modelviewer

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/samber/lo"
)

var modelViewerEFMIMarkerRE = regexp.MustCompile(
	`(?i)(?:global\s+\$required_efmi_version\b|(?:Resource|CommandList|\$)\\EFMIv1\\)`,
)

func isModelViewerEFMI(sections []modINISection) bool {
	for _, section := range sections {
		for _, line := range section.Lines {
			if modelViewerEFMIMarkerRE.MatchString(line) {
				return true
			}
		}
	}
	return false
}

// wwmiSlotMapRoles is the order WuWa character shaders read their maps from
// ps-t0 to ps-t3.
var wwmiSlotMapRoles = []string{"normal_map", "light_map", "material_map", "diffuse"}

func modelViewerSlotTextureRole(slot int) string {
	return "pst" + strconv.Itoa(slot)
}

func modelViewerSlotTextureNumber(role string) (int, bool) {
	slot, ok := modelViewerPsSlotNumber(role)
	return slot, ok && slot != 0
}

// attachSlotBoundDiffuse textures meshes from the game shader's ps-t slots.
// EFMI mods can bind their own textures straight to those slots under names
// that carry no role (Resource_A1), and the diffuse slot differs per shader, so
// an EFMI mesh every other source left untextured takes a slot texture.
// WWMI and EFMI shaders do not keep the diffuse at ps-t0 either: a component
// whose unnamed ps-t0 DDS is a linear, packed, flat, or normal map swaps it for
// an sRGB slot texture. Legacy DDS files can lack a declared color space, so
// decoded hints also identify helper maps; PNG bindings keep their ps-t0 file.
// An sRGB ps-t0 DDS dominated by one primary color is a mask, such as a face's
// effect map, and yields only to a slot texture that is not a mask itself.
// The lowest slot holding a fitting file wins, matching the game's
// diffuse-first slot order, and keeps every conditional assignment of that
// slot so texture toggles still switch. The caller drops the candidates, so slot
// roles never reach the payload.
func attachSlotBoundDiffuse(meshes []modelViewerDirectMesh, modDir string, wwmi, efmi bool) {
	hints := make(map[string]*wwmiTextureHint)
	hintOf := func(file string) *wwmiTextureHint {
		// The hint reads any other file as an unknown color space, which counts as diffuse.
		if !hashImageExtRE.MatchString(file) {
			return nil
		}
		resolved := viewerResourcePath(modDir, file)
		if resolved == "" {
			return nil
		}
		hint, cached := hints[resolved]
		if !cached {
			hint = inspectWwmiTextureHint(resolved)
			hints[resolved] = hint
		}
		return hint
	}
	likelyDiffuse := func(file string) bool {
		hint := hintOf(file)
		return hint != nil && isLikelyWwmiDiffuse(*hint)
	}
	declaredColorSpace := func(file string) string {
		if !strings.EqualFold(filepath.Ext(file), ".dds") {
			return ""
		}
		if hint := hintOf(file); hint != nil {
			return hint.ColorSpace
		}
		return ""
	}
	srgbDiffuse := func(file string) bool {
		return declaredColorSpace(file) == "srgb" && likelyDiffuse(file)
	}

	normalMaps := make(map[string]bool)
	isNormalMap := func(file string) bool {
		resolved := viewerResourcePath(modDir, file)
		if resolved == "" || !strings.EqualFold(filepath.Ext(file), ".dds") || declaredColorSpace(file) == "srgb" {
			return false
		}
		normal, cached := normalMaps[resolved]
		if !cached {
			normal = isModelViewerNormalMap(resolved)
			normalMaps[resolved] = normal
		}
		return normal
	}

	for index := range meshes {
		mesh := &meshes[index]
		wwmiMesh := wwmi || wwmiComponentIndex(mesh.component) != ""
		if !efmi && !wwmiMesh {
			replaceNormalMapDiffuse(mesh, srgbDiffuse, likelyDiffuse, isNormalMap)
			continue
		}
		candidates := mesh.slotTextureCandidates
		if len(candidates) == 0 {
			continue
		}
		if wwmi && !efmi && attachWwmiSlotMaps(mesh, candidates, srgbDiffuse) {
			continue
		}

		fits, replace := likelyDiffuse, false
		primaryMaskDiffuse := wwmiMesh && !efmi && meshDiffuseMatches(mesh, hintOf, func(hint *wwmiTextureHint) bool {
			return hint.Analyzed && hint.PrimaryColorFraction >= 0.5
		})
		switch {
		case !meshHasDiffuseTexture(mesh):
			if !efmi {
				fits = srgbDiffuse
			}
		case wwmiMesh && !mesh.textureAuthored &&
			(primaryMaskDiffuse || !efmi && mesh.textureDumpInferred ||
				meshDiffuseMatches(mesh, hintOf, func(hint *wwmiTextureHint) bool {
					return !isLikelyWwmiDiffuse(*hint)
				})):
			fits, replace = srgbDiffuse, true
		default:
			continue
		}

		bySlot := make(map[int][]modelViewerDirectTextureAssignment)
		var slots []int
		for _, candidate := range candidates {
			slot, ok := modelViewerSlotTextureNumber(candidate.role)
			if !ok || candidate.file == "" {
				continue
			}
			if _, seen := bySlot[slot]; !seen {
				slots = append(slots, slot)
			}
			bySlot[slot] = append(bySlot[slot], candidate)
		}
		sort.Ints(slots)

		// Keep an inferred dump over a primary-color slot mask. Without a dump,
		// a primary channel alone does not invalidate the only fitting texture.
		avoidPrimaryMask := mesh.textureDumpInferred || primaryMaskDiffuse || wwmiMesh && !efmi &&
			lo.SomeBy(candidates, func(candidate modelViewerDirectTextureAssignment) bool {
				hint := hintOf(candidate.file)
				return fits(candidate.file) && hint != nil && hint.Analyzed && hint.PrimaryColorFraction < 0.05
			})
		for _, slot := range slots {
			if !lo.SomeBy(bySlot[slot], func(candidate modelViewerDirectTextureAssignment) bool {
				if !fits(candidate.file) {
					return false
				}
				hint := hintOf(candidate.file)
				return !avoidPrimaryMask || hint == nil || !hint.Analyzed || hint.PrimaryColorFraction < 0.5
			}) {
				continue
			}

			// Conditional slots need the inferred dump outside their binding conditions.
			preserveDump := mesh.textureDumpInferred &&
				!lo.SomeBy(bySlot[slot], func(candidate modelViewerDirectTextureAssignment) bool {
					return modelViewerDNFIsTrue(candidate.conditions)
				})
			if replace && !preserveDump {
				mesh.textureAssignments = lo.Reject(
					mesh.textureAssignments,
					func(assignment modelViewerDirectTextureAssignment, _ int) bool {
						return assignment.role == "diffuse"
					},
				)
				mesh.textureDefaultFile = ""
			}
			mesh.textureDumpInferred = false
			for _, candidate := range bySlot[slot] {
				candidate.role = "diffuse"
				mesh.textureAssignments = mergeModelViewerTextureAssignment(mesh.textureAssignments, candidate)
				if mesh.textureDefaultFile == "" {
					mesh.textureDefaultFile = candidate.file
				}
			}
			break
		}
	}
}

// attachWwmiSlotMaps gives a WWMI mesh the maps of a WuWa character shader when
// it binds unnamed textures at each of ps-t0 to ps-t3 and ps-t3 holds an sRGB
// color map. Other WWMI shaders bind fewer slots, in no known order. The scan
// took the ps-t0 texture for the diffuse, so it becomes the normal map. A diffuse
// that never draws with the ps-t3 textures, as in another shader's branch of the
// same override, stays the diffuse.
func attachWwmiSlotMaps(
	mesh *modelViewerDirectMesh,
	candidates []modelViewerDirectTextureAssignment,
	srgbDiffuse func(file string) bool,
) bool {
	if mesh.textureAuthored || mesh.textureDumpInferred || !meshHasDiffuseTexture(mesh) ||
		lo.SomeBy(mesh.textureAssignments, func(assignment modelViewerDirectTextureAssignment) bool {
			return assignment.role != "diffuse" && slices.Contains(wwmiSlotMapRoles, assignment.role)
		}) {
		return false
	}
	bySlot := lo.GroupBy(
		lo.Filter(candidates, func(candidate modelViewerDirectTextureAssignment, _ int) bool {
			return candidate.file != ""
		}),
		func(candidate modelViewerDirectTextureAssignment) string { return candidate.role },
	)
	for slot := 1; slot < len(wwmiSlotMapRoles); slot++ {
		if len(bySlot[modelViewerSlotTextureRole(slot)]) == 0 {
			return false
		}
	}
	diffuses := bySlot[modelViewerSlotTextureRole(len(wwmiSlotMapRoles)-1)]
	if !lo.EveryBy(diffuses, func(candidate modelViewerDirectTextureAssignment) bool {
		return srgbDiffuse(candidate.file)
	}) {
		return false
	}

	diffuseConditions := modelViewerDNFFalse()
	for _, candidate := range diffuses {
		diffuseConditions = modelViewerDNFOr(diffuseConditions, candidate.conditions)
	}
	for index := range mesh.textureAssignments {
		assignment := &mesh.textureAssignments[index]
		if assignment.role == "diffuse" && modelViewerDNFIntersects(assignment.conditions, diffuseConditions) {
			assignment.role = wwmiSlotMapRoles[0]
		}
	}
	for slot := 1; slot < len(wwmiSlotMapRoles); slot++ {
		for _, candidate := range bySlot[modelViewerSlotTextureRole(slot)] {
			candidate.role = wwmiSlotMapRoles[slot]
			mesh.textureAssignments = mergeModelViewerTextureAssignment(mesh.textureAssignments, candidate)
		}
	}
	defaultDiffuse, found := lo.Find(diffuses, func(candidate modelViewerDirectTextureAssignment) bool {
		return modelViewerDNFIsTrue(candidate.conditions)
	})
	if !found {
		defaultDiffuse = diffuses[0]
	}
	mesh.textureDefaultFile = defaultDiffuse.file
	mesh.textureAuthored = true
	return true
}

// replaceNormalMapDiffuse corrects GIMI mods on the ps-t0 normal, ps-t1 diffuse,
// ps-t2 light map layout that keep the resource names of the ps-t0 diffuse, ps-t1
// light map layout. Names cannot tell the layouts apart, so the swap needs a ps-t0
// that holds a normal map and an sRGB color map to take its place. That map proves
// the layout, so toggle variants without a declared color space move with it; a
// variant declared linear cannot be told from a light map and stays one.
func replaceNormalMapDiffuse(
	mesh *modelViewerDirectMesh,
	isSRGBColorMap, isColorMap, isNormalMap func(file string) bool,
) {
	if !meshHasDiffuseTexture(mesh) {
		return
	}
	colorMaps := func(assignments []modelViewerDirectTextureAssignment, role string) []modelViewerDirectTextureAssignment {
		matching := lo.Filter(assignments, func(assignment modelViewerDirectTextureAssignment, _ int) bool {
			return assignment.role == role && assignment.file != "" && isColorMap(assignment.file)
		})
		if !lo.SomeBy(matching, func(assignment modelViewerDirectTextureAssignment) bool {
			return isSRGBColorMap(assignment.file)
		}) {
			return nil
		}
		return matching
	}
	replacements := colorMaps(mesh.slotTextureCandidates, modelViewerSlotTextureRole(1))
	if len(replacements) == 0 {
		replacements = colorMaps(mesh.textureAssignments, "light_map")
	}
	if len(replacements) == 0 || mesh.textureDefaultFile != "" && !isNormalMap(mesh.textureDefaultFile) ||
		!lo.EveryBy(mesh.textureAssignments, func(assignment modelViewerDirectTextureAssignment) bool {
			return assignment.role != "diffuse" || assignment.file == "" || isNormalMap(assignment.file)
		}) {
		return
	}

	replacedRole := replacements[0].role
	mesh.textureAssignments = lo.Reject(
		mesh.textureAssignments,
		func(assignment modelViewerDirectTextureAssignment, _ int) bool {
			return assignment.role == "diffuse" || assignment.role == replacedRole && isColorMap(assignment.file)
		},
	)
	mesh.textureDefaultFile = replacements[0].file
	for _, replacement := range replacements {
		replacement.role = "diffuse"
		mesh.textureAssignments = mergeModelViewerTextureAssignment(mesh.textureAssignments, replacement)
	}
}

// Normal maps center X and Y and either omit Z or store it near full blue.
// Sparse extreme texels must not disqualify an otherwise flat RGB normal map.
func isModelViewerNormalMap(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	decoded, err := decodeModelViewerDDSHint(path, info.Size())
	if err != nil {
		return false
	}
	analysis := analyzeNRGBA(decoded)
	return absFloat(analysis.meanR-0.5) <= 0.1 && absFloat(analysis.meanG-0.5) <= 0.1 &&
		(analysis.meanB <= 0.08 || analysis.meanB >= 0.9 && analysis.luminanceStdDev <= 0.12)
}

func meshDiffuseMatches(
	mesh *modelViewerDirectMesh,
	hintOf func(string) *wwmiTextureHint,
	match func(*wwmiTextureHint) bool,
) bool {
	matches := func(file string) bool {
		if !strings.EqualFold(filepath.Ext(file), ".dds") {
			return false
		}
		hint := hintOf(file)
		return hint != nil && match(hint)
	}
	if !matches(mesh.textureDefaultFile) {
		return false
	}
	return lo.EveryBy(mesh.textureAssignments, func(assignment modelViewerDirectTextureAssignment) bool {
		return assignment.role != "diffuse" || assignment.file == "" ||
			matches(assignment.file)
	})
}

func meshHasDiffuseTexture(mesh *modelViewerDirectMesh) bool {
	if mesh.textureDefaultFile != "" {
		return true
	}
	for _, assignment := range mesh.textureAssignments {
		if assignment.role == "diffuse" && assignment.file != "" {
			return true
		}
	}
	return false
}
