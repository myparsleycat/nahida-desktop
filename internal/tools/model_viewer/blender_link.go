package modelviewer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	"nahida.live/desktop/internal/blenderlink"
)

// EventLiveMeshes carries a ModelViewerLiveUpdate whenever a Blender live link replaces meshes or textures of a
// viewer session, or changes it so that the viewer has to open it again.
const EventLiveMeshes = "model-viewer:live-meshes"

// ModelViewerLiveMesh is a session mesh rebuilt from a Blender live link. Its buffers are stored under the
// session like the load's.
type ModelViewerLiveMesh struct {
	ID           string             `json:"id"`
	GeometryURL  string             `json:"geometryUrl"`
	BlendURL     string             `json:"blendUrl,omitempty"`
	BlendPalette string             `json:"blendPalette,omitempty"`
	VertexCount  int                `json:"vertexCount"`
	IndexCount   int                `json:"indexCount"`
	Bounds       *ModelViewerBounds `json:"bounds,omitempty"`
}

type ModelViewerLiveUpdate struct {
	MemorySessionID string `json:"memorySessionId"`
	// Meshes holds every object of a rebuilt component. An object without triangles comes with IndexCount 0, and
	// the session may not have its mesh.
	Meshes   []ModelViewerLiveMesh    `json:"meshes"`
	Textures []ModelViewerLiveTexture `json:"textures,omitempty"`
	// Reload reports that objects were added or removed or an object's images changed role, which the session's
	// meshes cannot show.
	Reload bool `json:"reload,omitempty"`
}

// blenderXXMISettings is the settings block of an "xxmi" Scene.
type blenderXXMISettings struct {
	// Game is empty when the scene has no XXMI-Tools game; the viewer then takes the one the user picks.
	Game       string                       `json:"game"`
	Components []blenderXXMIComponent       `json:"components"`
	Objects    map[string]blenderXXMIObject `json:"objects"`
}

type blenderXXMIComponent struct {
	FullName    string `json:"fullName"`
	HasBlend    bool   `json:"hasBlend"`
	HasTexcoord bool   `json:"hasTexcoord"`
	// Layout is empty for objects without a 3DMigoto:VBLayout property.
	Layout            []xxmiLayoutElement `json:"layout"`
	IBFormat          string              `json:"ibFormat"`
	FlipWinding       bool                `json:"flipWinding"`
	FlipNormal        bool                `json:"flipNormal"`
	FlipTangent       bool                `json:"flipTangent"`
	FlipBitangentSign bool                `json:"flipBitangentSign"`
	Mirror            bool                `json:"mirror"`
	FlipV             map[string]bool     `json:"flipV"`
	NormalizeWeights  bool                `json:"normalizeWeights"`
	Outlines          struct {
		Enabled        bool    `json:"enabled"`
		Precision      int     `json:"precision"`
		GateDivergence float64 `json:"gateDivergence"`
		CustomNormals  bool    `json:"customNormals"`
	} `json:"outlines"`
}

type blenderXXMIObject struct {
	// Polygons is the face count of the object's own mesh data, which decides whether XXMI-Tools exports it.
	Polygons int `json:"polygons"`
	// UV and Color name the layers sent as attribute layers 0, 1, ...
	UV       []string         `json:"uv"`
	Color    []string         `json:"color"`
	Textures []blenderTexture `json:"textures"`
}

// blenderTexture is an image an object's materials use. Role is the viewer slot the image's settings or its
// material wiring give it, or empty. Source is "file", with the file's Path and a Version that changes with its
// content, or "pixels", which the add-on sends in texture transfers.
type blenderTexture struct {
	Name    string `json:"name"`
	Role    string `json:"role"`
	Source  string `json:"source"`
	Path    string `json:"path"`
	Version string `json:"version"`
}

// blenderLinkScene is the newest scene of one connection, the newest revision of each of its objects and the
// newest pixels of each image it describes as "pixels".
type blenderLinkScene struct {
	scene    blenderlink.Scene
	settings blenderXXMISettings
	objects  map[string]blenderlink.ObjectUpdate
	pixels   map[string]blenderPixels
}

