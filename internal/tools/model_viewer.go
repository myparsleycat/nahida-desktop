package tools

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"nahida.live/desktop/internal/infra"
)

type ModelViewerDNFClause struct {
	Var    string `json:"var"`
	Value  string `json:"value"`
	Negate bool   `json:"negate"`
}

type ModelViewerDNF [][]ModelViewerDNFClause

type ModelViewerTextureVariant struct {
	Conditions ModelViewerDNF `json:"conditions"`
	TexKey     string         `json:"texKey"`
}

type ModelViewerShapeTarget struct {
	Var             string `json:"var"`
	PositionsURL    string `json:"positionsUrl"`
	Mode            string `json:"mode,omitempty"`
	LowPositionsURL string `json:"lowPositionsUrl,omitempty"`
}

type ModelViewerPositionVariant struct {
	Conditions  ModelViewerDNF `json:"conditions"`
	SourceURL   string         `json:"sourceUrl"`
	Stride      int            `json:"stride"`
	SourceBytes int64          `json:"sourceBytes"`
}

type ModelViewerMeshTransport struct {
	ID                  string                       `json:"id"`
	Component           string                       `json:"component"`
	PositionsURL        string                       `json:"positionsUrl"`
	NormalsURL          string                       `json:"normalsUrl,omitempty"`
	TangentsURL         string                       `json:"tangentsUrl,omitempty"`
	UVsURL              string                       `json:"uvsUrl,omitempty"`
	IndicesURL          string                       `json:"indicesUrl"`
	SourceIndicesURL    string                       `json:"sourceIndicesUrl,omitempty"`
	Conditions          ModelViewerDNF               `json:"conditions"`
	TexKey              *string                      `json:"texKey"`
	TextureVariants     []ModelViewerTextureVariant  `json:"textureVariants"`
	NormalMapKey        *string                      `json:"normalMapKey"`
	NormalMapVariants   []ModelViewerTextureVariant  `json:"normalMapVariants"`
	LightMapKey         *string                      `json:"lightMapKey"`
	LightMapVariants    []ModelViewerTextureVariant  `json:"lightMapVariants"`
	MaterialMapKey      *string                      `json:"materialMapKey"`
	MaterialMapVariants []ModelViewerTextureVariant  `json:"materialMapVariants"`
	ShapeTargets        []ModelViewerShapeTarget     `json:"shapeTargets"`
	PositionVariants    []ModelViewerPositionVariant `json:"positionVariants"`
}

type ModelViewerTextureTransport struct {
	URL  string `json:"url"`
	Role string `json:"role"`
}

type ModelViewerVariableValue struct {
	Value any    `json:"value"`
	Label string `json:"label"`
}
type ModelViewerMenuGuard struct {
	Var   string `json:"var"`
	Op    string `json:"op"`
	Value string `json:"value"`
}
type ModelViewerMenuEffect struct {
	When  *ModelViewerMenuGuard `json:"when,omitempty"`
	Var   string                `json:"var"`
	Value string                `json:"value"`
}
type ModelViewerVariable struct {
	ID            string                     `json:"id"`
	Label         string                     `json:"label"`
	DefaultValue  any                        `json:"defaultValue"`
	Values        []ModelViewerVariableValue `json:"values"`
	Order         int                        `json:"order"`
	Slot          int                        `json:"slot,omitempty"`
	IconPath      string                     `json:"iconPath,omitempty"`
	ControlType   string                     `json:"controlType,omitempty"`
	Slider        *ModelViewerSlider         `json:"slider,omitempty"`
	Effects       []ModelViewerMenuEffect    `json:"effects,omitempty"`
	alwaysVisible bool
}
type ModelViewerStateRule struct {
	Var        string         `json:"var"`
	Value      string         `json:"value"`
	Conditions ModelViewerDNF `json:"conditions"`
}
type ModelViewerAnimationFrame struct {
	Index  int            `json:"index"`
	Time   float64        `json:"time"`
	Values map[string]any `json:"values"`
}
type ModelViewerAnimationClip struct {
	ID          string                      `json:"id"`
	Label       string                      `json:"label"`
	VariableIDs []string                    `json:"variableIds"`
	FPS         float64                     `json:"fps"`
	FrameStart  int                         `json:"frameStart"`
	FrameEnd    int                         `json:"frameEnd"`
	Loop        bool                        `json:"loop"`
	Frames      []ModelViewerAnimationFrame `json:"frames"`
}

type ModelViewerTransport struct {
	MemorySessionID string                                 `json:"memorySessionId"`
	INIPath         string                                 `json:"iniPath"`
	ModPath         string                                 `json:"modPath"`
	Name            string                                 `json:"name"`
	MaterialProfile string                                 `json:"materialProfile,omitempty"`
	Meshes          []ModelViewerMeshTransport             `json:"meshes"`
	Textures        map[string]ModelViewerTextureTransport `json:"textures"`
	Variables       []ModelViewerVariable                  `json:"variables"`
	DefaultState    map[string]any                         `json:"defaultState"`
	StateRules      []ModelViewerStateRule                 `json:"stateRules"`
	UIAssets        ModelViewerUIAssets                    `json:"uiAssets"`
	Animations      []ModelViewerAnimationClip             `json:"animations"`
}

type modelViewerDirectMesh struct {
	id, component, sectionName, ibName string
	positionFile                       string
	geometry                           *modelViewerGeometry
	conditions                         ModelViewerDNF
	textureAssignments                 []modelViewerDirectTextureAssignment
	positionAssignments                []modelViewerDirectPositionAssignment
	textureAuthored                    bool
	textureDefaultFile                 string
	nonDiffuseTextureFiles             []string
	indexCount                         int
}

type modelViewerDirectTextureAssignment struct {
	role       string
	resource   string
	file       string
	authored   bool
	conditions ModelViewerDNF
}

type modelViewerDirectPositionAssignment struct {
	conditions  ModelViewerDNF
	sourcePath  string
	stride      int
	sourceBytes int64
}

type modelViewerSession struct {
	modPath string
}

type modelViewerTextureSettings struct {
	TextureFormat   string
	JPEGQuality     int
	MaterialProfile string
}

type modelViewerTexturePayload struct {
	Key      string
	Role     string
	Bytes    []byte
	MIMEType string
}

type modelViewerMeshPayload struct {
	Positions         []float32
	Normals           []float32
	Tangents          []float32
	UVs               []float32
	Indices           []uint32
	ShapePositions    [][]float32
	ShapeLowPositions [][]float32
	SourceIndices     []uint32
	PositionSources   []modelViewerDirectPositionAssignment
}

var modelViewerFreeOSMemory = debug.FreeOSMemory

