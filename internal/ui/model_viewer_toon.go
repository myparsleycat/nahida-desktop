package ui

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/d3d"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

// The GIMI toon shading follows the Genshin Impact shader of HoyoToon (https://github.com/Hoyotoon/HoyoToon,
// GPL-3.0) and kaze-mio's UnityGenshinToonShader (MIT); the SRMI, ZZMI and WWMI shading follows HoyoToon's Star
// Rail, Zenless Zone Zero and Wuthering Waves shaders, the first with StarRailNPRShader (GPL-3.0); the EFMI shading
// follows the HGRP character shaders as Warfarin Studio reconstructed them. All as the React viewer's material
// profiles ported them.

const (
	gimiProfile     = "gimi"
	srmiProfile     = "srmi"
	zzmiProfile     = "zzmi"
	wwmiProfile     = "wwmi"
	rabbitFXProfile = "wuwa:rabbitfx"
	efmiProfile     = "efmi"
)

// Toon shadings, as TOON_* in model_viewer.hlsl.
const (
	toonModeGIMI = 1 + iota
	toonModeSRMI
	toonModeZZMI
	toonModeWWMI
	toonModeEFMI
)

// viewerToonProfile is a material profile with a native toon shader.
type viewerToonProfile struct {
	mode float32
	// label is the style submenu's key under page.tools.model_viewer.menu.
	label string
	// styleKey is the profile whose stored style it reads and writes; RabbitFX mods draw with the WWMI shader.
	styleKey string
	// importer is the game of the character catalog whose characters the profile draws.
	importer string
}

var viewerToonProfiles = map[string]viewerToonProfile{
	gimiProfile:     {mode: toonModeGIMI, label: "gimi_toon_style", styleKey: gimiProfile, importer: "GIMI"},
	srmiProfile:     {mode: toonModeSRMI, label: "srmi_toon_style", styleKey: srmiProfile, importer: "SRMI"},
	zzmiProfile:     {mode: toonModeZZMI, label: "zzmi_toon_style", styleKey: zzmiProfile, importer: "ZZMI"},
	wwmiProfile:     {mode: toonModeWWMI, label: "wwmi_toon_style", styleKey: wwmiProfile, importer: "WWMI"},
	rabbitFXProfile: {mode: toonModeWWMI, label: "wwmi_toon_style", styleKey: wwmiProfile, importer: "WWMI"},
	efmiProfile:     {mode: toonModeEFMI, label: "toon_style", styleKey: efmiProfile, importer: "EFMI"},
}

// averagedOutlineProfiles extrude their hulls along seam-averaged normals, as the React viewer's zzmi.ts, wwmi.ts
// and efmi.ts do; the game keeps the outline normal where the viewer does not decode it.
var averagedOutlineProfiles = map[string]bool{
	zzmiProfile: true, wwmiProfile: true, rabbitFXProfile: true, efmiProfile: true,
}

var genshinFaceTypes = []string{"boy", "girl", "lady", "loli", "male"}

// toonFlags are the switches of every toon style.
type toonFlags struct {
	Outlines           bool `json:"outlines"`
	SelfShadow         bool `json:"selfShadow"`
	LightFollowsCamera bool `json:"lightFollowsCamera"`
}

// viewerToonStyle is what a toon menu stores under its profile's key of the toon styles setting. Only GIMI has
// Ramp, FaceType and AssetDir, and only EFMI FlatFace.
type viewerToonStyle struct {
	toonFlags
	// FlatFace lights EFMI's head meshes from the face mask instead of N.L.
	FlatFace bool   `json:"flatFace"`
	Ramp     string `json:"ramp"`
	// FaceType is "auto", which takes the body type from the ramp name, or one of genshinFaceTypes.
	FaceType string `json:"faceType"`
	AssetDir string `json:"assetDir"`
	Shading  toonShading
	// rampAuto is set when Ramp is the character's ramp found in the texture folder rather than a choice.
	rampAuto bool
	// characterBody is the character's body type, which the automatic face type prefers to the ramp's.
	characterBody string
	// characterShading holds the shading values the character's own entry and the menu set, which take over from
	// the character's material; values of the profile's style do not.
	characterShading toonShading
}

// toonRampAuto is the stored ramp of a character style that takes the character's ramp from the texture folder.
const toonRampAuto = "auto"

var defaultToonStyle = viewerToonStyle{
	toonFlags: toonFlags{Outlines: true, SelfShadow: true, LightFollowsCamera: true},
	FlatFace:  true,
	FaceType:  "auto",
}

// toonShading holds the shading values a style changes from their defaults, by toonParam key. A scalar uses the
// first element; a colour is linear.
type toonShading map[string][3]float64

// toonParam is one shading value of a profile, which the renderer packs into the toonParams constants at slot.
// A colour takes the three floats from slot, which is a multiple of four.
type toonParam struct {
	// key names the value in the stored style and, in snake case, its label
	// page.tools.model_viewer.menu.toon_param_<key>.
	key            string
	slot           int
	color          bool
	def            [3]float64
	min, max, step float64
	// hidden values are packed but not offered in the menu.
	hidden bool
}

func scalarToonParam(key string, slot int, def, low, high, step float64) toonParam {
	return toonParam{key: key, slot: slot, def: [3]float64{def}, min: low, max: high, step: step}
}

func colorToonParam(key string, slot int, def [3]float64) toonParam {
	return toonParam{key: key, slot: slot, color: true, def: def, max: 1}
}

