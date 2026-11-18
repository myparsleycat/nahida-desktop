package modelviewer

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/x448/float16"

	"nahida.live/desktop/internal/blenderlink"
	"nahida.live/desktop/internal/infra"
)

// The fixtures under testdata/blender_live/xxmi come from make_fixture.py: capture.json is what the live
// link add-on sends for a scene and export/ is what XXMI-Tools 1.8.2 writes for the same scene.

type xxmiCaptureMesh struct {
	Name               string               `json:"name"`
	Polygons           int                  `json:"polygons"`
	Positions          []float32            `json:"positions"`
	LoopVertices       []uint32             `json:"loopVertices"`
	LoopNormals        []float32            `json:"loopNormals"`
	LoopTangents       []float32            `json:"loopTangents"`
	LoopBitangentSigns []float32            `json:"loopBitangentSigns"`
	UVs                map[string][]float32 `json:"uvs"`
	Colors             map[string][]float32 `json:"colors"`
	GroupCounts        []uint8              `json:"groupCounts"`
	GroupIndices       []uint16             `json:"groupIndices"`
	GroupWeights       []float32            `json:"groupWeights"`
	GroupNames         []string             `json:"groupNames"`
}

type xxmiCapture struct {
	Game       string `json:"game"`
	Components []struct {
		FullName          string              `json:"fullName"`
		HasBlend          bool                `json:"hasBlend"`
		HasTexcoord       bool                `json:"hasTexcoord"`
		Layout            []xxmiLayoutElement `json:"layout"`
		IBFormat          string              `json:"ibFormat"`
		FlipWinding       bool                `json:"flipWinding"`
		FlipNormal        bool                `json:"flipNormal"`
		FlipTangent       bool                `json:"flipTangent"`
		FlipBitangentSign bool                `json:"flipBitangentSign"`
		Mirror            bool                `json:"mirror"`
		FlipV             map[string]bool     `json:"flipV"`
		NormalizeWeights  bool                `json:"normalizeWeights"`
		Outlines          xxmiOutlineSettings `json:"outlines"`
		Parts             []struct {
			FullName string            `json:"fullName"`
			Objects  []xxmiCaptureMesh `json:"objects"`
		} `json:"parts"`
	} `json:"components"`
}

func loadXXMICapture(t *testing.T, path string) []xxmiExportComponent {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var capture xxmiCapture
	if err := json.Unmarshal(raw, &capture); err != nil {
		t.Fatal(err)
	}
	game, err := parseXXMIExportGame(capture.Game)
	if err != nil {
		t.Fatal(err)
	}
	components := make([]xxmiExportComponent, 0, len(capture.Components))
	for _, source := range capture.Components {
		component := xxmiExportComponent{
			Game: game, FullName: source.FullName, HasBlend: source.HasBlend, HasTexcoord: source.HasTexcoord,
			Layout: source.Layout, IBFormat: source.IBFormat, FlipWinding: source.FlipWinding,
			FlipNormal: source.FlipNormal, FlipTangent: source.FlipTangent,
			FlipBitangentSign: source.FlipBitangentSign, Mirror: source.Mirror, FlipV: source.FlipV,
			NormalizeWeights: source.NormalizeWeights, Outlines: source.Outlines,
		}
		for _, part := range source.Parts {
			exportPart := xxmiExportPart{FullName: part.FullName}
			for _, object := range part.Objects {
				exportPart.Objects = append(exportPart.Objects, blenderExportMesh(object))
			}
			component.Parts = append(component.Parts, exportPart)
		}
		components = append(components, component)
	}
	return components
}

// xxmiValueTolerance is how far a value may differ from XXMI-Tools: outlines and weights go through
// operations whose float32 rounding order numpy does not fix, everything else must match exactly.
func xxmiValueTolerance(game xxmiExportGame, semantic xxmiSemantic) float64 {
	switch {
	case semantic.semantic == "TANGENT" && game != xxmiZenless:
		return 1e-5
	case semantic.semantic == "TEXCOORD" && semantic.index == 1 && game == xxmiZenless:
		return 2e-3
	case semantic.semantic == "BLENDWEIGHTS" || semantic.semantic == "BLENDWEIGHT":
		return 1e-6
	}
	return 0
}

func readXXMIValue(data []byte, kind exportValueKind) float64 {
	switch kind {
	case exportFloat32:
		return float64(math.Float32frombits(binary.LittleEndian.Uint32(data)))
	case exportFloat16:
		return float64(float16.Frombits(binary.LittleEndian.Uint16(data)).Float32())
	case exportUint32:
		return float64(binary.LittleEndian.Uint32(data))
	case exportSint32:
		return float64(int32(binary.LittleEndian.Uint32(data)))
	case exportUint16:
		return float64(binary.LittleEndian.Uint16(data))
	case exportSint16:
		return float64(int16(binary.LittleEndian.Uint16(data)))
	case exportSint8:
		return float64(int8(data[0]))
	default:
		return float64(data[0])
	}
}