func (t *Tools) LoadModViewer(ctx context.Context, modPath string) (transport ModelViewerTransport, err error) {
	startedAt := time.Now()
	if t.log != nil {
		t.log.Info("Starting model viewer load", "StaticGlb.loadForViewer")
	}
	defer func() {
		if t.log == nil {
			return
		}
		if err != nil {
			err = infra.ReportError(t.log, err, "StaticGlb.loadForViewer", infra.Diagnostic{Operation: "load-model-viewer", Fields: map[string]any{"message": fmt.Sprintf("Model viewer load failed after %dms", time.Since(startedAt).Milliseconds()), "elapsedMs": time.Since(startedAt).Milliseconds(), "path": modPath}})
			return
		}
		t.log.Info(fmt.Sprintf("Completed model viewer load in %dms (meshes=%d)", time.Since(startedAt).Milliseconds(), len(transport.Meshes)), "StaticGlb.loadForViewer")
	}()
	if t.protocol == nil {
		return ModelViewerTransport{}, fmt.Errorf("protocol service is unavailable")
	}
	requestedPath := modPath
	folder, absErr := filepath.Abs(requestedPath)
	if absErr != nil {
		return ModelViewerTransport{}, absErr
	}
	discoveryStartedAt := time.Now()
	diagnostics := &infra.DiagnosticBatch{}
	defer diagnostics.Report(t.log, "Tools", "model-viewer-discovery")
	iniPaths, discoverErr := discoverModelViewerActiveINIs(folder, diagnostics.Add)
	if discoverErr != nil {
		return ModelViewerTransport{}, discoverErr
	}
	if t.log != nil {
		t.log.Info(fmt.Sprintf("INI discovery completed in %dms (inis=%d)", time.Since(discoveryStartedAt).Milliseconds(), len(iniPaths)), "StaticGlb.loadForViewer")
	}
	if len(iniPaths) == 0 {
		return ModelViewerTransport{}, contractError("No active .ini files found in this folder.")
	}
	budget, budgetErr := newModelViewerLoadBudget(folder)
	if budgetErr != nil {
		return ModelViewerTransport{}, budgetErr
	}
	const assetPath = ""
	settings := modelViewerTextureSettings{TextureFormat: "jpeg-safe", JPEGQuality: 85}
	sessionID := t.protocol.CreateMemorySession()
	keep := false
	defer func() {
		if !keep {
			t.protocol.CleanupMemorySession(sessionID)
		}
	}()
	transport = ModelViewerTransport{
		MemorySessionID: sessionID,
		INIPath:         iniPaths[0],
		ModPath:         requestedPath,
		Name:            filepath.Base(strings.TrimRight(requestedPath, `\/`)),
		Meshes:          []ModelViewerMeshTransport{},
		Textures:        make(map[string]ModelViewerTextureTransport),
		Variables:       []ModelViewerVariable{},
		DefaultState:    make(map[string]any),
		StateRules:      []ModelViewerStateRule{},
		UIAssets:        ModelViewerUIAssets{},
		Animations:      []ModelViewerAnimationClip{},
	}
	if requestedPath == "" {
		transport.Name = ""
	}
	var allSections []modINISection
	var allShapeKeys []modelViewerShapeKey
	bufferCache := newModelViewerBufferCache()
	variableNames := make(map[string]modelViewerVariableName)
	meshPayloads := make([]modelViewerMeshPayload, 0)
	texturePayloads := make(map[string]modelViewerTexturePayload)
	type iniGeometryScan struct {
		sections  []modINISection
		resources []modelViewerResource
	}
	var iniGeometryScans []iniGeometryScan
	var iniParseMs, iniValidateMs, iniReferencedMs, meshBuildMs, meshPayloadMs, postProcessMs, payloadWriteMs int64
	meshTiming := &modelViewerMeshBuildTiming{}
	var stageStartedAt time.Time
	var textureWorks []modelViewerINITextureWork
	multi := len(iniPaths) > 1
	for iniIndex, iniPath := range iniPaths {
		parsed, readErr := readModelViewerINI(iniPath)
		if readErr != nil {
			return ModelViewerTransport{}, readErr
		}
		prefix, _ := modelViewerINIScope(iniPath, folder, multi)
		sections, scopedNames := scopeModelViewerSections(parsed.Sections, iniIndex, prefix)
		for key, value := range scopedNames {
			variableNames[key] = value
		}
		rebaseModelViewerResources(sections, iniPath, folder)
		for _, resource := range sanitizeModelViewerResourcePaths(sections, folder, folder) {
			if t.log != nil {
				t.log.Warn("Skipped unsafe Model Viewer resource path: "+resource, "StaticGlb.loadForViewer")
			}
		}
		resources := collectModelViewerResources(sections)
		iniGeometryScans = append(iniGeometryScans, iniGeometryScan{sections: sections, resources: resources})
		stageStartedAt = time.Now()
		referenced := collectModelViewerReferencedResources(sections)
		iniReferencedMs += time.Since(stageStartedAt).Milliseconds()
		stageStartedAt = time.Now()
		if validationErr := budget.validateReferencedResources(folder, resources, referenced); validationErr != nil {
			return ModelViewerTransport{}, validationErr
		}
		iniValidateMs += time.Since(stageStartedAt).Milliseconds()
		allSections = append(allSections, sections...)
		iniParseMs += time.Since(stageStartedAt).Milliseconds()
		stageStartedAt = time.Now()
		meshes, textureBindings, resources, shapeKeys, buildErr := buildModelViewerDirectMeshesAt(iniPath, folder, assetPath, sections, bufferCache, meshTiming)
		if buildErr != nil {
			return ModelViewerTransport{}, buildErr
		}
		allShapeKeys = append(allShapeKeys, shapeKeys...)
		meshBuildMs += time.Since(stageStartedAt).Milliseconds()
		textureWorks = append(textureWorks, modelViewerINITextureWork{
			meshes:   meshes,
			bindings: textureBindings,
			shapes:   shapeKeys,
			jobs:     collectModelViewerTextureJobs(len(textureWorks), folder, resources, textureBindings, meshes),
		})
	}
	// Mesh payloads own the extracted attributes from this point onward. Drop
	// interleaved vertex buffers and geometry caches before texture encoding so
	// the two peak-memory phases do not overlap.
	bufferCache.releaseGeometryScratch()
	runtime.GC()
	textureJobs := make([]modelViewerTextureJob, 0)
	for _, work := range textureWorks {
		textureJobs = append(textureJobs, work.jobs...)
	}
	transport.MaterialProfile = detectModelViewerMaterialProfile(allSections)
	settings.MaterialProfile = transport.MaterialProfile
	texturesByBatch, textureStats := runModelViewerTextureJobs(ctx, settings, len(textureWorks), textureJobs)
	stageStartedAt = time.Now()
	for batchIndex, work := range textureWorks {
		textures := texturesByBatch[batchIndex]
		for _, value := range textures {
			if value.Key != "" {
				texturePayloads[value.Key] = value
			}
		}
		for _, mesh := range work.meshes {
			item, payload := buildModelViewerDirectMeshPayload(mesh, work.bindings, textures, work.shapes, bufferCache)
			transport.Meshes = append(transport.Meshes, item)
			meshPayloads = append(meshPayloads, payload)
		}
	}
	bufferCache.releaseAll()
	runtime.GC()
	meshPayloadMs = time.Since(stageStartedAt).Milliseconds()
	if t.log != nil {
		t.log.Info(fmt.Sprintf("Texture encoding completed in %dms (textures=%d)", textureStats.TotalWallMs, textureStats.LogicalTextures), "StaticGlb.loadForViewer")
		t.log.Info(fmt.Sprintf("Texture preparation detail: jobs=%d paths=%d contents=%d decodes=%d encodes=%d hashBytes=%d hash=%dms prepare=%dms", textureStats.Jobs, textureStats.UniquePaths, textureStats.UniqueContents, textureStats.Decodes, textureStats.Encodes, textureStats.HashBytes, textureStats.HashWallMs, textureStats.PrepareWallMs), "StaticGlb.loadForViewer")
		t.log.Info(fmt.Sprintf("INI parse detail: referenced=%dms validate=%dms (total %dms)", iniReferencedMs, iniValidateMs, iniParseMs), "StaticGlb.loadForViewer")
		t.log.Info(fmt.Sprintf("Mesh build detail: scan=%dms(%d recs) setup=%dms geometry=%dms(%d) overrides=%dms attach=%dms normalize=%dms legacy=%dms(groups=%dms condScan=%dms prepare=%dms extract=%dms)", meshTiming.ScanMs, meshTiming.Records, meshTiming.SetupMs, meshTiming.GeometryMs, meshTiming.Geometries, meshTiming.OverridesMs, meshTiming.AttachMs, meshTiming.NormalizeMs, meshTiming.LegacyMs, meshTiming.GroupsMs, meshTiming.LegacyScanMs, meshTiming.LegacyPrepareMs, meshTiming.LegacyExtractMs), "StaticGlb.loadForViewer")
	}
	if len(transport.Meshes) == 0 {
		hasGeometryGroups := false
		for _, scan := range iniGeometryScans {
			if modelViewerHasGeometryGroup(scan.sections, scan.resources) {
				hasGeometryGroups = true
				break
			}
		}
		if !hasGeometryGroups {
			return ModelViewerTransport{}, contractError(fmt.Sprintf("No mesh geometry found across %d ini file(s).", len(iniPaths)))
		}
		return ModelViewerTransport{}, contractError("No mesh data could be extracted (buffer files missing?).")
	}
	stageStartedAt = time.Now()
	defaults := collectModelViewerDefaultVariables(allSections)
	bindings := collectModelViewerSlotBindings(allSections, defaults)
	animations := detectModelViewerPresentAnimations(allSections, defaults, bindings)
	stateRules := extractModelViewerDirectStateRules(allSections, defaults)
	variables := prependModelViewerShapeVariables(buildModelViewerDirectVariables(allSections, bindings, defaults), allShapeKeys, defaults)
	tracked := make(map[string]bool)
	for _, variable := range variables {
		tracked[modelViewerNormalizeKey(variable.ID)] = true
		for _, effect := range variable.Effects {
			tracked[modelViewerNormalizeKey(effect.Var)] = true
		}
	}
	for _, rule := range stateRules {
		tracked[modelViewerNormalizeKey(rule.Var)] = true
	}
	for _, clip := range animations {
		for _, id := range clip.VariableIDs {
			tracked[modelViewerNormalizeKey(id)] = true
		}
	}
	normalizeModelViewerTransportConditions(&transport, tracked)
	gating := modelViewerDirectGatingVariables(transport.Meshes, stateRules)
	animationVars := make(map[string]bool)
	for _, clip := range animations {
		for _, id := range clip.VariableIDs {
			animationVars[id] = true
		}
	}
	for _, variable := range variables {
		if !animationVars[variable.ID] && (variable.alwaysVisible || modelViewerVariableIsGating(variable, gating)) {
			transport.Variables = append(transport.Variables, variable)
		}
		transport.DefaultState[variable.ID] = variable.DefaultValue
	}
	for key, value := range defaults {
		transport.DefaultState[key] = value
	}
	for _, prepared := range animations {
		clip := ModelViewerAnimationClip{ID: prepared.ID, Label: prepared.Label, VariableIDs: prepared.VariableIDs, FPS: normalizeModelViewerAnimationFPS(prepared.FPS), FrameStart: prepared.FrameStart, FrameEnd: prepared.FrameEnd, Loop: prepared.Loop}
		for _, frame := range prepared.Frames {
			clip.Frames = append(clip.Frames, ModelViewerAnimationFrame(frame))
		}
		transport.Animations = append(transport.Animations, clip)
		for _, id := range clip.VariableIDs {
			if _, ok := transport.DefaultState[id]; !ok {
				transport.DefaultState[id] = float64(clip.FrameStart)
			}
		}
	}
	for _, rule := range stateRules {
		if !animationVars[rule.Var] {
			transport.StateRules = append(transport.StateRules, rule)
			if _, ok := transport.DefaultState[rule.Var]; !ok {
				transport.DefaultState[rule.Var] = rule.Value
			}
		}
	}
	remapModelViewerTransportVariables(&transport, variableNames)
	normalizeModelViewerTransportValueTypes(&transport)
	postProcessMs = time.Since(stageStartedAt).Milliseconds()
	stageStartedAt = time.Now()
	if writeErr := writeModelViewerPayload(t, sessionID, &transport, meshPayloads, texturePayloads); writeErr != nil {
		return ModelViewerTransport{}, writeErr
	}
	t.modelViewerMu.Lock()
	t.modelViewerSessions[sessionID] = &modelViewerSession{modPath: requestedPath}
	t.modelViewerMu.Unlock()
	keep = true
	payloadWriteMs = time.Since(stageStartedAt).Milliseconds()
	if t.log != nil {
		t.log.Info(fmt.Sprintf("Load stages: iniParse=%dms meshBuild=%dms meshPayload=%dms post=%dms payloadWrite=%dms", iniParseMs, meshBuildMs, meshPayloadMs, postProcessMs, payloadWriteMs), "StaticGlb.loadForViewer")
	}
	return transport, nil
}

