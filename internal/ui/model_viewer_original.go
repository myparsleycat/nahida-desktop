package ui

import (
	"context"
	"fmt"
	"image"
	"maps"
	"slices"
	"sync"
	"unsafe"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/d3d"
	"github.com/myparsleycat/fairy/kit"
	"golang.org/x/sync/errgroup"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/setting"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

// Texture slots a mesh can take from the original model, in viewerTextureFill order.
const (
	fillDiffuse = iota
	fillNormal
	fillLight
	fillMaterial
	fillSlots
)

var fillRoles = [fillSlots]string{
	modelviewer.OriginalTextureDiffuse, modelviewer.OriginalTextureNormal,
	modelviewer.OriginalTextureLight, modelviewer.OriginalTextureMaterial,
}

// viewerTextureFill holds the keys of the original textures a mesh takes in the slots its mod leaves empty.
type viewerTextureFill [fillSlots]string

// viewerOriginal is the character's original model shown with a mod: the face parts the mod leaves to the game,
// the parts the mod lacks that the user turns on, and the original textures behind the slots the mod leaves
// empty. It belongs to the UI thread; its GPU resources belong to the device generation it was made on.
type viewerOriginal struct {
	scene      *viewerScene
	character  string
	data       *modelviewer.CharacterOriginal
	parts      []*viewerOriginalPart
	fills      map[int]viewerTextureFill
	textures   map[string]viewerTextureGPU
	generation uint64
	// faceLight and faceShadow are the keys of the face maps of a shown face part, or "".
	faceLight, faceShadow string
	// skinFor is the motion skin holds the parts' bone weights for, and anchors the node turning each part's face
	// direction.
	skinFor *viewerMotion
	skin    []viewerSkinBlend
	anchors []int32
	// morphs marks a part with shape keys, and morph holds the weights a motion gives them.
	morphs        bool
	morph         viewerOriginalMorph
	presetScratch []float32
	// ctx bounds the texture loads of parts shown later; release cancels it. inflight holds the keys those loads
	// are fetching.
	ctx         context.Context
	cancel      context.CancelFunc
	reconstruct bool
	inflight    map[string]bool
}

// viewerOriginalPart is a part of the original model that the viewer may draw.
type viewerOriginalPart struct {
	index int
	// head marks a face part the mod does not replace, shown by default; the others are parts the mod lacks.
	head  bool
	shown bool
	label string
	mesh  *viewerMesh
	gpu   viewerMeshGPU
	// weights are the shape key weights the mesh's geometry has, once applied, and want those it should have.
	// targetIndex finds a shape key by name, blinkKeys are those that close the eyes, and morphKeys maps the keys
	// of the motion's morph to the part's, or -1.
	weights, want []float32
	applied       bool
	targetIndex   map[string]int
	blinkKeys     []int
	morph         int
	morphKeys     []int
	// pending are the textures this part uses that are not loaded yet; it loads them when it shows.
	pending map[string]originalTextureRequest
}

func (o *viewerOriginal) release() {
	if o == nil {
		return
	}
	if o.cancel != nil {
		o.cancel()
	}
	for _, part := range o.parts {
		part.gpu.release()
	}
	for _, texture := range o.textures {
		texture.view.Release()
	}
	o.releaseSkin()
	o.parts, o.textures = nil, nil
}

func (o *viewerOriginal) releaseSkin() {
	for i := range o.skin {
		o.skin[i].view.Release()
		o.skin[i].buffer.Release()
	}
	o.skin, o.anchors, o.skinFor = nil, nil, nil
}

// originalDataEnabled reports whether the viewer fills mods with their character's original model.
func (v *ModelViewer) originalDataEnabled() bool {
	if s := v.shell; s != nil && s.settingsLoaded(setting.KeyModelViewerOriginalData) {
		return s.boolSetting(setting.KeyModelViewerOriginalData)
	}
	return true
}

// originalDataAvailable reports whether the mod's character has a hosted original model. A tool edits and picks
// the mod's own geometry, so a viewer with one shows none.
func (v *ModelViewer) originalDataAvailable() bool {
	c := v.character
	return c != nil && v.tools != nil && v.tool == nil && modelviewer.HasCharacterOriginal(c.game, c.ID)
}

// syncOriginal loads the original model for the scene and character shown, or drops one that no longer matches
// them. Call it on each build.
func (v *ModelViewer) syncOriginal() {
	scene := v.scene
	if scene == nil {
		v.cancelOriginal()
		return
	}
	want := ""
	if v.originalDataEnabled() && v.originalDataAvailable() {
		want = v.character.styleKey()
	}
	if o := scene.original; o != nil && (o.character != want || o.generation != fairy.DeviceGeneration()) {
		v.dropOriginal(scene)
	}
	if want == "" {
		v.cancelOriginal()
		return
	}
	key := viewerOriginalKey{scene: scene, character: want}
	if scene.original != nil || v.originalLoading == key || v.originalFailed == key {
		return
	}
	v.loadOriginal(key, v.character)
}

// viewerOriginalKey names the original model of a character for a scene.
type viewerOriginalKey struct {
	scene     *viewerScene
	character string
}

func (v *ModelViewer) cancelOriginal() {
	if v.originalCancel != nil {
		v.originalCancel()
		v.originalCancel = nil
		v.originalLoading = viewerOriginalKey{}
		v.revision++
	}
}

// dropOriginal frees the scene's original model.
func (v *ModelViewer) dropOriginal(scene *viewerScene) {
	o := scene.original
	if o == nil {
		return
	}
	scene.original = nil
	if string(scene.transport.MaterialProfile) == gimiProfile {
		scene.genshinFace = slices.ContainsFunc(scene.meshes, func(m *viewerMesh) bool { return m.toonFace })
	}
	_ = fairy.UseDevice(func(fairy.GPU) error {
		o.release()
		return nil
	})
	v.revision++
}

// viewerOriginalSource is what the original model's match reads of a scene mesh: its base positions and UVs when
// loaded, else its transport to read them from, and which texture slots its mod leaves empty.
type viewerOriginalSource struct {
	info      modelviewer.ModelViewerMeshTransport
	averaged  bool
	positions []float32
	uvs       []float32
	empty     [fillSlots]bool
}

func (v *ModelViewer) loadOriginal(key viewerOriginalKey, c *viewerCharacter) {
	scene := key.scene
	ctx, cancel := context.WithCancel(v.ctx)
	v.cancelOriginal()
	v.originalCancel, v.originalLoading = cancel, key
	v.revision++

	sources := make([]viewerOriginalSource, len(scene.meshes))
	for i, mesh := range scene.meshes {
		info := mesh.info
		sources[i] = viewerOriginalSource{info: info, averaged: mesh.averagedOutline}
		if !mesh.unloaded && !mesh.cpuDropped && mesh.positions != nil {
			sources[i].positions, sources[i].uvs = mesh.positions, mesh.uvs
		}
		for slot, own := range []*string{info.TexKey, info.NormalMapKey, info.LightMapKey, info.MaterialMapKey} {
			sources[i].empty[slot] = own == nil || *own == ""
		}
	}
	input := viewerOriginalInput{
		sources:     sources,
		faces:       map[string]modelviewer.ModelViewerTextureTransport{},
		profile:     string(scene.transport.MaterialProfile),
		reconstruct: scene.transport.NormalXYReconstruct,
	}
	for key := range viewerFaceOnlyTextures(&scene.transport) {
		if entry, ok := scene.transport.Textures[key]; ok {
			input.faces[key] = entry
		}
	}
	tools, read, log := v.tools, v.read, v.log
	go func() {
		o, missing, err := buildViewerOriginal(ctx, tools, read, log, input, c)
		fairy.Go(func() {
			current := v.originalLoading == key && ctx.Err() == nil && v.scene == scene && !v.closed
			if !current || err != nil {
				if o != nil {
					_ = fairy.UseDevice(func(fairy.GPU) error {
						o.release()
						return nil
					})
				}
				if current {
					v.originalCancel, v.originalLoading, v.originalFailed = nil, viewerOriginalKey{}, key
					cancel()
					v.reportOriginalError(err, c)
					v.revision++
				}
				return
			}
			v.originalCancel, v.originalLoading = nil, viewerOriginalKey{}
			cancel()
			o.ctx, o.cancel = context.WithCancel(v.ctx)
			o.scene = scene
			scene.original = o
			v.applyOriginalMarks(scene)
			if missing > 0 {
				v.showOriginalTexturesMissing()
			}
			v.revision++
		})
	}()
}

func (v *ModelViewer) reportOriginalError(err error, c *viewerCharacter) {
	if err == nil || infra.IsCancellationError(err) {
		return
	}
	err = infra.ReportError(v.log, err, "ModelViewer.characterOriginal", infra.Diagnostic{
		Operation: "load-character-original",
		Fields:    map[string]any{"modPath": v.path, "game": c.game, "character": c.ID},
	})
	v.toasts.Show(kit.ToastError, T("page.tools.model_viewer.toast.original_error"), err.Error())
}

// applyOriginalMarks lets the toon request the face maps of a Genshin face the original model adds.
func (v *ModelViewer) applyOriginalMarks(scene *viewerScene) {
	o := scene.original
	if o == nil || string(scene.transport.MaterialProfile) != gimiProfile {
		return
	}
	for _, part := range o.parts {
		if part.shown && part.mesh.toonFace {
			scene.genshinFace = true
		}
	}
}

type viewerOriginalTools interface {
	LoadCharacterOriginal(context.Context, string, string) (*modelviewer.CharacterOriginal, error)
	LoadCharacterOriginalTexture(context.Context, modelviewer.CharacterOriginalTexture) (*image.NRGBA, error)
}

// viewerOriginalInput is what building the original model reads of the scene: its meshes, the face textures only the
// original model's face shows, the material profile, and whether normal maps keep only X and Y.
type viewerOriginalInput struct {
	sources     []viewerOriginalSource
	faces       map[string]modelviewer.ModelViewerTextureTransport
	profile     string
	reconstruct bool
}

// buildViewerOriginal loads the character's original model, matches it against the mod's meshes, and uploads the
// parts the viewer may draw with the textures they and the mod's empty slots take, and the mod's face textures.
func buildViewerOriginal(
	ctx context.Context,
	tools viewerOriginalTools,
	read viewerReader,
	log *infra.Log,
	input viewerOriginalInput,
	c *viewerCharacter,
) (*viewerOriginal, int, error) {
	sources, profile, reconstruct := input.sources, input.profile, input.reconstruct
	data, err := tools.LoadCharacterOriginal(ctx, c.game, c.ID)
	if err != nil {
		return nil, 0, err
	}
	meshes := make([]modelviewer.OriginalMatchMesh, len(sources))
	for i, source := range sources {
		positions, uvs := source.positions, source.uvs
		if positions == nil && source.info.GeometryURL != "" {
			decoded, err := readViewerMesh(ctx, read, source.info, source.averaged)
			if err != nil {
				return nil, 0, fmt.Errorf("mesh %s: %w", source.info.ID, err)
			}
			positions, uvs = decoded.positions, decoded.uvs
		}
		meshes[i].Positions = positions
		meshes[i].UVs = make([]float32, len(uvs)/2)
		for v := range len(uvs) / 4 {
			meshes[i].UVs[v*2], meshes[i].UVs[v*2+1] = uvs[v*4], uvs[v*4+1]
		}
	}
	match, err := modelviewer.MatchCharacterOriginal(ctx, data, meshes)
	if err != nil {
		return nil, 0, err
	}

	o := &viewerOriginal{
		character: c.styleKey(), data: data, fills: map[int]viewerTextureFill{},
		textures: map[string]viewerTextureGPU{},
	}
	fail := func(err error) (*viewerOriginal, int, error) {
		_ = fairy.UseDevice(func(fairy.GPU) error {
			o.release()
			return nil
		})
		return nil, 0, err
	}
	// requests are the textures loaded now; a hidden part keeps those only it uses in its own pending requests.
	requests := map[string]originalTextureRequest{}
	request := func(into map[string]originalTextureRequest, part, role int) string {
		texture, ok := data.Texture(part, fillRoles[role])
		if !ok {
			return ""
		}
		key := originalTextureKey(texture)
		if _, requested := requests[key]; requested {
			into = requests
		}
		addTextureRequest(into, key, originalTextureRequest{
			texture: texture,
			srgb:    role == fillDiffuse,
			normal:  role == fillNormal,
		})
		return key
	}
	requestFace := func(texture modelviewer.CharacterOriginalTexture) string {
		key := originalTextureKey(texture)
		addTextureRequest(requests, key, originalTextureRequest{texture: texture})
		return key
	}
	for i, part := range match.Sources {
		if part < 0 {
			continue
		}
		var fill viewerTextureFill
		for slot := range fillSlots {
			if sources[i].empty[slot] {
				fill[slot] = request(requests, part, slot)
			}
		}
		if fill != (viewerTextureFill{}) {
			o.fills[i] = fill
		}
	}

	add := func(index int, head bool) {
		part := data.Parts[index]
		mesh := originalViewerMesh(part, data.Game)
		into := requests
		var pending map[string]originalTextureRequest
		if !head {
			pending = map[string]originalTextureRequest{}
			into = pending
		}
		for slot := range fillSlots {
			if key := request(into, index, slot); key != "" {
				*stateKey(&mesh.state, slot) = key
			}
		}
		if part.Role == modelviewer.OriginalRoleFace && head {
			if texture, ok := data.Texture(index, modelviewer.OriginalTextureFaceLight); ok {
				o.faceLight = requestFace(texture)
			}
			if texture, ok := data.Texture(index, modelviewer.OriginalTextureFaceShadow); ok {
				o.faceShadow = requestFace(texture)
			}
		}
		if len(pending) == 0 {
			pending = nil
		}
		o.parts = append(o.parts, &viewerOriginalPart{
			index: index, head: head, shown: head, label: originalPartLabel(part), mesh: mesh, pending: pending,
		})
	}
	for _, index := range match.HeadParts {
		add(index, true)
	}
	for _, index := range match.AbsentParts {
		add(index, false)
	}
	// Absent parts load their textures apart, so each pending request carries the flags of every part sharing it.
	shared := map[string]originalTextureRequest{}
	for _, part := range o.parts {
		for key, req := range part.pending {
			addTextureRequest(shared, key, req)
		}
	}
	for _, part := range o.parts {
		for key := range part.pending {
			part.pending[key] = shared[key]
		}
	}
	shadeWithFace(o.parts, data)
	markOriginalMeshes(o.parts, profile)
	o.prepareMorphs()

	generations := map[uint64]bool{}
	for _, part := range o.parts {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		buffers, generation, err := uploadViewerMesh(part.mesh.upload())
		if err != nil {
			return fail(fmt.Errorf("upload original part %s: %w", part.label, err))
		}
		part.gpu, generations[generation] = buffers, true
	}

	var textures, faceTextures map[string]viewerTextureGPU
	var textureGenerations, faceGenerations map[uint64]bool
	var failed map[string]originalTextureRequest
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		var err error
		textures, textureGenerations, failed, err = loadOriginalTextures(groupCtx, tools, log, requests, reconstruct)
		return err
	})
	group.Go(func() error {
		var err error
		faceTextures, faceGenerations, err = uploadViewerTextures(groupCtx, read, log, input.faces, reconstruct)
		return err
	})
	if err := group.Wait(); err != nil {
		// Each load releases its own uploads when it fails, so only the one that finished holds any.
		_ = fairy.UseDevice(func(fairy.GPU) error {
			for _, texture := range textures {
				texture.view.Release()
			}
			for _, texture := range faceTextures {
				texture.view.Release()
			}
			return nil
		})
		return fail(err)
	}
	o.textures, o.reconstruct = textures, reconstruct
	maps.Copy(o.textures, faceTextures)
	maps.Copy(generations, textureGenerations)
	maps.Copy(generations, faceGenerations)
	o.generation = fairy.DeviceGeneration()
	if len(generations) == 1 {
		for generation := range generations {
			o.generation = generation
		}
	} else if len(generations) > 1 {
		o.generation = 0
	}
	return o, len(failed), nil
}

