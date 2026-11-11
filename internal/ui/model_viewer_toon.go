package ui

import (
	"context"
	"encoding/json"
	"math"
	"regexp"
	"slices"
	"strings"

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
}

var viewerToonProfiles = map[string]viewerToonProfile{
	gimiProfile:     {mode: toonModeGIMI, label: "gimi_toon_style", styleKey: gimiProfile},
	srmiProfile:     {mode: toonModeSRMI, label: "srmi_toon_style", styleKey: srmiProfile},
	zzmiProfile:     {mode: toonModeZZMI, label: "zzmi_toon_style", styleKey: zzmiProfile},
	wwmiProfile:     {mode: toonModeWWMI, label: "wwmi_toon_style", styleKey: wwmiProfile},
	rabbitFXProfile: {mode: toonModeWWMI, label: "wwmi_toon_style", styleKey: wwmiProfile},
	efmiProfile:     {mode: toonModeEFMI, label: "toon_style", styleKey: efmiProfile},
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
// the fields after FlatFace, and only EFMI FlatFace.
type viewerToonStyle struct {
	toonFlags
	// FlatFace lights EFMI's head meshes from the face mask instead of N.L.
	FlatFace bool   `json:"flatFace"`
	Ramp     string `json:"ramp"`
	// FaceType is "auto", which takes the body type from the ramp name, or one of genshinFaceTypes.
	FaceType string `json:"faceType"`
	AssetDir string `json:"assetDir"`
}

// efmiToonStyle is what the EFMI menu stores.
type efmiToonStyle struct {
	toonFlags
	FlatFace bool `json:"flatFace"`
}

var defaultToonStyle = viewerToonStyle{
	toonFlags: toonFlags{Outlines: true, SelfShadow: true, LightFollowsCamera: true},
	FlatFace:  true,
	FaceType:  "auto",
}

// The shading values the GIMI menu does not show: HoyoToon's defaults, except the light area, with which
// frontal light would shade limbs the game keeps lit; shadow brightness and specular strength come from
// kaze-mio's Nahida material.
const (
	gimiLightArea        = 0.5
	gimiShadowBrightness = 1.1
	gimiBrightness       = 1
	gimiSpecular         = 0.3
	gimiRimStrength      = 1
	gimiEmission         = 0.2
	gimiOutlineWidth     = 0.015
)

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
	style := defaultToonStyle
	var styles map[string]map[string]any
	if json.Unmarshal([]byte(stored), &styles) != nil {
		return style
	}
	profile = toonStyleKey(profile)
	record := styles[profile]
	flag := func(key string, target *bool) {
		if value, ok := record[key].(bool); ok {
			*target = value
		}
	}
	flag("outlines", &style.Outlines)
	flag("selfShadow", &style.SelfShadow)
	flag("lightFollowsCamera", &style.LightFollowsCamera)
	if profile == efmiProfile {
		flag("flatFace", &style.FlatFace)
	}
	if profile != gimiProfile {
		return style
	}
	if value, ok := record["ramp"].(string); ok {
		style.Ramp = value
	}
	if value, ok := record["faceType"].(string); ok && (value == "auto" || slices.Contains(genshinFaceTypes, value)) {
		style.FaceType = value
	}
	if value, ok := record["assetDir"].(string); ok {
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
	var entry []byte
	switch profile {
	case gimiProfile:
		entry, _ = json.Marshal(struct {
			toonFlags
			Ramp     string `json:"ramp"`
			FaceType string `json:"faceType"`
			AssetDir string `json:"assetDir"`
		}{style.toonFlags, style.Ramp, style.FaceType, style.AssetDir})
	case efmiProfile:
		entry, _ = json.Marshal(efmiToonStyle{style.toonFlags, style.FlatFace})
	default:
		entry, _ = json.Marshal(style.toonFlags)
	}
	styles[profile] = entry
	encoded, _ := json.Marshal(styles)
	return string(encoded)
}

var genshinBodyTypeRE = regexp.MustCompile(`(?i)^avatar_([a-z]+)_`)

// genshinFaceType is the face light map the style selects, or "" for none.
func genshinFaceType(style viewerToonStyle) string {
	if style.FaceType != "auto" {
		return style.FaceType
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
	}
}

// A Star Rail part name ends its override section, optionally followed by one submesh letter or digit, so
// accessories such as Headwear or HairClip do not match. Star Rail calls the face Head.
var (
	starRailHairRE = regexp.MustCompile(`(?i)hair[a-z\d]?$`)
	starRailFaceRE = regexp.MustCompile(`(?i)(?:face|head)[a-z\d]?$`)
)

func markStarRailMeshes(scene *viewerScene) {
	for _, mesh := range scene.meshes {
		mesh.toonHair = starRailHairRE.MatchString(mesh.info.Component)
		mesh.toonFace = !mesh.toonHair && starRailFaceRE.MatchString(mesh.info.Component)
	}
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
	present := func(key *string) bool { return key != nil && *key != "" }

	var narrow, bound, sized []*viewerMesh
	for _, mesh := range entries {
		b := mesh.info.Bounds
		if max(b.Max[0]-b.Min[0], b.Max[1]-b.Min[1]) > efmiHeadMaxWidth {
			continue
		}
		narrow = append(narrow, mesh)
		if present(mesh.info.LightMapKey) || len(mesh.info.LightMapVariants) > 0 {
			continue
		}
		if (present(mesh.info.MaterialMapKey) || present(mesh.info.HighlightMapKey)) &&
			heightOf(b.Min[2]) >= efmiBoundFaceMinHeight {
			bound = append(bound, mesh)
		}
		if indices := len(mesh.indices); indices >= efmiFaceMinIndices && indices <= efmiFaceMaxIndices &&
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
		}
		headFloor -= efmiHeadBelowFace
	}

	for _, mesh := range narrow {
		b := mesh.info.Bounds
		if b.Min[2] < headFloor {
			continue
		}
		height, indices := b.Max[2]-b.Min[2], len(mesh.indices)
		mesh.efmiHead = true
		mesh.overlayShadow = !present(mesh.info.TexKey) && len(mesh.info.TextureVariants) == 0 &&
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
	genshinRampWidth    = 256
	genshinRampRows     = 10
	genshinMetalMapSize = 64
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

// genshinProceduralMetalMap is a grey matcap with a bright horizontal band above the centre, the shape of the
// game's shared metal map, as sRGB RGBA rows from the top.
func genshinProceduralMetalMap() []byte {
	data := make([]byte, genshinMetalMapSize*genshinMetalMapSize*4)
	for y := range genshinMetalMapSize {
		v := float64(y) / (genshinMetalMapSize - 1)
		at := (genshinMetalMapSize - 1 - y) * genshinMetalMapSize * 4
		for x := range genshinMetalMapSize {
			u := float64(x)/(genshinMetalMapSize-1) - 0.5
			offset := (v - 0.65) / 0.1
			band := math.Exp(-offset*offset) * (1 - u*u*2)
			value := byte(math.Round((0.15 + 0.6*band) * 255))
			copy(data[at+x*4:], []byte{value, value, value, 255})
		}
	}
	return data
}

func smoothstep(edge0, edge1, x float64) float64 {
	t := min(max((x-edge0)/(edge1-edge0), 0), 1)
	return t * t * (3 - 2*t)
}

// viewerToonTextures are the game textures the GIMI style selects, as GPU views; a zero view falls back to
// the built-in map.
type viewerToonTextures struct {
	bodyRamp, hairRamp, metalMap, faceLightMap, faceShadow d3d.ShaderResourceView
}

// viewerToonAssets holds the listing of the GIMI texture folder and the textures loaded from it. It belongs
// to the UI thread.
type viewerToonAssets struct {
	dir     string
	listed  *modelviewer.GenshinToonAssetsTransport
	listing context.CancelFunc
	// textures holds the loaded views by URL and colour space, on one device generation.
	generation uint64
	textures   map[string]d3d.ShaderResourceView
	loading    map[string]bool
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

// toonStyle returns the stored style of the model's toon profile, or the default until the setting loads.
func (v *ModelViewer) toonStyle() viewerToonStyle {
	if s := v.shell; s != nil && s.settingsLoaded(setting.KeyModelViewerToonStyles) {
		return parseToonStyle(s.stringSetting(setting.KeyModelViewerToonStyles), v.toonProfile())
	}
	return defaultToonStyle
}

func (v *ModelViewer) setToonStyle(style viewerToonStyle) {
	profile := v.toonProfile()
	if s := v.shell; s != nil && profile != "" {
		s.setSetting(setting.KeyModelViewerToonStyles,
			storeToonStyle(s.stringSetting(setting.KeyModelViewerToonStyles), profile, style))
	}
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

// syncToonAssets lists the GIMI texture folder the style names and loads the textures it selects.
func (v *ModelViewer) syncToonAssets(style viewerToonStyle) {
	assets := &v.toonAssets
	dir := ""
	if v.toonProfile() == gimiProfile {
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
	if assets.listed == nil {
		return
	}
	generation := fairy.DeviceGeneration()
	if assets.generation != generation {
		for _, view := range assets.textures {
			view.Release()
		}
		assets.generation, assets.textures, assets.loading = generation, map[string]d3d.ShaderResourceView{}, nil
	}
	for _, request := range v.toonTextureRequests(style) {
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
// selects from the listing, in viewerToonTextures order.
func (v *ModelViewer) toonTextureRequests(style viewerToonStyle) [5]toonTextureRequest {
	var requests [5]toonTextureRequest
	listed := v.toonAssets.listed
	if listed == nil {
		return requests
	}
	for _, ramp := range listed.Ramps {
		if ramp.Name == style.Ramp {
			requests[0] = toonTextureRequest{url: ramp.BodyURL, srgb: true}
			requests[1] = toonTextureRequest{url: ramp.HairURL, srgb: true}
			break
		}
	}
	requests[2] = toonTextureRequest{url: listed.MetalMapURL}
	if faceType := genshinFaceType(style); faceType != "" {
		requests[3] = toonTextureRequest{url: listed.FaceLightmaps[faceType]}
	}
	requests[4] = toonTextureRequest{url: listed.FaceShadowURL}
	return requests
}

func (v *ModelViewer) toonTextures(style viewerToonStyle) viewerToonTextures {
	assets := &v.toonAssets
	if assets.generation != fairy.DeviceGeneration() {
		return viewerToonTextures{}
	}
	var views [5]d3d.ShaderResourceView
	for i, request := range v.toonTextureRequests(style) {
		if request.url != "" {
			views[i] = assets.textures[request.key()]
		}
	}
	return viewerToonTextures{
		bodyRamp: views[0], hairRamp: views[1], metalMap: views[2], faceLightMap: views[3], faceShadow: views[4],
	}
}

// loadToonTexture reads and uploads one game texture. A texture that fails is logged and stays unloaded, so
// the built-in map shows in its place.
func (v *ModelViewer) loadToonTexture(request toonTextureRequest, generation uint64) {
	ctx, key := v.ctx, request.key()
	go func() {
		var view d3d.ShaderResourceView
		data, err := v.read(ctx, request.url)
		var texture *viewerTextureData
		if err == nil {
			texture, err = decodeViewerImage(data)
		}
		if err == nil {
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
			latest := v.toonStyle()
			latest.AssetDir = result.FilePaths[0]
			v.setToonStyle(latest)
		})
	}()
}