func normalizeModelViewerTransportConditions(transport *ModelViewerTransport, tracked map[string]bool) {
	if transport == nil {
		return
	}
	for meshIndex := range transport.Meshes {
		mesh := &transport.Meshes[meshIndex]
		mesh.Conditions = normalizeModelViewerDNFWithTracked(mesh.Conditions, tracked)
		for _, variants := range [][]ModelViewerTextureVariant{
			mesh.TextureVariants,
			mesh.NormalMapVariants,
			mesh.LightMapVariants,
			mesh.MaterialMapVariants,
		} {
			for variantIndex := range variants {
				variants[variantIndex].Conditions = normalizeModelViewerDNFWithTracked(variants[variantIndex].Conditions, tracked)
			}
		}
		for variantIndex := range mesh.PositionVariants {
			mesh.PositionVariants[variantIndex].Conditions = normalizeModelViewerDNFWithTracked(mesh.PositionVariants[variantIndex].Conditions, tracked)
		}
	}
}

func (t *Tools) CleanupModelViewer(_ context.Context, memorySessionID string) (bool, error) {
	memorySessionID = strings.TrimSpace(memorySessionID)
	if memorySessionID == "" {
		return false, nil
	}
	t.modelViewerMu.Lock()
	_, exists := t.modelViewerSessions[memorySessionID]
	if exists {
		delete(t.modelViewerSessions, memorySessionID)
	}
	lastSession := exists && len(t.modelViewerSessions) == 0
	t.modelViewerMu.Unlock()
	if exists && t.protocol != nil {
		t.protocol.CleanupMemorySession(memorySessionID)
	}
	if lastSession {
		startedAt := time.Now()
		modelViewerFreeOSMemory()
		if t.log != nil {
			t.log.Info(fmt.Sprintf("Released model viewer memory in %dms (session=%s)", time.Since(startedAt).Milliseconds(), memorySessionID), "StaticGlb.cleanupViewer")
		}
	}
	return exists, nil
}

func (t *Tools) shutdownModelViewer() error {
	if t == nil {
		return nil
	}
	t.modelViewerMu.Lock()
	ids := make([]string, 0, len(t.modelViewerSessions))
	for id := range t.modelViewerSessions {
		ids = append(ids, id)
	}
	clear(t.modelViewerSessions)
	t.modelViewerMu.Unlock()
	if t.protocol != nil {
		for _, id := range ids {
			t.protocol.CleanupMemorySession(id)
		}
	}
	return nil
}

func modelViewerDirectGatingVariables(meshes []ModelViewerMeshTransport, _ []ModelViewerStateRule) map[string]bool {
	output := make(map[string]bool)
	addDNF := func(dnf ModelViewerDNF) {
		for _, group := range dnf {
			for _, clause := range group {
				output[modelViewerNormalizeKey(clause.Var)] = true
			}
		}
	}
	for _, mesh := range meshes {
		addDNF(mesh.Conditions)
		for _, variants := range [][]ModelViewerTextureVariant{mesh.TextureVariants, mesh.NormalMapVariants, mesh.LightMapVariants, mesh.MaterialMapVariants} {
			for _, variant := range variants {
				addDNF(variant.Conditions)
			}
		}
		for _, variant := range mesh.PositionVariants {
			addDNF(variant.Conditions)
		}
	}
	return output
}

func modelViewerVariableIsGating(variable ModelViewerVariable, gating map[string]bool) bool {
	return gating[modelViewerNormalizeKey(variable.ID)]
}

func buildModelViewerDirectMeshes(iniPath, assetPath string, sections []modINISection) ([]modelViewerDirectMesh, []modelViewerTextureBinding, []modelViewerResource, []modelViewerShapeKey, error) {
	return buildModelViewerDirectMeshesAt(iniPath, filepath.Dir(iniPath), assetPath, sections, newModelViewerBufferCache(), nil)
}