// BlenderLink turns what Blender live link clients send into model viewer sessions. It implements
// blenderlink.Handler.
type BlenderLink struct {
	service *Service
	emit    func(string, ...any)

	mu     sync.Mutex
	scenes map[uint64]*blenderLinkScene
	// rejected holds, by connection, the newest Scene that was rejected, so the objects and images sent for it
	// report why.
	rejected map[uint64]rejectedScene
	// apply serializes rebuilds and texture replacements, so the buffers of one session are replaced in the order
	// updates arrive. It guards serial, buffers and applied.
	apply  sync.Mutex
	serial uint64
	// buffers holds, by session and mesh or texture, the buffer IDs of the revisions still stored: the newest, and
	// the one before it, which a viewer may still be reading.
	buffers map[string]map[string][][]string
	// applied holds, by session, what each texture of the session shows.
	applied map[string]*blenderAppliedTextures
}

type rejectedScene struct {
	rev uint32
	err error
}

func newBlenderLink(service *Service, emit func(string, ...any)) *BlenderLink {
	return &BlenderLink{
		service: service, emit: emit, scenes: map[uint64]*blenderLinkScene{}, rejected: map[uint64]rejectedScene{},
		buffers: map[string]map[string][][]string{}, applied: map[string]*blenderAppliedTextures{},
	}
}

func (b *BlenderLink) Scene(
	ctx context.Context,
	link blenderlink.Link,
	scene blenderlink.Scene,
) (blenderlink.Result, error) {
	result, err := b.applyScene(ctx, link, scene)
	b.mu.Lock()
	if err != nil {
		b.rejected[link.Session] = rejectedScene{rev: scene.SceneRev, err: err}
	} else {
		delete(b.rejected, link.Session)
	}
	b.mu.Unlock()
	return result, err
}

// staleSceneError explains an object or image sent for scene rev that is not the connection's current scene. The
// caller holds b.mu.
func (b *BlenderLink) staleSceneError(link uint64, rev uint32, what string) error {
	if rejected, ok := b.rejected[link]; ok && rejected.rev == rev {
		return fmt.Errorf("scene %d was rejected: %w", rev, rejected.err)
	}
	return fmt.Errorf("%s does not belong to the current scene", what)
}

func (b *BlenderLink) applyScene(
	ctx context.Context,
	link blenderlink.Link,
	scene blenderlink.Scene,
) (blenderlink.Result, error) {
	if scene.Exporter != "xxmi" {
		return blenderlink.Result{}, fmt.Errorf("exporter %q is not supported", scene.Exporter)
	}
	// A scene the add-on cannot describe yet has no settings and only errors, which the link state shows.
	var settings blenderXXMISettings
	if len(scene.Settings) > 0 {
		if err := json.Unmarshal(scene.Settings, &settings); err != nil {
			return blenderlink.Result{}, fmt.Errorf("xxmi settings: %w", err)
		}
		if settings.Game != "" {
			if _, err := parseXXMIExportGame(settings.Game); err != nil {
				return blenderlink.Result{}, err
			}
		}
		// component pairs each scene component with the settings entry at its index.
		if len(settings.Components) != len(scene.Components) {
			return blenderlink.Result{}, fmt.Errorf(
				"xxmi settings describe %d components, the scene has %d", len(settings.Components),
				len(scene.Components))
		}
		for index, component := range scene.Components {
			if name := settings.Components[index].FullName; name != component.Name {
				return blenderlink.Result{}, fmt.Errorf(
					"xxmi settings component %d is %q, the scene names %q", index, name, component.Name)
			}
		}
	}

	b.mu.Lock()
	state := b.scenes[link.Session]
	if state == nil {
		state = &blenderLinkScene{
			objects: map[string]blenderlink.ObjectUpdate{}, pixels: map[string]blenderPixels{},
		}
		b.scenes[link.Session] = state
	}
	state.scene, state.settings = scene, settings
	state.dropUnusedPixels()
	keys := map[string]bool{}
	for _, component := range scene.Components {
		for _, part := range component.Parts {
			for _, object := range part.Objects {
				keys[object.ObjectKey] = true
			}
		}
	}
	for key := range state.objects {
		if !keys[key] {
			delete(state.objects, key)
		}
	}
	contents := state.contents()
	b.mu.Unlock()

	sessions := b.service.blenderSceneSessions(link.Session)
	var current []blenderSceneSession
	for _, session := range sessions {
		if session.contents == contents {
			current = append(current, session)
		} else if b.emit != nil {
			b.emit(EventLiveMeshes, ModelViewerLiveUpdate{MemorySessionID: session.id, Reload: true})
		}
	}
	if len(sessions) == 0 {
		return blenderlink.Result{Status: blenderlink.AckQueued}, nil
	}
	b.syncTextures(ctx, link.Session, current)
	return blenderlink.Result{Status: blenderlink.AckApplied, ViewerAttached: true}, nil
}

