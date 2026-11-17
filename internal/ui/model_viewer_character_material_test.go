package ui

import (
	"math"
	"regexp"
	"strconv"
	"testing"
	"unsafe"
)

// The shader reads the character array and the Mesh constants at the offsets the Go structs write, so a length or
// layout that differs shifts every value without an error.
func TestCharacterMaterialMatchesShaderConstants(t *testing.T) {
	t.Parallel()
	match := regexp.MustCompile(`float4 character\[(\d+)\];`).FindSubmatch(modelViewerHLSL)
	if match == nil {
		t.Fatal("model_viewer.hlsl declares no character array")
	}
	if slots, _ := strconv.Atoi(string(match[1])); slots != characterMaterialSlots {
		t.Errorf("shader holds %d character slots, Go %d", slots, characterMaterialSlots)
	}
	if size := unsafe.Sizeof(viewerMeshConstants{}); size != 64 {
		t.Errorf("Mesh constants are %d bytes, want the shader's 64", size)
	}
}

// Every bundled character must pack, and Star Rail's LUT rows run from the bottom of the image: read top-down,
// Acheron's outline row would come out as her specular values.
func TestBundledCharacterMaterialsPack(t *testing.T) {
	t.Parallel()
	for _, game := range []string{"gimi", "srmi", "zzmi"} {
		data, err := characterToonGames[game]()
		if err != nil {
			t.Fatal(err)
		}
		for id := range data.Characters {
			toon, err := loadCharacterToon(game, id)
			if err != nil || toon.materials == nil || toon.materials.body == nil {
				t.Errorf("%s/%s materials = %+v, %v; want a body material", game, id, toon.materials, err)
			}
		}
	}

	toon, err := loadCharacterToon("srmi", "acheron")
	if err != nil || toon.materials == nil || toon.materials.body == nil {
		t.Fatalf("acheron materials = %+v, %v", toon.materials, err)
	}
	outline := toon.materials.body[8]
	want := [3]float32{33.0 / 255, 6.0 / 255, 4.0 / 255}
	for c := range want {
		if math.Abs(float64(outline[c]-want[c])) > 0.5/255 {
			t.Fatalf("acheron material 0 outline = %v, want the LUT's dark brown %v", outline[:3], want)
		}
	}
}

func TestCharacterMaterialUseYieldsToChangedMenuValues(t *testing.T) {
	t.Parallel()
	overrides := toonShading{"shadowBrightness": {0.5}}
	shading := toonShading{"shadowBrightness": {0.5}, "shallowBrightness": {0.4}, "specular": {0.2}}
	use, scale := characterMaterialUse(zzmiProfile, overrides, shading)
	if use != characterUseValues|characterUseShallow|characterUseOutline || scale != 2 {
		t.Errorf("use = %b, scale = %v; want only the character's shadow left to the menu and twice the specular",
			use, scale)
	}
	if use, _ := characterMaterialUse(efmiProfile, nil, nil); use != 0 {
		t.Errorf("EFMI use = %b, want none", use)
	}
}