func buildModelViewerDirectMeshesAt(iniPath, modDir, assetPath string, sections []modINISection, cache *modelViewerBufferCache, timing *modelViewerMeshBuildTiming) ([]modelViewerDirectMesh, []modelViewerTextureBinding, []modelViewerResource, []modelViewerShapeKey, error) {
	resources := collectModelViewerResources(sections)
	variables := collectModelViewerDefaultVariables(sections)
	textures := collectModelViewerTextureBindings(sections, variables)
	scanned, scanErr := buildModelViewerDirectScannedMeshesAt(iniPath, modDir, sections, variables, cache, timing)
	if scanErr != nil {
		return nil, nil, nil, nil, scanErr
	}
	if len(scanned) > 0 {
		return scanned, textures, resources, collectModelViewerShapeKeys(sections, resources, modDir), nil
	}
	// Electron's component resolver can still construct a draw group when an
	// override supplies only an IB and the vertex buffers are discoverable by
	// resource family. Keep this path solely for that implicit-group behavior.
	legacyStartedAt := time.Now()
	layoutName := detectModelViewerLayout(sections, resources)
	groupsStartedAt := time.Now()
	groups, err := collectModelViewerBufferGroups(modDir, layoutName, resources, cache, nil)
	if timing != nil {
		timing.GroupsMs += time.Since(groupsStartedAt).Milliseconds()
	}
	if err != nil {
		return nil, nil, nil, nil, err
	}
	conditionVariables := modelViewerDirectConditionVariables(sections, variables)
	legacyScanStartedAt := time.Now()
	resourceConditions := collectModelViewerDirectResourceConditions(sections, variables)
	draws := collectModelViewerDrawBindings(sections, variables)
	ibs := collectModelViewerIBResources(resources, groups, sections, variables, textures, draws, true)
	if timing != nil {
		timing.LegacyScanMs += time.Since(legacyScanStartedAt).Milliseconds()
	}
	var output []modelViewerDirectMesh
	type legacyPreparedDraw struct {
		section string
		draw    modelViewerDrawInstruction
	}
	type legacyPreparedIB struct {
		ib      modelViewerIbResource
		group   *modelViewerBufferGroup
		layout  modelViewerFmtLayout
		indices []uint32
		draws   []legacyPreparedDraw
	}
	var prepared []legacyPreparedIB
	drawTotal := 0
	legacyPrepareStartedAt := time.Now()
	for _, ib := range ibs {
		var group *modelViewerBufferGroup
		for index := range groups {
			if modelViewerKeyMatches(groups[index].Key, ib.Key, true) {
				group = &groups[index]
				break
			}
		}
		if group == nil {
			for index := range groups {
				if modelViewerKeyMatches(groups[index].Key, ib.Key, false) {
					group = &groups[index]
					break
				}
			}
		}
		if group == nil {
			continue
		}
		localFMT := filepath.Join(modDir, strings.TrimSuffix(filepath.Base(ib.Filename), filepath.Ext(ib.Filename))+".fmt")
		fmtKey := fmt.Sprintf("%s|%s|%s|%d|%s|%s", modDir, assetPath, ib.Filename, group.Stride, layoutName, ib.Format)
		layout, loadErr := cache.fmtLayout(fmtKey, func() (modelViewerFmtLayout, error) {
			if assetPath != "" || regularFile(localFMT) {
				return loadModelViewerFmt(modDir, assetPath, ib, group.Stride, layoutName)
			}
			return modelViewerFmtLayout{}, fmt.Errorf("asset layout path is not configured")
		})
		if loadErr != nil {
			layout, loadErr = inferModelViewerFmtLayout(*group, resources, layoutName, ib.Format)
			if loadErr != nil {
				continue
			}
		}
		ibPath := filepath.Join(modDir, filepath.FromSlash(ib.Filename))
		raw, readErr := cache.read(ibPath)
		if readErr != nil {
			continue
		}
		indices, decodeErr := cache.decodeIndices(ibPath, firstModelViewerString(ib.Format, layout.IndexFormat), raw)
		if decodeErr != nil {
			continue
		}
		var ibDraws []legacyPreparedDraw
		for _, binding := range draws {
			if modelViewerNormalizeKey(binding.IBResourceName) == modelViewerNormalizeKey(ib.Name) {
				for _, draw := range binding.Draws {
					ibDraws = append(ibDraws, legacyPreparedDraw{section: binding.SectionName, draw: draw})
				}
			}
		}
		if len(ibDraws) == 0 {
			ibDraws = append(ibDraws, legacyPreparedDraw{draw: modelViewerDrawInstruction{IBResourceName: ib.Name, IndexCount: len(indices)}})
		}
		drawTotal += len(ibDraws)
		if drawTotal > maxModelViewerDraws {
			return nil, nil, nil, nil, contractError(fmt.Sprintf("Mod has too many draws (%d; limit %d).", drawTotal, maxModelViewerDraws))
		}
		prepared = append(prepared, legacyPreparedIB{ib: ib, group: group, layout: layout, indices: indices, draws: ibDraws})
	}
	if timing != nil {
		timing.LegacyPrepareMs += time.Since(legacyPrepareStartedAt).Milliseconds()
	}
	// Per-draw geometry extraction runs in parallel; results are reassembled in
	// the original ib+draw order so mesh output stays identical to the serial pass.
	type legacyTask struct {
		entry     legacyPreparedIB
		draw      legacyPreparedDraw
		drawIndex int
	}
	var tasks []legacyTask
	for _, entry := range prepared {
		for drawIndex, drawEntry := range entry.draws {
			tasks = append(tasks, legacyTask{entry: entry, draw: drawEntry, drawIndex: drawIndex})
		}
	}
	legacyExtractStartedAt := time.Now()
	taskMeshes := make([]modelViewerDirectMesh, len(tasks))
	taskValid := make([]bool, len(tasks))
	buildLegacyMesh := func(task legacyTask) (modelViewerDirectMesh, bool) {
		end := task.draw.draw.StartIndex + task.draw.draw.IndexCount
		if task.draw.draw.StartIndex < 0 || end > len(task.entry.indices) {
			return modelViewerDirectMesh{}, false
		}
		active := make([]uint32, 0, task.draw.draw.IndexCount)
		for _, index := range task.entry.indices[task.draw.draw.StartIndex:end] {
			value := int64(index) + int64(task.draw.draw.BaseVertex)
			if value >= 0 {
				active = append(active, uint32(value))
			}
		}
		if layoutName == "wwmi" {
			reverseModelViewerTriangleWinding(active)
		}
		geoKey := fmt.Sprintf("%s|%d|%s|%s|%s|%d|%d|%d", strings.Join(task.entry.group.SourceFiles, "|"), task.entry.group.Stride, modelViewerLayoutKey(task.entry.layout), filepath.Join(modDir, filepath.FromSlash(task.entry.ib.Filename)), firstModelViewerString(task.entry.ib.Format, task.entry.layout.IndexFormat), task.draw.draw.StartIndex, task.draw.draw.IndexCount, task.draw.draw.BaseVertex)
		geometry, geometryErr := cache.geometry(geoKey, func() (*modelViewerGeometry, error) {
			geometry, err := extractModelViewerGeometry(task.entry.group.VB, task.entry.group.Stride, task.entry.layout, active, true, false, true, nil)
			if err != nil || geometry == nil {
				return geometry, err
			}
			for offset := 1; offset < len(geometry.Texcoord0); offset += 2 {
				geometry.Texcoord0[offset] = 1 - geometry.Texcoord0[offset]
			}
			return geometry, nil
		})
		if geometryErr != nil || geometry == nil {
			return modelViewerDirectMesh{}, false
		}
		conditions := modelViewerConditionsToDNF(task.draw.draw.Conditions, conditionVariables)
		if assigned, exists := resourceConditions[modelViewerNormalizeKey(task.entry.ib.Name)]; exists {
			conditions = modelViewerDNFAnd(conditions, assigned)
		}
		// Electron names meshes after the TextureOverride section (displayName),
		// not the IB resource key.
		component := task.draw.section
		if component == "" {
			component = task.entry.ib.Name
		}
		id := modelViewerNormalizeKey(filepath.Base(iniPath)) + ":" + modelViewerNormalizeKey(component) + ":" + strconv.Itoa(task.drawIndex)
		positionFile := task.entry.group.VBFilename
		if len(task.entry.group.SourceFiles) > 0 {
			positionFile = task.entry.group.SourceFiles[0]
		}
		return modelViewerDirectMesh{id: id, component: component, sectionName: task.draw.section, ibName: task.entry.ib.Name, positionFile: positionFile, geometry: geometry, conditions: conditions}, true
	}
	if workers := min(len(tasks), runtime.GOMAXPROCS(0)); workers > 1 {
		work := make(chan int)
		var workerGroup sync.WaitGroup
		for range workers {
			workerGroup.Add(1)
			go func() {
				defer workerGroup.Done()
				for taskIndex := range work {
					taskMeshes[taskIndex], taskValid[taskIndex] = buildLegacyMesh(tasks[taskIndex])
				}
			}()
		}
		for taskIndex := range tasks {
			work <- taskIndex
		}
		close(work)
		workerGroup.Wait()
	} else {
		for taskIndex := range tasks {
			taskMeshes[taskIndex], taskValid[taskIndex] = buildLegacyMesh(tasks[taskIndex])
		}
	}
	if timing != nil {
		timing.LegacyExtractMs += time.Since(legacyExtractStartedAt).Milliseconds()
	}
	for taskIndex := range tasks {
		if taskValid[taskIndex] {
			output = append(output, taskMeshes[taskIndex])
		}
	}
	if err := attachModelViewerDirectPositionOverrides(output, sections, resources, modDir, conditionVariables, cache); err != nil {
		return nil, nil, nil, nil, err
	}
	attachFamilyAndStemTextures(output, sections, resources, conditionVariables)
	attachIbComponentDumpTextures(output, resources)
	bindHashImageTextures(output, sections, resources, modDir, conditionVariables)
	attachWwmiDumpTextures(output, resources, modDir)
	for meshIndex := range output {
		output[meshIndex].conditions = normalizeModelViewerDNFWithDomains(output[meshIndex].conditions, conditionVariables)
		for assignmentIndex := range output[meshIndex].textureAssignments {
			output[meshIndex].textureAssignments[assignmentIndex].conditions = normalizeModelViewerDNFWithDomains(output[meshIndex].textureAssignments[assignmentIndex].conditions, conditionVariables)
		}
		for assignmentIndex := range output[meshIndex].positionAssignments {
			output[meshIndex].positionAssignments[assignmentIndex].conditions = normalizeModelViewerDNFWithDomains(output[meshIndex].positionAssignments[assignmentIndex].conditions, conditionVariables)
		}
	}
	if timing != nil {
		timing.LegacyMs += time.Since(legacyStartedAt).Milliseconds()
	}
	return output, textures, resources, collectModelViewerShapeKeys(sections, resources, modDir), nil
}

func collectModelViewerIBResources(resources []modelViewerResource, groups []modelViewerBufferGroup, _ []modINISection, _ map[string]any, textureBindings []modelViewerTextureBinding, drawBindings []modelViewerDrawBinding, _ bool) []modelViewerIbResource {
	keys := sortedModelViewerGroupKeys(groups)
	var output []modelViewerIbResource
	seen := make(map[string]bool)
	for _, resource := range resources {
		filename := strings.TrimSpace(resource.Filename)
		nameKey := strings.ToLower(resource.Name)
		extension := strings.ToLower(filepath.Ext(filename))
		isIndexBuffer := extension == ".ib" || strings.Contains(strings.ToUpper(resource.Format), "UINT") || strings.HasSuffix(nameKey, "ib") || strings.Contains(nameKey, "indexbuffer")
		if filename == "" || !isIndexBuffer {
			continue
		}
		identity := modelViewerNormalizeKey(resource.Name + ":" + filename)
		if seen[identity] {
			continue
		}
		seen[identity] = true
		stem := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
		ib := modelViewerIbResource{Name: resource.Name, Filename: filename, Format: resource.Format, Key: modelViewerBestKeyForIB(stem, resource.Name, keys)}
		for _, binding := range drawBindings {
			if modelViewerNormalizeKey(binding.IBResourceName) == modelViewerNormalizeKey(resource.Name) && binding.OverrideHash != "" {
				ib.OverrideHashes = appendUniqueModelViewer(ib.OverrideHashes, binding.OverrideHash)
			}
		}
		for _, binding := range textureBindings {
			if modelViewerNormalizeKey(binding.IBResourceName) == modelViewerNormalizeKey(resource.Name) && binding.OverrideHash != "" {
				ib.OverrideHashes = appendUniqueModelViewer(ib.OverrideHashes, binding.OverrideHash)
			}
		}
		if len(ib.OverrideHashes) > 0 {
			ib.OverrideHash = ib.OverrideHashes[0]
		}
		output = append(output, ib)
	}
	return output
}

