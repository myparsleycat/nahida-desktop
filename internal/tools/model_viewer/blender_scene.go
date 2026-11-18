package modelviewer

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"maps"
	"path/filepath"
	"strings"

	"nahida.live/desktop/internal/infra"
)

// blenderSceneSource is what a session built from a Blender scene was opened with.
type blenderSceneSource struct {
	link uint64
	game xxmiExportGame
	// contents is blenderLinkScene.contents at open.
	contents string
}

// blenderSceneComponent is one component of a Blender scene: its export input, the object key of each of its
// parts' objects, and the textures of each object.
type blenderSceneComponent struct {
	export   xxmiExportComponent
	keys     [][]string
	textures map[string][]blenderTexture
}

// blenderLiveMesh is a session mesh rebuilt from a Blender scene, encoded as the load encodes it.
type blenderLiveMesh struct {
	ID string
	// Geometry is the mesh in the MVG3 encoding the load writes to GeometryURL.
	Geometry     []byte
	Blend        []uint16
	BlendPalette string
	VertexCount  int
	IndexCount   int
	Bounds       *ModelViewerBounds
}

// openBlenderScene exports the components as XXMI-Tools would, reads the buffers back with the mod loader's
// decoders and stores the result as a viewer session, with no mod folder behind it. slots and pixels describe the
// scene's textures; shown returns the slots of the texture keys the session's meshes refer to. A "pixels" slot
// whose pixels have not arrived keeps its key on the meshes without a texture.
func (t *Service) openBlenderScene(
	ctx context.Context,
	source blenderSceneSource,
	name string,
	components []blenderSceneComponent,
	slots map[string]blenderTextureSlot,
	pixels map[string]blenderPixels,
) (transport ModelViewerTransport, shown map[string]blenderTextureSlot, err error) {
	sessionID := t.protocol.CreateMemorySession()
	keep := false
	defer func() {
		if !keep {
			t.protocol.CleanupMemorySession(sessionID)
		}
		if err != nil && !infra.IsCancellationError(err) {
			err = infra.ReportError(t.log, err, "BlenderLink.OpenScene", infra.Diagnostic{
				Operation: "open-blender-scene", Fields: map[string]any{"scene": name, "link": source.link},
			})
		}
	}()

	cache := newModelViewerBufferCache(ctx)
	cache.hashSources = false
	defer cache.releaseAll()
	var meshes []modelViewerDirectMesh
	for index := range components {
		built, err := buildBlenderSceneMeshes(&components[index], cache)
		if err != nil {
			return ModelViewerTransport{}, nil, err
		}
		meshes = append(meshes, built...)
	}

	profile := source.game.materialProfile()
	shown = map[string]blenderTextureSlot{}
	for _, mesh := range meshes {
		for _, assignment := range mesh.textureAssignments {
			key := modelViewerAssignmentTextureKey(assignment)
			if slot, ok := slots[key]; ok {
				shown[key] = slot
			}
		}
	}
	textures, err := t.loadBlenderFileTextures(ctx, profile, shown)
	if err != nil {
		return ModelViewerTransport{}, nil, err
	}
	available := maps.Clone(textures)
	for key, slot := range shown {
		if slot.source != blenderTexturePixels {
			continue
		}
		if update, ok := pixels[slot.name]; ok && update.Version == slot.pixels && len(update.Pixels) > 0 {
			textures[key] = blenderPixelsPayload(key, slot, update)
			available[key] = textures[key]
		} else {
			available[key] = modelViewerTexturePayload{Key: key, Role: slot.role}
		}
	}

	transport = ModelViewerTransport{
		MemorySessionID:     sessionID,
		Name:                name,
		MaterialProfile:     profile,
		NormalXYReconstruct: profile.traits().normalXYReconstruct,
		Meshes:              []ModelViewerMeshTransport{},
		Textures:            make(map[string]ModelViewerTextureTransport),
		Variables:           []ModelViewerVariable{},
		DefaultState:        make(map[string]any),
		StateRules:          []ModelViewerStateRule{},
		Animations:          []ModelViewerAnimationClip{},
		ComputeDeformers:    []ModelViewerComputeDeformerTransport{},
		Forms:               []ModelViewerForm{},
		INIs:                []ModelViewerINI{},
	}
	payloads := make([]modelViewerMeshPayload, 0, len(meshes))
	for _, mesh := range meshes {
		item, payload := buildModelViewerDirectMeshPayload(mesh, nil, available, nil, cache)
		transport.Meshes = append(transport.Meshes, item)
		payloads = append(payloads, payload)
	}
	if err := writeModelViewerPayload(
		ctx, t, sessionID, &transport, payloads, textures, modelViewerPayloadOptions{},
	); err != nil {
		return ModelViewerTransport{}, nil, err
	}

	sessionContext, sessionCancel := context.WithCancel(context.Background())
	t.modelViewerMu.Lock()
	if err := ctx.Err(); err != nil {
		t.modelViewerMu.Unlock()
		sessionCancel()
		return ModelViewerTransport{}, nil, err
	}
	t.modelViewerSessions[sessionID] = &modelViewerSession{
		modPath:   "blender:" + name,
		evaluator: newModelViewerEvaluator(sessionContext, transport),
		cancel:    sessionCancel,
		blender:   &source,
	}
	t.modelViewerMu.Unlock()
	keep = true
	return transport, shown, nil
}