func (b *BlenderLink) Texture(
	ctx context.Context,
	link blenderlink.Link,
	update blenderlink.TextureUpdate,
) (blenderlink.Result, error) {
	b.mu.Lock()
	state := b.scenes[link.Session]
	if state == nil || state.scene.SceneRev != update.SceneRev {
		err := b.staleSceneError(link.Session, update.SceneRev, "image")
		b.mu.Unlock()
		return blenderlink.Result{}, err
	}
	previous, seen := state.pixels[update.Image]
	shown := state.showsPixels(update.Image)
	b.mu.Unlock()

	sessions := b.service.blenderSceneSessions(link.Session)
	if !shown {
		return blenderlink.Result{
			Status: blenderlink.AckUnmatched, ViewerAttached: len(sessions) > 0,
			Message: "no viewer slot shows the image",
		}, nil
	}

	// A connection sends one request at a time, so nothing else changes the image's pixels meanwhile.
	pixels := newBlenderPixels(update, previous, seen)
	b.mu.Lock()
	state.pixels[update.Image] = pixels
	b.mu.Unlock()

	if len(sessions) == 0 {
		return blenderlink.Result{Status: blenderlink.AckQueued}, nil
	}
	b.syncTextures(ctx, link.Session, sessions)
	return blenderlink.Result{Status: blenderlink.AckApplied, ViewerAttached: true}, nil
}

func (b *BlenderLink) Object(
	ctx context.Context,
	link blenderlink.Link,
	update blenderlink.ObjectUpdate,
) (blenderlink.Result, error) {
	b.mu.Lock()
	state := b.scenes[link.Session]
	if state == nil || state.scene.SceneRev != update.SceneRev {
		err := b.staleSceneError(link.Session, update.SceneRev, "object")
		b.mu.Unlock()
		return blenderlink.Result{}, err
	}
	state.objects[update.ObjectKey] = update
	b.mu.Unlock()

	sessions := b.service.blenderSceneSessions(link.Session)
	if len(sessions) == 0 {
		return blenderlink.Result{Status: blenderlink.AckQueued}, nil
	}
	for _, session := range sessions {
		b.mu.Lock()
		component, complete, found := state.component(update.ObjectKey, session.game)
		b.mu.Unlock()
		if !found {
			return blenderlink.Result{
				Status:         blenderlink.AckUnmatched,
				ViewerAttached: true,
				Message:        "the object is not part of a component",
			}, nil
		}
		if !complete {
			return blenderlink.Result{Status: blenderlink.AckQueued, ViewerAttached: true}, nil
		}
		if err := b.rebuild(ctx, session.id, &component); err != nil {
			return blenderlink.Result{}, err
		}
	}
	return blenderlink.Result{Status: blenderlink.AckApplied, ViewerAttached: true}, nil
}

func (b *BlenderLink) ClientError(_ context.Context, _ blenderlink.Link, report blenderlink.ClientError) {
	b.service.log.Warn(
		fmt.Sprintf("Blender live link error %s on %q: %s", report.Code, report.ObjectKey, report.Message),
		"BlenderLink.ClientError",
	)
}

func (b *BlenderLink) Disconnected(link blenderlink.Link) {
	b.mu.Lock()
	delete(b.scenes, link.Session)
	delete(b.rejected, link.Session)
	b.mu.Unlock()
	b.apply.Lock()
	maps.DeleteFunc(b.applied, func(_ string, applied *blenderAppliedTextures) bool {
		return applied.link == link.Session
	})
	b.apply.Unlock()
}

// SceneGame returns the importer key (GIMI, SRMI or ZZMI) of the XXMI-Tools game a connection's scene uses, or
// "" when it has none.
func (b *BlenderLink) SceneGame(link uint64) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	state := b.scenes[link]
	if state == nil || state.settings.Game == "" {
		return ""
	}
	game, err := parseXXMIExportGame(state.settings.Game)
	if err != nil {
		return ""
	}
	return game.importer()
}