func compareXXMIBuffer(t *testing.T, game xxmiExportGame, name string, semantics []xxmiSemantic, got, want []byte) {
	t.Helper()
	stride := xxmiStride(semantics)
	if len(got) != len(want) {
		t.Fatalf("%s: %d bytes, XXMI-Tools wrote %d", name, len(got), len(want))
	}
	failures := 0
	for row := 0; row*stride < len(want); row++ {
		offset := row * stride
		for _, semantic := range semantics {
			tolerance := xxmiValueTolerance(game, semantic)
			size := semantic.format.valueBytes()
			for k := range semantic.format.values {
				at := offset + k*size
				a := readXXMIValue(got[at:], semantic.format.storage())
				b := readXXMIValue(want[at:], semantic.format.storage())
				exact := bytes.Equal(got[at:at+size], want[at:at+size])
				if !exact && (tolerance == 0 || math.Abs(a-b) > tolerance) {
					if failures < 8 {
						t.Errorf("%s row %d %s[%d] = %v, XXMI-Tools wrote %v", name, row, semantic.name(), k, a, b)
					}
					failures++
				}
			}
			offset += semantic.format.byteWidth()
		}
	}
	if failures > 0 {
		t.Errorf("%s: %d values differ", name, failures)
	}
}

var (
	xxmiDrawLineRE    = regexp.MustCompile(`(?m)^drawindexed = (\d+), (\d+), 0\r?$`)
	xxmiVertexCountRE = regexp.MustCompile(`(?m)^override_vertex_count = (\d+)\r?$`)
)

func TestBlenderXXMIExportMatchesXXMITools(t *testing.T) {
	cases := []struct{ name, capture, export string }{
		{"gimi", "gimi/capture.json", "gimi/export"},
		{"gimi edited", "gimi/edited.json", "gimi/edited"},
		{"srmi", "srmi/capture.json", "srmi/export"},
		{"zzmi", "zzmi/capture.json", "zzmi/export"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join("testdata", "blender_live", "xxmi")
			components := loadXXMICapture(t, filepath.Join(root, test.capture))
			exportDir := filepath.Join(root, test.export)
			ini, err := os.ReadFile(filepath.Join(exportDir, "Fix.ini"))
			if err != nil {
				t.Fatal(err)
			}
			for index := range components {
				component := &components[index]
				result, err := exportXXMIComponent(component)
				if err != nil {
					t.Fatal(err)
				}
				layout, err := buildXXMILayout(component)
				if err != nil {
					t.Fatal(err)
				}
				buffers := map[string][]xxmiSemantic{
					"Position": layout.position, "Blend": layout.blend, "Texcoord": layout.texcoord,
				}
				for name, semantics := range buffers {
					file := component.FullName + name + ".buf"
					want, err := os.ReadFile(filepath.Join(exportDir, file))
					if err != nil {
						t.Fatal(err)
					}
					compareXXMIBuffer(t, component.Game, file, semantics, result.Files[file], want)
				}
				var drawLines []string
				for _, part := range result.Parts {
					file := part.FullName + ".ib"
					want, err := os.ReadFile(filepath.Join(exportDir, file))
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(result.Files[file], want) {
						t.Errorf("%s differs from XXMI-Tools", file)
					}
					for _, draw := range part.Draws {
						if draw.VertexCount > 0 {
							drawLines = append(drawLines, fmt.Sprintf("%d, %d", draw.IndexCount, draw.IndexOffset))
						}
					}
				}
				var wantLines []string
				for _, match := range xxmiDrawLineRE.FindAllStringSubmatch(string(ini), -1) {
					wantLines = append(wantLines, match[1]+", "+match[2])
				}
				if fmt.Sprint(drawLines) != fmt.Sprint(wantLines) {
					t.Errorf("draws = %v, XXMI-Tools wrote %v", drawLines, wantLines)
				}
				match := xxmiVertexCountRE.FindStringSubmatch(string(ini))
				if match == nil || match[1] != strconv.Itoa(result.VertexCount) {
					t.Errorf("vertex count %d, XXMI-Tools wrote %v", result.VertexCount, match)
				}
			}
		})
	}
}