// rebuildBlenderSceneComponent rebuilds the meshes of one component, encoded as openBlenderScene stores them, one
// per object. An object without triangles gets an empty mesh, so a viewer stops drawing the geometry it had.
func (t *Service) rebuildBlenderSceneComponent(
	ctx context.Context,
	component *blenderSceneComponent,
) ([]blenderLiveMesh, error) {
	cache := newModelViewerBufferCache(ctx)
	cache.hashSources = false
	defer cache.releaseAll()
	meshes, err := buildBlenderSceneMeshes(component, cache)
	if err != nil {
		return nil, err
	}
	encoded := make([]blenderLiveMesh, 0, len(meshes))
	built := map[string]bool{}
	for _, mesh := range meshes {
		live, err := encodeBlenderLiveMesh(ctx, mesh, cache)
		if err != nil {
			return nil, err
		}
		encoded = append(encoded, live)
		built[live.ID] = true
	}

	// The empty meshes share one encoding, which the stored buffers only read.
	var empty []byte
	for _, keys := range component.keys {
		for _, key := range keys {
			id := blenderMeshID(key)
			if built[id] {
				continue
			}
			if empty == nil {
				var geometry bytes.Buffer
				if err := writeModelViewerMeshBytes(&geometry, modelViewerMeshPayload{}); err != nil {
					return nil, fmt.Errorf("mesh %s encode: %w", id, err)
				}
				empty = geometry.Bytes()
			}
			encoded = append(encoded, blenderLiveMesh{ID: id, Geometry: empty})
		}
	}
	return encoded, nil
}

// blenderMeshID names the mesh of a Blender object after its key, so it keeps its ID while the object is edited.
func blenderMeshID(key string) string {
	return "blender:" + key
}

// buildBlenderSceneMeshes exports a component and decodes one mesh per object that has faces, as the mod loader
// decodes the draws of the exported INI: the vertex buffers through extractModelViewerGeometry with the V axis
// flipped, the blend buffer through the bone weight decoder. Each mesh is named with blenderMeshID.
func buildBlenderSceneMeshes(
	component *blenderSceneComponent,
	cache *modelViewerBufferCache,
) ([]modelViewerDirectMesh, error) {
	export := &component.export
	result, err := exportXXMIComponent(export)
	if err != nil {
		return nil, fmt.Errorf("export %s: %w", export.FullName, err)
	}
	if result.VertexCount == 0 {
		return nil, nil
	}
	layout, err := blenderSceneVertexLayout(export, result.Strides)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", export.FullName, err)
	}
	positions := result.Files[export.FullName+"Position.buf"]
	texcoords := result.Files[export.FullName+"Texcoord.buf"]
	positionStride, texcoordStride := result.Strides["position"], result.Strides["texcoord"]
	if len(texcoords) == 0 {
		texcoordStride = 0
	}
	vb := make([]byte, 0, result.VertexCount*layout.Stride)
	for vertex := range result.VertexCount {
		vb = append(vb, positions[vertex*positionStride:(vertex+1)*positionStride]...)
		if texcoordStride > 0 {
			vb = append(vb, texcoords[vertex*texcoordStride:(vertex+1)*texcoordStride]...)
		}
	}
	var blend *modelViewerBlendSource
	if data := result.Files[export.FullName+"Blend.buf"]; len(data) > 0 {
		blend = &modelViewerBlendSource{
			path:    filepath.Join("blender", export.FullName+"Blend.buf"),
			palette: export.FullName,
			stride:  result.Strides["blend"],
		}
		cache.setOverlay(blend.path, data)
	}

	var meshes []modelViewerDirectMesh
	for partIndex, part := range result.Parts {
		ib := result.Files[part.FullName+".ib"]
		for drawIndex, draw := range part.Draws {
			if draw.IndexCount == 0 {
				continue
			}
			indices := make([]uint32, draw.IndexCount)
			for k := range indices {
				indices[k] = binary.LittleEndian.Uint32(ib[(draw.IndexOffset+k)*4:])
			}
			geometry, err := extractModelViewerGeometry(vb, layout.Stride, layout, indices,
				modelViewerGeometryOptions{includeTangents: true, includeColors: true, compact: true}, nil)
			if err != nil {
				return nil, fmt.Errorf("decode %s: %w", draw.Name, err)
			}
			if geometry == nil {
				continue
			}
			for _, coords := range [][]float32{geometry.Texcoord0, geometry.Texcoord1} {
				for offset := 1; offset < len(coords); offset += 2 {
					coords[offset] = 1 - coords[offset]
				}
			}
			key := component.keys[partIndex][drawIndex]
			meshes = append(meshes, modelViewerDirectMesh{
				id:                 blenderMeshID(key),
				component:          part.FullName,
				geometry:           geometry,
				blend:              blend,
				conditions:         modelViewerDNFTrue(),
				textureAssignments: blenderTextureAssignments(component.textures[key]),
				indexCount:         len(geometry.Indices),
			})
		}
	}
	return meshes, nil
}