// viewerFaceOnlyTextures lists the face textures of transport that no mesh draws with. Only the original model's face
// shows them, so they load with it.
func viewerFaceOnlyTextures(transport *modelviewer.ModelViewerTransport) map[string]bool {
	faces := map[string]bool{}
	for _, face := range transport.FaceTextures {
		if face.TexKey != nil {
			faces[*face.TexKey] = true
		}
		for _, variant := range face.Variants {
			faces[variant.TexKey] = true
		}
	}
	if len(faces) == 0 {
		return nil
	}
	for _, mesh := range transport.Meshes {
		for _, key := range []*string{
			mesh.TexKey, mesh.NormalMapKey, mesh.LightMapKey, mesh.MaterialMapKey, mesh.CutoutMapKey,
			mesh.HighlightMapKey, mesh.RampMapKey,
		} {
			if key != nil {
				delete(faces, *key)
			}
		}
		for _, variants := range [][]modelviewer.ModelViewerTextureVariant{
			mesh.TextureVariants, mesh.NormalMapVariants, mesh.LightMapVariants, mesh.MaterialMapVariants,
			mesh.CutoutMapVariants,
		} {
			for _, variant := range variants {
				delete(faces, variant.TexKey)
			}
		}
	}
	return faces
}

// addTextureRequest adds req to into, or widens the request already under key: a texture several slots share takes
// the flags each slot needs, and each flag acts only in its own slot.
func addTextureRequest(into map[string]originalTextureRequest, key string, req originalTextureRequest) {
	if have, ok := into[key]; ok {
		have.srgb, have.normal = have.srgb || req.srgb, have.normal || req.normal
		into[key] = have
		return
	}
	into[key] = req
}

