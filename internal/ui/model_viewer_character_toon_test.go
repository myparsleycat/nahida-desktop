package ui

import (
	"slices"
	"strings"
	"testing"

	"nahida.live/desktop/internal/mod/character"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

// The importer and the shaders agree on each game's ramp layout only through these files, and a ramp that is
// missing or reshaped falls back to the default shading without an error the user sees.
func TestBundledCharacterRampsMatchShaderLayout(t *testing.T) {
	t.Parallel()
	// Star Rail's hair is one material, so its ramps hold a single row pair.
	shapes := map[string]struct {
		heights []uint32
		alpha   bool
	}{
		"gimi": {[]uint32{20}, false},
		"srmi": {[]uint32{16, 2}, false},
		"efmi": {[]uint32{1}, true},
	}
	for game, shape := range shapes {
		data, err := characterToonGames[game]()
		if err != nil {
			t.Fatal(err)
		}
		checked := map[string]bool{}
		for id := range data.Characters {
			if _, ok := character.Lookup(strings.ToUpper(game), id); !ok {
				t.Errorf("%s/%s is not in the character catalog", game, id)
			}
			toon, err := loadCharacterToon(game, id)
			ramps := toon.ramps
			if err != nil || ramps == nil || ramps.body == "" {
				t.Errorf("%s/%s ramps = %+v, %v; want a body ramp", game, id, ramps, err)
				continue
			}
			for _, name := range ramps.names {
				if checked[name] {
					continue
				}
				checked[name] = true
				raw, err := readBundledToonMap(ramps.url(name))
				var texture *viewerTextureData
				if err == nil {
					texture, err = decodeViewerImage(raw)
				}
				if err != nil {
					t.Errorf("%s ramp %s: %v", game, name, err)
					continue
				}
				if texture.width != 256 || !slices.Contains(shape.heights, texture.height) {
					t.Errorf("%s ramp %s is %dx%d, want 256 wide and %v high",
						game, name, texture.width, texture.height, shape.heights)
				}
				if shape.alpha && texture.mips[0][3] == texture.mips[0][len(texture.mips[0])-1] {
					t.Errorf("%s ramp %s has flat alpha, want coverage rising across the ramp", game, name)
				}
			}
		}
	}
}

func TestCharacterRampForMesh(t *testing.T) {
	t.Parallel()
	ramps := characterRampsOf("efmi", []characterToonMaterial{
		{Role: "cloth", Indices: []int{300}, Textures: map[string]string{"_RD": "small_cloth"}},
		{Role: "hair", Indices: []int{40000}, Textures: map[string]string{"_RD": "hair"}},
		{Role: "face", Indices: []int{9000}, Textures: map[string]string{"_RD": "face"}},
		{Role: "cloth", Indices: []int{90000}, Textures: map[string]string{"_RD": "cloth"}},
		{Role: "body", Indices: []int{12000}, Textures: map[string]string{"_RD": "face"}},
	})
	mesh := func(indices int, mark func(*viewerMesh)) *viewerMesh {
		m := &viewerMesh{}
		m.info = modelviewer.ModelViewerMeshTransport{IndexCount: indices}
		if mark != nil {
			mark(m)
		}
		return m
	}

	for _, test := range []struct {
		name string
		mesh *viewerMesh
		want string
	}{
		{"stock index count", mesh(300, func(m *viewerMesh) { m.efmiHead = true }), "small_cloth"},
		{"remodelled face", mesh(8100, func(m *viewerMesh) { m.efmiHead, m.efmiFace = true, true }), "face"},
		{"other head mesh", mesh(5000, func(m *viewerMesh) { m.efmiHead = true }), "hair"},
		{"eye", mesh(200, func(m *viewerMesh) { m.efmiHead, m.toonEye = true, true }), ""},
		{"rest of the model", mesh(70000, nil), "cloth"},
	} {
		if got := ramps.rampFor(test.mesh); got != test.want {
			t.Errorf("%s: ramp = %q, want %q", test.name, got, test.want)
		}
	}
}