// SceneNames returns the component names of a connection's scene, which name the character a mod is for.
func (b *BlenderLink) SceneNames(link uint64) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	state := b.scenes[link]
	if state == nil {
		return nil
	}
	var names []string
	for _, component := range state.scene.Components {
		names = append(names, component.Name)
	}
	return names
}

// SceneName names a connection's scene after its only component, or "Blender".
func (b *BlenderLink) SceneName(link uint64) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if state := b.scenes[link]; state != nil {
		return blenderSceneName(state.scene)
	}
	return "Blender"
}

// OpenScene builds a viewer session from the newest state of a connection's scene. importer (GIMI, SRMI or ZZMI)
// is the game to export for when the scene has none.
func (b *BlenderLink) OpenScene(ctx context.Context, link uint64, importer string) (ModelViewerTransport, error) {
	b.mu.Lock()
	state := b.scenes[link]
	if state == nil {
		b.mu.Unlock()
		return ModelViewerTransport{}, errors.New("the Blender instance is no longer connected")
	}
	game, err := state.game(importer)
	if err != nil {
		b.mu.Unlock()
		return ModelViewerTransport{}, err
	}
	name := blenderSceneName(state.scene)
	var components []blenderSceneComponent
	for _, source := range state.scene.Components {
		if len(source.Parts) == 0 || len(source.Parts[0].Objects) == 0 {
			continue
		}
		if component, complete, _ := state.component(source.Parts[0].Objects[0].ObjectKey, game); complete {
			components = append(components, component)
		}
	}
	contents := state.contents()
	slots := state.textureSlots()
	pixels := maps.Clone(state.pixels)
	b.mu.Unlock()
	transport, shown, err := b.service.openBlenderScene(
		ctx, blenderSceneSource{link: link, game: game, contents: contents}, name, components, slots, pixels,
	)
	if err != nil {
		return ModelViewerTransport{}, err
	}

	// Images that changed while the session opened are replaced at once; the viewer applies the update when it
	// installs the session.
	b.apply.Lock()
	b.applied[transport.MemorySessionID] = &blenderAppliedTextures{link: link, slots: shown}
	b.apply.Unlock()
	b.syncTextures(ctx, link, b.service.blenderSceneSessions(link))
	return transport, nil
}

func (b *BlenderLink) rebuild(ctx context.Context, sessionID string, component *blenderSceneComponent) error {
	b.apply.Lock()
	defer b.apply.Unlock()
	meshes, err := b.service.rebuildBlenderSceneComponent(ctx, component)
	if err != nil {
		return err
	}

	b.serial++
	revision := strconv.FormatUint(b.serial, 10)
	update := ModelViewerLiveUpdate{MemorySessionID: sessionID, Meshes: make([]ModelViewerLiveMesh, 0, len(meshes))}
	for _, mesh := range meshes {
		stored, ids, err := b.service.storeBlenderLiveMesh(sessionID, revision, mesh)
		if err != nil {
			return err
		}
		update.Meshes = append(update.Meshes, stored)
		b.keepBuffers(sessionID, mesh.ID, ids)
	}
	if b.emit != nil && len(update.Meshes) > 0 {
		b.emit(EventLiveMeshes, update)
	}
	return nil
}

// keepBuffers records the buffers of a mesh's newest revision and removes those of the revision two before.
func (b *BlenderLink) keepBuffers(sessionID, meshID string, ids []string) {
	meshes := b.buffers[sessionID]
	if meshes == nil {
		meshes = map[string][][]string{}
		b.buffers[sessionID] = meshes
	}
	revisions := meshes[meshID]
	revisions = append(revisions, ids)
	if len(revisions) > 2 {
		for _, id := range revisions[0] {
			b.service.protocol.RemoveMemoryBuffer(sessionID, id)
		}
		revisions = revisions[1:]
	}
	meshes[meshID] = revisions
}

func (s *blenderLinkScene) game(importer string) (xxmiExportGame, error) {
	if s.settings.Game != "" {
		return parseXXMIExportGame(s.settings.Game)
	}
	if importer == "" {
		return 0, errors.New("choose the game of the Blender scene")
	}
	return parseXXMIExportGame(importer)
}