// originalDecodeJobs bounds the original textures decoded at once; each holds one or two full-size images.
const originalDecodeJobs = 2

// loadOriginalTextures loads and uploads the requested textures. Those that cannot be loaded are left out, logged
// together and returned as failed; the load fails only when cancelled or the device cannot be used, and then
// releases what it uploaded.
func loadOriginalTextures(
	ctx context.Context,
	tools viewerOriginalTools,
	log *infra.Log,
	requests map[string]originalTextureRequest,
	reconstruct bool,
) (textures map[string]viewerTextureGPU, generations map[uint64]bool, failed map[string]originalTextureRequest,
	err error,
) {
	textures, generations = make(map[string]viewerTextureGPU, len(requests)), map[uint64]bool{}
	failed = map[string]originalTextureRequest{}
	var batch infra.DiagnosticBatch
	var mu sync.Mutex
	fail := func(key string, req originalTextureRequest, err error) {
		batch.Add(fmt.Errorf("texture %s (%s): %w", req.texture.Name, req.texture.Color.File, err))
		mu.Lock()
		failed[key] = req
		mu.Unlock()
	}
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(originalDecodeJobs)
	for key, req := range requests {
		group.Go(func() error {
			image, err := tools.LoadCharacterOriginalTexture(groupCtx, req.texture)
			if err != nil {
				if groupCtx.Err() != nil {
					return groupCtx.Err()
				}
				fail(key, req, err)
				return nil
			}
			texture := originalTextureData(image, req, reconstruct)
			return fairy.UseDevice(func(g fairy.GPU) error {
				view, err := texture.upload(g.Device, g.Context)
				if err != nil {
					fail(key, req, fmt.Errorf("upload: %w", err))
					return nil
				}
				mu.Lock()
				defer mu.Unlock()
				generations[g.Generation] = true
				textures[key] = viewerTextureGPU{
					view: view, flags: texture.flags, normalMode: texture.normalMode, format: texture.sourceFormat,
				}
				return nil
			})
		})
	}
	if err := group.Wait(); err != nil {
		_ = fairy.UseDevice(func(fairy.GPU) error {
			for _, texture := range textures {
				texture.view.Release()
			}
			return nil
		})
		if ctx.Err() != nil {
			return nil, nil, nil, ctx.Err()
		}
		return nil, nil, nil, err
	}
	batch.Report(log, "ModelViewer.characterOriginal", "load-character-original-textures")
	return textures, generations, failed, nil
}