func firstModelViewerString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func inferModelViewerFmtLayout(group modelViewerBufferGroup, resources []modelViewerResource, layoutName, indexFormat string) (modelViewerFmtLayout, error) {
	layout := modelViewerFmtLayout{Stride: group.Stride, Topology: "trianglelist", IndexFormat: indexFormat}
	if layout.IndexFormat == "" {
		layout.IndexFormat = "DXGI_FORMAT_R32_UINT"
	}
	if layoutName == "wwmi" {
		byKind := make(map[string]modelViewerResource)
		for _, resource := range resources {
			typed := parseModelViewerWwmiResourceName(resource.Name)
			if typed == nil || !modelViewerKeyMatches(typed.Key, group.Key, true) {
				continue
			}
			byKind[typed.Kind] = resource
		}
		offset, texcoordOffset := 0, -1
		for _, kind := range []string{"position", "vector", "blend", "color", "texcoord"} {
			resource, exists := byKind[kind]
			if !exists {
				continue
			}
			switch kind {
			case "position":
				layout.Elements = append(layout.Elements, modelViewerFmtElement{SemanticName: "POSITION", Format: "DXGI_FORMAT_R32G32B32_FLOAT", AlignedByteOffset: offset, InputSlotClass: "per-vertex"})
			case "vector":
				layout.Elements = append(layout.Elements, modelViewerFmtElement{SemanticName: "NORMAL", Format: firstModelViewerString(resource.Format, "DXGI_FORMAT_R8G8B8A8_SNORM"), AlignedByteOffset: offset, InputSlotClass: "per-vertex"})
			case "texcoord":
				texcoordOffset = offset
			}
			offset += resource.Stride
		}
		if texcoordOffset >= 0 {
			layout.Elements = append(layout.Elements, modelViewerFmtElement{SemanticName: "TEXCOORD", Format: "DXGI_FORMAT_R16G16_FLOAT", AlignedByteOffset: texcoordOffset, InputSlotClass: "per-vertex"})
		}
	} else {
		positionStride, blendStride, texcoordStride := 0, 0, 0
		for _, resource := range resources {
			typed := parseModelViewerMihoyoResourceName(resource.Name)
			if typed == nil || !modelViewerKeyMatches(typed.Key, group.Key, true) {
				continue
			}
			switch typed.Kind {
			case "position":
				positionStride = resource.Stride
			case "blend":
				blendStride = resource.Stride
			case "texcoord":
				texcoordStride = resource.Stride
			}
		}
		if positionStride == 0 {
			positionStride = group.Stride
		}
		layout.Elements = append(layout.Elements, modelViewerFmtElement{SemanticName: "POSITION", Format: "DXGI_FORMAT_R32G32B32_FLOAT", AlignedByteOffset: 0, InputSlotClass: "per-vertex"})
		if positionStride >= 40 && detectModelViewerPositionFrame(group.VB, group.Stride) {
			layout.Elements = append(layout.Elements, modelViewerFmtElement{SemanticName: "NORMAL", Format: "DXGI_FORMAT_R32G32B32_FLOAT", AlignedByteOffset: 12, InputSlotClass: "per-vertex"}, modelViewerFmtElement{SemanticName: "TANGENT", Format: "DXGI_FORMAT_R32G32B32A32_FLOAT", AlignedByteOffset: 24, InputSlotClass: "per-vertex"})
		}
		if texcoordStride > 0 {
			baseOffset := positionStride + blendStride
			uvOffset, uvFormat := detectModelViewerUVBest(group.VB, group.Stride, baseOffset, texcoordStride)
			layout.Elements = append(layout.Elements, modelViewerFmtElement{SemanticName: "TEXCOORD", Format: uvFormat, AlignedByteOffset: uvOffset, InputSlotClass: "per-vertex"})
		}
	}
	if findModelViewerElement(layout, "POSITION", -1) == nil {
		return layout, fmt.Errorf("could not infer vertex layout for %s", group.Key)
	}
	return layout, nil
}

func buildModelViewerDirectMeshPayload(mesh modelViewerDirectMesh, textures []modelViewerTextureBinding, availableTextures map[string]modelViewerTexturePayload, shapeKeys []modelViewerShapeKey, cache *modelViewerBufferCache) (ModelViewerMeshTransport, modelViewerMeshPayload) {
	item := ModelViewerMeshTransport{ID: mesh.id, Component: mesh.component, Conditions: mesh.conditions, TextureVariants: []ModelViewerTextureVariant{}, NormalMapVariants: []ModelViewerTextureVariant{}, LightMapVariants: []ModelViewerTextureVariant{}, MaterialMapVariants: []ModelViewerTextureVariant{}, ShapeTargets: []ModelViewerShapeTarget{}, PositionVariants: []ModelViewerPositionVariant{}}
	payload := modelViewerMeshPayload{
		Positions:     mesh.geometry.Position,
		Normals:       mesh.geometry.Normal,
		Tangents:      mesh.geometry.Tangent,
		UVs:           mesh.geometry.Texcoord0,
		Indices:       mesh.geometry.Indices,
		SourceIndices: mesh.geometry.SourceIndices,
	}
	if len(mesh.positionAssignments) > 0 {
		for _, assignment := range mesh.positionAssignments {
			if assignment.sourcePath == "" || assignment.stride <= 0 || assignment.sourceBytes <= 0 || len(assignment.conditions) == 0 {
				continue
			}
			item.PositionVariants = append(item.PositionVariants, ModelViewerPositionVariant{Conditions: assignment.conditions, Stride: assignment.stride, SourceBytes: assignment.sourceBytes})
			payload.PositionSources = append(payload.PositionSources, assignment)
		}
	}
	authoredRoles := make(map[string]bool)
	var firstDiffuseKey string
	skippedMissingDiffuse := false
	for _, assignment := range mesh.textureAssignments {
		key := modelViewerAssignmentTextureKey(assignment)
		if key == "" {
			continue
		}
		availableTexture, available := availableTextures[key]
		if !available {
			if fallback := modelViewerNormalizeKey(assignment.resource); fallback != "" {
				if availableTexture, available = availableTextures[fallback]; available {
					key = availableTexture.Key
				} else {
					if assignment.role == "diffuse" {
						skippedMissingDiffuse = true
					}
					continue
				}
			} else {
				if assignment.role == "diffuse" {
					skippedMissingDiffuse = true
				}
				continue
			}
		} else {
			key = availableTexture.Key
		}
		authoredRoles[assignment.role] = true
		if assignment.role == "diffuse" && firstDiffuseKey == "" {
			firstDiffuseKey = key
		}
		if modelViewerDNFIsTrue(assignment.conditions) {
			switch assignment.role {
			case "diffuse":
				item.TexKey = stringPointer(key)
			case "normal_map":
				item.NormalMapKey = stringPointer(key)
			case "light_map":
				item.LightMapKey = stringPointer(key)
			case "material_map":
				item.MaterialMapKey = stringPointer(key)
			}
			continue
		}
		variant := ModelViewerTextureVariant{Conditions: assignment.conditions, TexKey: key}
		switch assignment.role {
		case "diffuse":
			item.TextureVariants = append(item.TextureVariants, variant)
		case "normal_map":
			item.NormalMapVariants = append(item.NormalMapVariants, variant)
		case "light_map":
			item.LightMapVariants = append(item.LightMapVariants, variant)
		case "material_map":
			item.MaterialMapVariants = append(item.MaterialMapVariants, variant)
		}
	}
	if item.TexKey == nil && firstDiffuseKey != "" && !skippedMissingDiffuse {
		// Electron uses the mesh default file as texKey. If that file is missing,
		// remaining conditional variants stay variants only (load.test.ts).
		item.TexKey = stringPointer(firstDiffuseKey)
	}
	for _, binding := range textures {
		if modelViewerNormalizeKey(binding.IBResourceName) != modelViewerNormalizeKey(mesh.ibName) {
			continue
		}
		if mesh.sectionName != "" && !strings.EqualFold(binding.SectionName, mesh.sectionName) {
			continue
		}
		candidates := append([]string(nil), binding.TextureResourceNames...)
		candidates = appendUniqueModelViewer(candidates, binding.DiffuseResourceName)
		bestByRole := make(map[string]string)
		for _, name := range candidates {
			role := classifyModelViewerTextureRole(name)
			existing := bestByRole[role]
			if existing == "" || modelViewerTextureNamePriority(name) > modelViewerTextureNamePriority(existing) {
				bestByRole[role] = name
			}
		}
		for role, name := range bestByRole {
			if authoredRoles[role] {
				continue
			}
			key := modelViewerNormalizeKey(name)
			availableTexture, available := availableTextures[key]
			if !available {
				continue
			}
			key = availableTexture.Key
			switch role {
			case "diffuse":
				item.TexKey = stringPointer(key)
			case "normal_map":
				item.NormalMapKey = stringPointer(key)
			case "light_map":
				item.LightMapKey = stringPointer(key)
			case "material_map":
				item.MaterialMapKey = stringPointer(key)
			}
		}
		break
	}
	for _, shapeKey := range shapeKeys {
		matched := false
		for _, prefix := range shapeKey.TargetMeshPrefixes {
			if modelViewerKeyMatches(prefix, mesh.component, false) {
				matched = true
				break
			}
		}
		if !matched && len(shapeKey.TargetMeshPrefixes) == 0 && shapeKey.BasePath != "" && mesh.positionFile != "" {
			positionPath := mesh.positionFile
			if !filepath.IsAbs(positionPath) {
				positionPath = filepath.Join(filepath.Dir(shapeKey.BasePath), filepath.FromSlash(positionPath))
			}
			matched = samePathFold(shapeKey.BasePath, positionPath)
		}
		if !matched {
			continue
		}
		for _, dimension := range shapeKey.Dimensions {
			var high []float32
			var highErr error
			if dimension.Sparse {
				high, highErr = readModelViewerSparseShapePositions(cache, dimension, mesh.geometry)
			} else {
				high, highErr = readModelViewerShapePositions(cache, dimension.BiggerPath, shapeKey.VertexStride, mesh.geometry.SourceIndices, mesh.geometry.VertexCount)
			}
			low := append([]float32(nil), mesh.geometry.Position...)
			var lowErr error
			if dimension.SmallerPath != "" {
				low, lowErr = readModelViewerShapePositions(cache, dimension.SmallerPath, shapeKey.VertexStride, mesh.geometry.SourceIndices, mesh.geometry.VertexCount)
			}
			if highErr != nil || lowErr != nil {
				continue
			}
			item.ShapeTargets = append(item.ShapeTargets, ModelViewerShapeTarget{Var: dimension.VariableID, Mode: dimension.Mode})
			payload.ShapePositions = append(payload.ShapePositions, high)
			payload.ShapeLowPositions = append(payload.ShapeLowPositions, low)
		}
	}
	return item, payload
}

