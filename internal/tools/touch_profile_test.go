package tools

import (
	"context"
	"encoding/binary"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type touchTestMod struct{}

func (touchTestMod) Disable(_ context.Context, path string) (string, error) {
	target := filepath.Join(filepath.Dir(path), "DISABLED "+filepath.Base(path))
	return target, os.Rename(path, target)
}
func (touchTestMod) Enable(_ context.Context, path string) (string, error) {
	target := filepath.Join(filepath.Dir(path), touchDisabledPrefixRE.ReplaceAllString(filepath.Base(path), ""))
	return target, os.Rename(path, target)
}

func TestTouchProfileBoneApplyRegenerateRollback(t *testing.T) {
	ctx := context.Background()
	parent := t.TempDir()
	source := filepath.Join(parent, "Hero")
	writeTouchTestMod(t, source)
	service := NewWithOptions(Options{Mod: touchTestMod{}})
	useToolsTestAppData(t, service, filepath.Join(parent, "user-data"))
	inspection, err := service.TouchProfilePrepare(ctx, TouchProfileLoadInput{ModPath: source})
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.Components) != 1 || !inspection.Components[0].HasBlend {
		t.Fatalf("unexpected inspection: %#v", inspection)
	}
	channel := 0
	label := "Chest"
	draft, err := service.TouchProfileAnalyzeComponents(ctx, TouchProfileAnalyzeInput{SessionID: inspection.SessionID, ComponentIDs: []string{inspection.Components[0].ID}, BoneSelections: []TouchBoneComponentSelection{{ComponentID: inspection.Components[0].ID, Zones: []TouchBoneZoneSelection{{BoneID: 5, Channel: &channel, Label: &label}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !draft.CanAutoApply || len(draft.Components[0].Zones) != 1 {
		t.Fatalf("unexpected draft: %#v", draft)
	}
	topology, err := service.TouchProfileGetMeshDescriptor(ctx, TouchProfilePreviewInput{SessionID: inspection.SessionID, ComponentID: inspection.Components[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.TouchProfileGetPreviewDescriptor(ctx, TouchProfilePreviewInput{SessionID: inspection.SessionID, ComponentID: inspection.Components[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Zones) != 1 || preview.WeightsCount != 4 {
		t.Fatalf("unexpected preview: %#v", preview)
	}
	weightsResponse := httptest.NewRecorder()
	service.protocol.ServeHTTP(weightsResponse, httptest.NewRequest(http.MethodGet, preview.WeightsURL, nil))
	weights, decodeErr := decodeFloat32Bytes(weightsResponse.Body.Bytes())
	if weightsResponse.Code != http.StatusOK || decodeErr != nil || len(weights) != 4 || preview.Zones[0].WeightOffset != 0 {
		t.Fatalf("packed weights = %d %#v %v", weightsResponse.Code, weights, decodeErr)
	}
	settings := draft.Components[0].Zones[0].Settings
	settings.MaskStrength = .8
	updated, err := service.TouchProfileUpdateZoneSettingsBatch(ctx, TouchProfileUpdateZoneSettingsBatchInput{SessionID: inspection.SessionID, Changes: []TouchProfileZoneSettingsChange{{ComponentID: inspection.Components[0].ID, ZoneID: draft.Components[0].Zones[0].ID, Settings: settings}}})
	if err != nil || !updated.OK || !updated.PreviewChanged {
		t.Fatalf("batch update = %#v, %v", updated, err)
	}
	topologyAfter, err := service.TouchProfileGetMeshDescriptor(ctx, TouchProfilePreviewInput{SessionID: inspection.SessionID, ComponentID: inspection.Components[0].ID})
	if err != nil || topologyAfter.PositionsURL != topology.PositionsURL {
		t.Fatalf("topology changed = %#v, %v", topologyAfter, err)
	}
	previewAfter, err := service.TouchProfileGetPreviewDescriptor(ctx, TouchProfilePreviewInput{SessionID: inspection.SessionID, ComponentID: inspection.Components[0].ID})
	if err != nil || previewAfter.PreviewRevision <= preview.PreviewRevision {
		t.Fatalf("preview revision = %#v, %v", previewAfter, err)
	}
	applied, err := service.TouchProfileApply(ctx, TouchProfileApplyInput{SessionID: inspection.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	if !applied.Validation.OK || !pathIsDirectory(applied.OutputModRoot) || !pathIsDirectory(applied.SourceModRoot) {
		t.Fatalf("unexpected apply: %#v", applied)
	}
	ini, err := os.ReadFile(filepath.Join(applied.OutputModRoot, "mod.ini"))
	if err != nil {
		t.Fatal(err)
	}
	for _, snippet := range []string{"Nahida Touch Profile runtime", "global $nhd_touch_hero_active = 0", "$nhd_touch_hero_lmb_down = 1", "$nhd_touch_hero_active = 1", "[ResourceBodyPosition]", "rzm_jiggle_interaction.hlsl", "dispatch = (4 + 255) // 256, 1, 1"} {
		if !strings.Contains(string(ini), snippet) {
			t.Fatalf("generated INI missing %q", snippet)
		}
	}
	if issues := touchINIStructureErrors(string(ini)); len(issues) != 0 {
		t.Fatalf("generated INI structure errors: %v", issues)
	}
	if _, err = service.TouchProfileRegenerate(ctx, TouchProfileApplyInput{SessionID: inspection.SessionID}); err != nil {
		t.Fatal(err)
	}
	rolled, err := service.TouchProfileRollback(ctx, TouchProfileRollbackInput{SessionID: inspection.SessionID, OutputModRoot: applied.OutputModRoot, SourceModRoot: applied.SourceModRoot, ReenableSourceOnRollback: true})
	if err != nil {
		t.Fatal(err)
	}
	if !rolled.RemovedOutput || !rolled.ReenabledSource || pathIsDirectory(applied.OutputModRoot) || !pathIsDirectory(source) {
		t.Fatalf("unexpected rollback: %#v", rolled)
	}
}

func TestTouchProfileRegenerateRejectsChangedBlend(t *testing.T) {
	ctx := context.Background()
	parent := t.TempDir()
	source := filepath.Join(parent, "Hero")
	writeTouchTestMod(t, source)
	service := NewWithOptions(Options{Mod: touchTestMod{}})
	useToolsTestAppData(t, service, filepath.Join(parent, "user-data"))
	inspection, err := service.TouchProfilePrepare(ctx, TouchProfileLoadInput{ModPath: source})
	if err != nil {
		t.Fatal(err)
	}
	channel := 0
	draft, err := service.TouchProfileAnalyzeComponents(ctx, TouchProfileAnalyzeInput{SessionID: inspection.SessionID, ComponentIDs: []string{inspection.Components[0].ID}, BoneSelections: []TouchBoneComponentSelection{{ComponentID: inspection.Components[0].ID, Zones: []TouchBoneZoneSelection{{BoneID: 5, Channel: &channel}}}}})
	if err != nil || !draft.CanAutoApply {
		t.Fatalf("draft: %v %#v", err, draft)
	}
	applied, err := service.TouchProfileApply(ctx, TouchProfileApplyInput{SessionID: inspection.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	blend := filepath.Join(applied.SourceModRoot, "Meshes", "body-blend.buf")
	raw, err := os.ReadFile(blend)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] = 6
	if err = os.WriteFile(blend, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = service.TouchProfileRegenerate(ctx, TouchProfileApplyInput{SessionID: inspection.SessionID}); err == nil || !strings.Contains(err.Error(), "changed since analysis") {
		t.Fatalf("expected source changed error, got %v", err)
	}
}

func TestTouchZoneSettingsAndChannelConflict(t *testing.T) {
	settings := defaultTouchZoneSettings()
	settings.MaskStrength = 2.1
	if _, err := normalizeTouchZoneSettings(settings); err == nil {
		t.Fatal("expected mask strength error")
	}
	settings = defaultTouchZoneSettings()
	first := TouchComponentDraft{ObjectID: 1, Zones: []TouchZoneSpec{{Channel: 0, Settings: settings}}}
	settings.Advanced.Radius = .4
	second := TouchComponentDraft{ObjectID: 2, Zones: []TouchZoneSpec{{Channel: 0, Settings: settings}}}
	if _, err := buildTouchZoneOverrides([]touchInteractiveEntry{{Draft: first}, {Draft: second}}); err == nil {
		t.Fatal("expected conflicting channel error")
	}
}

func TestTouchFolderBaseNameAppendsSuffixAndStripsDisabled(t *testing.T) {
	if got := touchFolderBaseName("AliceMod"); got != "AliceMod (Touch)" {
		t.Fatalf("plain = %q", got)
	}
	if got := touchFolderBaseName("DISABLED_AliceMod"); got != "AliceMod (Touch)" {
		t.Fatalf("underscore = %q", got)
	}
	if got := touchFolderBaseName("DISABLED AliceMod"); got != "AliceMod (Touch)" {
		t.Fatalf("space = %q", got)
	}
	if got := touchFolderBaseName("DISABLED"); got != "DISABLED (Touch)" {
		t.Fatalf("bare prefix = %q", got)
	}
	if got := touchFolderBaseName("DISABLED\tDISABLED_AliceMod"); got != "AliceMod (Touch)" {
		t.Fatalf("repeated mixed separator = %q", got)
	}
}

func TestRebaseTouchAssetDoesNotMutateValidatorPaths(t *testing.T) {
	t.Parallel()
	outputRoot := t.TempDir()
	assetDir := filepath.Join(outputRoot, "Resources", "IM")
	asset := TouchGeneratedAssets{
		RelativeDir: "Resources/IM",
		MaskPaths:   []string{"Resources/IM/ModBodyJiggleMasks0.buf"},
		ObjectMapPaths: []TouchGeneratedObjectMap{{
			RelativePath: "Resources/IM/ModBodyObjectMap.buf",
			AbsolutePath: filepath.Join(assetDir, "ModBodyObjectMap.buf"),
		}},
		ParamsRelativePath:  "Resources/IM/ModBodyJiggleParams.buf",
		ParamsAbsolutePath:  filepath.Join(assetDir, "ModBodyJiggleParams.buf"),
		PreviewRelativePath: "Resources/IM/ModBodyTouchMaskPreview.png",
		PreviewAbsolutePath: filepath.Join(assetDir, "ModBodyTouchMaskPreview.png"),
	}

	rebased := rebaseTouchAsset(asset, filepath.Join(outputRoot, "Config"))
	if rebased.MaskPaths[0] != "../Resources/IM/ModBodyJiggleMasks0.buf" {
		t.Fatalf("rebased mask = %q", rebased.MaskPaths[0])
	}
	if asset.MaskPaths[0] != "Resources/IM/ModBodyJiggleMasks0.buf" {
		t.Fatalf("validator mask path was mutated: %q", asset.MaskPaths[0])
	}
	if asset.ObjectMapPaths[0].RelativePath != "Resources/IM/ModBodyObjectMap.buf" {
		t.Fatalf("validator object-map path was mutated: %q", asset.ObjectMapPaths[0].RelativePath)
	}
}

func TestTouchProfileSaveDraftPreservesClientDraft(t *testing.T) {
	dir := t.TempDir()
	service := New()
	previous := TouchDraft{SessionID: "session", Components: []TouchComponentDraft{{ComponentID: "old"}}}
	service.touchSessions["session"] = &touchSession{
		Dir:       dir,
		Draft:     &previous,
		Preview:   map[string]touchCachedPreview{"old": {descriptor: TouchProfilePreviewDescriptor{ComponentID: "old"}}},
		Operation: "apply",
	}
	draft := TouchDraft{
		SessionID:      "session",
		CreatedAt:      "client-created-at",
		SourceModRoot:  "client-source",
		Analysis:       TouchModAnalysis{ModRoot: "client-analysis", Components: []TouchComponentAnalysis{{ID: "analysis-only"}}},
		Components:     []TouchComponentDraft{{ComponentID: "client-only", Interactive: true, Confidence: .8}},
		VisionUsed:     true,
		ModelName:      "client-model",
		LLM:            TouchProfileLLMSettings{Protocol: "client", Endpoint: "endpoint", Model: "model", Reasoning: "reasoning"},
		PromptVersion:  "client-prompt",
		RuntimeVersion: "client-runtime",
		CanAutoApply:   false,
		Warnings:       []string{"client-warning"},
	}
	want := draft
	want.CanAutoApply = true
	got, err := service.TouchProfileSaveDraft(context.Background(), draft)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("saved draft changed client fields:\n got: %#v\nwant: %#v", got, want)
	}
	if len(service.touchSessions["session"].Preview) != 0 {
		t.Fatal("preview cache was not cleared")
	}
}

func TestTouchProfileAnalyzeProgressIsMonotonicForSelectedComponents(t *testing.T) {
	ctx := context.Background()
	parent := t.TempDir()
	source := filepath.Join(parent, "Hero")
	writeTouchTestMod(t, source)
	var events []TouchProgressEvent
	service := NewWithOptions(Options{EventEmit: func(name string, data ...any) {
		if name == touchProgressEventName && len(data) == 1 {
			if event, ok := data[0].(TouchProgressEvent); ok {
				events = append(events, event)
			}
		}
	}})
	useToolsTestAppData(t, service, filepath.Join(parent, "user-data"))
	inspection, err := service.TouchProfilePrepare(ctx, TouchProfileLoadInput{ModPath: source})
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.requireTouchSession(inspection.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	second := session.Analysis.Components[0]
	second.ID = "body-second"
	second.Name = "Body Second"
	session.Analysis.Components = append(session.Analysis.Components, second)
	events = nil
	channel := 0
	selections := []TouchBoneComponentSelection{
		{ComponentID: inspection.Components[0].ID, Zones: []TouchBoneZoneSelection{{BoneID: 5, Channel: &channel}}},
		{ComponentID: second.ID, Zones: []TouchBoneZoneSelection{{BoneID: 5, Channel: &channel}}},
	}
	if _, err = service.TouchProfileAnalyzeComponents(ctx, TouchProfileAnalyzeInput{
		SessionID: inspection.SessionID, ComponentIDs: []string{inspection.Components[0].ID, second.ID}, BoneSelections: selections,
	}); err != nil {
		t.Fatal(err)
	}
	last := 0.0
	for _, event := range events {
		if event.Progress < last {
			t.Fatalf("progress decreased from %.3f to %.3f at %s", last, event.Progress, event.Stage)
		}
		last = event.Progress
	}
}

func TestTouchProfileDiscardDraftDoesNotRejectBusySession(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	if err := os.WriteFile(marker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	service := New()
	service.touchSessions["session"] = &touchSession{Dir: dir, Operation: "apply"}
	result, err := service.TouchProfileDiscardDraft(context.Background(), "session")
	if err != nil || !result.OK {
		t.Fatalf("discard = %#v, %v", result, err)
	}
	if _, err = os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("session directory was not removed: %v", err)
	}
}

func TestTouchProfileRollbackKeepsSessionStateWhenDraftWriteFails(t *testing.T) {
	parent := t.TempDir()
	output := filepath.Join(parent, "output")
	source := filepath.Join(parent, "source")
	sessionDir := filepath.Join(parent, "session")
	for _, dir := range []string{output, source, filepath.Join(sessionDir, "draft.json")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	draft := TouchDraft{SessionID: "session", SourceModRoot: "original-source"}
	applied := &touchAppliedProfile{OutputRoot: output, SourceRoot: source}
	session := &touchSession{Dir: sessionDir, Draft: &draft, Applied: applied}
	service := New()
	service.touchSessions["session"] = session
	_, err := service.TouchProfileRollback(context.Background(), TouchProfileRollbackInput{
		SessionID: "session", OutputModRoot: output, SourceModRoot: source,
	})
	if err == nil {
		t.Fatal("expected draft persistence error")
	}
	if session.Draft.SourceModRoot != "original-source" {
		t.Fatalf("draft source mutated despite write failure: %q", session.Draft.SourceModRoot)
	}
	if session.Applied != applied {
		t.Fatal("applied state was cleared despite write failure")
	}
}

func TestSupportsTouchFrameNumberGuard(t *testing.T) {
	if !supportsTouchFrameNumberGuard("1.0.2") || !supportsTouchFrameNumberGuard("v1.0.3") {
		t.Fatal("expected 1.0.2 and newer to enable FRAME_NUMBER")
	}
	for _, version := range []string{"1.0.1", "", "1..2", "1.0.2."} {
		if supportsTouchFrameNumberGuard(version) {
			t.Fatalf("%q should not enable FRAME_NUMBER", version)
		}
	}
}

func TestBuildTouchIBInjectionUsesFrameNumberWhenRequested(t *testing.T) {
	component := TouchComponentAnalysis{ID: "body"}
	if !strings.Contains(buildTouchIBInjection(component, "nhd", "Touch", false), "if time !=") {
		t.Fatal("expected time guard")
	}
	if !strings.Contains(buildTouchIBInjection(component, "nhd", "Touch", true), "if FRAME_NUMBER !=") {
		t.Fatal("expected FRAME_NUMBER guard")
	}
}

func TestTouchINIInsertionPreservesLiteralVariablesAndNextSection(t *testing.T) {
	t.Parallel()
	const prefix = "nhd_touch_mod"
	source := `[Constants]
global $active = 0

[Present]
post $active = 0
run = CommandListCreditInfo

[CustomShaderTransparency]
blend = ADD BLEND_FACTOR INV_BLEND_FACTOR
handling = skip

[TextureOverrideAliceBodyBlend]
$active = 1

[TextureOverrideAliceBodyA]
ib = ResourceAliceBodyAIB
`

	text := ensureTouchConstants(source, prefix, []TouchComponentAnalysis{{ID: "AliceBodyPosition"}})
	text = ensureTouchKeys(text, prefix)
	text = ensureTouchPresent(text, prefix)
	blend := "AliceBodyBlend"
	text = patchTouchBlendSections(text, prefix, []TouchComponentAnalysis{{BlendSectionName: &blend}})

	for _, snippet := range []string{
		"global $nhd_touch_mod_active = 0",
		"global $nhd_touch_mod_last_dispatch_AliceBodyPosition = -1",
		"$nhd_touch_mod_lmb_down = 1",
		"post $nhd_touch_mod_lmb_down = 0",
		"post $nhd_touch_mod_active = 0",
		"[CustomShaderTransparency]\nblend = ADD BLEND_FACTOR INV_BLEND_FACTOR",
		"$active = 1\n\t$nhd_touch_mod_active = 1",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("compiled INI missing %q:\n%s", snippet, text)
		}
	}
	if issues := touchINIStructureErrors(text); len(issues) != 0 {
		t.Fatalf("compiled INI structure errors: %v\n%s", issues, text)
	}
}

func TestTouchINIStructureErrorsRejectsWailsPortCorruption(t *testing.T) {
	t.Parallel()
	broken := `[Constants]
; Nahida Touch Profile state (nhd_touch_mod)
global  = 0

[Present]
 = 1
post  = 0
CustomShaderTransparency]
`
	issues := touchINIStructureErrors(broken)
	if len(issues) < 3 {
		t.Fatalf("issues = %v, want empty assignment, malformed header, and missing state variable", issues)
	}
	joined := strings.Join(issues, "\n")
	for _, snippet := range []string{"without a variable name", "malformed section header", "$nhd_touch_mod_active"} {
		if !strings.Contains(joined, snippet) {
			t.Fatalf("issues missing %q: %v", snippet, issues)
		}
	}
}

func TestAnalyzeTouchComponentBones(t *testing.T) {
	const vertexCount = 300
	component := TouchComponentAnalysis{ID: "body", VertexCount: vertexCount, SupportGrade: "A"}
	positions := make([]float32, vertexCount*3)
	for vertex := range vertexCount {
		positions[vertex*3] = float32(vertex%10) * 0.1
		positions[vertex*3+1] = float32(vertex/10) * 0.1
	}
	blend := make([]byte, vertexCount*16)
	for vertex := range vertexCount {
		bone := byte(5)
		if vertex >= 200 {
			bone = 3
		}
		blend[vertex*16] = bone
		blend[vertex*16+8] = 255
	}
	channel := 0
	label := "Test"
	draft := analyzeTouchComponentBones(component, positions, blend, 16, []TouchBoneZoneSelection{{BoneID: 5, Channel: &channel, Label: &label}}, [2]float64{0.01, 1}, 1)
	if !draft.Interactive || len(draft.Zones) != 1 || draft.Zones[0].Channel != 0 || draft.Zones[0].Source != "bone" || draft.Zones[0].Label != "Test" || len(draft.Zones[0].Seeds) != 200 {
		t.Fatalf("draft = %#v", draft)
	}

	empty := analyzeTouchComponentBones(component, positions, nil, 0, []TouchBoneZoneSelection{{BoneID: 5, Channel: &channel}}, [2]float64{0.01, 1}, 1)
	if empty.Interactive || len(empty.Zones) != 0 || len(empty.Warnings) == 0 {
		t.Fatalf("no-blend = %#v", empty)
	}

	none := analyzeTouchComponentBones(component, positions, blend, 16, nil, [2]float64{0.01, 1}, 1)
	if none.Interactive || len(none.Zones) != 0 {
		t.Fatalf("no-selection = %#v", none)
	}

	gradeC := component
	gradeC.SupportGrade = "C"
	unsupported := analyzeTouchComponentBones(gradeC, positions, blend, 16, []TouchBoneZoneSelection{{BoneID: 5, Channel: &channel}}, [2]float64{0.01, 1}, 1)
	if unsupported.Interactive || !strings.Contains(strings.Join(unsupported.Warnings, " "), "grade is C") {
		t.Fatalf("grade C = %#v", unsupported)
	}

	outside := analyzeTouchComponentBones(component, positions, blend, 16, []TouchBoneZoneSelection{{BoneID: 5, Channel: &channel}}, [2]float64{0.5, 0.6}, 1)
	if outside.Interactive || len(outside.Zones) != 0 {
		t.Fatalf("threshold miss = %#v", outside)
	}
}

func writeTouchTestMod(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "Meshes"), 0755); err != nil {
		t.Fatal(err)
	}
	ini := `[Constants]

[Present]

[ResourceBodyPosition]
stride = 40
filename = Meshes/body-position.buf

[ResourceBodyIndex]
format = DXGI_FORMAT_R32_UINT
filename = Meshes/body-index.buf

[ResourceBodyBlend]
stride = 16
filename = Meshes/body-blend.buf

[TextureOverrideBodyPosition]
vb0 = ResourceBodyPosition
$active = 1

[TextureOverrideBodyIB]
hash = deadbeef
ib = ResourceBodyIndex
drawindexed = 6, 0, 0
`
	if err := os.WriteFile(filepath.Join(root, "mod.ini"), []byte(ini), 0600); err != nil {
		t.Fatal(err)
	}
	positions := make([]byte, 4*40)
	coords := [][3]float32{{-.2, 0, 0}, {.2, 0, 0}, {-.2, 0, .4}, {.2, 0, .4}}
	for vertex, position := range coords {
		base := vertex * 40
		for axis, value := range position {
			binary.LittleEndian.PutUint32(positions[base+axis*4:], math.Float32bits(value))
		}
		binary.LittleEndian.PutUint32(positions[base+12:], math.Float32bits(0))
		binary.LittleEndian.PutUint32(positions[base+16:], math.Float32bits(1))
		binary.LittleEndian.PutUint32(positions[base+20:], math.Float32bits(0))
	}
	if err := os.WriteFile(filepath.Join(root, "Meshes", "body-position.buf"), positions, 0600); err != nil {
		t.Fatal(err)
	}
	indices := make([]byte, 24)
	for i, value := range []uint32{0, 1, 2, 2, 1, 3} {
		binary.LittleEndian.PutUint32(indices[i*4:], value)
	}
	if err := os.WriteFile(filepath.Join(root, "Meshes", "body-index.buf"), indices, 0600); err != nil {
		t.Fatal(err)
	}
	blend := make([]byte, 4*16)
	for vertex := range 4 {
		blend[vertex*16] = 5
		blend[vertex*16+8] = 255
	}
	if err := os.WriteFile(filepath.Join(root, "Meshes", "body-blend.buf"), blend, 0600); err != nil {
		t.Fatal(err)
	}
}
