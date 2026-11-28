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
	OriginalTextureFaceSDF    = "face-sdf"
	OriginalTextureHighlight  = "highlight"
	OriginalTextureRamp       = "ramp"
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
	"EFMI": {
		OriginalTextureDiffuse:   "Qy3l",
		OriginalTextureNormal:    "Qzd7",
		OriginalTextureMaterial:  "Q3kk",
		OriginalTextureFaceSDF:   "Qmtx",
		OriginalTextureHighlight: "Q0g0",
		OriginalTextureRamp:      "Qlc8",
	},
	"WWMI": {OriginalTextureDiffuse: "MainTex", OriginalTextureNormal: "Normal_Roughness_Metallic"},
}

// Endfield's skin shader (Qebg, the body and face) has no colour map. Its colour comes from a 32³ skin colour LUT
// (T_actor_common_femaleskincolor*_lut_D) in Qtvf, whose output for white is the lit skin tone. Brows and cloth also
// carry LUTs in Qtvf, but those do not give their colour.
const (
	efmiSkinShader = "Qebg"
	efmiLUTSlot    = "Qtvf"
)

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
	if o.FaceShaded(part) {
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
	if texture, found := p.Textures[slot]; found {
		return texture, true
	}
	if o.Game == "EFMI" && role == OriginalTextureDiffuse && p.Shader == efmiSkinShader {
		if texture, found := p.Textures[efmiLUTSlot]; found {
			texture.LUT = true
			return texture, true
		}
	}
	return CharacterOriginalTexture{}, false
}

// FaceShaded reports whether the game shades part as the face: it is a face part, draws with a face part's material,
// or samples a face part's face map from a material of its own, as Nicole's eye area and brows do.
func (o *CharacterOriginal) FaceShaded(part int) bool {
	if part < 0 || part >= len(o.Parts) {
		return false
	}
	p := o.Parts[part]
	if p.Role == OriginalRoleFace {
		return true
	}
	slot := originalFaceSlots[o.Game][OriginalTextureFaceLight]
	faceMap, hasMap := p.Textures[slot]
	hasMap = hasMap && slot != "" && faceMap.Color.SHA256 != ""
	for _, face := range o.Parts {
		if face.Role != OriginalRoleFace {
			continue
		}
		if p.Material != "" && face.Material == p.Material {
			return true
		}
		if other, ok := face.Textures[slot]; hasMap && ok && other.Color.SHA256 == faceMap.Color.SHA256 {
			return true
		}
	}
	return false
}

const (
	// originalCoverCell is the distance, in metres, within which a mod vertex covers an original vertex.
	originalCoverCell = 0.015
	// originalHeadCell is the tighter cover distance for head parts: the irises and hair that mods draw without
	// the face lie within originalCoverCell of most of the eye area.
	originalHeadCell = 0.005
	// originalHeadCovered is the share of a head part's vertices mod vertices must cover for the mod to replace it.
	originalHeadCovered = 0.5
	// originalPartAbsent is the share below which another part counts as absent from the mod.
	originalPartAbsent = 0.1
	// originalSameVertex and originalSameUV bound how far a mod vertex may sit from an original vertex, in
	// position (metres) and UV, to be the same vertex.
	originalSameVertex = 0.002
	originalSameUV     = 0.002
	// originalUVShare is the share of a mod mesh's vertices that must match one part in position and UV.
	originalUVShare = 0.25
	// originalPlaceShare is the share of position-only votes one part must win when UVs match nowhere.
	originalPlaceShare = 0.6
	// originalKeptCovered is the share of a hair part that mod meshes matched to no part may cover before they count
	// as a replacement for it.
	originalKeptCovered = 0.3
)

// originalSeparateDraws are the games that draw each mesh of a character with its own draw call, which mods override
// one by one. A mod that overrides only some of them leaves the others, such as the hair, to the game.
var originalSeparateDraws = map[string]bool{"SRMI": true, "ZZMI": true, "WWMI": true}

// OriginalMatchMesh is a mod mesh in mod space, with its UVs when it has them.
type OriginalMatchMesh struct {
	Positions []float32
	UVs       []float32
}

// OriginalMatch tells how a mod relates to its character's original model.
type OriginalMatch struct {
	// OverlayParts are the EFMI shadow cards, only reused with the stock face.
	OverlayParts []int
	// HeadParts are the face group parts the mod does not replace, which the game draws itself.
	HeadParts []int
	// KeptParts are the parts of hair meshes that no mod mesh draws in place of, which the game keeps drawing.
	KeptParts []int
	// AbsentParts are the other parts with almost no mod geometry, which the mod may have removed on purpose.
	AbsentParts []int
	// Sources gives, for each mod mesh, the part whose material the game binds to it, or -1.
	Sources []int
}

