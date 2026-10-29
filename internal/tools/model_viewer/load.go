package modelviewer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/wailsapp/wails/v3/pkg/application"

	"nahida.live/desktop/internal/infra"
)

func (t *Service) LoadModViewer(ctx context.Context, modPath string) (ModelViewerTransport, error) {
	return t.loadModViewer(ctx, modPath, modelViewerPayloadOptions{})
}

func (t *Service) LoadModGridPreview(ctx context.Context, modPath string) (ModelViewerTransport, error) {
	return t.loadModViewer(ctx, modPath, modelViewerPayloadOptions{
		ddsPreviewMaxDimension:       modelViewerDDSPreviewMaxDimension,
		ddsPreviewPreferredDimension: modelViewerGridPreviewDDSDimension,
		includeAllStateVariables:     true,
	})
}

func (t *Service) loadModViewer(
	ctx context.Context,
	modPath string,
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
	var windowID uint
	if window, ok := ctx.Value(application.WindowKey).(application.Window); ok {
		windowID = window.ID()
	}
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
	prepared, prepareErr := t.prepareModelViewerGeometry(ctx, folder, iniPaths, budget)
	if prepareErr != nil {
		return ModelViewerTransport{}, prepareErr
	}
	defer prepared.cache.releaseAll()
	var meshPayloadMs, postProcessMs, payloadWriteMs int64
	var stageStartedAt time.Time
	// Mesh payloads own the extracted attributes from this point onward. Drop
	// interleaved vertex buffers and geometry caches before texture preparation so
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
	configureModelViewerState(
		&transport,
		prepared.sections,
		prepared.shapeKeys,
		prepared.variableNames,
		prepared.computeAnimations,
		options.includeAllStateVariables,
	)
	postProcessMs = time.Since(stageStartedAt).Milliseconds()
	stageStartedAt = time.Now()
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
	if t.modelViewerClosedWindows[windowID] {
		t.modelViewerMu.Unlock()
		return ModelViewerTransport{}, context.Canceled
	}
	t.modelViewerSessions[sessionID] = &modelViewerSession{
		modPath:     requestedPath,
		windowID:    windowID,
		evaluator:   evaluator,
		cancel:      sessionCancel,
		drawSources: prepared.drawSources,
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
	drawSources                                map[string]DrawSource
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
) (*modelViewerPreparedGeometry, error) {
	prepared := &modelViewerPreparedGeometry{
		cache:             newModelViewerBufferCache(),
		variableNames:     make(map[string]modelViewerVariableName),
		timing:            &modelViewerMeshBuildTiming{},
		computeDeformers:  []ModelViewerComputeDeformerTransport{},
		computeAnimations: []modelViewerPreparedAnimationClip{},
		inis:              make([]ModelViewerINI, 0, len(iniPaths)),
	}
	keep := false
	defer func() {
		if !keep {
			prepared.cache.releaseAll()
		}
	}()
	var stageStartedAt time.Time
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
	}
	built := make([]builtUnit, len(units))
	var declaredResourcePaths []string
	for unitIndex, unit := range units {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		parseStartedAt := time.Now()
		scoped := scopeModelViewerUnit(unit, unitIndex, parsedINIs, iniPaths, scopes, unitByNamespace, folder)
		sections := scoped.sections
		for key, value := range scoped.names {
			prepared.variableNames[key] = value
		}
		for _, resource := range sanitizeModelViewerResourcePaths(sections, folder, folder) {
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
		)
		for _, resource := range resources {
			if resource.Filename != "" {
				declaredResourcePaths = append(
					declaredResourcePaths,
					filepath.Join(folder, filepath.FromSlash(slashPath(resource.Filename))),
				)
			}
		}
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
		built[unitIndex] = builtUnit{scoped: scoped, resources: resources}
	}

	domains := collectModelViewerBindingDomains(prepared.sections)
	for unitIndex, unit := range units {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		iniPath := iniPaths[unit.primary]
		sections := built[unitIndex].scoped.sections
		stageStartedAt = time.Now()
		meshes, textureBindings, resources, shapeKeys, buildErr := buildModelViewerDirectMeshesPrepared(
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
		if slices.ContainsFunc(unit.members, func(member int) bool {
			return detectModelViewerLayout(parsedINIs[member].Sections, resources) == "wwmi"
		}) {
			if err := recoverWwmiDumpTextures(ctx, folder, meshes, resources, declaredResourcePaths); err != nil {
				return nil, err
			}
		}
		attributeModelViewerDrawSources(meshes, built[unitIndex].scoped, iniPaths)
		prepared.scopeMeshes(meshes, filepath.Base(iniPath), scopes[unit.primary], folder)
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
	folder string,
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
		label = filepath.Base(folder)
	}
	p.forms = append(p.forms, ModelViewerForm{ID: scope.form, Label: label})
}

// dropForms leaves a folder whose meshes all come from one place without
// forms to choose between.
func (p *modelViewerPreparedGeometry) dropForms() {
	p.forms = nil
	for _, work := range p.textures {
		for index := range work.meshes {
			work.meshes[index].form = ""
		}
	}
	for key, name := range p.variableNames {
		name.Form = ""
		p.variableNames[key] = name
	}
}