func writeModelViewerPayload(t *Tools, sessionID string, transport *ModelViewerTransport, meshes []modelViewerMeshPayload, textures map[string]modelViewerTexturePayload) error {
	if t == nil || t.protocol == nil || transport == nil {
		return fmt.Errorf("protocol service is unavailable")
	}
	textureKeys := make([]string, 0, len(textures))
	for key := range textures {
		textureKeys = append(textureKeys, key)
	}
	sort.Strings(textureKeys)
	for _, key := range textureKeys {
		texture := textures[key]
		url, err := t.protocol.StoreMemoryBuffer(sessionID, "tex:"+key, texture.Bytes, texture.MIMEType)
		if err != nil {
			return err
		}
		transport.Textures[key] = ModelViewerTextureTransport{URL: url, Role: texture.Role}
	}
	if len(meshes) != len(transport.Meshes) {
		return fmt.Errorf("model viewer payload mesh count mismatch")
	}
	writeMesh := func(mesh *ModelViewerMeshTransport, payload modelViewerMeshPayload) error {
		write := func(suffix string, data []byte) (string, error) {
			return t.protocol.StoreMemoryBuffer(sessionID, mesh.ID+suffix, data, "application/octet-stream")
		}
		var err error
		mesh.PositionsURL, err = write(".pos", modelViewerFloat32Bytes(payload.Positions))
		if err != nil {
			return err
		}
		mesh.IndicesURL, err = write(".idx", modelViewerUint32Bytes(payload.Indices))
		if err != nil {
			return err
		}
		if !modelViewerSourceIndicesAreIdentity(payload.SourceIndices) {
			mesh.SourceIndicesURL, err = write(".source-idx", modelViewerUint32Bytes(payload.SourceIndices))
			if err != nil {
				return err
			}
		}
		if payload.Normals != nil {
			mesh.NormalsURL, err = write(".normal", modelViewerFloat32Bytes(payload.Normals))
			if err != nil {
				return err
			}
		}
		if payload.Tangents != nil {
			mesh.TangentsURL, err = write(".tangent", modelViewerFloat32Bytes(payload.Tangents))
			if err != nil {
				return err
			}
		}
		if payload.UVs != nil {
			mesh.UVsURL, err = write(".uv", modelViewerFloat32Bytes(payload.UVs))
			if err != nil {
				return err
			}
		}
		if len(payload.ShapePositions) != len(mesh.ShapeTargets) || len(payload.ShapeLowPositions) != len(mesh.ShapeTargets) {
			return fmt.Errorf("model viewer shape payload count mismatch for %s", mesh.ID)
		}
		for targetIndex := range mesh.ShapeTargets {
			target := &mesh.ShapeTargets[targetIndex]
			target.PositionsURL, err = write(fmt.Sprintf(".shape.%d", targetIndex), modelViewerFloat32Bytes(payload.ShapePositions[targetIndex]))
			if err != nil {
				return err
			}
			target.LowPositionsURL, err = write(fmt.Sprintf(".shape.%d.low", targetIndex), modelViewerFloat32Bytes(payload.ShapeLowPositions[targetIndex]))
			if err != nil {
				return err
			}
		}
		if len(payload.PositionSources) != len(mesh.PositionVariants) {
			return fmt.Errorf("model viewer position variant payload count mismatch for %s", mesh.ID)
		}
		for variantIndex := range mesh.PositionVariants {
			mesh.PositionVariants[variantIndex].SourceURL = t.protocol.LocalFileURL(payload.PositionSources[variantIndex].sourcePath, true)
		}
		return nil
	}
	meshErrors := make([]error, len(transport.Meshes))
	if workers := min(len(transport.Meshes), runtime.GOMAXPROCS(0)); workers > 1 {
		work := make(chan int)
		var workerGroup sync.WaitGroup
		for range workers {
			workerGroup.Add(1)
			go func() {
				defer workerGroup.Done()
				for meshIndex := range work {
					meshErrors[meshIndex] = writeMesh(&transport.Meshes[meshIndex], meshes[meshIndex])
				}
			}()
		}
		for meshIndex := range transport.Meshes {
			work <- meshIndex
		}
		close(work)
		workerGroup.Wait()
	} else {
		for meshIndex := range transport.Meshes {
			meshErrors[meshIndex] = writeMesh(&transport.Meshes[meshIndex], meshes[meshIndex])
		}
	}
	for _, err := range meshErrors {
		if err != nil {
			return err
		}
	}
	return nil
}

func modelViewerSourceIndicesAreIdentity(indices []uint32) bool {
	if len(indices) == 0 {
		return true
	}
	for index, source := range indices {
		if source != uint32(index) {
			return false
		}
	}
	return true
}

func readModelViewerShapePositions(cache *modelViewerBufferCache, path string, stride int, sources []uint32, vertexCount int) ([]float32, error) {
	raw, err := cache.read(path)
	if err != nil {
		return nil, err
	}
	if stride <= 0 {
		stride = 40
	}
	if sources == nil {
		sources = make([]uint32, vertexCount)
		for index := range sources {
			sources[index] = uint32(index)
		}
	}
	output := make([]float32, len(sources)*3)
	for index, source := range sources {
		offset := int(source) * stride
		if offset+12 > len(raw) {
			continue
		}
		output[index*3] = math.Float32frombits(binary.LittleEndian.Uint32(raw[offset:]))
		output[index*3+1] = math.Float32frombits(binary.LittleEndian.Uint32(raw[offset+4:]))
		output[index*3+2] = math.Float32frombits(binary.LittleEndian.Uint32(raw[offset+8:]))
	}
	return output, nil
}

const modelViewerTextureConcurrency = 8

type modelViewerINITextureWork struct {
	meshes   []modelViewerDirectMesh
	bindings []modelViewerTextureBinding
	shapes   []modelViewerShapeKey
	jobs     []modelViewerTextureJob
}

type modelViewerTextureJob struct {
	batchIndex   int
	path         string
	resourceName string
	keys         []string
	role         string
	canonicalKey string
}

type modelViewerTextureRunStats struct {
	Jobs            int
	UniquePaths     int
	UniqueContents  int
	LogicalTextures int
	Decodes         int
	Encodes         int
	HashBytes       int64
	HashWallMs      int64
	PrepareWallMs   int64
	TotalWallMs     int64
}