// toggleOriginalPart shows or hides a part the mod lacks, loading the textures only it uses when it shows and they
// are neither loaded nor loading.
func (v *ModelViewer) toggleOriginalPart(o *viewerOriginal, part *viewerOriginalPart) {
	part.shown = !part.shown
	v.revision++
	if !part.shown || o.ctx == nil {
		return
	}
	requests := map[string]originalTextureRequest{}
	for key, req := range part.pending {
		if _, ok := o.textures[key]; ok {
			delete(part.pending, key)
		} else if !o.inflight[key] {
			requests[key] = req
		}
	}
	if len(requests) == 0 {
		return
	}
	if o.inflight == nil {
		o.inflight = map[string]bool{}
	}
	for key := range requests {
		o.inflight[key] = true
	}
	ctx, tools, log, reconstruct := o.ctx, v.tools, v.log, o.reconstruct
	go func() {
		textures, generations, failed, err := loadOriginalTextures(ctx, tools, log, requests, reconstruct)
		fairy.Go(func() {
			for key := range requests {
				delete(o.inflight, key)
			}
			current := err == nil && ctx.Err() == nil && v.scene != nil && v.scene.original == o &&
				(len(generations) == 0 || len(generations) == 1 && generations[o.generation])
			_ = fairy.UseDevice(func(fairy.GPU) error {
				for key, texture := range textures {
					if _, ok := o.textures[key]; ok || !current {
						texture.view.Release()
						continue
					}
					o.textures[key] = texture
				}
				return nil
			})
			if !current {
				return
			}
			for _, part := range o.parts {
				for key := range part.pending {
					if _, ok := o.textures[key]; ok {
						delete(part.pending, key)
					}
				}
			}
			if len(failed) > 0 {
				v.showOriginalTexturesMissing()
			}
			v.revision++
		})
	}()
}

