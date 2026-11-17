package ui

import (
	"cmp"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// characterMaterialSlots is the length of the character array of the Mesh constants in model_viewer.hlsl.
const characterMaterialSlots = 24

// characterMaterial is a stock material's values packed as its game's toon shader reads them from the character
// array of the Mesh constants:
//
//	GIMI, by HoyoToon material ID 1 to 5 as i: [i] specular colour and shininess, [5+i] outline colour and
//	specular multiplier, [10] and [11].x outline intensities, [11].yz light area and the metal switch, [12] the
//	switches of materials 2 to 5, [13] to [16] the metal matcap's dark colour and brightness, light colour and
//	shininess, shadow colour and specular scale, and specular colour and shadow attenuation.
//	SRMI, by material ID 0 to 7 as i: [i] specular colour and shininess, [8+i] outline colour and roughness,
//	[16+i] rim colour and specular intensity.
//	ZZMI, by material ID 1 to 5 as i: [i] shadow colour and toon specular, [5+i] shallow colour, [10+i] specular
//	colour, [15+i] outline colour, [20] specular intensity, glossiness and metallic.
type characterMaterial [characterMaterialSlots][4]float32

// characterToonMaterials are the packed stock materials a mod's meshes take by part.
type characterToonMaterials struct {
	body, hair, face, dress *characterMaterial
}

// Bits of characterUse in the Mesh constants, as CHARACTER_* in model_viewer.hlsl: which of the character's values
// replace the toon shader's defaults and the menu's values.
const (
	characterUseValues    = 1
	characterUseLightArea = 2
	characterUseOutline   = 4
	characterUseShadow    = 8
	characterUseShallow   = 16
)

// characterMenuKeys are the menu values that, once the user changes them, take over from the character's.
var characterMenuKeys = map[string]map[string]uint32{
	gimiProfile: {"lightArea": characterUseLightArea, "outlineColor": characterUseOutline},
	srmiProfile: {"outlineColor": characterUseOutline},
	zzmiProfile: {
		"shadowBrightness": characterUseShadow, "shallowBrightness": characterUseShallow,
		"outlineBrightness": characterUseOutline,
	},
}

// characterMaterialUse returns the characterUse bits, without those of the values overrides sets, and the specular
// scale, shading's specular strength relative to its default, which the character's specular values are multiplied
// by.
func characterMaterialUse(profile string, overrides, shading toonShading) (uint32, float32) {
	keys := characterMenuKeys[profile]
	if keys == nil {
		return 0, 1
	}
	use := uint32(characterUseValues)
	for _, bit := range keys {
		use |= bit
	}
	for key, bit := range keys {
		if _, changed := overrides[key]; changed {
			use &^= bit
		}
	}
	scale := float32(1)
	for _, param := range viewerToonParams[profile] {
		if value, changed := shading[param.key]; changed && param.key == "specular" && param.def[0] > 0 {
			scale = float32(value[0] / param.def[0])
		}
	}
	return use, scale
}

// characterShadingValue is the menu value of key that stands for what the body material sets in its place: its
// first material ID's, as the menu holds one value where the material holds one per ID.
func characterShadingValue(profile, key string, body *characterMaterial) ([3]float64, bool) {
	rgb := func(v [4]float32) [3]float64 { return [3]float64{float64(v[0]), float64(v[1]), float64(v[2])} }
	mean := func(v [3]float64) [3]float64 { return [3]float64{(v[0] + v[1] + v[2]) / 3} }
	switch profile + "/" + key {
	case gimiProfile + "/lightArea":
		return [3]float64{float64(body[11][1])}, true
	case gimiProfile + "/outlineColor":
		return rgb(body[5]), true
	case srmiProfile + "/outlineColor":
		return rgb(body[8]), true
	case zzmiProfile + "/shadowBrightness":
		return mean(rgb(body[0])), true
	case zzmiProfile + "/shallowBrightness":
		return mean(rgb(body[5])), true
	case zzmiProfile + "/outlineBrightness":
		color := rgb(body[15])
		for i := range color {
			color[i] = math.Pow(color[i]*0.5, 1.5)
		}
		return mean(color), true
	}
	return [3]float64{}, false
}

// forMesh is the material a mesh draws with, or nil.
func (m *characterToonMaterials) forMesh(mesh *viewerMesh) *characterMaterial {
	switch {
	case m == nil:
		return nil
	case mesh.toonFace:
		return m.face
	case mesh.toonHair:
		return cmp.Or(m.hair, m.body)
	case mesh.toonDress && m.dress != nil:
		return m.dress
	}
	return m.body
}

// characterMaterialsOf packs the first face, hair, dress and body materials, and takes the first other material
// for the body when there is none.
func characterMaterialsOf(game string, materials []characterToonMaterial) (*characterToonMaterials, error) {
	pack := map[string]func(characterToonMaterial) (*characterMaterial, error){
		"gimi": packGenshinMaterial, "srmi": packStarRailMaterial, "zzmi": packZenlessMaterial,
	}[game]
	if pack == nil {
		return nil, nil
	}
	parts := &characterToonMaterials{}
	var other *characterMaterial
	for _, material := range materials {
		var part **characterMaterial
		switch role := material.Role; {
		case role == "face":
			part = &parts.face
		case strings.HasPrefix(role, "hair"):
			part = &parts.hair
		case strings.HasPrefix(role, "dress"):
			part = &parts.dress
		case strings.HasPrefix(role, "body"):
			part = &parts.body
		default:
			part = &other
		}
		if *part != nil {
			continue
		}
		packed, err := pack(material)
		if err != nil {
			return nil, err
		}
		*part = packed
	}
	parts.body = cmp.Or(parts.body, other)
	if *parts == (characterToonMaterials{}) {
		return nil, nil
	}
	return parts, nil
}

func (m characterToonMaterial) float(key string, def float64) float32 {
	if value, ok := m.Floats[key]; ok {
		return float32(value)
	}
	return float32(def)
}

func (m characterToonMaterial) color(key string, def [3]float64) [3]float32 {
	value, ok := m.Colors[key]
	if !ok {
		value = [4]float64{def[0], def[1], def[2]}
	}
	return [3]float32{float32(value[0]), float32(value[1]), float32(value[2])}
}

func packColor(color [3]float32, w float32) [4]float32 {
	return [4]float32{color[0], color[1], color[2], w}
}

// genshinSuffix numbers Genshin's and Zenless's per-material properties: none for the first, then 2 to 5.
func genshinSuffix(i int) string {
	if i == 0 {
		return ""
	}
	return strconv.Itoa(i + 1)
}

// packGenshinMaterial falls back to HoyoToon's defaults. The outline colour is drawn as it is rather than over the
// albedo, so it is converted from the material's sRGB value.
func packGenshinMaterial(m characterToonMaterial) (*characterMaterial, error) {
	var p characterMaterial
	white := [3]float64{1, 1, 1}
	var intensities [5]float32
	for i := range 5 {
		suffix := genshinSuffix(i)
		p[i] = packColor(m.color("_SpecularColor"+suffix, white), m.float("_Shininess"+suffix, 10))
		outline := m.color("_OutlineColor"+suffix, [3]float64{})
		for c := range outline {
			outline[c] = float32(srgbToLinear(float64(outline[c])))
		}
		p[5+i] = packColor(outline, m.float("_SpecMulti"+suffix, 0.1))
		intensities[i] = m.float("_OutLineIntensity"+suffix, 0)
	}
	p[10] = [4]float32(intensities[:4])
	p[11] = [4]float32{intensities[4], m.float("_LightArea", 0.55), m.float("_MetalMaterial", 1)}
	for i := range 4 {
		p[12][i] = m.float("_UseMaterial"+strconv.Itoa(i+2), 1)
	}
	p[13] = packColor(m.color("_MTMapDarkColor", [3]float64{0.51, 0.3, 0.19}), m.float("_MTMapBrightness", 3))
	p[14] = packColor(m.color("_MTMapLightColor", white), m.float("_MTShininess", 90))
	p[15] = packColor(m.color("_MTShadowMultiColor", [3]float64{0.78, 0.77, 0.82}), m.float("_MTSpecularScale", 15))
	p[16] = packColor(m.color("_MTSpecularColor", white), m.float("_MTSpecularAttenInShadow", 0.2))
	return &p, nil
}

// packStarRailMaterial reads the material values LUT where the material uses one, as HoyoToon does: column i holds
// material ID i and rows run from the bottom of the image, Unity's first row. Its specular values are scaled by
// HoyoToon's 10, 2 and 2. Without a LUT the material's properties apply, with HoyoToon's defaults.
func packStarRailMaterial(m characterToonMaterial) (*characterMaterial, error) {
	var lut *viewerTextureData
	if name := m.Textures["_MaterialValuesPackLUT"]; name != "" && m.Floats["_UseMaterialValuesLUT"] != 0 {
		raw, err := readBundledToonMap(bundledToonScheme + characterToonDir + "srmi/" + name + ".webp")
		if err == nil {
			lut, err = decodeViewerImage(raw)
		}
		if err != nil {
			return nil, fmt.Errorf("decode material values LUT %s: %w", name, err)
		}
		if lut.width != 8 || lut.height != 8 {
			return nil, fmt.Errorf("material values LUT %s is %dx%d, want 8x8", name, lut.width, lut.height)
		}
	}
	row := func(id, r int) [3]float32 {
		texel := lut.mips[0][(7-r)*int(lut.pitches[0])+id*4:]
		return [3]float32{float32(texel[0]) / 255, float32(texel[1]) / 255, float32(texel[2]) / 255}
	}

	var p characterMaterial
	white := [3]float64{1, 1, 1}
	for i := range 8 {
		suffix := strconv.Itoa(i)
		specular := m.color("_SpecularColor"+suffix, white)
		values := [3]float32{
			m.float("_SpecularShininess"+suffix, 10), m.float("_SpecularRoughness"+suffix, 0.02),
			m.float("_SpecularIntensity"+suffix, 1),
		}
		outline := m.color("_OutlineColor"+suffix, [3]float64{})
		if _, ok := m.Colors["_OutlineColor"+suffix]; !ok {
			outline = m.color("_OutlineColor", [3]float64{})
		}
		rim := m.color("_RimColor"+suffix, white)
		if lut != nil {
			specular, outline, rim = row(i, 0), row(i, 2), row(i, 3)
			values = row(i, 1)
			values[0], values[1], values[2] = values[0]*10, values[1]*2, values[2]*2
		}
		p[i] = packColor(specular, values[0])
		p[8+i] = packColor(outline, values[1])
		p[16+i] = packColor(rim, max(values[2], 0))
	}
	return &p, nil
}

// packZenlessMaterial falls back to HoyoToon's defaults.
func packZenlessMaterial(m characterToonMaterial) (*characterMaterial, error) {
	var p characterMaterial
	white := [3]float64{1, 1, 1}
	for i := range 5 {
		suffix := genshinSuffix(i)
		p[i] = packColor(
			m.color("_ShadowColor"+suffix, [3]float64{0.6, 0.6, 0.6}),
			m.float("_ToonSpecular"+suffix, 0.01),
		)
		p[5+i] = packColor(m.color("_ShallowColor"+suffix, [3]float64{0.8, 0.8, 0.8}), 0)
		p[10+i] = packColor(m.color("_SpecularColor"+suffix, white), 0)
		p[15+i] = packColor(m.color("_OutlineColor"+suffix, white), 0)
	}
	p[20] = [4]float32{m.float("_SpecIntensity", 0.1), m.float("_Glossiness", 0.5), m.float("_Metallic", 0)}
	return &p, nil
}