// contents identifies the objects of the scene and the texture keys they refer to, which a session cannot change
// without opening again. The content behind a key changes in place.
func (s *blenderLinkScene) contents() string {
	var builder strings.Builder
	for _, component := range s.scene.Components {
		for _, part := range component.Parts {
			for _, object := range part.Objects {
				builder.WriteString(object.ObjectKey)
				builder.WriteByte(0)
				for _, choice := range blenderTextureChoices(s.settings.Objects[object.ObjectKey].Textures) {
					builder.WriteString(choice.key + "\x01")
				}
			}
		}
	}
	return builder.String()
}

// component builds the export input of the component that holds key. complete is false while an object of
// the component has not arrived yet.
func (s *blenderLinkScene) component(key string, game xxmiExportGame) (blenderSceneComponent, bool, bool) {
	for index, source := range s.scene.Components {
		holds := slices.ContainsFunc(source.Parts, func(part blenderlink.Part) bool {
			return slices.ContainsFunc(part.Objects, func(object blenderlink.SceneObject) bool {
				return object.ObjectKey == key
			})
		})
		if !holds {
			continue
		}
		if index >= len(s.settings.Components) {
			return blenderSceneComponent{}, false, false
		}
		settings := &s.settings.Components[index]
		component := blenderSceneComponent{export: settings.export(game), textures: map[string][]blenderTexture{}}
		complete := true
		for _, part := range source.Parts {
			exportPart := xxmiExportPart{FullName: part.Name}
			var keys []string
			for _, object := range part.Objects {
				update, ok := s.objects[object.ObjectKey]
				if !ok {
					complete = false
					continue
				}
				info := s.settings.Objects[object.ObjectKey]
				exportPart.Objects = append(exportPart.Objects, blenderExportMeshOf(object.Name, update, info))
				keys = append(keys, object.ObjectKey)
				component.textures[object.ObjectKey] = info.Textures
			}
			component.export.Parts = append(component.export.Parts, exportPart)
			component.keys = append(component.keys, keys)
		}
		if complete && len(component.export.Layout) == 0 {
			component.export.Layout = defaultXXMILayout(&component.export)
		}
		return component, complete, true
	}
	return blenderSceneComponent{}, false, false
}

func (c *blenderXXMIComponent) export(game xxmiExportGame) xxmiExportComponent {
	return xxmiExportComponent{
		Game: game, FullName: c.FullName, HasBlend: c.HasBlend, HasTexcoord: c.HasTexcoord, Layout: c.Layout,
		IBFormat: c.IBFormat, FlipWinding: c.FlipWinding, FlipNormal: c.FlipNormal, FlipTangent: c.FlipTangent,
		FlipBitangentSign: c.FlipBitangentSign, Mirror: c.Mirror, FlipV: c.FlipV,
		NormalizeWeights: c.NormalizeWeights,
		Outlines: xxmiOutlineSettings{
			Enabled: c.Outlines.Enabled, Precision: c.Outlines.Precision,
			GateDivergence: c.Outlines.GateDivergence, CustomNormals: c.Outlines.CustomNormals,
		},
	}
}

// blenderExportMeshOf names the object's layers. An object without a TEXCOORD.xy layer, such as one made in
// Blender rather than imported by XXMI-Tools, gives its first UV layer that name.
func blenderExportMeshOf(name string, update blenderlink.ObjectUpdate, info blenderXXMIObject) blenderExportMesh {
	mesh := blenderExportMesh{
		Name:               name,
		Polygons:           info.Polygons,
		Positions:          update.Positions,
		LoopVertices:       update.LoopVertices,
		LoopNormals:        update.Normals,
		LoopTangents:       update.Tangents,
		LoopBitangentSigns: update.BitangentSigns,
		UVs:                map[string][]float32{},
		Colors:             map[string][]float32{},
		GroupCounts:        update.Weights.Counts,
		GroupIndices:       update.Weights.Groups,
		GroupWeights:       update.Weights.Values,
		GroupNames:         update.VertexGroups,
	}
	for _, layer := range update.UVs {
		if layer.Index < len(info.UV) {
			mesh.UVs[info.UV[layer.Index]] = layer.Data
		}
	}
	if _, ok := mesh.UVs["TEXCOORD.xy"]; !ok && len(update.UVs) > 0 {
		mesh.UVs["TEXCOORD.xy"] = update.UVs[0].Data
	}
	for _, layer := range update.Colors {
		if layer.Index < len(info.Color) {
			mesh.Colors[info.Color[layer.Index]] = layer.Data
		}
	}
	return mesh
}