func (v *ModelViewer) showOriginalTexturesMissing() {
	v.toasts.Show(kit.ToastWarning, T("page.tools.model_viewer.toast.original_textures_missing"), "")
}

type originalTextureRequest struct {
	texture      modelviewer.CharacterOriginalTexture
	srgb, normal bool
}

func originalTextureKey(texture modelviewer.CharacterOriginalTexture) string {
	key := "original:" + texture.Color.File
	if texture.Alpha != nil {
		key += "+" + texture.AlphaChannel + ":" + texture.Alpha.File
	}
	return key
}

func originalTextureData(img *image.NRGBA, req originalTextureRequest, reconstruct bool) *viewerTextureData {
	texture := &viewerTextureData{
		format:       d3d.FORMAT_R8G8B8A8_UNORM,
		width:        uint32(img.Rect.Dx()),
		height:       uint32(img.Rect.Dy()),
		mips:         [][]byte{img.Pix},
		pitches:      []uint32{uint32(img.Stride)},
		generateMips: true,
		sourceFormat: "rgba8-unorm",
	}
	if req.srgb {
		texture.flags |= viewerFlagDiffuseSRGB
	}
	if req.normal && reconstruct {
		texture.normalMode = 1
	}
	return texture
}

// originalPartLabel names a part in the menu by its material, or its mesh when the material has no name.
func originalPartLabel(part modelviewer.CharacterOriginalPart) string {
	if part.Material != "" {
		return part.Material
	}
	return part.Mesh
}

