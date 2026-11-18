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
