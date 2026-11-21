package ui

import (
	"encoding/json"
	"maps"
	"regexp"
	"strings"
	"testing"

	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

func TestMarkEFMIMeshesHidesOverlayShadows(t *testing.T) {
	texture := func(key string) *string { return &key }
	mesh := func(id string, tex *string, indices int, lo, hi [3]float64) *viewerMesh {
		m := &viewerMesh{}
		m.info = modelviewer.ModelViewerMeshTransport{
			ID: id, TexKey: tex, IndexCount: indices, Bounds: &modelviewer.ModelViewerBounds{Min: lo, Max: hi},
		}
		m.state.Visible = true
		return m
	}
	body := mesh("body", texture("body"), 30000, [3]float64{-0.25, -0.15, 0}, [3]float64{0.25, 0.15, 1.7})
	face := mesh("face", texture("face"), 9000, [3]float64{-0.08, -0.1, 1.45}, [3]float64{0.08, 0.1, 1.62})
	face.info.MaterialMapKey = texture("mask")
	eyeShadow := mesh("eyeShadow", nil, 150, [3]float64{-0.05, -0.1, 1.52}, [3]float64{0.05, -0.09, 1.53})
	hairShadow := mesh("hairShadow", nil, 900, [3]float64{-0.08, -0.1, 1.4}, [3]float64{0.08, 0.1, 1.5})
	iris := mesh("iris", texture("eye"), 300, [3]float64{-0.04, -0.1, 1.52}, [3]float64{0.04, -0.09, 1.54})
	shoe := mesh("shoe", nil, 150, [3]float64{-0.05, -0.1, 0}, [3]float64{0.05, 0.1, 0.1})
	scene := &viewerScene{
		transport: modelviewer.ModelViewerTransport{MaterialProfile: efmiProfile},
		meshes:    []*viewerMesh{body, face, eyeShadow, hairShadow, iris, shoe},
	}

	markViewerMeshes(scene)
	for _, m := range scene.meshes {
		wantHidden := m == eyeShadow || m == hairShadow
		if m.drawn() == wantHidden {
			t.Errorf("%s drawn = %v, want hidden: %v", m.info.ID, m.drawn(), wantHidden)
		}
	}
	if !face.efmiHead || body.efmiHead || shoe.efmiHead || !iris.toonEye || face.toonEye {
		t.Errorf("head marks: face %v body %v shoe %v, eyes: iris %v face %v",
			face.efmiHead, body.efmiHead, shoe.efmiHead, iris.toonEye, face.toonEye)
	}
}

// The shader reads each profile's values at fixed slots, so the Go table and the HLSL #defines must agree on
// which slots hold values; a slot only one side knows reads zero or is never written.
func TestViewerToonParamsMatchShaderSlots(t *testing.T) {
	defineRE := regexp.MustCompile(`(?m)^#define (GIMI|SRMI|ZZMI|WWMI|EFMI)_\w+ \(?-?toonParams\[(\d)\]\.([xyzw]+)`)
	shaderSlots := map[string]map[int]bool{}
	for _, match := range defineRE.FindAllStringSubmatch(string(modelViewerHLSL), -1) {
		profile := strings.ToLower(match[1])
		if shaderSlots[profile] == nil {
			shaderSlots[profile] = map[int]bool{}
		}
		row := int(match[2][0] - '0')
		for _, component := range match[3] {
			shaderSlots[profile][row*4+strings.IndexRune("xyzw", component)] = true
		}
	}

	for profile, params := range viewerToonParams {
		tableSlots := map[int]bool{}
		for _, param := range params {
			count := 1
			if param.color {
				count = 3
				if param.slot%4 != 0 {
					t.Errorf("%s %s: colour at slot %d, want a multiple of 4", profile, param.key, param.slot)
				}
			}
			for i := range count {
				slot := param.slot + i
				if slot >= 12 || tableSlots[slot] {
					t.Errorf("%s %s: slot %d is out of range or taken", profile, param.key, slot)
				}
				tableSlots[slot] = true
			}
			for i := range count {
				if param.def[i] < param.min || param.def[i] > param.max {
					t.Errorf("%s %s: default %v outside %v..%v", profile, param.key, param.def[i], param.min, param.max)
				}
			}
		}
		if !maps.Equal(tableSlots, shaderSlots[profile]) {
			t.Errorf("%s slots: table %v, shader %v", profile, tableSlots, shaderSlots[profile])
		}
	}
}

func TestToonStyleAliasesAndFlatFace(t *testing.T) {
	style := parseToonStyle(`{}`, efmiProfile)
	if !style.FlatFace {
		t.Fatal("EFMI flat face is off by default")
	}
	style.FlatFace = false
	stored := storeToonStyle(`{}`, efmiProfile, style)
	stored = storeToonStyle(stored, rabbitFXProfile, viewerToonStyle{toonFlags: toonFlags{SelfShadow: false}})
	if parseToonStyle(stored, efmiProfile).FlatFace {
		t.Errorf("stored %s keeps EFMI flat face on", stored)
	}
	if parseToonStyle(stored, wwmiProfile).Outlines {
		t.Errorf("stored %s: RabbitFX did not write the WWMI style", stored)
	}
}

func TestStoreToonStyleKeepsOtherProfiles(t *testing.T) {
	stored := `{"gimi":{"outlines":false,"ramp":"Avatar_Girl_Tex_FaceLightmap","faceType":"girl"},"zzmi":{"outlines":false}}`
	style := parseToonStyle(stored, srmiProfile)
	style.SelfShadow = false

	updated := storeToonStyle(stored, srmiProfile, style)
	var styles map[string]map[string]any
	if err := json.Unmarshal([]byte(updated), &styles); err != nil {
		t.Fatal(err)
	}
	if got := styles["zzmi"]["outlines"]; got != false {
		t.Errorf("zzmi outlines = %v, want false", got)
	}
	if got := parseToonStyle(updated, gimiProfile); got.Outlines || got.FaceType != "girl" ||
		got.Ramp != "Avatar_Girl_Tex_FaceLightmap" {
		t.Errorf("gimi style = %+v, want the stored entry", got)
	}
	srmi := styles[srmiProfile]
	if _, ok := srmi["ramp"]; ok || len(srmi) != 3 || srmi["selfShadow"] != false {
		t.Errorf("srmi entry = %v, want only its three flags with selfShadow off", srmi)
	}
}

// Stored shading comes from settings that older or hand-edited versions wrote, so what the shader cannot take must
// not reach it, and what the menu stores must read back the same.
func TestToonShadingDropsInvalidValues(t *testing.T) {
	stored := `{"gimi":{"shading":{"lightArea":5,"rimStrength":"high","toneCurve":1,"unknown":1,` +
		`"outlineColor":"#ff8000","specular":0.5}}}`
	shading := parseToonStyle(stored, gimiProfile).Shading
	if got := shading["lightArea"][0]; got != 0.8 {
		t.Errorf("lightArea = %v, want clamped to 0.8", got)
	}
	for _, key := range []string{"rimStrength", "toneCurve", "unknown"} {
		if _, ok := shading[key]; ok {
			t.Errorf("%s kept as %v, want dropped", key, shading[key])
		}
	}

	roundTrip := parseToonStyle(storeToonStyle(`{}`, gimiProfile, viewerToonStyle{Shading: shading}), gimiProfile)
	if !maps.Equal(roundTrip.Shading, shading) {
		t.Errorf("stored shading reads back as %v, want %v", roundTrip.Shading, shading)
	}
	if got := packToonParams(gimiProfile, shading)[0][3]; got != 0.5 {
		t.Errorf("packed specular = %v, want 0.5", got)
	}
}

// A character entry holds only what was changed for that character, so the rest keeps following the profile's
// style, and the light direction and texture folder stay shared by every character.
func TestCharacterToonStyleStoresOnlyChanges(t *testing.T) {
	const key = "GIMI/nahida"
	global := `{"gimi":{"outlines":false,"assetDir":"D:/tex","ramp":"Avatar_Girl_Sword_Ayaka",` +
		`"shading":{"lightArea":0.6}}}`
	stored := `{"GIMI/nahida":{"selfShadow":false,"shading":{"specular":0.5}},"GIMI/other":{"outlines":true}}`

	before := parseCharacterToonStyle(parseToonStyle(global, gimiProfile), stored, key, gimiProfile)
	if before.Outlines || before.SelfShadow || before.AssetDir != "D:/tex" ||
		before.Shading["lightArea"][0] != 0.6 || before.Shading["specular"][0] != 0.5 {
		t.Fatalf("character style = %+v, want its entry over the profile's style", before)
	}

	after := before
	after.Outlines, after.LightFollowsCamera, after.AssetDir = true, false, "elsewhere"
	after.Shading = maps.Clone(before.Shading)
	after.Shading["emission"] = [3]float64{1}
	var styles map[string]map[string]any
	if err := json.Unmarshal(
		[]byte(storeCharacterToonStyle(stored, key, gimiProfile, before, after)),
		&styles,
	); err != nil {
		t.Fatal(err)
	}
	entry := styles[key]
	shading, _ := entry["shading"].(map[string]any)
	if entry["outlines"] != true || entry["selfShadow"] != false || len(entry) != 3 ||
		len(shading) != 2 || shading["specular"] != 0.5 || shading["emission"] != 1.0 {
		t.Errorf("entry = %v, want outlines, the kept selfShadow and its own shading only", entry)
	}
	if styles["GIMI/other"]["outlines"] != true {
		t.Errorf("other characters = %v, want kept", styles)
	}

	reset := before
	reset.Shading = nil
	updated := storeCharacterToonStyle(stored, key, gimiProfile, before, reset)
	if got := parseCharacterToonStyle(
		parseToonStyle(global, gimiProfile),
		updated,
		key,
		gimiProfile,
	); len(
		got.Shading,
	) != 1 ||
		got.Shading["lightArea"][0] != 0.6 {
		t.Errorf("after reset shading = %v, want the profile's", got.Shading)
	}
}

func TestGenshinCharacterRamp(t *testing.T) {
	ramps := []modelviewer.GenshinToonRampTransport{
		{Name: "Avatar_Lady_Sword_Ayaka"}, {Name: "Avatar_Girl_Sword_Ayaka"}, {Name: "Avatar_Girl_Sword_Ayakax"},
	}
	if got := genshinCharacterRamp(ramps, "girl", "Ayaka"); got != "Avatar_Girl_Sword_Ayaka" {
		t.Errorf("ramp = %q, want the girl body's exact avatar", got)
	}
	if got := genshinCharacterRamp(ramps, "loli", "Ayaka"); got != "" {
		t.Errorf("ramp = %q, want none for another body", got)
	}
}

func TestGenshinFaceRow(t *testing.T) {
	procedural := genshinProceduralRamp()
	if got := genshinFaceRow(procedural, genshinRampWidth, genshinRampRows, genshinRampWidth*4); got != 3 {
		t.Errorf("built-in ramp face row = %d, want the skin row 3", got)
	}

	// Only daytime row 1 holds skin; a warmer night row must not win.
	pix := make([]byte, genshinRampRows*4)
	for band := range genshinRampRows {
		copy(pix[band*4:], []byte{200, 200, 230, 255})
	}
	copy(pix[3*4:], []byte{250, 210, 200, 255})
	copy(pix[5*4:], []byte{255, 200, 180, 255})
	if got := genshinFaceRow(pix, 1, genshinRampRows, 4); got != 1 {
		t.Errorf("face row = %d, want daytime row 1", got)
	}

	// Nicole's row 3 is lavender and her skin is in row 2.
	raw, err := readBundledToonMap(bundledToonScheme + characterToonDir +
		"gimi/Avatar_Lady_Catalyst_Nicole_Tex_Body_Shadow_Ramp.webp")
	if err != nil {
		t.Fatal(err)
	}
	ramp, err := decodeViewerImage(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := genshinFaceRow(ramp.mips[0], int(ramp.width), int(ramp.height), int(ramp.pitches[0])); got != 2 {
		t.Errorf("Nicole face row = %d, want 2", got)
	}
}