// MatchCharacterOriginal finds the original parts a mod does not cover and the original material behind each mod
// mesh. A mesh whose vertices coincide with a part in position and UV belongs to it; otherwise a clear majority of
// nearby original vertices decides. In games that draw each mesh apart, a hair mesh no mod mesh belongs to stays
// drawn by the game.
func MatchCharacterOriginal(
	ctx context.Context,
	original *CharacterOriginal,
	meshes []OriginalMatchMesh,
) (OriginalMatch, error) {
	match := OriginalMatch{Sources: make([]int, len(meshes))}
	unit := modUnitsPerMetre(original.Game)
	coverCell, headCell, sameVertex := originalCoverCell*unit, originalHeadCell*unit, float32(originalSameVertex*unit)

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
			cell := motionCell(at[0], at[1], at[2], coverCell)
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
			cell := motionCell(at[0], at[1], at[2], coverCell)
			nearest, nearestDistance := int32(-1), float32(math.Inf(1))
			matched := int32(-1)
			forNeighbours(grid, cell, func(index int32) {
				o := positions[index*3 : index*3+3]
				dx, dy, dz := o[0]-at[0], o[1]-at[1], o[2]-at[2]
				distance := dx*dx + dy*dy + dz*dz
				if distance < nearestDistance {
					nearest, nearestDistance = vertices[index].part, distance
				}
				if hasUV && matched < 0 && distance <= sameVertex*sameVertex &&
					abs32(vertices[index].uv[0]-uv[0]) <= originalSameUV &&
					abs32(vertices[index].uv[1]-uv[1]) <= originalSameUV {
					matched = vertices[index].part
				}
			})
			if matched >= 0 {
				same[matched]++
			}
			if nearest >= 0 && float64(nearestDistance) <= coverCell*coverCell {
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

	modGrid, headGrid := map[[3]int32][]int32{}, map[[3]int32][]int32{}
	var modPositions []float32
	for _, mesh := range meshes {
		for v := 0; v+2 < len(mesh.Positions); v += 3 {
			at, index := mesh.Positions[v:v+3], int32(len(modPositions)/3)
			cell := motionCell(at[0], at[1], at[2], coverCell)
			modGrid[cell] = append(modGrid[cell], index)
			cell = motionCell(at[0], at[1], at[2], headCell)
			headGrid[cell] = append(headGrid[cell], index)
			modPositions = append(modPositions, at...)
		}
	}

	// A hair mesh counts as left to the game unless a mod mesh is matched to it or mod meshes matched to no part
	// cover it.
	drawn, hair := map[string]bool{}, map[string]bool{}
	// Genshin's Bang mesh has its own draw, while hair materials within Body share the body's buffers.
	for _, part := range original.Parts {
		if part.Role == OriginalRoleHair &&
			(originalSeparateDraws[original.Game] || original.Game == "GIMI" && part.Mesh == "Bang") {
			hair[part.Mesh] = true
		}
	}
	unmatchedGrid := map[[3]int32][]int32{}
	var unmatched []float32
	if len(hair) > 0 {
		for m, source := range match.Sources {
			if source >= 0 {
				drawn[original.Parts[source].Mesh] = true
				continue
			}
			mesh := meshes[m].Positions
			for v := 0; v+2 < len(mesh); v += 3 {
				at := mesh[v : v+3]
				cell := motionCell(at[0], at[1], at[2], coverCell)
				unmatchedGrid[cell] = append(unmatchedGrid[cell], int32(len(unmatched)/3))
				unmatched = append(unmatched, at...)
			}
		}
	}

	// The pieces split from one part are covered or not together, as the part they came from.
	splitPositions := map[int][]float32{}
	for _, part := range original.Parts {
		if part.splitFrom != 0 {
			splitPositions[part.splitFrom] = append(splitPositions[part.splitFrom], part.Positions...)
		}
	}
	headCovered := func(positions []float32) bool {
		return coveredShare(headGrid, modPositions, positions, float32(headCell)) >= originalHeadCovered
	}
	splitCovered := make(map[int]bool, len(splitPositions))
	for from, positions := range splitPositions {
		splitCovered[from] = headCovered(positions)
	}
	for p, part := range original.Parts {
		if err := ctx.Err(); err != nil {
			return OriginalMatch{}, err
		}
		if part.Role == OriginalRoleOverlay {
			if original.Game == "EFMI" {
				match.OverlayParts = append(match.OverlayParts, p)
			}
			continue
		}
		if IsHeadRole(part.Role) {
			covered := splitCovered[part.splitFrom]
			if part.splitFrom == 0 {
				covered = headCovered(part.Positions)
			}
			if !covered {
				match.HeadParts = append(match.HeadParts, p)
			}
			continue
		}
		if hair[part.Mesh] && !drawn[part.Mesh] &&
			coveredShare(unmatchedGrid, unmatched, part.Positions, float32(coverCell)) < originalKeptCovered {
			match.KeptParts = append(match.KeptParts, p)
			continue
		}
		if coveredShare(modGrid, modPositions, part.Positions, float32(coverCell)) < originalPartAbsent {
			match.AbsentParts = append(match.AbsentParts, p)
		}
	}
	return match, nil
}

// coveredShare is the share of the vertices at that lie within radius of a vertex of positions, which grid indexes
// in cells of radius.
func coveredShare(grid map[[3]int32][]int32, positions, at []float32, radius float32) float64 {
	count := len(at) / 3
	covered := 0
	for v := range count {
		if within(grid, positions, at[v*3:v*3+3], radius) {
			covered++
		}
	}
	return float64(covered) / float64(max(count, 1))
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
