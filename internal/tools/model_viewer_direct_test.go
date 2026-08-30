package tools

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"nahida.live/desktop/internal/infra"
)

func assertModelViewerProtocolOK(t *testing.T, protocol *infra.Protocol, path string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	protocol.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.Len() == 0 {
		t.Fatalf("GET %s = %d, %q", path, response.Code, response.Body.String())
	}
}

func readModelViewerProtocolBytes(t *testing.T, protocol *infra.Protocol, path string) []byte {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	protocol.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, %q", path, response.Code, response.Body.String())
	}
	return response.Body.Bytes()
}

func TestLoadModelViewerForViewerWritesRawTransport(t *testing.T) {
	ctx := context.Background()
	modDir := t.TempDir()
	ini := `[Constants]
global persist $dress = 0
[KeyDress]
type = cycle
$dress = 0, 1
[TextureOverrideBody]
ib = ResourceBodyIB
if $dress == 0
drawindexed = 3, 0, 0
endif
[ResourceBody]
filename = Body.buf
stride = 12
[ResourceBodyIB]
filename = Body.ib
format = DXGI_FORMAT_R16_UINT`
	if err := os.WriteFile(filepath.Join(modDir, "mod.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	vb := make([]byte, 36)
	for index, value := range []float32{0, 0, 0, 1, 0, 0, 0, 1, 0} {
		binary.LittleEndian.PutUint32(vb[index*4:], math.Float32bits(value))
	}
	if err := os.WriteFile(filepath.Join(modDir, "Body.buf"), vb, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modDir, "Body.ib"), []byte{0, 0, 1, 0, 2, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	protocol := infra.NewProtocol()
	service := NewWithOptions(Options{Protocol: protocol})
	service.UseClient(openToolsTestDB(t))
	result, err := service.LoadModViewer(ctx, modDir)
	if err != nil {
		t.Fatal(err)
	}
	if result.MemorySessionID == "" || len(result.Meshes) != 1 || len(result.Variables) != 1 {
		t.Fatalf("result = %#v", result)
	}
	assertModelViewerProtocolOK(t, protocol, result.Meshes[0].PositionsURL)
	assertModelViewerProtocolOK(t, protocol, result.Meshes[0].IndicesURL)
	if removed, err := service.CleanupModelViewer(ctx, result.MemorySessionID); err != nil || !removed {
		t.Fatalf("cleanup = %v, %v", removed, err)
	}
}

func TestLoadModViewerBuildsPresentPositionVariants(t *testing.T) {
	modDir := t.TempDir()
	ini := `[Constants]
global $fps = 30
global $frame = 0
[Present]
$frame = time * $fps
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[TextureOverrideBodyPosition]
handling = skip
if $frame == 0
vb0 = ResourcePos
elif $frame == 1
vb0 = ResourcePos1
endif
[ResourcePos]
filename = pos.buf
stride = 40
[ResourcePos1]
filename = pos1.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT`
	if err := os.WriteFile(filepath.Join(modDir, "mod.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	writePositions := func(name string, first float32) {
		data := make([]byte, 3*40)
		for vertex, x := range []float32{first, first + 1, first} {
			binary.LittleEndian.PutUint32(data[vertex*40:], math.Float32bits(x))
			binary.LittleEndian.PutUint32(data[vertex*40+4:], math.Float32bits(float32(vertex/2)))
		}
		if err := os.WriteFile(filepath.Join(modDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writePositions("pos.buf", 0)
	writePositions("pos1.buf", 10)
	if err := os.WriteFile(filepath.Join(modDir, "tc.buf"), make([]byte, 3*20), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modDir, "body.ib"), modelViewerUint32Bytes([]uint32{0, 1, 2}), 0o600); err != nil {
		t.Fatal(err)
	}

	protocol := infra.NewProtocol()
	service := NewWithOptions(Options{Protocol: protocol})
	service.UseClient(openToolsTestDB(t))
	result, err := service.LoadModViewer(context.Background(), modDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Animations) != 1 || len(result.Meshes) != 1 || len(result.Meshes[0].PositionVariants) != 2 {
		t.Fatalf("result = %#v", result)
	}
	firstVariant, secondVariant := result.Meshes[0].PositionVariants[0], result.Meshes[0].PositionVariants[1]
	if firstVariant.Stride != 40 || secondVariant.Stride != 40 || firstVariant.SourceBytes != 120 || secondVariant.SourceBytes != 120 {
		t.Fatalf("position descriptors = %#v", result.Meshes[0].PositionVariants)
	}
	if !strings.HasPrefix(firstVariant.SourceURL, "/protocol/local?") || !strings.HasPrefix(secondVariant.SourceURL, "/protocol/local?") {
		t.Fatalf("position sources must use local file protocol: %#v", result.Meshes[0].PositionVariants)
	}
	if result.Meshes[0].SourceIndicesURL != "" {
		t.Fatalf("identity source indices URL = %q", result.Meshes[0].SourceIndicesURL)
	}
	first := readModelViewerProtocolBytes(t, protocol, firstVariant.SourceURL)
	second := readModelViewerProtocolBytes(t, protocol, secondVariant.SourceURL)
	if len(first) < 4 || len(second) < 4 || math.Float32frombits(binary.LittleEndian.Uint32(first)) != 0 || math.Float32frombits(binary.LittleEndian.Uint32(second)) != 10 {
		t.Fatalf("position variants do not contain the expected frames")
	}
}

func TestModelViewerPositionOverridesPruneMutuallyExclusiveMeshes(t *testing.T) {
	dir := t.TempDir()
	var ini strings.Builder
	ini.WriteString("[TextureOverrideBodyPosition]\n")
	for index := range 20 {
		if index == 0 {
			fmt.Fprintf(&ini, "if $swapvar == %d\n", index)
		} else {
			fmt.Fprintf(&ini, "elif $swapvar == %d\n", index)
		}
		fmt.Fprintf(&ini, "vb0 = ResourcePos%d\n", index)
	}
	ini.WriteString("endif\n")
	for index := range 20 {
		fmt.Fprintf(&ini, "[ResourcePos%d]\nfilename = pos%d.buf\nstride = 40\n", index, index)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("pos%d.buf", index)), make([]byte, 40), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sections := parseModINI(ini.String())
	variables := map[string]any{"swapvar": float64(0)}
	meshes := make([]modelViewerDirectMesh, 20)
	for index := range meshes {
		meshes[index] = modelViewerDirectMesh{
			component:    "Body",
			positionFile: fmt.Sprintf("pos%d.buf", index),
			conditions:   ModelViewerDNF{{{Var: "swapvar", Value: modelViewerString(index)}}},
			geometry:     &modelViewerGeometry{Position: make([]float32, 3), VertexCount: 1},
		}
	}
	if err := attachModelViewerDirectPositionOverrides(meshes, sections, collectModelViewerResources(sections), dir, variables, newModelViewerBufferCache()); err != nil {
		t.Fatal(err)
	}
	for index, mesh := range meshes {
		if len(mesh.positionAssignments) != 0 {
			t.Fatalf("mesh %d retained incompatible position assignments: %#v", index, mesh.positionAssignments)
		}
	}
}

func TestReadModelViewerShapePositionsMatchesIdentityAndCompactSources(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "positions.buf")
	raw := make([]byte, 4*40)
	for index := range 4 {
		for component := range 3 {
			binary.LittleEndian.PutUint32(raw[index*40+component*4:], math.Float32bits(float32(index*10+component)))
		}
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cache := newModelViewerBufferCache()
	identity, err := readModelViewerShapePositions(cache, path, 40, nil, 4)
	if err != nil {
		t.Fatal(err)
	}
	compact, err := readModelViewerShapePositions(cache, path, 40, []uint32{3, 1}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(identity, []float32{0, 1, 2, 10, 11, 12, 20, 21, 22, 30, 31, 32}) {
		t.Fatalf("identity positions = %v", identity)
	}
	if !slices.Equal(compact, []float32{30, 31, 32, 10, 11, 12}) {
		t.Fatalf("compact positions = %v", compact)
	}
}

func TestModelViewerPositionOverridesIgnoreSameFileAliases(t *testing.T) {
	dir := t.TempDir()
	writeViewerGeometry(t, dir)
	sections := parseModINI(`[TextureOverrideBodyPosition]
if $frame == 0
vb0 = ResourcePosA
elif $frame == 1
vb0 = ResourcePosB
endif
[ResourcePosA]
filename = pos.buf
stride = 40
[ResourcePosB]
filename = pos.buf
stride = 40`)
	variables := map[string]any{"frame": float64(0), "__domain:frame": []any{float64(0), float64(1)}}
	positions := make([]float32, 9)
	meshes := []modelViewerDirectMesh{{
		component:    "Body",
		positionFile: "pos.buf",
		conditions:   modelViewerDNFTrue(),
		geometry: &modelViewerGeometry{
			Position:      positions,
			VertexCount:   3,
			SourceIndices: []uint32{0, 1, 2},
		},
	}}
	if err := attachModelViewerDirectPositionOverrides(meshes, sections, collectModelViewerResources(sections), dir, variables, newModelViewerBufferCache()); err != nil {
		t.Fatal(err)
	}
	if len(meshes[0].positionAssignments) != 0 {
		t.Fatalf("position assignments = %#v", meshes[0].positionAssignments)
	}
}

func TestModelViewerPositionOverridesKeepDistinctPaths(t *testing.T) {
	dir := t.TempDir()
	dashedDir := filepath.Join(dir, "a-b")
	plainDir := filepath.Join(dir, "ab")
	if err := os.MkdirAll(dashedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(plainDir, 0o700); err != nil {
		t.Fatal(err)
	}
	dashedPath := filepath.Join(dashedDir, "pos.buf")
	plainPath := filepath.Join(plainDir, "pos.buf")
	if err := os.WriteFile(dashedPath, make([]byte, 40), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plainPath, make([]byte, 80), 0o600); err != nil {
		t.Fatal(err)
	}
	sections := parseModINI(`[TextureOverrideBodyPosition]
if $frame == 0
vb0 = ResourceDashed
elif $frame == 1
vb0 = ResourcePlain
endif
[ResourceDashed]
filename = a-b/pos.buf
stride = 40
[ResourcePlain]
filename = ab/pos.buf
stride = 40`)
	variables := map[string]any{"frame": float64(0), "__domain:frame": []any{float64(0), float64(1)}}
	meshes := []modelViewerDirectMesh{{
		component:    "Body",
		positionFile: "base.buf",
		conditions:   modelViewerDNFTrue(),
		geometry:     &modelViewerGeometry{Position: make([]float32, 3), VertexCount: 1},
	}}
	if err := attachModelViewerDirectPositionOverrides(meshes, sections, collectModelViewerResources(sections), dir, variables, newModelViewerBufferCache()); err != nil {
		t.Fatal(err)
	}
	if len(meshes[0].positionAssignments) != 2 {
		t.Fatalf("position assignments = %#v", meshes[0].positionAssignments)
	}
	sources := make(map[string]int64, len(meshes[0].positionAssignments))
	for _, assignment := range meshes[0].positionAssignments {
		sources[filepath.Clean(assignment.sourcePath)] = assignment.sourceBytes
	}
	if sources[filepath.Clean(dashedPath)] != 40 || sources[filepath.Clean(plainPath)] != 80 {
		t.Fatalf("position sources = %#v", sources)
	}
}

func TestModelViewerSourceIndicesPayloadOmitsIdentityAndPreservesCompactMapping(t *testing.T) {
	protocol := infra.NewProtocol()
	service := NewWithOptions(Options{Protocol: protocol})
	sessionID := protocol.CreateMemorySession()
	transport := ModelViewerTransport{Meshes: []ModelViewerMeshTransport{{ID: "identity"}, {ID: "compact"}}, Textures: map[string]ModelViewerTextureTransport{}}
	payloads := []modelViewerMeshPayload{
		{Positions: make([]float32, 9), Indices: []uint32{0, 1, 2}, SourceIndices: []uint32{0, 1, 2}},
		{Positions: make([]float32, 9), Indices: []uint32{0, 1, 2}, SourceIndices: []uint32{7, 3, 11}},
	}
	if err := writeModelViewerPayload(service, sessionID, &transport, payloads, nil); err != nil {
		t.Fatal(err)
	}
	if transport.Meshes[0].SourceIndicesURL != "" {
		t.Fatalf("identity mapping URL = %q", transport.Meshes[0].SourceIndicesURL)
	}
	got := readModelViewerProtocolBytes(t, protocol, transport.Meshes[1].SourceIndicesURL)
	if want := modelViewerUint32Bytes([]uint32{7, 3, 11}); string(got) != string(want) {
		t.Fatalf("source indices = %v, want %v", got, want)
	}
}

func TestCleanupModelViewerReleasesMemoryOnlyAfterLastSession(t *testing.T) {
	protocol := infra.NewProtocol()
	service := NewWithOptions(Options{Protocol: protocol})
	first, second := protocol.CreateMemorySession(), protocol.CreateMemorySession()
	service.modelViewerSessions[first] = &modelViewerSession{}
	service.modelViewerSessions[second] = &modelViewerSession{}
	previous := modelViewerFreeOSMemory
	called := 0
	modelViewerFreeOSMemory = func() { called++ }
	t.Cleanup(func() { modelViewerFreeOSMemory = previous })

	if removed, err := service.CleanupModelViewer(context.Background(), first); err != nil || !removed || called != 0 {
		t.Fatalf("first cleanup = removed %v, err %v, releases %d", removed, err, called)
	}
	if removed, err := service.CleanupModelViewer(context.Background(), second); err != nil || !removed || called != 1 {
		t.Fatalf("last cleanup = removed %v, err %v, releases %d", removed, err, called)
	}
}

func TestModelViewerDirectConditionDomainExpansion(t *testing.T) {
	variables := map[string]any{"top": float64(0), "__domain:top": []any{float64(0), float64(1), float64(2)}}
	dnf := modelViewerConditionsToDNF([]modelViewerConditionClause{{Expression: "$top < 2", Expected: true}}, variables)
	if len(dnf) != 2 || dnf[0][0].Value != "0" || dnf[1][0].Value != "1" {
		t.Fatalf("dnf = %#v", dnf)
	}
	inverse := modelViewerConditionsToDNF([]modelViewerConditionClause{{Expression: "$top < 2", Expected: false}}, variables)
	if len(inverse) != 1 || len(inverse[0]) != 2 || inverse[0][0].Value != "0" || !inverse[0][0].Negate || inverse[0][1].Value != "1" || !inverse[0][1].Negate {
		t.Fatalf("inverse = %#v", inverse)
	}
}

func TestDetectModelViewerUVBestChoosesFloat32AndOffsetFour(t *testing.T) {
	const stride = 20
	data := make([]byte, 3*stride)
	for index, uv := range [][2]float32{{0, .25}, {1, .75}, {.5, .5}} {
		binary.LittleEndian.PutUint32(data[index*stride+4:], math.Float32bits(uv[0]))
		binary.LittleEndian.PutUint32(data[index*stride+8:], math.Float32bits(uv[1]))
	}
	offset, format := detectModelViewerUVBest(data, stride, 0, stride)
	if offset != 4 || format != "DXGI_FORMAT_R32G32_FLOAT" {
		t.Fatalf("offset=%d format=%s", offset, format)
	}
}

func TestModelViewerDirectToggleEffectsStayOffPanel(t *testing.T) {
	sections := parseModINI(`[Constants]
global persist $outfit = 0
global persist $piece = 1
[KeyOutfit]
type = cycle
$outfit = 0, 1
$piece = 0`)
	defaults := collectModelViewerDefaultVariables(sections)
	variables := buildModelViewerDirectVariables(sections, collectModelViewerSlotBindings(sections, defaults), defaults)
	if len(variables) != 1 || variables[0].ID != "outfit" || variables[0].Label != "Outfit" || len(variables[0].Effects) != 1 || variables[0].Effects[0].Var != "piece" {
		t.Fatalf("variables = %#v", variables)
	}
}

func TestModelViewerDirectToggleEffectsKeepINILineOrder(t *testing.T) {
	sections := parseModINI(`[Constants]
global persist $outfit = 0
global persist $second = 0
global persist $first = 0
[KeyOutfit]
type = cycle
$outfit = 0, 1
$second = 2
$first = 1`)
	defaults := collectModelViewerDefaultVariables(sections)
	variables := buildModelViewerDirectVariables(sections, collectModelViewerSlotBindings(sections, defaults), defaults)
	if len(variables) != 1 || len(variables[0].Effects) != 2 || variables[0].Effects[0].Var != "second" || variables[0].Effects[1].Var != "first" {
		t.Fatalf("variables = %#v", variables)
	}
}

func TestBuildModelViewerDirectVariablesOrdersMenusBeforeKeyToggles(t *testing.T) {
	bindings := []modelViewerSlotBinding{
		{Slot: 1, Variable: "keyfirst", Values: []any{float64(0), float64(1)}},
		{Slot: 9, Variable: "menunine", Values: []any{float64(0), float64(1)}, AlwaysVisible: true},
		{Slot: 2, Variable: "menutwo", Values: []any{float64(0), float64(1)}, AlwaysVisible: true},
		{Slot: 0, Variable: "keyzero", Values: []any{float64(0), float64(1)}},
	}

	variables := buildModelViewerDirectVariables(nil, bindings, nil)
	want := []string{"menunine", "menutwo", "keyfirst", "keyzero"}
	if len(variables) != len(want) {
		t.Fatalf("variables = %#v", variables)
	}
	for index, id := range want {
		if variables[index].ID != id || variables[index].Order != index {
			t.Fatalf("variables = %#v", variables)
		}
	}
}

func TestModelViewerVariableGatingDoesNotUseEffectVariables(t *testing.T) {
	variable := ModelViewerVariable{ID: "outfit", Effects: []ModelViewerMenuEffect{{Var: "underwear", Value: "0"}}}
	if modelViewerVariableIsGating(variable, map[string]bool{"underwear": true}) {
		t.Fatal("effect-only gating made the source variable visible")
	}
	if !modelViewerVariableIsGating(variable, map[string]bool{"outfit": true}) {
		t.Fatal("directly gated variable was hidden")
	}
}

func TestModelViewerClickedSlotConditionalEffects(t *testing.T) {
	sections := parseModINI(`[Constants]
global persist $top = 1
global persist $pasties = 0
global $clickedSlot
[CommandListClickedSlot]
if $clickedSlot == 1
$top = 1 - $top
if $top == 0
$pasties = 1
endif
endif`)
	defaults := collectModelViewerDefaultVariables(sections)
	bindings := collectModelViewerSlotBindings(sections, defaults)
	variables := buildModelViewerDirectVariables(sections, bindings, defaults)
	if len(variables) != 1 || variables[0].ID != "top" || len(variables[0].Effects) != 1 || variables[0].Effects[0].Var != "pasties" || variables[0].Effects[0].When == nil || variables[0].Effects[0].When.Value != "0" {
		t.Fatalf("variables = %#v", variables)
	}
}

func TestModelViewerNonClickedSlotBranchEffects(t *testing.T) {
	sections := parseModINI(`[Constants]
global persist $top = 0
global persist $pasties = 0
global $menuSlot
[CommandListCustomMenu]
if $menuSlot == 1
$top = ($top + 1) % 2
if $top == 0
$pasties = 1
endif
elif $menuSlot == 2
$other = ($other + 1) % 2
endif`)
	defaults := collectModelViewerDefaultVariables(sections)
	variables := buildModelViewerDirectVariables(sections, collectModelViewerSlotBindings(sections, defaults), defaults)
	var top *ModelViewerVariable
	for index := range variables {
		if variables[index].ID == "top" {
			top = &variables[index]
		}
	}
	if top == nil || len(top.Effects) != 1 || top.Effects[0].Var != "pasties" || top.Effects[0].When == nil || top.Effects[0].When.Var != "top" || top.Effects[0].When.Value != "0" {
		t.Fatalf("variables = %#v", variables)
	}
}

func TestExtractModelViewerDirectStateRulesDropsImpossibleConditions(t *testing.T) {
	sections := parseModINI(`[Constants]
global $mode = 0
global $hidden = 0
[KeyMode]
type = cycle
$mode = 0, 1
[Present]
if $mode == 0
if $mode == 1
$hidden = 1
endif
endif`)
	if rules := extractModelViewerDirectStateRules(sections, collectModelViewerDefaultVariables(sections)); len(rules) != 0 {
		t.Fatalf("rules = %#v", rules)
	}
}

func TestModelViewerDirectScannerUsesExplicitVBResources(t *testing.T) {
	dir := t.TempDir()
	iniText := `[Constants]
global persist $top = 0
[KeySwap]
type = cycle
$top = 0,1
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $top == 0
drawindexed = 3, 0, 0
else
drawindexed = 3, 3, 0
endif
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT`
	iniPath := filepath.Join(dir, "mod.ini")
	if err := os.WriteFile(iniPath, []byte(iniText), 0o600); err != nil {
		t.Fatal(err)
	}
	pos := make([]byte, 8*40)
	for vertex := range 8 {
		binary.LittleEndian.PutUint32(pos[vertex*40:], math.Float32bits(float32(vertex)))
	}
	if err := os.WriteFile(filepath.Join(dir, "pos.buf"), pos, 0o600); err != nil {
		t.Fatal(err)
	}
	tc := make([]byte, 8*20)
	for vertex, uv := range [][2]float32{{0, .25}, {1, .75}, {.5, .5}, {0, .25}, {1, .75}, {.5, .5}, {0, .25}, {1, .75}} {
		binary.LittleEndian.PutUint32(tc[vertex*20+4:], math.Float32bits(uv[0]))
		binary.LittleEndian.PutUint32(tc[vertex*20+8:], math.Float32bits(uv[1]))
	}
	if err := os.WriteFile(filepath.Join(dir, "tc.buf"), tc, 0o600); err != nil {
		t.Fatal(err)
	}
	ib := modelViewerUint32Bytes([]uint32{0, 1, 2, 3, 4, 5})
	if err := os.WriteFile(filepath.Join(dir, "body.ib"), ib, 0o600); err != nil {
		t.Fatal(err)
	}
	sections := parseModINI(iniText)
	meshes, _, _, _, err := buildModelViewerDirectMeshes(iniPath, "", sections)
	if err != nil {
		t.Fatal(err)
	}
	if len(meshes) != 2 {
		t.Fatalf("meshes = %#v", meshes)
	}
	if len(meshes[0].geometry.Texcoord0) != 6 || math.Abs(float64(meshes[0].geometry.Texcoord0[1]-.75)) > 1e-6 || len(meshes[0].conditions) != 1 || len(meshes[1].conditions) != 1 {
		t.Fatalf("uv=%v firstConditions=%#v secondConditions=%#v", meshes[0].geometry.Texcoord0, meshes[0].conditions, meshes[1].conditions)
	}
}

func TestModelViewerDirectScannerUsesCommandListGlobalBuffers(t *testing.T) {
	dir := t.TempDir()
	iniText := `[CommandListGlobalBuffers]
ib = ResourceBodyIB
vb0 = ResourceMissingPosition
vb1 = ResourceTc
[TextureOverrideBody]
drawindexed = 3, 0, 0
[ResourcePos]
filename = pos.buf
format = DXGI_FORMAT_R32G32B32_FLOAT
stride = 12
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT`
	iniPath := filepath.Join(dir, "mod.ini")
	if err := os.WriteFile(iniPath, []byte(iniText), 0o600); err != nil {
		t.Fatal(err)
	}
	pos := make([]byte, 3*12)
	for index, value := range []float32{0, 0, 0, 1, 0, 0, 0, 1, 0} {
		binary.LittleEndian.PutUint32(pos[index*4:], math.Float32bits(value))
	}
	if err := os.WriteFile(filepath.Join(dir, "pos.buf"), pos, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tc.buf"), make([]byte, 3*20), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "body.ib"), modelViewerUint32Bytes([]uint32{0, 1, 2}), 0o600); err != nil {
		t.Fatal(err)
	}

	sections := parseModINI(iniText)
	meshes, _, _, _, err := buildModelViewerDirectMeshes(iniPath, "", sections)
	if err != nil {
		t.Fatal(err)
	}
	if len(meshes) != 1 || len(meshes[0].geometry.Position) != 9 || len(meshes[0].geometry.Indices) != 3 {
		t.Fatalf("meshes = %#v", meshes)
	}
}
