package modelviewer

import (
	"context"
	"math"
)

// Texture roles an original part can supply, matching the viewer's mesh texture keys and toon face maps.
const (
	OriginalTextureDiffuse    = "diffuse"
	OriginalTextureNormal     = "normal"
	OriginalTextureLight      = "light"
	OriginalTextureMaterial   = "material"
	OriginalTextureFaceLight  = "face-light"
	OriginalTextureFaceShadow = "face-shadow"
)

// originalTextureSlots names, per game and role, the source shader slots that hold the texture mods bind for that
// role. Warfarin obfuscates its slot names; Qy3l holds the _P colour map and Qzd7 the _N normal map.
var originalTextureSlots = map[string]map[string]string{
	"GIMI": {OriginalTextureDiffuse: "_MainTex", OriginalTextureLight: "_LightMapTex"},
	"SRMI": {OriginalTextureDiffuse: "_MainTex", OriginalTextureLight: "_LightMap"},
	"ZZMI": {
		OriginalTextureDiffuse: "_MainTex", OriginalTextureNormal: "_LightTex",
		OriginalTextureLight: "_OtherDataTex", OriginalTextureMaterial: "_OtherDataTex2",
	},
	"EFMI": {OriginalTextureDiffuse: "Qy3l", OriginalTextureNormal: "Qzd7"},
}

// originalFaceSlots are the face maps of the face material, which Genshin and Star Rail sample from their own slots.
var originalFaceSlots = map[string]map[string]string{
	"GIMI": {OriginalTextureFaceLight: "_FaceMapTex", OriginalTextureFaceShadow: "_LightMapTex"},
	"SRMI": {OriginalTextureFaceLight: "_FaceMap"},
}

// Texture returns the texture of part for role, if the source has one.
func (o *CharacterOriginal) Texture(part int, role string) (CharacterOriginalTexture, bool) {
	if part < 0 || part >= len(o.Parts) {
		return CharacterOriginalTexture{}, false
	}
	p := o.Parts[part]
	slots := originalTextureSlots[o.Game]
	if p.Role == OriginalRoleFace {
		if slot, ok := originalFaceSlots[o.Game][role]; ok {
			texture, found := p.Textures[slot]
			return texture, found
		}
		// Genshin's face samples its shadow map, not a light map, from the light map slot.
		if o.Game == "GIMI" && role == OriginalTextureLight {
			return CharacterOriginalTexture{}, false
		}
	}
	slot, ok := slots[role]
	if !ok {
		return CharacterOriginalTexture{}, false
	}
	texture, found := p.Textures[slot]
	return texture, found
}

const (
	// originalCoverCell is the distance within which a mod vertex covers an original vertex.
	originalCoverCell = 0.015
	// originalHeadCell is the tighter cover distance for head parts: the irises and hair that mods draw without
	// the face lie within originalCoverCell of most of the eye area.
	originalHeadCell = 0.005
	// originalHeadCovered is the share of a head part's vertices mod vertices must cover for the mod to replace it.
	originalHeadCovered = 0.5
	// originalPartAbsent is the share below which another part counts as absent from the mod.
	originalPartAbsent = 0.1
	// originalSameVertex and originalSameUV bound how far a mod vertex may sit from an original vertex, in
	// position and UV, to be the same vertex.
	originalSameVertex = 0.002
	originalSameUV     = 0.002
	// originalUVShare is the share of a mod mesh's vertices that must match one part in position and UV.
	originalUVShare = 0.25
	// originalPlaceShare is the share of position-only votes one part must win when UVs match nowhere.
	originalPlaceShare = 0.6
)

// OriginalMatchMesh is a mod mesh in mod space, with its UVs when it has them.
type OriginalMatchMesh struct {
	Positions []float32
	UVs       []float32
}

// OriginalMatch tells how a mod relates to its character's original model.
type OriginalMatch struct {
	// HeadParts are the face group parts the mod does not replace, which the game draws itself.
	HeadParts []int
	// AbsentParts are the other parts with almost no mod geometry, which the mod may have removed on purpose.
	AbsentParts []int
	// Sources gives, for each mod mesh, the part whose material the game binds to it, or -1.
	Sources []int
}