// viewerToonParams are the shading values of each toon style, by style key, at the slots the #defines at the top
// of model_viewer.hlsl read.
//
// GIMI: HoyoToon's defaults, except the light area, with which frontal light would shade limbs the game keeps lit;
// shadow brightness and specular strength come from kaze-mio's Nahida material, and the outline colour was measured
// against lit skin in game. SRMI: StarRailNPRShader's black outline reads too harsh, and neither its tint nor the
// shadow tint is measured. EFMI: Warfarin's brightness default of 2 clips less because its materials carry tints
// that mods lack, and 1.5 approximates its output for the same textures.
var viewerToonParams = map[string][]toonParam{
	gimiProfile: {
		scalarToonParam("lightArea", 0, 0.5, 0.2, 0.8, 0.01),
		scalarToonParam("shadowBrightness", 1, 1.1, 0.5, 1.5, 0.01),
		scalarToonParam("brightness", 2, 1, 0.5, 1.5, 0.01),
		scalarToonParam("specular", 3, 0.3, 0, 2, 0.01),
		scalarToonParam("rimStrength", 4, 1, 0, 3, 0.05),
		scalarToonParam("emission", 5, 0.2, 0, 2, 0.01),
		scalarToonParam("outlineWidth", 6, 0.015, 0, 0.05, 0.001),
		{key: "toneCurve", slot: 7, max: 1, hidden: true},
		colorToonParam("outlineColor", 8, [3]float64{0.26, 0.16, 0.24}),
	},
	srmiProfile: {
		scalarToonParam("lightArea", 0, 0.575, 0.3, 0.85, 0.005),
		scalarToonParam("specular", 1, 0.35, 0, 1.5, 0.01),
		scalarToonParam("rimStrength", 2, 0.1, 0, 0.5, 0.01),
		scalarToonParam("outlineWidth", 3, 1, 0, 3, 0.05),
		colorToonParam("shadowTint", 4, [3]float64{0.85, 0.74, 0.78}),
		colorToonParam("outlineColor", 8, [3]float64{0.3, 0.22, 0.24}),
	},
	zzmiProfile: {
		scalarToonParam("shadowBrightness", 0, 0.6, 0.3, 1, 0.01),
		scalarToonParam("shallowBrightness", 1, 0.8, 0.4, 1, 0.01),
		scalarToonParam("specular", 2, 0.1, 0, 0.5, 0.01),
		scalarToonParam("rimStrength", 3, 1.5, 0, 4, 0.05),
		scalarToonParam("emission", 4, 1, 0, 3, 0.05),
		scalarToonParam("outlineWidth", 5, 0.5, 0, 2, 0.05),
		scalarToonParam("outlineBrightness", 6, math.Pow(0.5, 1.5), 0, 1, 0.01),
	},
	wwmiProfile: {
		scalarToonParam("lightArea", 0, 0.55, 0.3, 0.8, 0.01),
		scalarToonParam("shadowSoftness", 1, 0.01, 0, 0.2, 0.005),
		scalarToonParam("shadowBrightness", 2, 0.5, 0, 1, 0.01),
		scalarToonParam("specular", 3, 1, 0, 3, 0.05),
		scalarToonParam("rimStrength", 4, 0.5, 0, 2, 0.05),
		scalarToonParam("outlineWidth", 5, 1, 0, 3, 0.05),
		colorToonParam("outlineColor", 8, [3]float64{0.765, 0.765, 0.765}),
	},
	efmiProfile: {
		scalarToonParam("brightness", 0, 1.5, 0.5, 3, 0.05),
		scalarToonParam("shadowBrightness", 1, 0.5, 0, 1, 0.01),
		scalarToonParam("shadowSoftness", 2, 0.02, 0, 0.3, 0.005),
		scalarToonParam("lightArea", 3, 0.4, -0.5, 1, 0.01),
		scalarToonParam("specular", 4, 1, 0, 3, 0.05),
		scalarToonParam("rimStrength", 5, 0.5, 0, 2, 0.05),
		scalarToonParam("outlineWidth", 6, 0.25, 0, 1, 0.01),
		scalarToonParam("outlineBrightness", 7, 0.5, 0, 1, 0.01),
	},
}

// parseToonShading reads a stored shading object: a number for a scalar, "#rrggbb" sRGB for a colour. Unknown and
// hidden keys and values of the wrong type are dropped, and scalars are clamped to their range.
func parseToonShading(profile string, raw any) toonShading {
	record, _ := raw.(map[string]any)
	var shading toonShading
	for _, param := range viewerToonParams[toonStyleKey(profile)] {
		value, ok := record[param.key]
		if !ok || param.hidden {
			continue
		}
		var parsed [3]float64
		if param.color {
			text, _ := value.(string)
			color, ok := parseToonColor(text)
			if !ok {
				continue
			}
			parsed = color
		} else {
			number, ok := value.(float64)
			if !ok || math.IsNaN(number) {
				continue
			}
			parsed[0] = min(max(number, param.min), param.max)
		}
		if shading == nil {
			shading = toonShading{}
		}
		shading[param.key] = parsed
	}
	return shading
}

// encodeToonShading is the stored form parseToonShading reads, or nil when nothing is changed.
func encodeToonShading(profile string, shading toonShading) map[string]any {
	var encoded map[string]any
	for _, param := range viewerToonParams[toonStyleKey(profile)] {
		value, ok := shading[param.key]
		if !ok || param.hidden {
			continue
		}
		if encoded == nil {
			encoded = map[string]any{}
		}
		if param.color {
			encoded[param.key] = formatToonColor(value)
		} else {
			encoded[param.key] = value[0]
		}
	}
	return encoded
}

func parseToonColor(text string) ([3]float64, bool) {
	hex, ok := strings.CutPrefix(text, "#")
	if !ok || len(hex) != 6 {
		return [3]float64{}, false
	}
	packed, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return [3]float64{}, false
	}
	var color [3]float64
	for i := range 3 {
		color[i] = srgbToLinear(float64(packed>>(16-8*i)&0xff) / 255)
	}
	return color, true
}

func formatToonColor(color [3]float64) string {
	var packed uint32
	for i := range 3 {
		channel := math.Round(min(max(linearToSRGB(color[i]), 0), 1) * 255)
		packed |= uint32(channel) << (16 - 8*i)
	}
	return fmt.Sprintf("#%06x", packed)
}

func linearToSRGB(c float64) float64 {
	if c <= 0.0031308 {
		return c * 12.92
	}
	return 1.055*math.Pow(c, 1/2.4) - 0.055
}

// packToonParams lays out a profile's shading values as the toonParams constants, taking each value from shading
// or its default.
func packToonParams(profile string, shading toonShading) [3][4]float32 {
	var packed [3][4]float32
	for _, param := range viewerToonParams[toonStyleKey(profile)] {
		value, ok := shading[param.key]
		if !ok {
			value = param.def
		}
		count := 1
		if param.color {
			count = 3
		}
		for i := range count {
			slot := param.slot + i
			packed[slot/4][slot%4] = float32(value[i])
		}
	}
	return packed
}

// toonFieldOfView is the lens of every toon style.
const toonFieldOfView = 20 * math.Pi / 180

// toonStyleKey is the profile whose entry of the stored toon styles a profile reads and writes.
func toonStyleKey(profile string) string {
	if key := viewerToonProfiles[profile].styleKey; key != "" {
		return key
	}
	return profile
}

