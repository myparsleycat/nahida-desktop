package ui

import (
	"encoding/json"
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
		if m.drawn(false) == wantHidden || !m.drawn(true) {
			t.Errorf("%s drawn = %v, highlighted %v; want hidden only without highlight: %v",
				m.info.ID, m.drawn(false), m.drawn(true), wantHidden)
		}
	}
	if !face.efmiHead || body.efmiHead || shoe.efmiHead || !iris.toonEye || face.toonEye {
		t.Errorf("head marks: face %v body %v shoe %v, eyes: iris %v face %v",
			face.efmiHead, body.efmiHead, shoe.efmiHead, iris.toonEye, face.toonEye)
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