// MatchCharacterOriginal finds the original parts a mod does not cover and the original material behind each mod
// mesh. A mesh whose vertices coincide with a part in position and UV belongs to it; otherwise a clear majority of
// nearby original vertices decides.
func MatchCharacterOriginal(
	ctx context.Context,
	original *CharacterOriginal,
	meshes []OriginalMatchMesh,
) (OriginalMatch, error) {
	match := OriginalMatch{Sources: make([]int, len(meshes))}

	modGrid, headGrid := map[[3]int32][]int32{}, map[[3]int32][]int32{}
	var modPositions []float32
	for _, mesh := range meshes {
		for v := 0; v+2 < len(mesh.Positions); v += 3 {
			at, index := mesh.Positions[v:v+3], int32(len(modPositions)/3)
			cell := motionCell(at[0], at[1], at[2], originalCoverCell)
			modGrid[cell] = append(modGrid[cell], index)
			cell = motionCell(at[0], at[1], at[2], originalHeadCell)
			headGrid[cell] = append(headGrid[cell], index)
			modPositions = append(modPositions, at...)
		}
	}
	for p, part := range original.Parts {
		if err := ctx.Err(); err != nil {
			return OriginalMatch{}, err
		}
		if part.Role == OriginalRoleOverlay {
			continue
		}
		count := len(part.Positions) / 3
		grid, radius := modGrid, float32(originalCoverCell)
		if IsHeadRole(part.Role) {
			grid, radius = headGrid, originalHeadCell
		}
		covered := 0
		for v := range count {
			if within(grid, modPositions, part.Positions[v*3:v*3+3], radius) {
				covered++
			}
		}
		share := float64(covered) / float64(max(count, 1))
		switch {
		case IsHeadRole(part.Role) && share < originalHeadCovered:
			match.HeadParts = append(match.HeadParts, p)
		case !IsHeadRole(part.Role) && share < originalPartAbsent:
			match.AbsentParts = append(match.AbsentParts, p)
		}
	}

	// Endfield mod buffers can lay out UVs differently from Warfarin's model, so a Warfarin texture fits only the
	// meshes whose UVs agree with it.
	placeVotes := original.Game != "EFMI"
	type vertex struct {
		part int32
		uv   [2]float32
	}
	var vertices []vertex
	var positions []float32
	grid := map[[3]int32][]int32{}
	for p, part := range original.Parts {
		if part.Role == OriginalRoleOverlay {
			continue
		}
		for v := range len(part.Positions) / 3 {
			at := part.Positions[v*3 : v*3+3]
			cell := motionCell(at[0], at[1], at[2], originalCoverCell)
			grid[cell] = append(grid[cell], int32(len(vertices)))
			item := vertex{part: int32(p)}
			if len(part.UVs) >= v*2+2 {
				item.uv = [2]float32{part.UVs[v*2], part.UVs[v*2+1]}
			}
			vertices = append(vertices, item)
			positions = append(positions, at...)
		}
	}
	for m, mesh := range meshes {
		if err := ctx.Err(); err != nil {
			return OriginalMatch{}, err
		}
		match.Sources[m] = -1
		count := len(mesh.Positions) / 3
		if count == 0 {
			continue
		}
		same := make([]int, len(original.Parts))
		near := make([]int, len(original.Parts))
		voted := 0
		for v := range count {
			at := mesh.Positions[v*3 : v*3+3]
			var uv [2]float32
			hasUV := len(mesh.UVs) >= v*2+2
			if hasUV {
				uv = [2]float32{mesh.UVs[v*2], mesh.UVs[v*2+1]}
			}
			cell := motionCell(at[0], at[1], at[2], originalCoverCell)
			nearest, nearestDistance := int32(-1), float32(math.Inf(1))
			matched := int32(-1)
			forNeighbours(grid, cell, func(index int32) {
				o := positions[index*3 : index*3+3]
				dx, dy, dz := o[0]-at[0], o[1]-at[1], o[2]-at[2]
				distance := dx*dx + dy*dy + dz*dz
				if distance < nearestDistance {
					nearest, nearestDistance = vertices[index].part, distance
				}
				if hasUV && matched < 0 && distance <= originalSameVertex*originalSameVertex &&
					abs32(vertices[index].uv[0]-uv[0]) <= originalSameUV &&
					abs32(vertices[index].uv[1]-uv[1]) <= originalSameUV {
					matched = vertices[index].part
				}
			})
			if matched >= 0 {
				same[matched]++
			}
			if nearest >= 0 && nearestDistance <= originalCoverCell*originalCoverCell {
				near[nearest]++
				voted++
			}
		}
		if best := argmax(same); same[best] > 0 && float64(same[best]) >= originalUVShare*float64(count) {
			match.Sources[m] = best
		} else if best := argmax(near); placeVotes && voted > 0 &&
			float64(near[best]) >= originalPlaceShare*float64(voted) {
			match.Sources[m] = best
		}
	}
	return match, nil
}

func within(grid map[[3]int32][]int32, positions, at []float32, radius float32) bool {
	found := false
	forNeighbours(grid, motionCell(at[0], at[1], at[2], float64(radius)), func(index int32) {
		if found {
			return
		}
		o := positions[index*3 : index*3+3]
		dx, dy, dz := o[0]-at[0], o[1]-at[1], o[2]-at[2]
		found = dx*dx+dy*dy+dz*dz <= radius*radius
	})
	return found
}

func forNeighbours(grid map[[3]int32][]int32, cell [3]int32, visit func(int32)) {
	for dx := int32(-1); dx <= 1; dx++ {
		for dy := int32(-1); dy <= 1; dy++ {
			for dz := int32(-1); dz <= 1; dz++ {
				for _, index := range grid[[3]int32{cell[0] + dx, cell[1] + dy, cell[2] + dz}] {
					visit(index)
				}
			}
		}
	}
}

func argmax(values []int) int {
	best := 0
	for i, value := range values {
		if value > values[best] {
			best = i
		}
	}
	return best
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
