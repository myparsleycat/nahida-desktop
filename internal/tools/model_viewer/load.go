package modelviewer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/samber/lo"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/modname"
	"nahida.live/desktop/internal/platform"
)

func (t *Service) LoadModViewer(ctx context.Context, modPath string) (ModelViewerTransport, error) {
	// The load leaves a heap sized for its peak and the spool now holds the meshes; return the pages now
	// instead of waiting for the background scavenger. Grid previews skip this because they load in batches.
	defer debug.FreeOSMemory()
	return t.loadModViewer(ctx, modPath, true, modelViewerPayloadOptions{})
}

func (t *Service) LoadModGridPreview(ctx context.Context, modPath string) (ModelViewerTransport, error) {
	return t.loadModViewer(ctx, modPath, false, modelViewerPayloadOptions{
		ddsPreviewMaxDimension:       modelViewerDDSPreviewMaxDimension,
		ddsPreviewPreferredDimension: modelViewerGridPreviewDDSDimension,
		includeAllStateVariables:     true,
	})
}

func (t *Service) loadModViewer(
	ctx context.Context,
	modPath string,
	fullViewer bool,
	options modelViewerPayloadOptions,
) (transport ModelViewerTransport, err error) {
	startedAt := time.Now()
	stage := "discover"
	sessionID := ""
	folder := ""
	t.log.Info("Starting model viewer load", "StaticGlb.loadForViewer")
	defer func() {
		if err != nil {
			err = infra.ReportError(
				t.log,
				err,
				"StaticGlb.loadForViewer",
				infra.Diagnostic{
					Operation: "load-model-viewer",
					Fields: map[string]any{
						"message": fmt.Sprintf(
							"Model viewer load failed after %dms",
							time.Since(startedAt).Milliseconds(),
						),
						"elapsedMs":            time.Since(startedAt).Milliseconds(),
						"path":                 modPath,
						"resolvedPath":         folder,
						"stage":                stage,
						"memorySessionId":      sessionID,
						"memorySessionCleaned": sessionID != "",
					},
				},
			)
			return
		}
		t.log.Info(
			fmt.Sprintf(
				"Completed model viewer load in %dms (meshes=%d)",
				time.Since(startedAt).Milliseconds(),
				len(transport.Meshes),
			),
			"StaticGlb.loadForViewer",
		)
	}()
	if err := ctx.Err(); err != nil {
		return ModelViewerTransport{}, err
	}
	ctx, endLoad := t.beginModelViewerLoad(ctx)
	defer endLoad()
	requestedPath := modPath
	var absErr error
	folder, absErr = filepath.Abs(requestedPath)
	if absErr != nil {
		return ModelViewerTransport{}, absErr
	}
	if info, statErr := os.Stat(folder); statErr != nil {
		return ModelViewerTransport{}, statErr
	} else if !info.IsDir() {
		return ModelViewerTransport{}, infra.ContractError("Model viewer path must be a folder.")
	}
	var openedPath, logicalRoot string
	if fullViewer {
		options.spoolGeometry = true
		if resolved, resolveErr := platform.FinalPath(folder); resolveErr == nil {
			if !platform.SamePathFold(folder, resolved) {
				logicalRoot = folder
			}
			openedPath, folder = resolved, resolved
		} else {
			_ = infra.ReportError(t.log, resolveErr, "StaticGlb.loadForViewer", infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: "load-model-viewer", Stage: "resolve-directory",
				Fields: map[string]any{"modPath": requestedPath},
			})
		}
	}
	discoveryStartedAt := time.Now()
	diagnostics := &infra.DiagnosticBatch{}
	defer diagnostics.Report(t.log, "Tools", "model-viewer-discovery")
	iniPaths, discoverErr := discoverModelViewerActiveINIs(folder, diagnostics.Add)
	if discoverErr != nil {
		return ModelViewerTransport{}, discoverErr
	}
	t.log.Info(
		fmt.Sprintf(
			"INI discovery completed in %dms (inis=%d)",
			time.Since(discoveryStartedAt).Milliseconds(),
			len(iniPaths),
		),
		"StaticGlb.loadForViewer",
	)
	if len(iniPaths) == 0 {
		return ModelViewerTransport{}, infra.ContractError("No active .ini files found in this folder.")
	}
	if err := ctx.Err(); err != nil {
		return ModelViewerTransport{}, err
	}
	budget, budgetErr := newModelViewerLoadBudget(folder)
	if budgetErr != nil {
		return ModelViewerTransport{}, budgetErr
	}
	budget.logicalRoot = logicalRoot
	sessionID = t.protocol.CreateMemorySession()
	if options.spoolGeometry {
		options.spool = t.openSpool(sessionID)
	}
	keep := false
	defer func() {
		if !keep {
			t.protocol.CleanupMemorySession(sessionID)
			t.closeSpool(sessionID, options.spool)
		}
	}()
	transport = ModelViewerTransport{
		MemorySessionID:  sessionID,
		INIPath:          iniPaths[0],
		ModPath:          requestedPath,
		OpenedModPath:    openedPath,
		Name:             modname.DisplayName(requestedPath),
		Meshes:           []ModelViewerMeshTransport{},
		Textures:         make(map[string]ModelViewerTextureTransport),
		Variables:        []ModelViewerVariable{},
		DefaultState:     make(map[string]any),
		StateRules:       []ModelViewerStateRule{},
		UIAssets:         ModelViewerUIAssets{},
		Animations:       []ModelViewerAnimationClip{},
		ComputeDeformers: []ModelViewerComputeDeformerTransport{},
		Forms:            []ModelViewerForm{},
		INIs:             []ModelViewerINI{},
	}
	if requestedPath == "" {
		transport.Name = ""
	}
	if t.findModelViewerPreview != nil {
		transport.PreviewPath = t.findModelViewerPreview(folder)
	}
	stage = "prepare-geometry"
	prepared, prepareErr := t.prepareModelViewerGeometry(ctx, folder, iniPaths, budget, options.spoolGeometry)
	if prepareErr != nil {
		return ModelViewerTransport{}, prepareErr
	}
	defer prepared.cache.releaseAll()
	transport.DefaultYaw = prepared.defaultYaw
	var meshPayloadMs, postProcessMs, payloadWriteMs int64
	var stageStartedAt time.Time
	// Mesh payloads own the extracted attributes from this point onward. Drop
	// interleaved vertex buffers and geometry caches before texture preparation so
	// the two peak-memory phases do not overlap.
	prepared.cache.releaseGeometryScratch()
	runtime.GC()
	stage = "prepare-payload"
	stageStartedAt = time.Now()
	textureDiagnostics := &infra.DiagnosticBatch{}
	defer textureDiagnostics.Report(t.log, "StaticGlb.loadForViewer", "model-viewer-textures")
	meshPayloads, texturePayloads, textureStats, payloadErr := prepareModelViewerPayload(
		ctx,
		prepared,
		&transport,
		textureDiagnostics,
	)
	if payloadErr != nil {
		return ModelViewerTransport{}, payloadErr
	}
	prepared.cache.releaseAll()
	prepared.textures = nil // Extracted payloads now own the mesh attributes.
	runtime.GC()
	meshPayloadMs = time.Since(stageStartedAt).Milliseconds()
	t.log.Info(
		fmt.Sprintf(
			"Texture preparation completed in %dms (textures=%d directDDS=%d preparedImages=%d)",
			textureStats.TotalWallMs,
			textureStats.LogicalTextures,
			textureStats.DirectDDS,
			textureStats.PreparedImages,
		),
		"StaticGlb.loadForViewer",
	)
	t.log.Info(
		fmt.Sprintf(
			"Texture preparation detail: jobs=%d paths=%d contents=%d decodes=%d encodes=%d hashBytes=%d hash=%dms prepare=%dms",
			textureStats.Jobs,
			textureStats.UniquePaths,
			textureStats.UniqueContents,
			textureStats.Decodes,
			textureStats.Encodes,
			textureStats.HashBytes,
			textureStats.HashWallMs,
			textureStats.PrepareWallMs,
		),
		"StaticGlb.loadForViewer",
	)
	t.log.Info(
		fmt.Sprintf(
			"INI parse detail: referenced=%dms validate=%dms (total %dms)",
			prepared.referencedMs,
			prepared.validateMs,
			prepared.parseMs,
		),
		"StaticGlb.loadForViewer",
	)
	t.log.Info(
		fmt.Sprintf(
			"Mesh build detail: scan=%dms(%d recs) setup=%dms geometry=%dms(%d) overrides=%dms attach=%dms normalize=%dms legacy=%dms(groups=%dms condScan=%dms prepare=%dms extract=%dms)",
			prepared.timing.ScanMs,
			prepared.timing.Records,
			prepared.timing.SetupMs,
			prepared.timing.GeometryMs,
			prepared.timing.Geometries,
			prepared.timing.OverridesMs,
			prepared.timing.AttachMs,
			prepared.timing.NormalizeMs,
			prepared.timing.LegacyMs,
			prepared.timing.GroupsMs,
			prepared.timing.LegacyScanMs,
			prepared.timing.LegacyPrepareMs,
			prepared.timing.LegacyExtractMs,
		),
		"StaticGlb.loadForViewer",
	)
	if len(transport.Meshes) == 0 {
		if resource := firstUnresolvedModelViewerPositionResource(prepared.scans); resource != "" {
			return ModelViewerTransport{}, infra.ContractError(
				fmt.Sprintf("Position resource Resource%s has no resolvable file-backed source.", resource),
			)
		}
		hasGeometryGroups := false
		for _, scan := range prepared.scans {
			if modelViewerHasGeometryGroup(scan.sections, scan.resources) {
				hasGeometryGroups = true
				break
			}
		}
		if !hasGeometryGroups {
			return ModelViewerTransport{}, infra.ContractError(
				fmt.Sprintf("No mesh geometry found across %d ini file(s).", len(iniPaths)),
			)
		}
		if prepared.timing.SkippedMissingTexcoord {
			return ModelViewerTransport{}, infra.ContractError("Draw sections are missing a texcoord buffer (vb1).")
		}
		return ModelViewerTransport{}, infra.ContractError("No mesh data could be extracted (buffer files missing?).")
	}
	if err := ctx.Err(); err != nil {
		return ModelViewerTransport{}, err
	}
	stage = "configure-state"
	stageStartedAt = time.Now()
	transport.ComputeDeformers = prepared.computeDeformers
	transport.Forms = prepared.forms
	transport.INIs = prepared.inis
	transport.bangOverrides = prepared.bangOverrides
	configureModelViewerState(
		&transport,
		prepared.sections,
		prepared.shapeKeys,
		prepared.variableNames,
		prepared.computeAnimations,
		prepared.bufferRules,
		options.includeAllStateVariables,
	)
	transport.OverlayMenus = t.serveModelViewerOverlayTextures(ctx, sessionID, buildModelViewerOverlayMenus(
		ctx,
		folder,
		prepared.scans,
		slices.DeleteFunc(slices.Clone(transport.Variables), func(variable ModelViewerVariable) bool {
			return !variable.gatesDirectly()
		}),
		prepared.variableNames,
		modelViewerOverlayMenuOnlyVariables(&transport),
	))
	transport.Variables = withoutModelViewerOverlayStateVariables(transport.Variables, transport.OverlayMenus)
	// An overlay menu that keeps the variable picking the forms as its own state leaves the forms to the menu.
	detectModelViewerFormSelector(&transport)
	transport.OverlayMenus = append(
		transport.OverlayMenus,
		t.serveModelViewerOverlayTextures(ctx, sessionID, buildModelViewerOverlayPanels(
			ctx,
			folder,
			prepared.scans,
			transport.ComputeDeformers,
			prepared.variableNames,
		))...,
	)
	meshCount := len(transport.Meshes)
	meshPayloads, meshAliases, consolidateErr := consolidateModelViewerMeshes(
		ctx,
		&transport,
		meshPayloads,
		prepared.drawSources,
	)
	if consolidateErr != nil {
		return ModelViewerTransport{}, consolidateErr
	}
	if len(meshAliases) > 0 {
		t.log.Info(
			fmt.Sprintf("Mesh consolidation: %d meshes became %d", meshCount, len(transport.Meshes)),
			"StaticGlb.loadForViewer",
		)
	}
	postProcessMs = time.Since(stageStartedAt).Milliseconds()
	stageStartedAt = time.Now()
	if options.spoolGeometry {
		if err := prepareSurfaceSnapshots(
			ctx,
			prepared.drawSources,
			transport.Meshes,
			meshPayloads,
			options.spool,
		); err != nil {
			return ModelViewerTransport{}, err
		}
	}
	// A tool that saved the ID of a mesh that was folded or removed still finds its draw.
	for id, target := range meshAliases {
		if source, exists := prepared.drawSources[target]; exists {
			prepared.drawSources[id] = source
		}
	}
	if logicalRoot != "" {
		// Toggle presets and tool requests address INIs through the mod path they opened.
		requestedINIPath := func(path string) string {
			if !platform.SameOrChildPath(folder, path) {
				return path
			}
			if relative, err := filepath.Rel(folder, path); err == nil {
				return filepath.Join(logicalRoot, relative)
			}
			return path
		}
		transport.INIPath = requestedINIPath(transport.INIPath)
		for i := range transport.INIs {
			transport.INIs[i].Path = requestedINIPath(transport.INIs[i].Path)
		}
		for id, source := range prepared.drawSources {
			source.INIPath = requestedINIPath(source.INIPath)
			source.UnitINIPaths = lo.Map(source.UnitINIPaths, func(path string, _ int) string {
				return requestedINIPath(path)
			})
			prepared.drawSources[id] = source
		}
	}
	stage = "write-payload"
	if writeErr := writeModelViewerPayload(
		ctx,
		t,
		sessionID,
		&transport,
		meshPayloads,
		texturePayloads,
		options,
	); writeErr != nil {
		return ModelViewerTransport{}, writeErr
	}
	sessionContext, sessionCancel := context.WithCancel(context.Background())
	defer func() {
		if !keep {
			sessionCancel()
		}
	}()
	evaluator := newModelViewerEvaluator(sessionContext, transport)
	t.modelViewerMu.Lock()
	if err := ctx.Err(); err != nil {
		t.modelViewerMu.Unlock()
		return ModelViewerTransport{}, err
	}
	t.modelViewerSessions[sessionID] = &modelViewerSession{
		modPath:     requestedPath,
		evaluator:   evaluator,
		cancel:      sessionCancel,
		drawSources: prepared.drawSources,
		spool:       options.spool,
	}
	t.modelViewerMu.Unlock()
	keep = true
	payloadWriteMs = time.Since(stageStartedAt).Milliseconds()
	t.log.Info(
		fmt.Sprintf(
			"Load stages: iniParse=%dms meshBuild=%dms meshPayload=%dms post=%dms payloadWrite=%dms",
			prepared.parseMs,
			prepared.buildMs,
			meshPayloadMs,
			postProcessMs,
			payloadWriteMs,
		),
		"StaticGlb.loadForViewer",
	)
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
	return lo.Min(unresolved)
}

