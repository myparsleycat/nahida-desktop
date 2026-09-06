package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"nahida.live/desktop/internal/infra"
)

func (t *Tools) LoadModViewer(ctx context.Context, modPath string) (transport ModelViewerTransport, err error) {
	startedAt := time.Now()
	stage := "discover"
	sessionID := ""
	folder := ""
	if t.log != nil {
		t.log.Info("Starting model viewer load", "StaticGlb.loadForViewer")
	}
	defer func() {
		if t.log == nil {
			return
		}
		if err != nil {
			err = infra.ReportError(t.log, err, "StaticGlb.loadForViewer", infra.Diagnostic{Operation: "load-model-viewer", Fields: map[string]any{"message": fmt.Sprintf("Model viewer load failed after %dms", time.Since(startedAt).Milliseconds()), "elapsedMs": time.Since(startedAt).Milliseconds(), "path": modPath, "resolvedPath": folder, "stage": stage, "memorySessionId": sessionID, "memorySessionCleaned": sessionID != ""}})
			return
		}
		t.log.Info(fmt.Sprintf("Completed model viewer load in %dms (meshes=%d)", time.Since(startedAt).Milliseconds(), len(transport.Meshes)), "StaticGlb.loadForViewer")
	}()
	if err := ctx.Err(); err != nil {
		return ModelViewerTransport{}, err
	}
	if t.protocol == nil {
		return ModelViewerTransport{}, fmt.Errorf("protocol service is unavailable")
	}
	requestedPath := modPath
	var absErr error
	folder, absErr = filepath.Abs(requestedPath)
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
	if err := ctx.Err(); err != nil {
		return ModelViewerTransport{}, err
	}
	budget, budgetErr := newModelViewerLoadBudget(folder)
	if budgetErr != nil {
		return ModelViewerTransport{}, budgetErr
	}
	sessionID = t.protocol.CreateMemorySession()
	keep := false
	defer func() {
		if !keep {
			t.protocol.CleanupMemorySession(sessionID)
		}
	}()
	transport = ModelViewerTransport{
		MemorySessionID:  sessionID,
		INIPath:          iniPaths[0],
		ModPath:          requestedPath,
		Name:             filepath.Base(strings.TrimRight(requestedPath, `\/`)),
		Meshes:           []ModelViewerMeshTransport{},
		Textures:         make(map[string]ModelViewerTextureTransport),
		Variables:        []ModelViewerVariable{},
		DefaultState:     make(map[string]any),
		StateRules:       []ModelViewerStateRule{},
		UIAssets:         ModelViewerUIAssets{},
		Animations:       []ModelViewerAnimationClip{},
		ComputeDeformers: []ModelViewerComputeDeformerTransport{},
	}
	if requestedPath == "" {
		transport.Name = ""
	}
	stage = "prepare-geometry"
	prepared, prepareErr := t.prepareModelViewerGeometry(ctx, folder, iniPaths, budget)
	if prepareErr != nil {
		return ModelViewerTransport{}, prepareErr
	}
	defer prepared.cache.releaseAll()
	var meshPayloadMs, postProcessMs, payloadWriteMs int64
	var stageStartedAt time.Time
	// Mesh payloads own the extracted attributes from this point onward. Drop
	// interleaved vertex buffers and geometry caches before texture encoding so
	// the two peak-memory phases do not overlap.
	prepared.cache.releaseGeometryScratch()
	runtime.GC()
	stage = "prepare-payload"
	stageStartedAt = time.Now()
	meshPayloads, texturePayloads, textureStats, payloadErr := prepareModelViewerPayload(ctx, prepared, &transport)
	if payloadErr != nil {
		return ModelViewerTransport{}, payloadErr
	}
	prepared.cache.releaseAll()
	prepared.textures = nil // Extracted payloads now own the mesh attributes.
	runtime.GC()
	meshPayloadMs = time.Since(stageStartedAt).Milliseconds()
	if t.log != nil {
		t.log.Info(fmt.Sprintf("Texture encoding completed in %dms (textures=%d)", textureStats.TotalWallMs, textureStats.LogicalTextures), "StaticGlb.loadForViewer")
		t.log.Info(fmt.Sprintf("Texture preparation detail: jobs=%d paths=%d contents=%d decodes=%d encodes=%d hashBytes=%d hash=%dms prepare=%dms", textureStats.Jobs, textureStats.UniquePaths, textureStats.UniqueContents, textureStats.Decodes, textureStats.Encodes, textureStats.HashBytes, textureStats.HashWallMs, textureStats.PrepareWallMs), "StaticGlb.loadForViewer")
		t.log.Info(fmt.Sprintf("INI parse detail: referenced=%dms validate=%dms (total %dms)", prepared.referencedMs, prepared.validateMs, prepared.parseMs), "StaticGlb.loadForViewer")
		t.log.Info(fmt.Sprintf("Mesh build detail: scan=%dms(%d recs) setup=%dms geometry=%dms(%d) overrides=%dms attach=%dms normalize=%dms legacy=%dms(groups=%dms condScan=%dms prepare=%dms extract=%dms)", prepared.timing.ScanMs, prepared.timing.Records, prepared.timing.SetupMs, prepared.timing.GeometryMs, prepared.timing.Geometries, prepared.timing.OverridesMs, prepared.timing.AttachMs, prepared.timing.NormalizeMs, prepared.timing.LegacyMs, prepared.timing.GroupsMs, prepared.timing.LegacyScanMs, prepared.timing.LegacyPrepareMs, prepared.timing.LegacyExtractMs), "StaticGlb.loadForViewer")
	}
	if len(transport.Meshes) == 0 {
		if resource := firstUnresolvedModelViewerPositionResource(prepared.scans); resource != "" {
			return ModelViewerTransport{}, contractError(fmt.Sprintf("Position resource Resource%s has no resolvable file-backed source.", resource))
		}
		hasGeometryGroups := false
		for _, scan := range prepared.scans {
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
	if err := ctx.Err(); err != nil {
		return ModelViewerTransport{}, err
	}
	stage = "configure-state"
	stageStartedAt = time.Now()
	transport.ComputeDeformers = prepared.computeDeformers
	configureModelViewerState(&transport, prepared.sections, prepared.shapeKeys, prepared.variableNames, prepared.computeAnimations)
	postProcessMs = time.Since(stageStartedAt).Milliseconds()
	stageStartedAt = time.Now()
	stage = "write-payload"
	if writeErr := writeModelViewerPayload(ctx, t, sessionID, &transport, meshPayloads, texturePayloads); writeErr != nil {
		return ModelViewerTransport{}, writeErr
	}
	t.modelViewerMu.Lock()
	if err := ctx.Err(); err != nil {
		t.modelViewerMu.Unlock()
		return ModelViewerTransport{}, err
	}
	t.modelViewerSessions[sessionID] = &modelViewerSession{modPath: requestedPath}
	t.modelViewerMu.Unlock()
	keep = true
	payloadWriteMs = time.Since(stageStartedAt).Milliseconds()
	if t.log != nil {
		t.log.Info(fmt.Sprintf("Load stages: iniParse=%dms meshBuild=%dms meshPayload=%dms post=%dms payloadWrite=%dms", prepared.parseMs, prepared.buildMs, meshPayloadMs, postProcessMs, payloadWriteMs), "StaticGlb.loadForViewer")
	}
	return transport, nil
}

func firstUnresolvedModelViewerPositionResource(scans []modelViewerGeometryScan) string {
	var unresolved []string
	for _, scan := range scans {
		resources := modelViewerResourceMap(scan.resources)
		for _, section := range scan.sections {
			if !strings.EqualFold(section.Header, "TextureOverride") {
				continue
			}
			for _, raw := range section.Lines {
				left, right, ok := strings.Cut(strings.TrimSpace(strings.SplitN(raw, ";", 2)[0]), "=")
				if !ok || !strings.EqualFold(strings.TrimSpace(left), "vb0") {
					continue
				}
				name := modelViewerResourceToken(right)
				resource, exists := resources[modelViewerNormalizeKey(name)]
				if !exists || resource.Filename != "" {
					continue
				}
				typed := parseModelViewerMihoyoResourceName(resource.Name)
				if typed == nil || typed.Kind != "position" {
					continue
				}
				unresolved = append(unresolved, resource.Name)
			}
		}
	}
	if len(unresolved) == 0 {
		return ""
	}
	sort.Strings(unresolved)
	return unresolved[0]
}

type modelViewerGeometryScan struct {
	sections  []modINISection
	resources []modelViewerResource
}
type modelViewerPreparedGeometry struct {
	sections                                   []modINISection
	shapeKeys                                  []modelViewerShapeKey
	cache                                      *modelViewerBufferCache
	variableNames                              map[string]modelViewerVariableName
	scans                                      []modelViewerGeometryScan
	textures                                   []modelViewerINITextureWork
	computeDeformers                           []ModelViewerComputeDeformerTransport
	computeAnimations                          []modelViewerPreparedAnimationClip
	parseMs, validateMs, referencedMs, buildMs int64
	timing                                     *modelViewerMeshBuildTiming
}

func (t *Tools) prepareModelViewerGeometry(ctx context.Context, folder string, iniPaths []string, budget *modelViewerLoadBudget) (*modelViewerPreparedGeometry, error) {
	prepared := &modelViewerPreparedGeometry{cache: newModelViewerBufferCache(), variableNames: make(map[string]modelViewerVariableName), timing: &modelViewerMeshBuildTiming{}}
	keep := false
	defer func() {
		if !keep {
			prepared.cache.releaseAll()
		}
	}()
	var stageStartedAt time.Time
	multi := len(iniPaths) > 1
	for iniIndex, iniPath := range iniPaths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		parseStartedAt := time.Now()
		parsed, readErr := readModelViewerINI(iniPath)
		if readErr != nil {
			return nil, readErr
		}
		prefix, _ := modelViewerINIScope(iniPath, folder, multi)
		sections, scopedNames := scopeModelViewerSections(parsed.Sections, iniIndex, prefix)
		for key, value := range scopedNames {
			prepared.variableNames[key] = value
		}
		rebaseModelViewerResources(sections, iniPath, folder)
		for _, resource := range sanitizeModelViewerResourcePaths(sections, folder, folder) {
			if t.log != nil {
				t.log.Warn("Skipped unsafe Model Viewer resource path: "+sanitizeModelViewerLogValue(resource), "StaticGlb.loadForViewer")
			}
		}
		resources := resolveModelViewerEffectiveResourcesAt(folder, folder, sections, collectModelViewerResources(sections))
		prepared.scans = append(prepared.scans, modelViewerGeometryScan{sections: sections, resources: resources})
		stageStartedAt = time.Now()
		referenced := collectModelViewerReferencedResources(sections)
		prepared.referencedMs += time.Since(stageStartedAt).Milliseconds()
		stageStartedAt = time.Now()
		if validationErr := budget.validateReferencedResources(folder, resources, referenced); validationErr != nil {
			return nil, validationErr
		}
		prepared.validateMs += time.Since(stageStartedAt).Milliseconds()
		prepared.sections = append(prepared.sections, sections...)
		prepared.parseMs += time.Since(parseStartedAt).Milliseconds()
		stageStartedAt = time.Now()
		meshes, textureBindings, resources, shapeKeys, buildErr := buildModelViewerDirectMeshesAt(iniPath, folder, "", sections, prepared.cache, prepared.timing)
		if buildErr != nil {
			return nil, buildErr
		}
		computeScopeID := ""
		if multi {
			computeScopeID = modelViewerString(iniIndex)
		}
		if deformer, clips := detectModelViewerComputeAnimation(folder, filepath.Dir(iniPath), computeScopeID, sections, resources, meshes, scopedNames); deformer != nil {
			prepared.computeDeformers = append(prepared.computeDeformers, *deformer)
			prepared.computeAnimations = append(prepared.computeAnimations, clips...)
		}
		prepared.shapeKeys = append(prepared.shapeKeys, shapeKeys...)
		prepared.buildMs += time.Since(stageStartedAt).Milliseconds()
		prepared.textures = append(prepared.textures, modelViewerINITextureWork{
			meshes:   meshes,
			bindings: textureBindings,
			shapes:   shapeKeys,
			jobs:     collectModelViewerTextureJobs(len(prepared.textures), folder, resources, textureBindings, meshes),
		})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	keep = true
	return prepared, nil
}