// stateKey is the field of a mesh state that holds the texture of a fill slot.
func stateKey(state *modelviewer.ViewerMeshState, slot int) *string {
	switch slot {
	case fillNormal:
		return &state.NormalMapKey
	case fillLight:
		return &state.LightMapKey
	case fillMaterial:
		return &state.MaterialMapKey
	}
	return &state.TexKey
}

// originalViewerMesh lays out a part of the original model as a scene mesh, shown and drawn whole.
func originalViewerMesh(part modelviewer.CharacterOriginalPart, game string) *viewerMesh {
	vertices := len(part.Positions) / 3
	mesh := &viewerMesh{
		info: modelviewer.ModelViewerMeshTransport{
			ID: "original:" + part.Mesh + ":" + part.Material, Component: part.Mesh, VertexCount: vertices,
			IndexCount: len(part.Indices),
		},
		vertexCount: vertices, variants: map[int]*viewerPositionGeometry{}, averagedOutline: true,
		positions: part.Positions, normals: part.Normals, indices: part.Indices,
	}
	if len(mesh.normals) != len(mesh.positions) {
		mesh.normals = vertexNormals(mesh.positions, mesh.indices)
	}
	uv1 := part.UVs1
	mesh.hasUV1 = len(uv1) == vertices*2
	if !mesh.hasUV1 {
		uv1 = part.UVs
	}
	mesh.uvs = make([]float32, vertices*4)
	for v := range vertices {
		if len(part.UVs) == vertices*2 {
			copy(mesh.uvs[v*4:v*4+2], part.UVs[v*2:v*2+2])
		}
		if len(uv1) == vertices*2 {
			copy(mesh.uvs[v*4+2:v*4+4], uv1[v*2:v*2+2])
		}
	}
	tangents := part.Tangents
	if len(tangents) != vertices*4 {
		tangents = nil
	}
	colors := part.Colors
	if len(colors) != vertices*4 {
		colors = nil
	}
	mesh.hasTangents, mesh.hasColors = tangents != nil, colors != nil
	mesh.tangentUsable = usableTangentFrame(tangents, mesh.normals, nil)
	mesh.faceForward = meanNormal(mesh.normals)
	outline := viewerOutline(false, mesh.positions, mesh.normals, tangents)
	mesh.extra = viewerVertexExtras(tangents, outline, colors, vertices)
	mesh.baseExtra = mesh.extra
	mesh.current, mesh.geometryKey = interleaveViewerGeometry(mesh.positions, mesh.normals), baseGeometryKey
	mesh.state = modelviewer.ViewerMeshState{Visible: true, PositionVariant: -1}
	switch part.Role {
	case modelviewer.OriginalRoleFace:
		mesh.toonFace = true
	case modelviewer.OriginalRoleHair:
		mesh.toonHair = true
	case modelviewer.OriginalRoleEye:
		mesh.toonEye = true
	}
	if game == "EFMI" && modelviewer.IsHeadRole(part.Role) {
		mesh.efmiHead, mesh.efmiFace = true, part.Role == modelviewer.OriginalRoleFace
	}
	return mesh
}

