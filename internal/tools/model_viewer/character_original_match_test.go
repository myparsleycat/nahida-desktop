package modelviewer

import (
	"slices"
	"testing"
)

func originalTestPart(role string, x float32, uv float32) CharacterOriginalPart {
	var part CharacterOriginalPart
	part.Role = role
	for i := range 20 {
		y := float32(i) * 0.01
		part.Positions = append(part.Positions, x, y, 0)
		part.UVs = append(part.UVs, uv, y)
	}
	return part
}

func TestMatchCharacterOriginal(t *testing.T) {
	original := &CharacterOriginal{
		Game: "GIMI",
		Parts: []CharacterOriginalPart{
			originalTestPart(OriginalRoleFace, 0, 0.1),
			originalTestPart(OriginalRoleBody, 1, 0.2),
			originalTestPart(OriginalRoleBody, 2, 0.3),
			originalTestPart(OriginalRoleBrow, 3, 0.4),
		},
	}
	body := original.Parts[1]
	brow := original.Parts[3]
	remodel := original.Parts[1]
	remodel.UVs = slices.Clone(remodel.UVs)
	for i := 0; i < len(remodel.UVs); i += 2 {
		remodel.UVs[i] = 0.9
	}

	irises := slices.Clone(original.Parts[0].Positions)
	for i := 2; i < len(irises); i += 3 {
		irises[i] -= 0.008
	}
	meshes := []OriginalMatchMesh{
		{Positions: body.Positions, UVs: body.UVs},
		{Positions: brow.Positions, UVs: brow.UVs},
		{Positions: remodel.Positions, UVs: remodel.UVs},
		{Positions: irises},
	}
	match, err := MatchCharacterOriginal(t.Context(), original, meshes)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(match.HeadParts, []int{0}) {
		t.Errorf("HeadParts = %v, want the face behind the irises only", match.HeadParts)
	}
	if !slices.Equal(match.AbsentParts, []int{2}) {
		t.Errorf("AbsentParts = %v, want the removed body part", match.AbsentParts)
	}
	if want := []int{1, 3, 1, 0}; !slices.Equal(match.Sources, want) {
		t.Errorf("Sources = %v, want %v", match.Sources, want)
	}

	original.Game = "EFMI"
	match, err = MatchCharacterOriginal(t.Context(), original, meshes)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{1, 3, -1, -1}; !slices.Equal(match.Sources, want) {
		t.Errorf("EFMI Sources = %v, want %v without the remodelled UVs", match.Sources, want)
	}
}

func TestCharacterOriginalEFMISkinLUT(t *testing.T) {
	skin := CharacterOriginalTexture{Name: "T_actor_common_femaleskincolor01_lut_D"}
	clothLUT := CharacterOriginalTexture{Name: "T_actor_common_cloth_lut_01_D"}
	colour := CharacterOriginalTexture{Name: "T_actor_x_cloth_01_D"}
	parts := []CharacterOriginalPart{
		{Role: OriginalRoleBody, Shader: "Qebg", Textures: map[string]CharacterOriginalTexture{"Qtvf": skin}},
		{Role: OriginalRoleFace, Shader: "Qebg", Textures: map[string]CharacterOriginalTexture{"Qtvf": skin}},
		{Role: OriginalRoleBody, Shader: "Qpgh", Textures: map[string]CharacterOriginalTexture{
			"Qy3l": colour, "Qtvf": clothLUT,
		}},
		// Brows and fur carry LUTs that do not give their colour.
		{Role: OriginalRoleBrow, Shader: "Qxkp", Textures: map[string]CharacterOriginalTexture{"Qtvf": skin}},
		{Role: OriginalRoleHair, Shader: "Qpgh", Textures: map[string]CharacterOriginalTexture{"Qtvf": clothLUT}},
	}
	original := &CharacterOriginal{Game: "EFMI", Parts: parts}
	for i, want := range []CharacterOriginalTexture{
		{Name: skin.Name, LUT: true}, {Name: skin.Name, LUT: true}, colour, {}, {},
	} {
		got, ok := original.Texture(i, OriginalTextureDiffuse)
		if ok != (want.Name != "") || got.Name != want.Name || got.LUT != want.LUT {
			t.Errorf("part %d diffuse = %+v, %v; want %+v", i, got, ok, want)
		}
	}
	original.Game = "SRMI"
	if got, ok := original.Texture(0, OriginalTextureDiffuse); ok {
		t.Errorf("SRMI diffuse = %+v, want none", got)
	}
}

func TestMatchCharacterOriginalKeptHair(t *testing.T) {
	body := originalTestPart(OriginalRoleBody, 0, 0.2)
	body.Mesh = "Body"
	hair := originalTestPart(OriginalRoleHair, 1, 0.4)
	hair.Mesh = "Hair"
	near := func(part CharacterOriginalPart, uv float32) OriginalMatchMesh {
		var mesh OriginalMatchMesh
		for v := range len(part.Positions) / 3 {
			mesh.Positions = append(mesh.Positions, part.Positions[v*3], part.Positions[v*3+1], 0.01)
			mesh.UVs = append(mesh.UVs, uv, part.UVs[v*2+1])
		}
		return mesh
	}
	join := func(a, b OriginalMatchMesh) OriginalMatchMesh {
		return OriginalMatchMesh{
			Positions: slices.Concat(a.Positions, b.Positions),
			UVs:       slices.Concat(a.UVs, b.UVs),
		}
	}
	// The hood lies within the cover distance of the hair but belongs to the body mesh, as in a mod that replaces
	// only the body draw.
	hooded := join(OriginalMatchMesh{Positions: body.Positions, UVs: body.UVs}, near(hair, 0.9))

	tests := []struct {
		name   string
		game   string
		meshes []OriginalMatchMesh
		want   []int
	}{
		{name: "body only", game: "SRMI", meshes: []OriginalMatchMesh{hooded}, want: []int{1}},
		{
			name:   "hair replaced",
			game:   "SRMI",
			meshes: []OriginalMatchMesh{hooded, {Positions: hair.Positions, UVs: hair.UVs}},
		},
		{
			name:   "unmatched mesh over the hair",
			game:   "SRMI",
			meshes: []OriginalMatchMesh{hooded, join(near(hair, 0.9), near(body, 0.9))},
		},
		{name: "one draw for the whole body", game: "GIMI", meshes: []OriginalMatchMesh{hooded}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := &CharacterOriginal{Game: tt.game, Parts: []CharacterOriginalPart{body, hair}}
			match, err := MatchCharacterOriginal(t.Context(), original, tt.meshes)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(match.KeptParts, tt.want) {
				t.Errorf("KeptParts = %v, want %v (Sources %v)", match.KeptParts, tt.want, match.Sources)
			}
		})
	}
}