// parseToonStyle reads a profile's entry of the stored toon styles; values of the wrong type keep the defaults.
func parseToonStyle(stored, profile string) viewerToonStyle {
	var styles map[string]map[string]any
	if json.Unmarshal([]byte(stored), &styles) != nil {
		return defaultToonStyle
	}
	return applyToonRecord(defaultToonStyle, toonStyleKey(profile), styles[toonStyleKey(profile)], false)
}

// parseCharacterToonStyle lays a character's entry of the stored character toon styles over style. The light
// direction and the texture folder are not per character, and the shading values a character entry leaves out
// keep style's.
func parseCharacterToonStyle(style viewerToonStyle, stored, characterKey, profile string) viewerToonStyle {
	var styles map[string]map[string]any
	if json.Unmarshal([]byte(stored), &styles) != nil {
		return style
	}
	return applyToonRecord(style, toonStyleKey(profile), styles[characterKey], true)
}

// applyToonRecord lays the fields of a stored style record over style; values of the wrong type are skipped.
func applyToonRecord(style viewerToonStyle, profile string, record map[string]any, overlay bool) viewerToonStyle {
	flag := func(key string, target *bool) {
		if value, ok := record[key].(bool); ok {
			*target = value
		}
	}
	flag("outlines", &style.Outlines)
	flag("selfShadow", &style.SelfShadow)
	if !overlay {
		flag("lightFollowsCamera", &style.LightFollowsCamera)
	}
	if profile == efmiProfile {
		flag("flatFace", &style.FlatFace)
	}
	shading := parseToonShading(profile, record["shading"])
	if overlay && shading != nil {
		merged := maps.Clone(style.Shading)
		if merged == nil {
			merged = toonShading{}
		}
		maps.Copy(merged, shading)
		shading = merged
	}
	if !overlay || shading != nil {
		style.Shading = shading
	}
	if profile != gimiProfile {
		return style
	}
	if value, ok := record["ramp"].(string); ok && (overlay || value != toonRampAuto) {
		style.Ramp = value
	}
	if value, ok := record["faceType"].(string); ok && (value == "auto" || slices.Contains(genshinFaceTypes, value)) {
		style.FaceType = value
	}
	if value, ok := record["assetDir"].(string); ok && !overlay {
		style.AssetDir = value
	}
	return style
}

// storeToonStyle writes style over a profile's entry of the stored toon styles and keeps the other profiles'
// entries as they are.
func storeToonStyle(stored, profile string, style viewerToonStyle) string {
	styles := map[string]json.RawMessage{}
	if json.Unmarshal([]byte(stored), &styles) != nil || styles == nil {
		styles = map[string]json.RawMessage{}
	}
	profile = toonStyleKey(profile)
	record := map[string]any{
		"outlines":           style.Outlines,
		"selfShadow":         style.SelfShadow,
		"lightFollowsCamera": style.LightFollowsCamera,
	}
	switch profile {
	case gimiProfile:
		record["ramp"], record["faceType"], record["assetDir"] = style.Ramp, style.FaceType, style.AssetDir
	case efmiProfile:
		record["flatFace"] = style.FlatFace
	}
	if shading := encodeToonShading(profile, style.Shading); shading != nil {
		record["shading"] = shading
	}
	entry, _ := json.Marshal(record)
	styles[profile] = entry
	encoded, _ := json.Marshal(styles)
	return string(encoded)
}

// storeCharacterToonStyle writes the fields that differ between before and after, two styles of the profile as the
// viewer showed them with the character's entry applied, into that entry of the stored character toon styles.
// The light direction and the texture folder are left to the profile's style.
func storeCharacterToonStyle(stored, characterKey, profile string, before, after viewerToonStyle) string {
	styles := map[string]map[string]any{}
	if json.Unmarshal([]byte(stored), &styles) != nil || styles == nil {
		styles = map[string]map[string]any{}
	}
	profile = toonStyleKey(profile)
	record := styles[characterKey]
	if record == nil {
		record = map[string]any{}
	}
	if after.Outlines != before.Outlines {
		record["outlines"] = after.Outlines
	}
	if after.SelfShadow != before.SelfShadow {
		record["selfShadow"] = after.SelfShadow
	}
	if profile == efmiProfile && after.FlatFace != before.FlatFace {
		record["flatFace"] = after.FlatFace
	}
	if profile == gimiProfile && (after.Ramp != before.Ramp || after.rampAuto != before.rampAuto) {
		if after.Ramp == toonRampAuto {
			delete(record, "ramp")
		} else {
			record["ramp"] = after.Ramp
		}
	}
	if profile == gimiProfile && after.FaceType != before.FaceType {
		record["faceType"] = after.FaceType
	}

	shading := parseToonShading(profile, record["shading"])
	for key, value := range after.Shading {
		if previous, ok := before.Shading[key]; !ok || previous != value {
			if shading == nil {
				shading = toonShading{}
			}
			shading[key] = value
		}
	}
	for key := range before.Shading {
		if _, ok := after.Shading[key]; !ok {
			delete(shading, key)
		}
	}
	if encoded := encodeToonShading(profile, shading); encoded != nil {
		record["shading"] = encoded
	} else {
		delete(record, "shading")
	}

	if len(record) == 0 {
		delete(styles, characterKey)
	} else {
		styles[characterKey] = record
	}
	encoded, _ := json.Marshal(styles)
	return string(encoded)
}

// removeCharacterToonStyle drops a character's entry of the stored character toon styles.
func removeCharacterToonStyle(stored, characterKey string) string {
	styles := map[string]json.RawMessage{}
	if json.Unmarshal([]byte(stored), &styles) != nil || styles == nil {
		return stored
	}
	delete(styles, characterKey)
	encoded, _ := json.Marshal(styles)
	return string(encoded)
}

// genshinCharacterRamp finds a character's shadow ramp, Avatar_<Body>_<Weapon>_<Avatar>, among the listed ones.
func genshinCharacterRamp(ramps []modelviewer.GenshinToonRampTransport, body, avatar string) string {
	if body == "" || avatar == "" {
		return ""
	}
	prefix, suffix := "avatar_"+body+"_", "_"+strings.ToLower(avatar)
	for _, ramp := range ramps {
		name := strings.ToLower(ramp.Name)
		weapon, ok := strings.CutPrefix(name, prefix)
		if !ok {
			continue
		}
		if weapon, ok = strings.CutSuffix(weapon, suffix); ok && weapon != "" && !strings.Contains(weapon, "_") {
			return ramp.Name
		}
	}
	return ""
}