// shadeWithFace shades the parts that share a face part's material, such as Genshin's eye area and brows, as one
// face facing the mean of their normals, so the face map's shadow edge runs across them without a seam.
func shadeWithFace(parts []*viewerOriginalPart, data *modelviewer.CharacterOriginal) {
	materials := map[string]bool{}
	for _, part := range data.Parts {
		if part.Role == modelviewer.OriginalRoleFace && part.Material != "" {
			materials[part.Material] = true
		}
	}
	var faces []*viewerMesh
	var normals []float32
	for _, part := range parts {
		if materials[data.Parts[part.index].Material] {
			part.mesh.toonFace, part.mesh.toonEye = true, false
			faces = append(faces, part.mesh)
			normals = append(normals, part.mesh.normals...)
		}
	}
	forward := meanNormal(normals)
	for _, mesh := range faces {
		mesh.faceForward = forward
	}
}

// markOriginalMeshes applies the marks a profile gives its own parts: a ZZMI face keeps its SDF where a normal map
// would be.
func markOriginalMeshes(parts []*viewerOriginalPart, profile string) {
	for _, part := range parts {
		if profile == zzmiProfile && part.mesh.toonFace {
			part.mesh.normalMapUnused = true
		}
	}
}

// originalDraw is an original part ready to draw: its mesh, the state it draws in, buffers and, under a motion, its
// bone weights and the node turning its face direction.
type originalDraw struct {
	mesh    *viewerMesh
	state   modelviewer.ViewerMeshState
	gpu     viewerMeshGPU
	blend   d3d.ShaderResourceView
	skinned bool
	anchor  int32
}

// draws lists the shown parts with their buffers, making their bone weights for skin's motion on dev when needed.
func (o *viewerOriginal) draws(dev d3d.Device, skin *viewerMotion, generation uint64) []originalDraw {
	if o == nil || o.generation != generation {
		return nil
	}
	if skin != o.skinFor {
		o.releaseSkin()
		if skin != nil {
			o.makeSkin(dev, skin)
		}
	}
	// A face texture that did not load leaves the face with the original one.
	face := ""
	if s := o.scene; s != nil {
		if _, ok := s.texture(s.faceTexture); ok {
			face = s.faceTexture
		}
	}
	var out []originalDraw
	for i, part := range o.parts {
		if !part.shown || part.gpu.indexCount == 0 {
			continue
		}
		draw := originalDraw{mesh: part.mesh, state: part.mesh.state, gpu: part.gpu, anchor: -1}
		if face != "" && part.head && part.mesh.toonFace {
			draw.state.TexKey = face
		}
		if i < len(o.skin) && o.skin[i].view.P != nil {
			draw.blend, draw.skinned = o.skin[i].view, true
			draw.anchor = o.anchors[i]
		}
		out = append(out, draw)
	}
	return out
}