func collectModelViewerTextureJobs(batchIndex int, modDir string, resources []modelViewerResource, bindings []modelViewerTextureBinding, meshes []modelViewerDirectMesh) []modelViewerTextureJob {
	resourceMap := make(map[string]modelViewerResource)
	for _, resource := range resources {
		key := modelViewerNormalizeKey(resource.Name)
		resourceMap[key] = resource
	}
	var resourceNames []string
	bindingRoles := make(map[string]string)
	for _, binding := range bindings {
		for _, name := range appendUniqueModelViewer(binding.TextureResourceNames, binding.DiffuseResourceName) {
			resourceNames = appendUniqueModelViewer(resourceNames, name)
		}
		for key, role := range binding.TextureRoles {
			bindingRoles[key] = role
		}
	}
	for _, mesh := range meshes {
		for _, assignment := range mesh.textureAssignments {
			resourceNames = appendUniqueModelViewer(resourceNames, assignment.resource)
		}
	}
	jobs := make([]modelViewerTextureJob, 0, len(resourceNames))
	seen := make(map[string]bool)
	for _, name := range resourceNames {
		resourceKey := modelViewerNormalizeKey(name)
		if seen[resourceKey] {
			continue
		}
		seen[resourceKey] = true
		resource, ok := resourceMap[resourceKey]
		if !ok || resource.Filename == "" {
			continue
		}
		role := bindingRoles[resourceKey]
		if role == "" {
			role = classifyModelViewerTextureRole(name)
		}
		for _, mesh := range meshes {
			for _, assignment := range mesh.textureAssignments {
				if modelViewerNormalizeKey(assignment.resource) == resourceKey && assignment.role != "" {
					role = assignment.role
				}
			}
		}
		fileKey := modelViewerTextureKey(resource.Filename, role)
		texturePath, pathErr := resolveModelViewerResourcePath(modDir, modDir, resource.Filename)
		if pathErr != nil {
			continue
		}
		keys := []string{resourceKey}
		if fileKey != "" {
			keys = append(keys, fileKey)
			seen[fileKey] = true
		}
		jobs = append(jobs, modelViewerTextureJob{
			batchIndex:   batchIndex,
			path:         texturePath,
			resourceName: name,
			keys:         keys,
			role:         role,
			canonicalKey: fileKey,
		})
	}
	for _, mesh := range meshes {
		for _, assignment := range mesh.textureAssignments {
			key := modelViewerAssignmentTextureKey(assignment)
			if key == "" || seen[key] || assignment.file == "" {
				continue
			}
			texturePath, pathErr := resolveModelViewerResourcePath(modDir, modDir, assignment.file)
			if pathErr != nil {
				continue
			}
			seen[key] = true
			jobs = append(jobs, modelViewerTextureJob{
				batchIndex:   batchIndex,
				path:         texturePath,
				resourceName: assignment.resource,
				keys:         []string{key},
				role:         assignment.role,
				canonicalKey: key,
			})
		}
	}
	return jobs
}

func runModelViewerTextureJobs(ctx context.Context, settings modelViewerTextureSettings, batchCount int, jobs []modelViewerTextureJob) ([]map[string]modelViewerTexturePayload, modelViewerTextureRunStats) {
	outputs := make([]map[string]modelViewerTexturePayload, batchCount)
	for index := range outputs {
		outputs[index] = make(map[string]modelViewerTexturePayload)
	}
	stats := modelViewerTextureRunStats{Jobs: len(jobs)}
	if len(jobs) == 0 {
		return outputs, stats
	}
	startedAt := time.Now()
	type pathGroup struct {
		key         string
		path        string
		jobs        []modelViewerTextureJob
		contentKey  string
		hashedBytes int64
	}
	pathIndexes := make(map[string]int, len(jobs))
	pathGroups := make([]pathGroup, 0, len(jobs))
	for _, job := range jobs {
		pathKey := strings.ToLower(filepath.Clean(job.path))
		index, ok := pathIndexes[pathKey]
		if !ok {
			index = len(pathGroups)
			pathIndexes[pathKey] = index
			pathGroups = append(pathGroups, pathGroup{key: pathKey, path: job.path})
		}
		pathGroups[index].jobs = append(pathGroups[index].jobs, job)
	}
	stats.UniquePaths = len(pathGroups)
	hashStartedAt := time.Now()
	hashWork := make(chan int)
	hashWorkers := min(modelViewerTextureConcurrency, runtime.GOMAXPROCS(0), len(pathGroups))
	if hashWorkers < 1 {
		hashWorkers = 1
	}
	var hashGroup sync.WaitGroup
	for range hashWorkers {
		hashGroup.Add(1)
		go func() {
			defer hashGroup.Done()
			for index := range hashWork {
				group := &pathGroups[index]
				if ctx.Err() != nil {
					group.contentKey = "path:" + group.key
					continue
				}
				hash, size, err := modelViewerTextureFileHash(group.path)
				if err != nil {
					group.contentKey = "path:" + group.key
					continue
				}
				group.contentKey = hash
				group.hashedBytes = size
			}
		}()
	}
	for index := range pathGroups {
		hashWork <- index
	}
	close(hashWork)
	hashGroup.Wait()
	stats.HashWallMs = time.Since(hashStartedAt).Milliseconds()
	for _, group := range pathGroups {
		stats.HashBytes += group.hashedBytes
	}
	type preparedJob struct {
		job     modelViewerTextureJob
		texture *modelViewerPreparedTexture
	}
	type contentGroup struct {
		path     string
		jobs     []modelViewerTextureJob
		prepared []preparedJob
		decodes  int
		encodes  int
	}
	contentIndexes := make(map[string]int, len(pathGroups))
	contentGroups := make([]contentGroup, 0, len(pathGroups))
	for _, path := range pathGroups {
		index, ok := contentIndexes[path.contentKey]
		if !ok {
			index = len(contentGroups)
			contentIndexes[path.contentKey] = index
			contentGroups = append(contentGroups, contentGroup{path: path.path})
		}
		contentGroups[index].jobs = append(contentGroups[index].jobs, path.jobs...)
	}
	stats.UniqueContents = len(contentGroups)
	format := normalizeModelViewerFormat(settings.TextureFormat)
	quality := normalizeJPEGQuality(settings.JPEGQuality)
	type encodeVariant struct {
		invert    bool
		transform modelViewerTextureTransform
		format    string
		quality   int
		profile   string
		role      string
	}
	prepareStartedAt := time.Now()
	prepareWork := make(chan int)
	prepareWorkers := min(modelViewerTextureConcurrency, runtime.GOMAXPROCS(0), len(contentGroups))
	if prepareWorkers < 1 {
		prepareWorkers = 1
	}
	var prepareGroup sync.WaitGroup
	for range prepareWorkers {
		prepareGroup.Add(1)
		go func() {
			defer prepareGroup.Done()
			for index := range prepareWork {
				group := &contentGroups[index]
				if ctx.Err() != nil {
					continue
				}
				group.decodes++
				decoded, err := decodeModelViewerTextureSource(ctx, group.path)
				if err != nil {
					continue
				}
				variants := make(map[encodeVariant]*modelViewerPreparedTexture, 2)
				for _, job := range group.jobs {
					variant := encodeVariant{
						invert:    modelViewerTextureShouldInvertAlpha(job.resourceName, decoded),
						transform: modelViewerTextureTransformFor(settings.MaterialProfile, job.role),
						format:    modelViewerTextureFormatFor(settings.MaterialProfile, job.role, format),
						quality:   quality,
						profile:   settings.MaterialProfile,
						role:      job.role,
					}
					texture, exists := variants[variant]
					if !exists {
						group.encodes++
						texture, err = encodeModelViewerPreparedTexture(decoded, job.path, job.resourceName, variant.transform, variant.format, variant.quality)
						if err != nil {
							texture = nil
						}
						variants[variant] = texture
					}
					if texture != nil {
						group.prepared = append(group.prepared, preparedJob{job: job, texture: texture})
					}
				}
			}
		}()
	}
	for index := range contentGroups {
		prepareWork <- index
	}
	close(prepareWork)
	prepareGroup.Wait()
	stats.PrepareWallMs = time.Since(prepareStartedAt).Milliseconds()
	for _, group := range contentGroups {
		stats.Decodes += group.decodes
		stats.Encodes += group.encodes
		for _, prepared := range group.prepared {
			if prepared.job.batchIndex < 0 || prepared.job.batchIndex >= len(outputs) {
				continue
			}
			item := modelViewerTexturePayload{Key: prepared.job.canonicalKey, Role: prepared.job.role, Bytes: prepared.texture.bytes, MIMEType: prepared.texture.mimeType}
			for _, key := range prepared.job.keys {
				outputs[prepared.job.batchIndex][key] = item
			}
		}
	}
	for _, output := range outputs {
		seen := make(map[string]bool, len(output))
		for _, item := range output {
			key := item.Key
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			stats.LogicalTextures++
		}
	}
	stats.TotalWallMs = time.Since(startedAt).Milliseconds()
	return outputs, stats
}

func modelViewerAssignmentTextureKey(assignment modelViewerDirectTextureAssignment) string {
	if assignment.file != "" && assignment.role != "" {
		return modelViewerTextureKey(assignment.file, assignment.role)
	}
	return modelViewerNormalizeKey(assignment.resource)
}

func classifyModelViewerTextureRole(name string) string {
	key := modelViewerNormalizeKey(name)
	switch {
	case strings.Contains(key, "normal") || strings.Contains(key, "bump"):
		return "normal_map"
	case strings.Contains(key, "light"):
		return "light_map"
	case strings.Contains(key, "material") || strings.Contains(key, "metal") || strings.Contains(key, "rough"):
		return "material_map"
	default:
		return "diffuse"
	}
}

func buildModelViewerDirectVariables(sections []modINISection, bindings []modelViewerSlotBinding, defaults map[string]any) []ModelViewerVariable {
	seen := make(map[string]bool)
	var output []ModelViewerVariable
	sort.SliceStable(bindings, func(i, j int) bool {
		return bindings[i].AlwaysVisible && !bindings[j].AlwaysVisible
	})
	for _, binding := range bindings {
		if seen[binding.Variable] {
			continue
		}
		seen[binding.Variable] = true
		variable := ModelViewerVariable{ID: binding.Variable, Label: humanizeModelViewerLabel(binding.Variable), DefaultValue: defaults[binding.Variable], Order: len(output), Slot: binding.Slot, ControlType: "buttons", alwaysVisible: binding.AlwaysVisible}
		variable.Label, variable.Effects = modelViewerDirectVariableMetadata(sections, binding, variable.Label)
		if variable.DefaultValue == nil && len(binding.Values) > 0 {
			variable.DefaultValue = binding.Values[0]
		}
		for _, value := range binding.Values {
			variable.Values = append(variable.Values, ModelViewerVariableValue{Value: value, Label: modelViewerString(value)})
		}
		if slider := inferModelViewerSlider(binding.Variable, binding.Values, false); slider != nil {
			variable.ControlType = "slider"
			variable.Slider = slider
		}
		output = append(output, variable)
	}
	return output
}