var genshinBodyTypeRE = regexp.MustCompile(`(?i)^avatar_([a-z]+)_`)

// genshinFaceType is the face light map the style selects, or "" for none.
func genshinFaceType(style viewerToonStyle) string {
	if style.FaceType != "auto" {
		return style.FaceType
	}
	if style.characterBody != "" {
		return style.characterBody
	}
	if match := genshinBodyTypeRE.FindStringSubmatch(style.Ramp); match != nil {
		if bodyType := strings.ToLower(match[1]); slices.Contains(genshinFaceTypes, bodyType) {
			return bodyType
		}
	}
	return ""
}

var (
	genshinFaceRE = regexp.MustCompile(`(?i)face`)
	genshinHairRE = regexp.MustCompile(`(?i)hair|head`)
)

// markGenshinMeshes tells faces and hair apart by their names, which GIMI mods take from the game's own parts
// (Head, Body, Dress and a separately drawn Face).
func markGenshinMeshes(scene *viewerScene) {
	for _, mesh := range scene.meshes {
		texKey := ""
		if mesh.info.TexKey != nil {
			texKey = *mesh.info.TexKey
		}
		names := mesh.info.Component + " " + mesh.info.ID + " " + texKey
		mesh.toonFace = genshinFaceRE.MatchString(names)
		mesh.toonHair = !mesh.toonFace && genshinHairRE.MatchString(names)
		mesh.toonDress = strings.Contains(strings.ToLower(mesh.info.Component), "dress")
		scene.genshinFace = scene.genshinFace || mesh.toonFace
	}
}

// A Star Rail part name ends its override section, optionally followed by one submesh letter or digit, so
// accessories such as Headwear or HairClip do not match. Star Rail calls the face Head.
var (
	starRailHairRE = regexp.MustCompile(`(?i)hair[a-z\d]?$`)
	starRailFaceRE = regexp.MustCompile(`(?i)(?:face|head)[a-z\d]?$`)
)

// Star Rail's eye shadow is a small part of the head's index buffer, 138 to 180 indices in the SRMI mods surveyed,
// where the face itself has thousands. It binds no diffuse of its own, so it either has none or inherits the face's.
const starRailEyeShadowMaxIndices = 240

// starRailEyeShadowColor is StarRailNPRShader's EyeShadow colour, applied to the display-referred image.
var starRailEyeShadowColor = [3]float32{0.6770648, 0.7038123, 0.8018868}

func markStarRailMeshes(scene *viewerScene) {
	faceTextures := map[string]bool{}
	for _, mesh := range scene.meshes {
		mesh.toonHair = starRailHairRE.MatchString(mesh.info.Component)
		mesh.toonFace = !mesh.toonHair && starRailFaceRE.MatchString(mesh.info.Component)
		if mesh.toonFace && mesh.info.IndexCount > starRailEyeShadowMaxIndices && texturePresent(mesh.info.TexKey) {
			faceTextures[*mesh.info.TexKey] = true
		}
	}
	for _, mesh := range scene.meshes {
		info := mesh.info
		mesh.eyeShadow = mesh.toonFace && info.IndexCount > 0 && info.IndexCount <= starRailEyeShadowMaxIndices &&
			len(info.TextureVariants) == 0 && len(info.TextureChoices) == 0 &&
			(!texturePresent(info.TexKey) || faceTextures[*info.TexKey])
	}
}

func texturePresent(key *string) bool {
	return key != nil && *key != ""
}

// ZZMI and WWMI components end in a part name, such as EllenHair or AnbySFaceHead.
var (
	partFaceRE = regexp.MustCompile(`(?i)face(?:head)?[a-z\d]?$`)
	zzmiHairRE = regexp.MustCompile(`(?i)hair[a-z\d]?$`)
	wwmiHairRE = regexp.MustCompile(`(?i)(?:hair|bangs)[a-z\d]?$`)
	wwmiEyeRE  = regexp.MustCompile(`(?i)eyes?[a-z\d]?$`)
)

// markZZMIMeshes marks faces, whose normal map holds the SDF and chin AO instead of a normal, and hair.
func markZZMIMeshes(scene *viewerScene) {
	for _, mesh := range scene.meshes {
		mesh.toonFace = partFaceRE.MatchString(mesh.info.Component)
		mesh.normalMapUnused = mesh.toonFace
		mesh.toonHair = !mesh.toonFace && zzmiHairRE.MatchString(mesh.info.Component)
	}
}

func markWWMIMeshes(scene *viewerScene) {
	for _, mesh := range scene.meshes {
		component := mesh.info.Component
		mesh.toonFace = partFaceRE.MatchString(component)
		mesh.toonHair = !mesh.toonFace && wwmiHairRE.MatchString(component)
		mesh.toonEye = !mesh.toonFace && !mesh.toonHair && wwmiEyeRE.MatchString(component)
	}
}

// The EFMI head zone, measured in the mod's Z-up space.
const (
	efmiHeadMinHeight = 0.75
	efmiHeadMaxWidth  = 0.3
	// Stock faces span 8040 to 12876 indices across the 34 characters, leaving room for edits.
	efmiFaceMinIndices       = 8000
	efmiFaceMaxIndices       = 16000
	efmiBoundFaceMinHeight   = 0.55
	efmiSizedFaceMinHeight   = 0.66
	efmiHeadBelowFace        = 0.08 // overlay shadows and hair reach below the chin by about 5 cm
	efmiEyeShadowMaxIndices  = 200
	efmiHairShadowMaxIndices = 1200
	efmiHairShadowMinHeight  = 0.075
	efmiIrisMaxHeight        = 0.035
)

// markEFMIMeshes finds the head meshes in each form. The game lights faces from an SDF texture bound outside the
// mod, and mods name no parts, so the head zone is measured from the face: the mesh binding the face's mask or
// highlight, else a mesh sized like a stock face, else, as a prop may be taller than the character, a fraction of
// the model's height. Small head meshes that bind no diffuse are the game's overlay shadows; across the 34 stock
// characters eye shadows stop at 156 indices and hair shadows at 1158, irises reach 480 indices but are at most
// 3 cm tall, and hair shadows at least 7.8 cm.
func markEFMIMeshes(scene *viewerScene) {
	forms := map[string]bool{}
	for _, mesh := range scene.meshes {
		if mesh.info.Form != "" {
			forms[mesh.info.Form] = true
		}
	}
	if len(forms) == 0 {
		forms[""] = true
	}
	for form := range forms {
		var entries []*viewerMesh
		for _, mesh := range scene.meshes {
			if mesh.info.Bounds != nil && (mesh.info.Form == "" || mesh.info.Form == form) {
				entries = append(entries, mesh)
			}
		}
		markEFMIHeads(entries)
	}
}