// defaultXXMILayout is the GIMI layout, with the color and second UV only when every object has them, for a
// component whose objects carry no 3DMigoto:VBLayout.
func defaultXXMILayout(component *xxmiExportComponent) []xxmiLayoutElement {
	layout := []xxmiLayoutElement{
		{SemanticName: "POSITION", Format: "R32G32B32_FLOAT"},
		{SemanticName: "NORMAL", Format: "R32G32B32_FLOAT"},
		{SemanticName: "TANGENT", Format: "R32G32B32A32_FLOAT"},
		{SemanticName: "BLENDWEIGHTS", Format: "R32G32B32A32_FLOAT"},
		{SemanticName: "BLENDINDICES", Format: "R32G32B32A32_SINT"},
	}
	every := func(has func(*blenderExportMesh) bool) bool {
		for _, part := range component.Parts {
			for index := range part.Objects {
				if mesh := &part.Objects[index]; mesh.hasTriangles() && !has(mesh) {
					return false
				}
			}
		}
		return true
	}
	if every(func(mesh *blenderExportMesh) bool { _, ok := mesh.Colors["COLOR"]; return ok }) {
		layout = append(layout, xxmiLayoutElement{SemanticName: "COLOR", Format: "R8G8B8A8_UNORM"})
	}
	layout = append(layout, xxmiLayoutElement{SemanticName: "TEXCOORD", Format: "R32G32_FLOAT"})
	if every(func(mesh *blenderExportMesh) bool { _, ok := mesh.UVs["TEXCOORD1.xy"]; return ok }) {
		layout = append(layout, xxmiLayoutElement{SemanticName: "TEXCOORD", SemanticIndex: 1, Format: "R32G32_FLOAT"})
	}
	return layout
}

func blenderSceneName(scene blenderlink.Scene) string {
	if len(scene.Components) == 1 {
		return scene.Components[0].Name
	}
	return "Blender"
}

// blenderSceneSessions returns the open sessions built from a connection's scene.
func (t *Service) blenderSceneSessions(link uint64) []blenderSceneSession {
	t.modelViewerMu.Lock()
	defer t.modelViewerMu.Unlock()
	var sessions []blenderSceneSession
	for id, session := range t.modelViewerSessions {
		if session.blender != nil && session.blender.link == link {
			sessions = append(sessions, blenderSceneSession{id: id, blenderSceneSource: *session.blender})
		}
	}
	return sessions
}

type blenderSceneSession struct {
	id string
	blenderSceneSource
}

// storeBlenderLiveMesh stores a rebuilt mesh's buffers under the session. Each revision gets its own
// buffers, so a viewer still reading the previous ones is not handed a mix.
func (t *Service) storeBlenderLiveMesh(
	sessionID, revision string,
	mesh blenderLiveMesh,
) (ModelViewerLiveMesh, []string, error) {
	prefix := "live/" + revision + "/" + mesh.ID
	ids := []string{prefix + "/geometry"}
	geometryURL, err := t.protocol.StoreMemoryBuffer(sessionID, ids[0], mesh.Geometry, "application/octet-stream")
	if err != nil {
		return ModelViewerLiveMesh{}, nil, fmt.Errorf("store live mesh %s: %w", mesh.ID, err)
	}
	stored := ModelViewerLiveMesh{
		ID: mesh.ID, GeometryURL: geometryURL, BlendPalette: mesh.BlendPalette,
		VertexCount: mesh.VertexCount, IndexCount: mesh.IndexCount, Bounds: mesh.Bounds,
	}
	if len(mesh.Blend) > 0 {
		blend := make([]byte, 0, len(mesh.Blend)*2)
		for _, value := range mesh.Blend {
			blend = append(blend, byte(value), byte(value>>8))
		}
		ids = append(ids, prefix+"/blend")
		if stored.BlendURL, err = t.protocol.StoreMemoryBuffer(
			sessionID, ids[1], blend, "application/octet-stream"); err != nil {
			return ModelViewerLiveMesh{}, ids[:1], fmt.Errorf("store live mesh %s weights: %w", mesh.ID, err)
		}
	}
	return stored, ids, nil
}

var _ blenderlink.Handler = (*BlenderLink)(nil)