func prependModelViewerShapeVariables(variables []ModelViewerVariable, shapeKeys []modelViewerShapeKey, defaults map[string]any) []ModelViewerVariable {
	seen := make(map[string]bool)
	output := make([]ModelViewerVariable, 0, len(variables))
	for _, shapeKey := range shapeKeys {
		for _, dimension := range shapeKey.Dimensions {
			id := modelViewerNormalizeKey(dimension.VariableID)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			defaultValue, ok := defaults[id]
			if !ok {
				defaultValue = float64(0)
			}
			output = append(output, ModelViewerVariable{
				ID:            id,
				Label:         humanizeModelViewerLabel(id),
				DefaultValue:  defaultValue,
				Values:        []ModelViewerVariableValue{},
				ControlType:   "slider",
				Slider:        &ModelViewerSlider{Min: 0, Max: 1, Step: 0.01},
				alwaysVisible: true,
			})
		}
	}
	for _, variable := range variables {
		id := modelViewerNormalizeKey(variable.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		output = append(output, variable)
	}
	for index := range output {
		output[index].Order = index
	}
	return output
}

func modelViewerDirectVariableMetadata(sections []modINISection, binding modelViewerSlotBinding, fallbackLabel string) (string, []ModelViewerMenuEffect) {
	label := fallbackLabel
	var effects []ModelViewerMenuEffect
	for _, section := range sections {
		if !strings.HasPrefix(strings.ToLower(section.Header), "key") {
			continue
		}
		containsBinding := false
		for _, line := range section.Lines {
			assignment := modelViewerMenuAssignRE.FindStringSubmatch(strings.TrimSpace(line))
			if assignment != nil && modelViewerNormalizeKey(assignment[1]) == binding.Variable && strings.Contains(assignment[2], ",") {
				containsBinding = true
				break
			}
		}
		if !containsBinding {
			continue
		}
		fullName := section.Header
		if strings.EqualFold(section.Header, "Key") {
			fullName += section.Name
		}
		if strings.HasPrefix(strings.ToLower(fullName), "key") && len(fullName) > 3 {
			label = fullName[3:]
		}
		for _, line := range section.Lines {
			assignment := modelViewerMenuAssignRE.FindStringSubmatch(strings.TrimSpace(line))
			if assignment == nil {
				continue
			}
			key, raw := assignment[1], strings.TrimSpace(assignment[2])
			if strings.Contains(raw, ",") || modelViewerNormalizeKey(key) == binding.Variable {
				continue
			}
			value := strings.TrimSpace(raw)
			if value != "" {
				effects = append(effects, ModelViewerMenuEffect{Var: modelViewerNormalizeKey(key), Value: value})
			}
		}
	}
	effects = append(effects, binding.Effects...)
	return label, effects
}

func modelViewerDirectConditionVariables(sections []modINISection, defaults map[string]any) map[string]any {
	output := cloneModelViewerState(defaults)
	bindings := collectModelViewerSlotBindings(sections, defaults)
	for _, binding := range bindings {
		key := "__domain:" + modelViewerNormalizeKey(binding.Variable)
		current, _ := output[key].([]any)
		for _, value := range binding.Values {
			current = appendUniqueModelViewerValue(current, value)
		}
		output[key] = current
	}
	for _, clip := range detectModelViewerPresentAnimations(sections, defaults, bindings) {
		for _, variable := range clip.VariableIDs {
			key := "__domain:" + modelViewerNormalizeKey(variable)
			current, _ := output[key].([]any)
			for frame := clip.FrameStart; frame <= clip.FrameEnd; frame++ {
				current = appendUniqueModelViewerValue(current, float64(frame))
			}
			output[key] = current
		}
	}
	output["__aliases"] = buildModelViewerBoolAliases(sections, output)
	return output
}

func detectModelViewerUVBest(data []byte, vertexStride, baseOffset, texcoordStride int) (int, string) {
	total := 0
	if vertexStride > 0 {
		total = len(data) / vertexStride
	}
	if total == 0 {
		return baseOffset + 4, "DXGI_FORMAT_R16G16_FLOAT"
	}
	type score struct {
		live            bool
		inRange, spread float64
		offset          int
		format          string
	}
	var scores []score
	step := max(1, total/4096)
	for _, relative := range []int{0, 4} {
		for _, format := range []string{"DXGI_FORMAT_R16G16_FLOAT", "DXGI_FORMAT_R32G32_FLOAT"} {
			decoder, decoderErr := resolveModelViewerFormatDecoder(format)
			if decoderErr != nil || decoder.byteSize <= 0 {
				continue
			}
			size := decoder.byteSize
			if relative+size > texcoordStride {
				continue
			}
			var us, vs []float64
			values := make([]float32, max(decoder.components, 2))
			sampled := 0
			for vertex := 0; vertex < total; vertex += step {
				if err := readModelViewerDecoded(data, vertex*vertexStride+baseOffset+relative, decoder, values); err != nil {
					break
				}
				sampled++
				u, v := float64(values[0]), float64(values[1])
				if u >= -.01 && u <= 2 && v >= -.01 && v <= 2 {
					us = append(us, u)
					vs = append(vs, v)
				}
			}
			if sampled == 0 || len(us) == 0 {
				continue
			}
			inRange := float64(len(us)) / float64(sampled)
			if inRange < .95 {
				continue
			}
			minU, maxU, minV, maxV := us[0], us[0], vs[0], vs[0]
			for i := range us {
				minU = min(minU, us[i])
				maxU = max(maxU, us[i])
				minV = min(minV, vs[i])
				maxV = max(maxV, vs[i])
			}
			du, dv := maxU-minU, maxV-minV
			scores = append(scores, score{live: du >= 1e-4 && dv >= 1e-4, inRange: inRange, spread: du + dv, offset: baseOffset + relative, format: format})
		}
	}
	sort.SliceStable(scores, func(i, j int) bool {
		if scores[i].live != scores[j].live {
			return scores[i].live
		}
		if scores[i].inRange != scores[j].inRange {
			return scores[i].inRange > scores[j].inRange
		}
		return scores[i].spread > scores[j].spread
	})
	if len(scores) > 0 {
		return scores[0].offset, scores[0].format
	}
	if texcoordStride >= 8 {
		return baseOffset + 4, "DXGI_FORMAT_R16G16_FLOAT"
	}
	return baseOffset, "DXGI_FORMAT_R16G16_FLOAT"
}

func extractModelViewerDirectStateRules(sections []modINISection, variables map[string]any) []ModelViewerStateRule {
	variables = modelViewerDirectConditionVariables(sections, variables)
	assignmentRE := regexp.MustCompile(`^\$([\w.]+)\s*=\s*(-?\d+(?:\.\d+)?)\s*$`)
	var rules []ModelViewerStateRule
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "Present") {
			continue
		}
		var stack []modelViewerBranchFrame
		for _, raw := range section.Lines {
			line := strings.TrimSpace(raw)
			lower := strings.ToLower(line)
			switch {
			case strings.HasPrefix(lower, "if "):
				expression := strings.TrimSpace(line[3:])
				stack = append(stack, modelViewerBranchFrame{active: []modelViewerConditionClause{{Expression: expression, Expected: true}}, inverse: []modelViewerConditionClause{{Expression: expression, Expected: false}}})
				continue
			case strings.HasPrefix(lower, "elif "), strings.HasPrefix(lower, "else if "):
				expression := strings.TrimSpace(line[5:])
				if strings.HasPrefix(lower, "else if ") {
					expression = strings.TrimSpace(line[8:])
				}
				previous := modelViewerBranchFrame{}
				if len(stack) > 0 {
					previous = stack[len(stack)-1]
					stack = stack[:len(stack)-1]
				}
				stack = append(stack, modelViewerBranchFrame{active: append(append([]modelViewerConditionClause(nil), previous.inverse...), modelViewerConditionClause{Expression: expression, Expected: true}), inverse: append(append([]modelViewerConditionClause(nil), previous.inverse...), modelViewerConditionClause{Expression: expression, Expected: false})})
				continue
			case lower == "else":
				if len(stack) > 0 {
					previous := stack[len(stack)-1]
					stack[len(stack)-1] = modelViewerBranchFrame{active: previous.inverse}
				}
				continue
			case lower == "endif":
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
				continue
			}
			match := assignmentRE.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			var clauses []modelViewerConditionClause
			for _, frame := range stack {
				clauses = append(clauses, frame.active...)
			}
			conditions := modelViewerConditionsToDNF(clauses, variables)
			if len(conditions) == 0 {
				continue
			}
			rules = append(rules, ModelViewerStateRule{Var: modelViewerNormalizeKey(match[1]), Value: match[2], Conditions: conditions})
		}
	}
	return rules
}