func markEFMIHeads(entries []*viewerMesh) {
	if len(entries) == 0 {
		return
	}
	top, bottom := math.Inf(-1), math.Inf(1)
	for _, mesh := range entries {
		top, bottom = max(top, mesh.info.Bounds.Max[2]), min(bottom, mesh.info.Bounds.Min[2])
	}
	heightOf := func(z float64) float64 { return (z - bottom) / (top - bottom) }

	var narrow, bound, sized []*viewerMesh
	for _, mesh := range entries {
		b := mesh.info.Bounds
		if max(b.Max[0]-b.Min[0], b.Max[1]-b.Min[1]) > efmiHeadMaxWidth {
			continue
		}
		narrow = append(narrow, mesh)
		if texturePresent(mesh.info.LightMapKey) || len(mesh.info.LightMapVariants) > 0 {
			continue
		}
		if (texturePresent(mesh.info.MaterialMapKey) || texturePresent(mesh.info.HighlightMapKey)) &&
			heightOf(b.Min[2]) >= efmiBoundFaceMinHeight {
			bound = append(bound, mesh)
		}
		if indices := mesh.info.IndexCount; indices >= efmiFaceMinIndices && indices <= efmiFaceMaxIndices &&
			heightOf(b.Min[2]) >= efmiSizedFaceMinHeight {
			sized = append(sized, mesh)
		}
	}
	faces := bound
	if len(faces) == 0 {
		faces = sized
	}
	headFloor := bottom + (top-bottom)*efmiHeadMinHeight
	if len(faces) > 0 {
		headFloor = math.Inf(-1)
		for _, mesh := range faces {
			headFloor = max(headFloor, mesh.info.Bounds.Min[2])
			mesh.efmiFace = true
		}
		headFloor -= efmiHeadBelowFace
	}

	for _, mesh := range narrow {
		b := mesh.info.Bounds
		if b.Min[2] < headFloor {
			continue
		}
		height, indices := b.Max[2]-b.Min[2], mesh.info.IndexCount
		mesh.efmiHead = true
		mesh.overlayShadow = !texturePresent(mesh.info.TexKey) && len(mesh.info.TextureVariants) == 0 &&
			len(mesh.info.TextureChoices) == 0 &&
			(indices <= efmiEyeShadowMaxIndices ||
				(indices <= efmiHairShadowMaxIndices && height >= efmiHairShadowMinHeight))
		mesh.toonEye = !mesh.overlayShadow && height <= efmiIrisMaxHeight
	}
}

// markViewerMeshes marks the parts the scene's material profile shades apart.
func markViewerMeshes(scene *viewerScene) {
	switch string(scene.transport.MaterialProfile) {
	case gimiProfile:
		markGenshinMeshes(scene)
	case srmiProfile:
		markStarRailMeshes(scene)
	case zzmiProfile:
		markZZMIMeshes(scene)
	case wwmiProfile, rabbitFXProfile:
		markWWMIMeshes(scene)
	case efmiProfile:
		markEFMIMeshes(scene)
	}
}

// toonLightDirection aims the key light as the game aims its menu light: from the camera, turned 10 degrees
// and kept at least 32 degrees above the horizon.
func toonLightDirection(forward vec3) vec3 {
	const degree = math.Pi / 180
	yaw := math.Atan2(-forward[0], forward[2]) + 10*degree
	pitch := max(32*degree, math.Asin(min(max(-forward[1], -1), 1)))
	return vec3{math.Sin(yaw) * math.Cos(pitch), math.Sin(pitch), -math.Cos(yaw) * math.Cos(pitch)}
}

const (
	genshinRampWidth = 256
	genshinRampRows  = 10
)

// genshinRampShadows are the sRGB shadow ends of the built-in ramp rows, by light map alpha 0.7, 0.3, 0.5,
// 1.0 (skin) and 0.0. Skin matches the shadow-to-lit ratio of an in-game character screen.
var genshinRampShadows = [5][3]float64{
	{0.8, 0.72, 0.84},
	{0.78, 0.71, 0.82},
	{0.76, 0.72, 0.82},
	{0.88, 0.72, 0.7},
	{0.79, 0.73, 0.84},
}

var genshinRampNightTint = [3]float64{0.82, 0.84, 0.97}

// genshinProceduralRamp is the built-in shadow ramp as sRGB RGBA rows. Rows run from the top as an image
// does, so the shader samples every ramp at 1 - v.
func genshinProceduralRamp() []byte {
	data := make([]byte, genshinRampWidth*genshinRampRows*4)
	for row := range genshinRampRows {
		night := row < genshinRampRows/2
		shadow := genshinRampShadows[row%5]
		if night {
			for channel := range 3 {
				shadow[channel] *= genshinRampNightTint[channel]
			}
		}
		at := (genshinRampRows - 1 - row) * genshinRampWidth * 4
		for column := range genshinRampWidth {
			u := float64(column) / (genshinRampWidth - 1)

			// A lighter tone toward the terminator, then a narrow darker and more saturated band right before it,
			// like the game's double-step ramps.
			lighten := smoothstep(0.5, 0.95, u) * 0.2
			band := smoothstep(0.78, 0.9, u) * (1 - smoothstep(0.97, 1, u))
			for channel := range 3 {
				base := shadow[channel] + (1-shadow[channel])*lighten
				deep := shadow[channel] * shadow[channel]
				data[at+column*4+channel] = byte(math.Round((base + (deep-base)*band) * 255))
			}
			data[at+column*4+3] = 255
		}
	}
	return data
}

// genshinFaceRow is the row of a GIMI ramp, given as RGBA8 rows from the top, that a face shades with: row 3,
// which holds skin in most characters' ramps, unless its deepest daytime tone is not a skin tone and another row's
// is, as in Nicole's ramp. The warmest such row wins.
func genshinFaceRow(pix []byte, width, height, stride int) int {
	const skin = 3
	if width <= 0 || height < genshinRampRows || len(pix) < height*stride {
		return skin
	}
	// warmth is how much red exceeds blue in a row's deepest daytime tone, or -1 for a tone too dark or cool for
	// skin. Daytime rows run up from the middle of the image.
	warmth := func(row int) int {
		at := (2*(genshinRampRows/2-1-row) + 1) * height / (2 * genshinRampRows) * stride
		if r, b := int(pix[at]), int(pix[at+2]); r >= 200 && r-b >= 20 {
			return r - b
		}
		return -1
	}
	if warmth(skin) >= 0 {
		return skin
	}
	best := skin
	for row := range genshinRampRows / 2 {
		if warmth(row) > warmth(best) {
			best = row
		}
	}
	return best
}