type modelViewerGeometryScan struct {
	sections  []modINISection
	resources []modelViewerResource
	paths     *modelViewerBufferCache
	// iniDir is what the shader paths of the sections are relative to.
	iniDir string
}

type modelViewerPreparedGeometry struct {
	defaultYaw                                 float64
	sections                                   []modINISection
	shapeKeys                                  []modelViewerShapeKey
	cache                                      *modelViewerBufferCache
	variableNames                              map[string]modelViewerVariableName
	scans                                      []modelViewerGeometryScan
	textures                                   []modelViewerINITextureWork
	computeDeformers                           []ModelViewerComputeDeformerTransport
	computeAnimations                          []modelViewerPreparedAnimationClip
	bufferRules                                []ModelViewerStateRule
	drawSources                                map[string]DrawSource
	bangOverrides                              []modelViewerBangOverride
	forms                                      []ModelViewerForm
	inis                                       []ModelViewerINI
	parseMs, validateMs, referencedMs, buildMs int64
	timing                                     *modelViewerMeshBuildTiming
}

func (t *Service) prepareModelViewerGeometry(
	ctx context.Context,
	folder string,
	iniPaths []string,
	budget *modelViewerLoadBudget,
	hashSources bool,
) (*modelViewerPreparedGeometry, error) {
	prepared := &modelViewerPreparedGeometry{
		cache:             newModelViewerBufferCache(ctx),
		variableNames:     make(map[string]modelViewerVariableName),
		timing:            &modelViewerMeshBuildTiming{},
		computeDeformers:  []ModelViewerComputeDeformerTransport{},
		computeAnimations: []modelViewerPreparedAnimationClip{},
		inis:              make([]ModelViewerINI, 0, len(iniPaths)),
	}
	prepared.cache.hashSources = hashSources
	prepared.cache.logicalRoot = budget.logicalRoot
	defer prepared.cache.diagnostics.Report(t.log, "StaticGlb.loadForViewer", "model-viewer-buffers")
	keep := false
	defer func() {
		if !keep {
			prepared.cache.releaseAll()
		}
	}()
	var stageStartedAt time.Time
	rootLabel := modname.DisplayName(folder)
	if budget.logicalRoot != "" {
		rootLabel = modname.DisplayName(budget.logicalRoot)
	}
	scopes := modelViewerINIScopes(iniPaths, folder)
	parsedINIs := make([]modelViewerParsedINI, len(iniPaths))
	for iniIndex, iniPath := range iniPaths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		parseStartedAt := time.Now()
		parsed, readErr := readModelViewerINI(iniPath)
		if readErr != nil {
			return nil, readErr
		}
		parsedINIs[iniIndex] = parsed
		prepared.inis = append(prepared.inis, ModelViewerINI{Path: iniPath, Prefix: scopes[iniIndex].prefix})
		prepared.parseMs += time.Since(parseStartedAt).Milliseconds()
	}

	units := groupModelViewerINIUnits(parsedINIs)
	unitByNamespace := make(map[string]int, len(units))
	for unitIndex, unit := range units {
		if unit.namespace != "" {
			unitByNamespace[strings.ToLower(unit.namespace)] = unitIndex
		}
	}
	type builtUnit struct {
		scoped    modelViewerScopedUnit
		resources []modelViewerResource
		// hashTextures are the referenced resources of a unit that draws
		// nothing, which can only replace textures by hash.
		hashTextures []modelViewerResource
	}
	built := make([]builtUnit, len(units))
	var declaredResourcePaths []string
	for unitIndex, unit := range units {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		parseStartedAt := time.Now()
		scoped := scopeModelViewerUnit(units, unitIndex, parsedINIs, iniPaths, scopes, unitByNamespace, folder)
		sections := scoped.sections
		for key, value := range scoped.names {
			prepared.variableNames[key] = value
		}
		for _, resource := range sanitizeModelViewerResourcePaths(sections, folder, folder, prepared.cache) {
			t.log.Warn(
				"Skipped unsafe Model Viewer resource path: "+sanitizeModelViewerLogValue(resource),
				"StaticGlb.loadForViewer",
			)
		}
		resources := resolveModelViewerEffectiveResourcesAt(
			folder,
			folder,
			sections,
			collectModelViewerResources(sections),
			prepared.cache,
		)
		for _, resource := range resources {
			if resource.Filename != "" {
				declaredResourcePaths = append(
					declaredResourcePaths,
					filepath.Join(folder, filepath.FromSlash(slashPath(resource.Filename))),
				)
			}
		}
		prepared.scans = append(prepared.scans, modelViewerGeometryScan{
			sections:  sections,
			resources: resources,
			paths:     prepared.cache,
			iniDir:    filepath.Dir(iniPaths[unit.primary]),
		})
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
		built[unitIndex] = builtUnit{scoped: scoped, resources: resources}
		if !slices.ContainsFunc(sections, sectionHasIBOrDraw) {
			built[unitIndex].hashTextures = lo.Filter(resources, func(resource modelViewerResource, _ int) bool {
				return resource.Filename != "" && referenced[modelViewerNormalizeKey(resource.Name)]
			})
		}
	}

	domains := collectModelViewerBindingDomains(prepared.sections)
	recoveries := make(map[*modelViewerGeometry]*modelViewerGeometryRecovery)
	defer func() {
		if ctx.Err() == nil {
			reportModelViewerGeometryRecoveries(t.log, recoveries)
		}
	}()
	for unitIndex, unit := range units {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		iniPath := iniPaths[unit.primary]
		sections := built[unitIndex].scoped.sections
		stageStartedAt = time.Now()
		meshes, textureBindings, resources, shapeKeys, bufferRules, buildErr := buildModelViewerDirectMeshesPrepared(
			iniPath,
			folder,
			"",
			sections,
			built[unitIndex].resources,
			domains,
			prepared.cache,
			prepared.timing,
		)
		if buildErr != nil {
			return nil, buildErr
		}
		prepared.bufferRules = append(prepared.bufferRules, bufferRules...)
		validMeshes := meshes[:0]
		for _, mesh := range meshes {
			recovery := recoveries[mesh.geometry]
			if recovery == nil {
				geometry, stats, err := recoverModelViewerGeometry(ctx, mesh.geometry)
				if err != nil {
					return nil, fmt.Errorf("mesh %s geometry: %w", mesh.id, err)
				}
				recovery = &modelViewerGeometryRecovery{geometry: geometry, stats: stats}
				recoveries[mesh.geometry] = recovery
			}
			if !slices.ContainsFunc(
				recovery.iniPaths,
				func(path string) bool { return platform.SamePathFold(path, iniPath) },
			) {
				recovery.iniPaths = append(recovery.iniPaths, iniPath)
			}
			positionPath := mesh.positionFile
			if positionPath != "" && !filepath.IsAbs(positionPath) {
				positionPath = filepath.Join(folder, filepath.FromSlash(positionPath))
			}
			if !slices.ContainsFunc(
				recovery.positionPaths,
				func(path string) bool { return platform.SamePathFold(path, positionPath) },
			) {
				recovery.positionPaths = append(recovery.positionPaths, positionPath)
			}
			recovery.meshIDs = append(recovery.meshIDs, mesh.id)
			if recovery.geometry != nil {
				mesh.geometry = recovery.geometry
				validMeshes = append(validMeshes, mesh)
			}
		}
		meshes = validMeshes
		if slices.ContainsFunc(unit.members, func(member int) bool {
			return detectModelViewerLayout(parsedINIs[member].Sections, resources) == "wwmi"
		}) {
			prepared.defaultYaw = 180
			var siblingTextures []modelViewerResource
			for otherIndex, other := range units {
				if otherIndex != unitIndex &&
					platform.SamePathFold(filepath.Dir(iniPaths[other.primary]), filepath.Dir(iniPath)) {
					siblingTextures = append(siblingTextures, built[otherIndex].hashTextures...)
				}
			}
			if err := attachSiblingWwmiDumpDiffuse(ctx, folder, meshes, siblingTextures); err != nil {
				return nil, err
			}
			if err := recoverWwmiDumpTextures(ctx, folder, meshes, resources, declaredResourcePaths); err != nil {
				return nil, err
			}
			if err := refineWwmiDumpAtlases(ctx, folder, meshes, resources, prepared.cache); err != nil {
				return nil, err
			}
			if err := refineWwmiHashAtlases(ctx, folder, meshes, prepared.cache); err != nil {
				return nil, err
			}
		}
		attributeModelViewerDrawSources(meshes, built[unitIndex].scoped, iniPaths)
		prepared.scopeMeshes(meshes, filepath.Base(iniPath), scopes[unit.primary], rootLabel)
		prepared.scopeCommandForms(meshes, iniPath, scopes[unit.primary])
		computeScopeID := ""
		if len(units) > 1 {
			computeScopeID = modelViewerString(unitIndex)
		}
		if deformer, clips := detectModelViewerComputeAnimation(
			folder,
			filepath.Dir(iniPath),
			computeScopeID,
			sections,
			resources,
			meshes,
			built[unitIndex].scoped.names,
			func(message string) {
				t.log.Warn(fmt.Sprintf("INI=%q %s", iniPath, message), "StaticGlb.loadForViewer")
			},
		); deformer != nil {
			prepared.computeDeformers = append(prepared.computeDeformers, *deformer)
			prepared.computeAnimations = append(prepared.computeAnimations, clips...)
		}
		prepared.shapeKeys = append(prepared.shapeKeys, shapeKeys...)
		prepared.buildMs += time.Since(stageStartedAt).Milliseconds()
		overrides, err := collectModelViewerBangOverrides(sections, domains, scopes[unit.primary].form)
		if err != nil {
			return nil, fmt.Errorf("scan original Bang overrides in %s: %w", iniPath, err)
		}
		prepared.bangOverrides = append(prepared.bangOverrides, overrides...)
		face := collectModelViewerFaceTextures(folder, sections, resources, domains, meshes, textureBindings)
		prepared.textures = append(prepared.textures, modelViewerINITextureWork{
			meshes:   meshes,
			bindings: textureBindings,
			shapes:   shapeKeys,
			jobs: collectModelViewerTextureJobs(len(prepared.textures), folder, resources, textureBindings, meshes,
				modelViewerFaceTextureAssignments(face), prepared.cache),
			face: face,
			form: scopes[unit.primary].form,
		})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	forms := prepared.forms
	prepared.forms = lo.Filter(forms, func(form ModelViewerForm, _ int) bool {
		return !slices.ContainsFunc(forms, func(other ModelViewerForm) bool {
			return strings.HasPrefix(other.ID, form.ID+"::")
		})
	})
	if len(prepared.forms) < 2 {
		prepared.dropForms()
	}
	qualifyModelViewerFormGroups(prepared.forms)
	keep = true
	return prepared, nil
}