// blenderSceneVertexLayout lays out one vertex buffer holding a row of the component's Position buffer followed
// by a row of its Texcoord buffer, with the elements of its VBLayout at the offsets the export writes them.
func blenderSceneVertexLayout(component *xxmiExportComponent, strides map[string]int) (modelViewerFmtLayout, error) {
	xxmi, err := buildXXMILayout(component)
	if err != nil {
		return modelViewerFmtLayout{}, err
	}
	inBuffer := func(semantics []xxmiSemantic, name string, index int) bool {
		for _, semantic := range semantics {
			if semantic.same(xxmiSemantic{semantic: name, index: index}) {
				return true
			}
		}
		return false
	}
	layout := modelViewerFmtLayout{Topology: "trianglelist", IndexFormat: "DXGI_FORMAT_R32_UINT"}
	offsets := map[string]int{"position": 0, "texcoord": strides["position"]}
	placed := map[string]bool{}
	for _, element := range component.Layout {
		buffer := ""
		switch {
		case inBuffer(xxmi.position, element.SemanticName, element.SemanticIndex):
			buffer = "position"
		case inBuffer(xxmi.texcoord, element.SemanticName, element.SemanticIndex):
			buffer = "texcoord"
		default:
			continue
		}
		key := xxmiConverterKey(element.SemanticName, element.SemanticIndex)
		if placed[key] {
			continue
		}
		placed[key] = true
		format, err := parseExportFormat(element.Format)
		if err != nil {
			return modelViewerFmtLayout{}, err
		}
		layout.Elements = append(layout.Elements, modelViewerFmtElement{
			SemanticName:      element.SemanticName,
			SemanticIndex:     element.SemanticIndex,
			Format:            strings.TrimPrefix(strings.ToUpper(element.Format), "DXGI_FORMAT_"),
			AlignedByteOffset: offsets[buffer],
			InputSlotClass:    "per-vertex",
		})
		offsets[buffer] += format.byteWidth()
	}
	if offsets["position"] != strides["position"] || offsets["texcoord"] != strides["position"]+strides["texcoord"] {
		return modelViewerFmtLayout{}, fmt.Errorf("VBLayout does not match the exported buffers")
	}
	layout.Stride = strides["position"] + strides["texcoord"]
	return layout, nil
}

// blenderTextureAssignments assigns the image of each role to the object's meshes. The file of an assignment
// names the image rather than a path, so its key is blenderTextureKey.
func blenderTextureAssignments(textures []blenderTexture) []modelViewerDirectTextureAssignment {
	choices := blenderTextureChoices(textures)
	assignments := make([]modelViewerDirectTextureAssignment, 0, len(choices))
	for _, choice := range choices {
		assignments = append(assignments, modelViewerDirectTextureAssignment{
			role: choice.role, resource: choice.texture.Name, file: "blender-image/" + choice.texture.Name,
			authored: true, conditions: modelViewerDNFTrue(),
		})
	}
	return assignments
}

func encodeBlenderLiveMesh(
	ctx context.Context,
	mesh modelViewerDirectMesh,
	cache *modelViewerBufferCache,
) (blenderLiveMesh, error) {
	item, payload := buildModelViewerDirectMeshPayload(mesh, nil, nil, nil, cache)
	bounds, err := modelViewerGeometryBounds(ctx, payload.Positions)
	if err != nil {
		return blenderLiveMesh{}, fmt.Errorf("mesh %s bounds: %w", mesh.id, err)
	}
	if payload.Normals == nil {
		if payload.Normals, err = modelViewerVertexNormals(ctx, payload.Positions, payload.Indices); err != nil {
			return blenderLiveMesh{}, fmt.Errorf("mesh %s normals: %w", mesh.id, err)
		}
	}
	var geometry bytes.Buffer
	geometry.Grow(modelViewerMeshByteLength(payload))
	if err := writeModelViewerMeshBytes(&geometry, payload); err != nil {
		return blenderLiveMesh{}, fmt.Errorf("mesh %s encode: %w", mesh.id, err)
	}
	return blenderLiveMesh{
		ID:           item.ID,
		Geometry:     geometry.Bytes(),
		Blend:        payload.Blend,
		BlendPalette: item.BlendPalette,
		VertexCount:  len(payload.Positions) / 3,
		IndexCount:   len(payload.Indices),
		Bounds:       bounds,
	}, nil
}