func smoothstep(edge0, edge1, x float64) float64 {
	t := min(max((x-edge0)/(edge1-edge0), 0), 1)
	return t * t * (3 - 2*t)
}

// viewerToonTextures are the game textures the GIMI style selects, as GPU views; a zero view falls back to
// the renderer's built-in or bundled map.
type viewerToonTextures struct {
	bodyRamp, hairRamp, metalMap, faceLightMap, faceShadow d3d.ShaderResourceView
	// faceRow is the row of bodyRamp a face shades with, when faceRowSet; the built-in ramp holds skin in row 3.
	faceRow    int
	faceRowSet bool
	// ramps holds the views of the character's bundled ramps in the order of character.names, zero until loaded,
	// for the profiles that bind them per mesh; character picks each mesh's.
	ramps     []d3d.ShaderResourceView
	character *characterToonRamps
}

// characterRamp is the loaded bundled ramp the mesh draws with, or a zero view.
func (t viewerToonTextures) characterRamp(mesh *viewerMesh) d3d.ShaderResourceView {
	if t.character == nil {
		return d3d.ShaderResourceView{}
	}
	if i, ok := t.character.index[t.character.rampFor(mesh)]; ok {
		return t.ramps[i]
	}
	return d3d.ShaderResourceView{}
}

// viewerToonAssets holds the listing of the GIMI texture folder and the textures loaded from it or from the
// bundled maps. It belongs to the UI thread.
type viewerToonAssets struct {
	dir     string
	listed  *modelviewer.GenshinToonAssetsTransport
	listing context.CancelFunc
	// textures holds the loaded views by URL and colour space, on one device generation.
	generation uint64
	textures   map[string]d3d.ShaderResourceView
	loading    map[string]bool
	// faceRows holds genshinFaceRow of each loaded texture by the key of textures.
	faceRows map[string]int
}

func (a *viewerToonAssets) release() {
	if a.listing != nil {
		a.listing()
	}
	for _, view := range a.textures {
		view.Release()
	}
	*a = viewerToonAssets{}
}

// toonProfile is the model's material profile when it has a native toon shader, or "".
func (v *ModelViewer) toonProfile() string {
	if v.scene == nil {
		return ""
	}
	if profile := string(v.scene.transport.MaterialProfile); viewerToonProfiles[profile].mode != 0 {
		return profile
	}
	return ""
}

// toonCharacter is the mod's character when its game draws with the model's toon profile, or nil.
func (v *ModelViewer) toonCharacter() *viewerCharacter {
	if c := v.character; c != nil && c.game == viewerToonProfiles[v.toonProfile()].importer {
		return c
	}
	return nil
}

// toonStyle returns the stored style of the model's toon profile, with the character's entry and then the shading
// being changed over it, or the default until the settings load.
func (v *ModelViewer) toonStyle() viewerToonStyle {
	style := defaultToonStyle
	profile := v.toonProfile()
	s := v.shell
	if s != nil && s.settingsLoaded(setting.KeyModelViewerToonStyles) {
		style = parseToonStyle(s.stringSetting(setting.KeyModelViewerToonStyles), profile)
	}
	if c := v.toonCharacter(); c != nil && s != nil && s.settingsLoaded(setting.KeyModelViewerCharacterToonStyles) {
		genshin := c.Genshin
		if profile == gimiProfile {
			if genshin != nil {
				style.characterBody = genshin.Body
			}
			if (genshin != nil && genshin.Avatar != "") || c.ramps != nil {
				style.Ramp = toonRampAuto
			}
		}
		stored := s.stringSetting(setting.KeyModelViewerCharacterToonStyles)
		style = parseCharacterToonStyle(style, stored, c.styleKey(), profile)
		style.characterShading = parseCharacterToonStyle(viewerToonStyle{}, stored, c.styleKey(), profile).Shading
		if style.Ramp == toonRampAuto {
			style.Ramp, style.rampAuto = "", true
			if c.ramps != nil {
				style.Ramp = c.ramps.body
			} else if listed := v.toonAssets.listed; listed != nil && genshin != nil {
				style.Ramp = genshinCharacterRamp(listed.Ramps, genshin.Body, genshin.Avatar)
			}
		}
	}
	if len(v.toonDraft) > 0 {
		style.Shading = withToonShading(style.Shading, v.toonDraft)
		style.characterShading = withToonShading(style.characterShading, v.toonDraft)
	}
	return style
}

// withToonShading returns shading with the values of changes laid over it, leaving shading as it is.
func withToonShading(shading, changes toonShading) toonShading {
	merged := maps.Clone(shading)
	if merged == nil {
		merged = toonShading{}
	}
	maps.Copy(merged, changes)
	return merged
}

// toonDraftDelay is how long a shading value stays unchanged before it is stored, so a slider drag writes the
// setting once.
const toonDraftDelay = 400 * time.Millisecond

// setToonShading shows a changed shading value at once and stores it once it stops changing.
func (v *ModelViewer) setToonShading(key string, value [3]float64) {
	if v.toonDraft == nil {
		v.toonDraft = toonShading{}
	}
	v.toonDraft[key] = value
	v.revision++
	if v.toonDraftTimer != nil {
		v.toonDraftTimer.Stop()
	}
	v.toonDraftTimer = time.AfterFunc(toonDraftDelay, func() { fairy.Go(v.flushToonDraft) })
}

// flushToonDraft stores the shading values being changed.
func (v *ModelViewer) flushToonDraft() {
	if v.toonDraftTimer != nil {
		v.toonDraftTimer.Stop()
		v.toonDraftTimer = nil
	}
	if len(v.toonDraft) == 0 {
		return
	}
	draft := v.toonDraft
	v.toonDraft = nil
	v.updateToonStyle(func(style *viewerToonStyle) { style.Shading = withToonShading(style.Shading, draft) })
}

// resetToonShading drops the shading values of the character's entry, or of the profile's style without a
// character.
func (v *ModelViewer) resetToonShading() {
	if v.toonDraftTimer != nil {
		v.toonDraftTimer.Stop()
		v.toonDraftTimer = nil
	}
	v.toonDraft = nil
	v.updateToonStyle(func(style *viewerToonStyle) { style.Shading = nil })
}