// makeSkin makes the parts' bone weights for the motion's skeleton, matching nodes by name. Parts whose buffers
// cannot be made draw unposed.
func (o *viewerOriginal) makeSkin(dev d3d.Device, m *viewerMotion) {
	o.skinFor = m
	nodes := m.motion.NodeCount()
	byName := make(map[string]int, nodes)
	for i := range nodes {
		if name := m.motion.NodeName(i); name != "" {
			byName[name] = i
		}
	}
	parents := make([]int32, nodes)
	for i := range parents {
		parents[i] = int32(m.motion.NodeParent(i))
	}
	o.skin = make([]viewerSkinBlend, len(o.parts))
	o.anchors = make([]int32, len(o.parts))
	totals := make([]float64, nodes)
	for i, part := range o.parts {
		o.anchors[i] = -1
		source := o.data.Parts[part.index]
		vertices := len(source.Positions) / 3
		if vertices == 0 || len(source.Nodes) != vertices*4 || len(source.Weights) != vertices*4 {
			continue
		}
		clear(totals)
		packed := make([]uint32, vertices*4)
		for v := range vertices {
			var ids [4]uint32
			var weights [4]uint32
			for k := range 4 {
				ids[k] = viewerMotionNoNode
				node := source.Nodes[v*4+k]
				if node < 0 || int(node) >= len(o.data.NodeNames) {
					continue
				}
				if target, ok := byName[o.data.NodeNames[node]]; ok && target < viewerMotionNoNode {
					ids[k] = uint32(target)
					weight := source.Weights[v*4+k]
					weights[k] = uint32(min(max(weight, 0), 1)*65535 + 0.5)
					totals[target] += float64(weight)
				}
			}
			packed[v*4] = ids[0] | ids[1]<<16
			packed[v*4+1] = ids[2] | ids[3]<<16
			packed[v*4+2] = weights[0] | weights[1]<<16
			packed[v*4+3] = weights[2] | weights[3]<<16
		}
		blend := &o.skin[i]
		var err error
		if blend.buffer, err = dev.CreateBuffer(&d3d.BufferDesc{
			ByteWidth: uint32(len(packed) * 4), Usage: d3d.USAGE_IMMUTABLE, BindFlags: d3d.BIND_SHADER_RESOURCE,
			MiscFlags: d3d.RESOURCE_MISC_BUFFER_STRUCTURED, StructureByteStride: 16,
		}, &d3d.SubresourceData{SysMem: unsafe.Pointer(&packed[0])}); err != nil {
			continue
		}
		if blend.view, err = structuredView(dev, blend.buffer, vertices); err != nil {
			blend.buffer.Release()
			blend.buffer = d3d.Buffer{}
			continue
		}
		blend.vertices = vertices
		o.anchors[i] = anchorNode(totals, parents)
	}
}

// fillState returns the state of scene mesh i with the original textures in the slots it leaves empty.
func (o *viewerOriginal) fillState(i int, state modelviewer.ViewerMeshState) modelviewer.ViewerMeshState {
	if o == nil {
		return state
	}
	for slot, key := range o.fills[i] {
		if key != "" && *stateKey(&state, slot) == "" {
			*stateKey(&state, slot) = key
		}
	}
	return state
}

// texture looks a texture up among the scene's and then the original model's, while both belong to one device.
func (s *viewerScene) texture(key string) (viewerTextureGPU, bool) {
	if key == "" {
		return viewerTextureGPU{}, false
	}
	if texture, ok := s.gpu.textures[key]; ok {
		return texture, true
	}
	if o := s.original; o != nil && o.generation == s.gpu.generation {
		texture, ok := o.textures[key]
		return texture, ok
	}
	return viewerTextureGPU{}, false
}

// originalMenuItems are the Model menu's entries for the original model: whether it fills the mod, and which of
// the parts the mod lacks it shows.
func (v *ModelViewer) originalMenuItems() []kit.MenuItem {
	if !v.originalDataAvailable() {
		return nil
	}
	enabled := v.originalDataEnabled()
	items := []kit.MenuItem{{Separator: true}, {
		Label: T("page.tools.model_viewer.menu.original_data"), Checkbox: true, Checked: enabled,
		Loading: v.originalCancel != nil,
		OnClick: func() {
			v.originalFailed = viewerOriginalKey{}
			v.setRenderSetting(setting.KeyModelViewerOriginalData, !enabled)
		},
	}}
	o := (*viewerOriginal)(nil)
	if v.scene != nil {
		o = v.scene.original
	}
	if !enabled || o == nil {
		return items
	}
	var parts []kit.MenuItem
	for _, part := range o.parts {
		if part.head {
			continue
		}
		parts = append(parts, kit.MenuItem{
			Label: part.label, Checkbox: true, Checked: part.shown,
			OnClick: func() { v.toggleOriginalPart(o, part) },
		})
	}
	if len(parts) > 0 {
		items = append(items, kit.MenuItem{Label: T("page.tools.model_viewer.menu.original_parts"), Submenu: parts})
	}
	return append(items, v.expressionMenuItems(o)...)
}
