package modelviewer

import (
	"path/filepath"
	"regexp"
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
// whose unnamed ps-t0 file is a helper map, such as a linear mask bound beside
// an sRGB diffuse at ps-t3, swaps it for an sRGB slot texture. Only DDS headers
// declare a color space, so the swap needs both files to declare theirs; a
// guessed one, as for PNG, keeps the ps-t0 file.
// The lowest slot holding a fitting file wins, matching the game's
// diffuse-first slot order, and keeps every conditional assignment of that
// slot so texture toggles still switch. Each mesh drops its candidates either
// way, so slot roles never reach the payload.
func attachSlotBoundDiffuse(meshes []modelViewerDirectMesh, modDir string, efmi bool) {
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

	for index := range meshes {
		mesh := &meshes[index]
		candidates := mesh.slotTextureCandidates
		mesh.slotTextureCandidates = nil
		if len(candidates) == 0 {
			continue
		}

		fits, replace := likelyDiffuse, false
		switch {
		case !meshHasDiffuseTexture(mesh):
			if !efmi {
				continue
			}
		case wwmiComponentIndex(mesh.component) != "" && !mesh.textureAuthored &&
			meshDiffuseIsDeclaredLinear(mesh, declaredColorSpace):
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

		for _, slot := range slots {
			if !lo.SomeBy(bySlot[slot], func(candidate modelViewerDirectTextureAssignment) bool {
				return fits(candidate.file)
			}) {
				continue
			}

			if replace {
				mesh.textureAssignments = lo.Reject(
					mesh.textureAssignments,
					func(assignment modelViewerDirectTextureAssignment, _ int) bool {
						return assignment.role == "diffuse"
					},
				)
				mesh.textureDefaultFile = ""
			}
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

func meshDiffuseIsDeclaredLinear(mesh *modelViewerDirectMesh, declaredColorSpace func(string) string) bool {
	if declaredColorSpace(mesh.textureDefaultFile) != "linear" {
		return false
	}
	return lo.EveryBy(mesh.textureAssignments, func(assignment modelViewerDirectTextureAssignment) bool {
		return assignment.role != "diffuse" || assignment.file == "" ||
			declaredColorSpace(assignment.file) == "linear"
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