// updateToonStyle stores a change to the shown toon style: in the character's entry when the mod has a character,
// except the light direction and the texture folder, and in the profile's style otherwise. change must replace
// Shading rather than modify it.
func (v *ModelViewer) updateToonStyle(change func(*viewerToonStyle)) {
	profile := v.toonProfile()
	s := v.shell
	if s == nil || profile == "" {
		return
	}
	before := v.toonStyle()
	after := before
	change(&after)

	stored := s.stringSetting(setting.KeyModelViewerToonStyles)
	c := v.toonCharacter()
	if c == nil {
		s.setSetting(setting.KeyModelViewerToonStyles, storeToonStyle(stored, profile, after))
		return
	}
	if after.LightFollowsCamera != before.LightFollowsCamera || after.AssetDir != before.AssetDir {
		global := parseToonStyle(stored, profile)
		global.LightFollowsCamera, global.AssetDir = after.LightFollowsCamera, after.AssetDir
		s.setSetting(setting.KeyModelViewerToonStyles, storeToonStyle(stored, profile, global))
	}
	characterStyles := s.stringSetting(setting.KeyModelViewerCharacterToonStyles)
	if updated := storeCharacterToonStyle(characterStyles, c.styleKey(), profile, before, after); updated !=
		characterStyles {
		s.setSetting(setting.KeyModelViewerCharacterToonStyles, updated)
	}
}

// hasCharacterToonStyle reports whether the mod's character has an entry in the character toon styles.
func (v *ModelViewer) hasCharacterToonStyle() bool {
	c := v.toonCharacter()
	if c == nil || v.shell == nil || !v.shell.settingsLoaded(setting.KeyModelViewerCharacterToonStyles) {
		return false
	}
	var styles map[string]json.RawMessage
	if json.Unmarshal([]byte(v.shell.stringSetting(setting.KeyModelViewerCharacterToonStyles)), &styles) != nil {
		return false
	}
	_, ok := styles[c.styleKey()]
	return ok
}

func (v *ModelViewer) clearCharacterToonStyle() {
	c := v.toonCharacter()
	if c == nil || v.shell == nil {
		return
	}
	if v.toonDraftTimer != nil {
		v.toonDraftTimer.Stop()
		v.toonDraftTimer = nil
	}
	v.toonDraft = nil
	s := v.shell
	s.setSetting(setting.KeyModelViewerCharacterToonStyles,
		removeCharacterToonStyle(s.stringSetting(setting.KeyModelViewerCharacterToonStyles), c.styleKey()))
}

// toonShadows reports the stored Toon Shadows switch, which is on until the setting loads, as its default is.
func (v *ModelViewer) toonShadows() bool {
	if s := v.shell; s != nil && s.settingsLoaded(setting.KeyModelViewerToonShadows) {
		return s.boolSetting(setting.KeyModelViewerToonShadows)
	}
	return true
}

// hasToon reports a model whose material profile has a native toon shader.
func (v *ModelViewer) hasToon() bool { return v.toonProfile() != "" }

func (v *ModelViewer) toonActive() bool { return v.hasToon() && v.toonShadows() }

// syncFieldOfView switches the lens to the toon style's and dollies with it, so the model keeps its size on
// screen.
func (v *ModelViewer) syncFieldOfView() {
	fov := modelViewerFieldOfView
	if v.toonActive() {
		fov = toonFieldOfView
	}
	for _, camera := range []*orbitCamera{&v.camera, &v.home} {
		current := camera.lens()
		if current == fov {
			continue
		}
		scale := math.Tan(current/2) / math.Tan(fov/2)
		camera.distance *= scale
		camera.fov = fov
		if camera == &v.camera {
			v.motion.distance *= scale
		}
	}
}

// syncToonAssets lists the GIMI texture folder the style names and loads the textures it selects and the
// character's bundled ramps.
func (v *ModelViewer) syncToonAssets(style viewerToonStyle) {
	assets := &v.toonAssets
	gimi := v.toonProfile() == gimiProfile
	dir := ""
	if gimi {
		dir = style.AssetDir
	}
	if dir != assets.dir {
		if assets.listing != nil {
			assets.listing()
			assets.listing = nil
		}
		assets.dir, assets.listed = dir, nil
		if dir != "" && v.tools != nil {
			v.listToonAssets(dir)
		}
	}
	requests := v.characterRampRequests()
	if gimi {
		fixed := v.toonTextureRequests(style)
		if dir != "" && assets.listed == nil && assets.listing != nil {
			// Until the folder is listed only the character's bundled ramps are certain; its face maps replace the
			// bundled ones.
			fixed = [5]toonTextureRequest{fixed[0], fixed[1]}
		}
		requests = append(requests, fixed[:]...)
	}
	if len(requests) == 0 {
		return
	}
	generation := fairy.DeviceGeneration()
	if assets.generation != generation {
		for _, view := range assets.textures {
			view.Release()
		}
		assets.generation, assets.textures, assets.loading = generation, map[string]d3d.ShaderResourceView{}, nil
		assets.faceRows = map[string]int{}
	}
	for _, request := range requests {
		key := request.key()
		if request.url == "" || assets.loading[key] || assets.textures[key].P != nil {
			continue
		}
		if assets.loading == nil {
			assets.loading = map[string]bool{}
		}
		assets.loading[key] = true
		v.loadToonTexture(request, generation)
	}
}

func (v *ModelViewer) listToonAssets(dir string) {
	ctx, cancel := context.WithCancel(v.ctx)
	v.toonAssets.listing = cancel
	toasts := v.toasts
	go func() {
		listed, err := v.tools.ListGenshinToonAssets(ctx, dir)
		fairy.Go(func() {
			if ctx.Err() != nil || v.toonAssets.dir != dir {
				return
			}
			v.toonAssets.listing = nil
			cancel()
			if err != nil {
				toasts.Show(kit.ToastError, T("page.tools.model_viewer.menu.gimi_toon_assets_failed"), "")
				return
			}
			v.toonAssets.listed = &listed
			v.revision++
		})
	}()
}

type toonTextureRequest struct {
	url  string
	srgb bool
}

func (r toonTextureRequest) key() string {
	if r.srgb {
		return "srgb:" + r.url
	}
	return "linear:" + r.url
}