func copyXXMIFixture(t *testing.T, from string) string {
	t.Helper()
	to := t.TempDir()
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(from, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(to, entry.Name()), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return to
}

// The live rebuild of an exported mod, given the buffers of a later export with other vertex counts, must
// produce the meshes a fresh load of that later export produces.
func TestBlenderSceneShowsWhatALoadOfTheExportShows(t *testing.T) {
	root := filepath.Join("testdata", "blender_live", "xxmi", "gimi")
	protocol := infra.NewProtocol()
	var updates []ModelViewerLiveUpdate
	service := NewWithOptions(Options{Protocol: protocol, EventEmit: func(name string, data ...any) {
		if update, ok := data[0].(ModelViewerLiveUpdate); ok && name == EventLiveMeshes {
			updates = append(updates, update)
		}
	}})
	ctx := context.Background()
	base, err := service.LoadModViewer(ctx, copyXXMIFixture(t, filepath.Join(root, "export")))
	if err != nil {
		t.Fatal(err)
	}
	edited, err := service.LoadModViewer(ctx, copyXXMIFixture(t, filepath.Join(root, "edited")))
	if err != nil {
		t.Fatal(err)
	}

	link, client := service.BlenderLink(), blenderlink.Link{Session: 1}
	scene, objects := blenderLinkCapture(t, filepath.Join(root, "capture.json"), 1)
	if result, err := link.Scene(ctx, client, scene); err != nil || result.Status != blenderlink.AckQueued {
		t.Fatalf("scene: %+v, %v", result, err)
	}
	for _, object := range objects {
		if result, err := link.Object(ctx, client, object); err != nil || result.Status != blenderlink.AckQueued {
			t.Fatalf("object %s: %+v, %v", object.ObjectKey, result, err)
		}
	}
	opened, err := link.OpenScene(ctx, client.Session, "")
	if err != nil {
		t.Fatal(err)
	}
	shown := make([]blenderShownMesh, len(opened.Meshes))
	for index, mesh := range opened.Meshes {
		shown[index] = blenderShownMesh{mesh.ID, mesh.Component, mesh.GeometryURL, mesh.BlendURL}
	}
	compareBlenderScene(t, protocol, "opened", shown, base.Meshes)

	scene, objects = blenderLinkCapture(t, filepath.Join(root, "edited.json"), 2)
	if result, err := link.Scene(ctx, client, scene); err != nil || result.Status != blenderlink.AckApplied {
		t.Fatalf("edited scene: %+v, %v", result, err)
	}
	for _, object := range objects {
		if result, err := link.Object(ctx, client, object); err != nil || result.Status != blenderlink.AckApplied {
			t.Fatalf("edited object %s: %+v, %v", object.ObjectKey, result, err)
		}
	}
	// Each object of a component rebuilds it; the last update of each mesh holds every edited object.
	latest := map[string]ModelViewerLiveMesh{}
	for _, update := range updates {
		if update.Reload || update.MemorySessionID != opened.MemorySessionID {
			t.Fatalf("unexpected update %+v", update)
		}
		for _, mesh := range update.Meshes {
			latest[mesh.ID] = mesh
		}
	}
	for index := range shown {
		mesh, ok := latest[shown[index].id]
		if !ok {
			t.Fatalf("mesh %s was not rebuilt", shown[index].id)
		}
		shown[index].geometry, shown[index].blend = mesh.GeometryURL, mesh.BlendURL
	}
	compareBlenderScene(t, protocol, "edited", shown, edited.Meshes)
}

// An object whose triangles are all deleted must reach the viewer as an empty mesh under its ID, not drop out of
// the update and leave its last geometry on screen. A scene without any triangles still opens.
func TestBlenderSceneEmptiesAnObjectWithoutTriangles(t *testing.T) {
	protocol := infra.NewProtocol()
	var updates []ModelViewerLiveUpdate
	service := NewWithOptions(Options{Protocol: protocol, EventEmit: func(name string, data ...any) {
		if update, ok := data[0].(ModelViewerLiveUpdate); ok && name == EventLiveMeshes {
			updates = append(updates, update)
		}
	}})
	ctx := context.Background()
	link, client := service.BlenderLink(), blenderlink.Link{Session: 1}
	scene, objects := blenderLinkCapture(
		t,
		filepath.Join("testdata", "blender_live", "xxmi", "gimi", "capture.json"),
		1,
	)
	if _, err := link.Scene(ctx, client, scene); err != nil {
		t.Fatal(err)
	}
	for _, object := range objects {
		if _, err := link.Object(ctx, client, object); err != nil {
			t.Fatalf("object %s: %v", object.ObjectKey, err)
		}
	}
	opened, err := link.OpenScene(ctx, client.Session, "")
	if err != nil {
		t.Fatal(err)
	}

	// The second object (ExtraA1) shares its component with others. Its settings keep the face count, as
	// Blender reports while the object is in Edit mode.
	target := objects[1]
	id := "blender:" + target.ObjectKey
	index := slices.IndexFunc(opened.Meshes, func(mesh ModelViewerMeshTransport) bool { return mesh.ID == id })
	if index < 0 {
		t.Fatalf("mesh %s is not in the opened scene", id)
	}
	sent := func(object blenderlink.ObjectUpdate) ModelViewerLiveMesh {
		t.Helper()
		updates = nil
		if _, err := link.Object(ctx, client, object); err != nil {
			t.Fatalf("object %s: %v", object.ObjectKey, err)
		}
		for _, update := range updates {
			for _, mesh := range update.Meshes {
				if mesh.ID == id && update.MemorySessionID == opened.MemorySessionID {
					return mesh
				}
			}
		}
		t.Fatalf("no update carries mesh %s", id)
		return ModelViewerLiveMesh{}
	}
	empty := blenderlink.ObjectUpdate{
		SceneRev: target.SceneRev, ObjectKey: target.ObjectKey, Revision: target.Revision + 1,
		VertexGroups: target.VertexGroups,
	}

	cleared := sent(empty)
	if cleared.IndexCount != 0 || cleared.VertexCount != 0 || cleared.Bounds != nil {
		t.Fatalf("emptied mesh = %+v, want no vertices, indices or bounds", cleared)
	}
	geometry := readModelViewerProtocolBytes(t, protocol, cleared.GeometryURL)
	if len(geometry) != 32 || string(geometry[:4]) != "MVG3" || slices.ContainsFunc(geometry[4:], func(b byte) bool {
		return b != 0
	}) {
		t.Fatalf("emptied mesh geometry = %v, want an MVG3 header with zero counts", geometry)
	}

	restored := sent(target)
	if restored.IndexCount != opened.Meshes[index].IndexCount {
		t.Fatalf("restored mesh has %d indices, want %d", restored.IndexCount, opened.Meshes[index].IndexCount)
	}

	for _, object := range objects {
		if _, err := link.Object(ctx, client, blenderlink.ObjectUpdate{
			SceneRev: object.SceneRev, ObjectKey: object.ObjectKey, Revision: object.Revision + 2,
		}); err != nil {
			t.Fatalf("object %s: %v", object.ObjectKey, err)
		}
	}
	reopened, err := link.OpenScene(ctx, client.Session, "")
	if err != nil {
		t.Fatalf("open a scene without triangles: %v", err)
	}
	if len(reopened.Meshes) != 0 {
		t.Fatalf("scene without triangles opened with %d meshes", len(reopened.Meshes))
	}
}

// Settings whose components do not follow the Scene's would leave every object without a component and the viewer
// empty, so the Scene is rejected, and the objects sent for it report why.
func TestBlenderSceneRejectsSettingsOfOtherComponents(t *testing.T) {
	ctx, client := context.Background(), blenderlink.Link{Session: 1}
	link := NewWithOptions(Options{Protocol: infra.NewProtocol()}).BlenderLink()
	scene, objects := blenderLinkCapture(
		t,
		filepath.Join("testdata", "blender_live", "xxmi", "gimi", "capture.json"),
		1,
	)
	scene.Components[0].Name += "Renamed"
	rejected, err := link.Scene(ctx, client, scene)
	if err == nil {
		t.Fatalf("a Scene whose settings name other components was accepted: %+v", rejected)
	}
	if _, objectErr := link.Object(ctx, client, objects[0]); objectErr == nil ||
		!strings.Contains(objectErr.Error(), err.Error()) {
		t.Fatalf("object of the rejected scene: %v, want the scene's error %q", objectErr, err)
	}
}

type blenderShownMesh struct {
	id, component, geometry, blend string
}

// compareBlenderScene matches the meshes of a Blender scene to those of a loaded export by part and draw order,
// and requires the same geometry and bone weights.
func compareBlenderScene(
	t *testing.T,
	protocol *infra.Protocol,
	label string,
	shown []blenderShownMesh,
	loaded []ModelViewerMeshTransport,
) {
	t.Helper()
	if len(shown) == 0 || len(shown) != len(loaded) {
		t.Fatalf("%s: %d meshes, a load of the export has %d", label, len(shown), len(loaded))
	}
	ordinal := map[string]int{}
	for _, mesh := range shown {
		index := ordinal[mesh.component]
		ordinal[mesh.component]++
		var want *ModelViewerMeshTransport
		seen := 0
		for candidate := range loaded {
			if strings.HasSuffix(strings.ToLower(loaded[candidate].Component), strings.ToLower(mesh.component)) {
				if seen == index {
					want = &loaded[candidate]
					break
				}
				seen++
			}
		}
		if want == nil {
			t.Fatalf("%s: no loaded mesh for %s draw %d", label, mesh.component, index)
		}
		got, expected := readModelViewerProtocolBytes(t, protocol, mesh.geometry),
			readModelViewerProtocolBytes(t, protocol, want.GeometryURL)
		if !bytes.Equal(withoutMVG3SecondUV(got, expected), expected) {
			t.Errorf("%s: %s geometry differs from %s of the loaded export", label, mesh.id, want.ID)
		}
		if !bytes.Equal(readModelViewerProtocolBytes(t, protocol, mesh.blend),
			readModelViewerProtocolBytes(t, protocol, want.BlendURL)) {
			t.Errorf("%s: %s bone weights differ from %s of the loaded export", label, mesh.id, want.ID)
		}
	}
}

// blenderLinkCapture turns a capture into the Scene and objects the add-on sends for it, with UV and color
// layers numbered by name.
func blenderLinkCapture(t *testing.T, path string, rev uint32) (blenderlink.Scene, []blenderlink.ObjectUpdate) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var capture xxmiCapture
	var settings map[string]any
	if err := json.Unmarshal(raw, &capture); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	objects := map[string]blenderXXMIObject{}
	scene := blenderlink.Scene{SceneRev: rev, Exporter: "xxmi"}
	var updates []blenderlink.ObjectUpdate
	for index, component := range capture.Components {
		delete(settings["components"].([]any)[index].(map[string]any), "parts")
		sceneComponent := blenderlink.Component{Name: component.FullName}
		for _, part := range component.Parts {
			scenePart := blenderlink.Part{Name: part.FullName}
			for _, mesh := range part.Objects {
				key := "uid:" + strconv.Itoa(len(updates)+1)
				scenePart.Objects = append(scenePart.Objects, blenderlink.SceneObject{ObjectKey: key, Name: mesh.Name})
				update := blenderlink.ObjectUpdate{
					SceneRev: rev, ObjectKey: key, Revision: rev, VertexCount: len(mesh.Positions) / 3,
					LoopCount: len(mesh.LoopVertices), VertexGroups: mesh.GroupNames, Positions: mesh.Positions,
					LoopVertices: mesh.LoopVertices, Normals: mesh.LoopNormals, Tangents: mesh.LoopTangents,
					BitangentSigns: mesh.LoopBitangentSigns,
					Weights: blenderlink.Weights{
						Counts: mesh.GroupCounts, Groups: mesh.GroupIndices, Values: mesh.GroupWeights,
					},
				}
				names := blenderXXMIObject{Polygons: mesh.Polygons}
				for _, name := range slices.Sorted(maps.Keys(mesh.UVs)) {
					update.UVs = append(update.UVs, blenderlink.Layer{Index: len(names.UV), Data: mesh.UVs[name]})
					names.UV = append(names.UV, name)
				}
				for _, name := range slices.Sorted(maps.Keys(mesh.Colors)) {
					update.Colors = append(update.Colors,
						blenderlink.Layer{Index: len(names.Color), Data: mesh.Colors[name]})
					names.Color = append(names.Color, name)
				}
				objects[key] = names
				updates = append(updates, update)
			}
			sceneComponent.Parts = append(sceneComponent.Parts, scenePart)
		}
		scene.Components = append(scene.Components, sceneComponent)
	}
	settings["objects"] = objects
	if scene.Settings, err = json.Marshal(settings); err != nil {
		t.Fatal(err)
	}
	return scene, updates
}

// withoutMVG3SecondUV drops the second UV set of mesh when reference has none. A mod folder's loader infers the
// vertex layout from buffer strides and reads only the first UV set, where a Blender scene knows the layout.
func withoutMVG3SecondUV(mesh, reference []byte) []byte {
	const header, uv1Count = 32, 4 + 4*4
	if len(mesh) < header || len(reference) < header || binary.LittleEndian.Uint32(reference[uv1Count:]) != 0 {
		return mesh
	}
	offset := header
	for field := range 4 {
		offset += int(binary.LittleEndian.Uint32(mesh[4+field*4:])) * 4
	}
	uv1 := int(binary.LittleEndian.Uint32(mesh[uv1Count:])) * 4
	out := append(append([]byte{}, mesh[:offset]...), mesh[offset+uv1:]...)
	binary.LittleEndian.PutUint32(out[uv1Count:], 0)
	return out
}