// scopeMeshes gives the meshes of one INI the IDs and form of its scope. The
// mesh builders start IDs with the INI's file name, which only identifies the
// INI where no other loaded INI shares it.
func (p *modelViewerPreparedGeometry) scopeMeshes(
	meshes []modelViewerDirectMesh,
	fileName string,
	scope modelViewerINIScope,
	rootLabel string,
) {
	if len(meshes) == 0 || scope.form == "" {
		return
	}
	defaultKey := modelViewerNormalizeKey(fileName)
	for index := range meshes {
		mesh := &meshes[index]
		mesh.id = scope.meshKey + strings.TrimPrefix(mesh.id, defaultKey)
		mesh.form = scope.form
	}
	if slices.ContainsFunc(p.forms, func(form ModelViewerForm) bool { return form.ID == scope.form }) {
		return
	}
	label := scope.form
	if label == "." {
		label = rootLabel
	}
	p.forms = append(p.forms, ModelViewerForm{ID: scope.form, Label: label})
}

// dropForms leaves a folder whose meshes all come from one place without
// forms to choose between.
func (p *modelViewerPreparedGeometry) dropForms() {
	p.forms = nil
	for index := range p.bangOverrides {
		p.bangOverrides[index].form = ""
	}
	for workIndex := range p.textures {
		work := &p.textures[workIndex]
		work.form = ""
		for index := range work.meshes {
			work.meshes[index].form = ""
		}
	}
	for key, name := range p.variableNames {
		name.Form = ""
		p.variableNames[key] = name
	}
}