// toonTextureRequests lists the body ramp, hair ramp, metal map, face light map and face shadow the style
// selects from the character's bundled ramps and the listing, in viewerToonTextures order. The face maps are requested only for a mod with a face,
// and the face light map falls back to the bundled one; the other empty requests fall back to the renderer's maps.
func (v *ModelViewer) toonTextureRequests(style viewerToonStyle) [5]toonTextureRequest {
	var requests [5]toonTextureRequest
	face := v.scene != nil && v.scene.genshinFace
	faceType := genshinFaceType(style)
	if face {
		requests[3] = toonTextureRequest{url: bundledFaceLightmapURL(faceType)}
	}
	c := v.toonCharacter()
	bundled := style.rampAuto && c != nil && c.ramps != nil && c.ramps.body != ""
	if bundled {
		requests[0] = toonTextureRequest{url: c.ramps.url(c.ramps.body), srgb: true}
		requests[1] = toonTextureRequest{url: c.ramps.url(cmp.Or(c.ramps.hair, c.ramps.body)), srgb: true}
	}
	listed := v.toonAssets.listed
	if listed == nil {
		return requests
	}
	for _, ramp := range listed.Ramps {
		if !bundled && ramp.Name == style.Ramp {
			requests[0] = toonTextureRequest{url: ramp.BodyURL, srgb: true}
			requests[1] = toonTextureRequest{url: ramp.HairURL, srgb: true}
			break
		}
	}
	requests[2] = toonTextureRequest{url: listed.MetalMapURL, srgb: true}
	if !face {
		return requests
	}
	if url := listed.FaceLightmaps[faceType]; url != "" {
		requests[3] = toonTextureRequest{url: url}
	}
	requests[4] = toonTextureRequest{url: listed.FaceShadowURL}
	return requests
}

// characterRampRequests lists the character's bundled ramps for the profiles that bind them per mesh; GIMI takes
// its two through toonTextureRequests.
func (v *ModelViewer) characterRampRequests() []toonTextureRequest {
	c := v.toonCharacter()
	if c == nil || c.ramps == nil || v.toonProfile() == gimiProfile {
		return nil
	}
	return c.ramps.requests
}

func (v *ModelViewer) toonTextures(style viewerToonStyle) viewerToonTextures {
	assets := &v.toonAssets
	if assets.generation != fairy.DeviceGeneration() {
		return viewerToonTextures{}
	}
	var textures viewerToonTextures
	if v.toonProfile() == gimiProfile {
		var views [5]d3d.ShaderResourceView
		for i, request := range v.toonTextureRequests(style) {
			if request.url != "" {
				views[i] = assets.textures[request.key()]
			}
		}
		textures = viewerToonTextures{
			bodyRamp: views[0], hairRamp: views[1], metalMap: views[2], faceLightMap: views[3], faceShadow: views[4],
		}
		if views[0].P != nil {
			textures.faceRow, textures.faceRowSet = assets.faceRows[v.toonTextureRequests(style)[0].key()]
		}
		// The face maps of an original face belong to that face.
		if o := v.scene.original; o != nil && o.generation == assets.generation {
			if texture, ok := o.textures[o.faceLight]; ok {
				textures.faceLightMap = texture.view
			}
			if texture, ok := o.textures[o.faceShadow]; ok {
				textures.faceShadow = texture.view
			}
		}
	}
	if c := v.toonCharacter(); c != nil && c.ramps != nil && v.toonProfile() != gimiProfile {
		textures.character, textures.ramps = c.ramps, make([]d3d.ShaderResourceView, len(c.ramps.keys))
		for i, key := range c.ramps.keys {
			textures.ramps[i] = assets.textures[key]
		}
	}
	return textures
}

// loadToonTexture reads and uploads one game texture from the texture folder or the bundled maps. A texture
// that fails is logged and stays unloaded, so the built-in map shows in its place.
func (v *ModelViewer) loadToonTexture(request toonTextureRequest, generation uint64) {
	ctx, key := v.ctx, request.key()
	go func() {
		var view d3d.ShaderResourceView
		var data []byte
		var err error
		if strings.HasPrefix(request.url, bundledToonScheme) {
			data, err = readBundledToonMap(request.url)
		} else {
			data, err = v.read(ctx, request.url)
		}
		var texture *viewerTextureData
		faceRow := 0
		if err == nil {
			texture, err = decodeViewerImage(data)
		}
		if err == nil {
			faceRow = genshinFaceRow(texture.mips[0], int(texture.width), int(texture.height), int(texture.pitches[0]))
			texture.generateMips = false
			if request.srgb {
				texture.format = d3d.FORMAT_R8G8B8A8_UNORM_SRGB
			}
			err = fairy.UseDevice(func(g fairy.GPU) error {
				if g.Generation != generation {
					return nil
				}
				var uploadErr error
				view, uploadErr = texture.upload(g.Device, g.Context)
				return uploadErr
			})
		}
		if err != nil && ctx.Err() == nil {
			_ = infra.ReportError(v.log, err, "ModelViewer.loadToonTexture", infra.Diagnostic{
				Operation: "load-toon-texture", Fields: map[string]any{"url": request.url},
			})
		}
		fairy.Go(func() {
			assets := &v.toonAssets
			if v.closed || assets.generation != generation || assets.textures == nil {
				view.Release()
				return
			}
			if view.P != nil {
				assets.textures[key] = view
				if assets.faceRows == nil {
					assets.faceRows = map[string]int{}
				}
				assets.faceRows[key] = faceRow
				delete(assets.loading, key)
				v.revision++
			}
		})
	}()
}

// pickToonAssetDir asks for the folder of extracted Genshin textures.
func (v *ModelViewer) pickToonAssetDir(style viewerToonStyle) {
	s := v.shell
	if s == nil || s.deps.Dialog == nil {
		return
	}
	go func() {
		result, err := s.deps.Dialog.ShowOpenDialog(platform.OpenDialogOptions{
			DefaultPath: style.AssetDir,
			Properties:  []string{"openDirectory"},
			Role:        "model-viewer-genshin-textures",
		})
		fairy.Go(func() {
			if err != nil {
				_ = infra.ReportError(v.log, err, "ModelViewer.pickToonAssetDir", infra.Diagnostic{
					Operation: "pick-toon-asset-dir",
				})
				return
			}
			if result.Canceled || len(result.FilePaths) == 0 {
				return
			}
			v.updateToonStyle(func(style *viewerToonStyle) { style.AssetDir = result.FilePaths[0] })
		})
	}()
}
