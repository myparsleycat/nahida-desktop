package modelviewer

import (
	"regexp"
	"sort"
	"strconv"

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

// attachSlotBoundDiffuse textures EFMI meshes that every other source left
// untextured. EFMI mods can bind their own textures straight to the game
// shader's ps-t slots under names that carry no role (Resource_A1), and the
// diffuse slot differs per shader. The lowest slot holding a likely diffuse
// file wins, matching the game's diffuse-first slot order, and keeps every
// conditional assignment of that slot so texture toggles still switch. Each
// mesh drops its candidates either way, so slot roles never reach the payload.
func attachSlotBoundDiffuse(meshes []modelViewerDirectMesh, modDir string) {
	hints := make(map[string]*wwmiTextureHint)
	likelyDiffuse := func(file string) bool {
		// The hint reads any other file as an unknown color space, which counts as diffuse.
		if !hashImageExtRE.MatchString(file) {
			return false
		}
		resolved := viewerResourcePath(modDir, file)
		if resolved == "" {
			return false
		}
		hint, cached := hints[resolved]
		if !cached {
			hint = inspectWwmiTextureHint(resolved)
			hints[resolved] = hint
		}
		return hint != nil && isLikelyWwmiDiffuse(*hint)
	}

	for index := range meshes {
		mesh := &meshes[index]
		candidates := mesh.slotTextureCandidates
		mesh.slotTextureCandidates = nil
		if len(candidates) == 0 || meshHasDiffuseTexture(mesh) {
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
				return likelyDiffuse(candidate.file)
			}) {
				continue
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
