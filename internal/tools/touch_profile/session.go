package touchprofile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/tools/modmesh"
)

const touchProgressEventName = "tools:touchProfileProgress"

type touchAppliedProfile struct {
	OutputRoot, SourceRoot string
	Reenable               bool
}

type touchSession struct {
	mu              sync.Mutex
	Analysis        TouchModAnalysis
	Dir             string
	Mesh            map[string]touchMeshBuffers
	ProtocolID      string
	Topology        map[string]TouchMeshDescriptor
	Preview         map[string]touchCachedPreview
	DraftRevision   uint64
	PreviewRevision uint64
	Draft           *TouchDraft
	Applied         *touchAppliedProfile
	Operation       string
}

type touchCachedPreview struct {
	descriptor TouchProfilePreviewDescriptor
	bufferID   string
}

func (t *Service) TouchProfilePrepare(
	ctx context.Context,
	input TouchProfileLoadInput,
) (result TouchModInspection, err error) {
	stage := "validate"
	defer func() {
		if err != nil {
			err = infra.ReportError(t.log, err, "Tools", infra.Diagnostic{
				Operation: "touch-profile-prepare", Stage: stage,
				Fields: map[string]any{"modPath": input.ModPath},
			})
		}
	}()
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if strings.TrimSpace(input.ModPath) == "" {
		return result, infra.ContractError("Touch profile mod path is required")
	}
	id, err := newTouchID()
	if err != nil {
		return result, err
	}
	if len(id) > 12 {
		id = id[:12]
	}
	if t.appData == nil {
		return result, errors.New("tools service has no app data store")
	}
	sessionDir, err := t.appData.EnsureDir(filepath.Join(appdata.ToolsDir, "touch-profile", id))
	if err != nil {
		return result, err
	}
	defer func() {
		if err != nil {
			if cleanupErr := os.RemoveAll(sessionDir); cleanupErr != nil {
				_ = infra.ReportError(t.log, cleanupErr, "Tools", infra.Diagnostic{
					Severity: infra.DiagnosticError, Operation: "touch-profile-prepare", Stage: "cleanup",
					Fields: map[string]any{"sessionId": id, "sessionDir": sessionDir},
				})
			}
		}
	}()
	stage = "scan"
	t.emitTouchProgress(id, "scan", .05, "Scanning mod structure", "")
	analysis, err := analyzeTouchMod(input.ModPath, func(message string) {
		if t.log != nil {
			t.log.Warn(message, "TouchProfile")
		}
	})
	if err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	protocolID := t.protocol.CreateMemorySession()
	session := &touchSession{
		Analysis:   analysis,
		Dir:        sessionDir,
		ProtocolID: protocolID,
		Mesh:       map[string]touchMeshBuffers{},
		Topology:   map[string]TouchMeshDescriptor{},
		Preview:    map[string]touchCachedPreview{},
	}
	t.touchMu.Lock()
	t.touchSessions[id] = session
	t.touchMu.Unlock()
	result = TouchModInspection{
		SessionID:                id,
		ModRoot:                  analysis.ModRoot,
		INIRelativePath:          analysis.INIRelativePath,
		SourceFilesRelativePaths: analysis.SourceFilesRelativePaths,
		SupportGrade:             analysis.SupportGrade,
		SupportReasons:           analysis.SupportReasons,
		Components:               []TouchComponentInspection{},
	}
	for _, component := range analysis.Components {
		result.Components = append(
			result.Components,
			TouchComponentInspection{
				ID:                   component.ID,
				Name:                 component.Name,
				Kind:                 component.Kind,
				SupportGrade:         component.SupportGrade,
				InteractiveCandidate: component.InteractiveCandidate,
				VertexCount:          component.VertexCount,
				IndexCount:           component.IndexCount,
				VariantKey:           component.VariantKey,
				VariantCondition:     component.VariantCondition,
				ObjectMaps:           component.ObjectMaps,
				HasBlend:             component.BlendPath != nil,
				Bones:                component.Bones,
			},
		)
	}
	return result, nil
}

func (t *Service) TouchProfileGetMeshDescriptor(
	ctx context.Context,
	input TouchProfilePreviewInput,
) (TouchMeshDescriptor, error) {
	session, err := t.requireTouchSession(input.SessionID)
	if err != nil {
		return TouchMeshDescriptor{}, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return TouchMeshDescriptor{}, err
	}
	if descriptor, ok := session.Topology[input.ComponentID]; ok {
		return descriptor, nil
	}
	mesh, ok := session.Mesh[input.ComponentID]
	if !ok {
		component := findTouchComponent(session.Analysis.Components, input.ComponentID)
		if component == nil {
			return TouchMeshDescriptor{}, infra.ContractError(
				fmt.Sprintf("Touch component not found: %s", input.ComponentID),
			)
		}
		mesh, err = loadTouchMeshBuffers(*component)
		if err != nil {
			return TouchMeshDescriptor{}, err
		}
		session.Mesh[input.ComponentID] = mesh
	}
	revision := session.Analysis.MeshHash + ":" + input.ComponentID
	positionsURL, err := t.protocol.StoreMemoryBuffer(
		session.ProtocolID,
		"topology:"+input.ComponentID+":positions",
		modmesh.Float32Bytes(mesh.Positions),
		"application/octet-stream",
	)
	if err != nil {
		return TouchMeshDescriptor{}, err
	}
	descriptor := TouchMeshDescriptor{
		SessionID:        input.SessionID,
		ComponentID:      input.ComponentID,
		TopologyRevision: revision,
		VertexCount:      len(mesh.Positions) / 3,
		PositionsURL:     positionsURL,
		PositionsCount:   len(mesh.Positions),
		IndexCount:       len(mesh.Indices),
		Bones:            mesh.Bones,
		BlendStride:      mesh.BlendStride,
		BlendBytes:       len(mesh.BlendBytes),
	}
	if len(mesh.Indices) > 0 {
		value, storeErr := t.protocol.StoreMemoryBuffer(
			session.ProtocolID,
			"topology:"+input.ComponentID+":indices",
			modmesh.Uint32Bytes(mesh.Indices),
			"application/octet-stream",
		)
		if storeErr != nil {
			return TouchMeshDescriptor{}, storeErr
		}
		descriptor.IndicesURL = &value
	}
	if len(mesh.BlendBytes) > 0 {
		value, storeErr := t.protocol.StoreMemoryBuffer(
			session.ProtocolID,
			"topology:"+input.ComponentID+":blend",
			mesh.BlendBytes,
			"application/octet-stream",
		)
		if storeErr != nil {
			return TouchMeshDescriptor{}, storeErr
		}
		descriptor.BlendURL = &value
	}
	session.Topology[input.ComponentID] = descriptor
	return descriptor, nil
}

func (t *Service) TouchProfileAnalyzeComponents(
	ctx context.Context,
	input TouchProfileAnalyzeInput,
) (result TouchDraft, err error) {
	session, err := t.requireTouchSession(input.SessionID)
	if err != nil {
		return result, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	selected := map[string]bool{}
	for _, id := range input.ComponentIDs {
		selected[id] = true
	}
	selectionMap := map[string][]TouchBoneZoneSelection{}
	for _, selection := range input.BoneSelections {
		selectionMap[selection.ComponentID] = selection.Zones
	}
	threshold := [2]float64{defaultBoneWeightThreshold, defaultBoneWeightThresholdMax}
	if input.WeightThreshold != nil {
		threshold = *input.WeightThreshold
	}
	if threshold[0] < 0 || threshold[1] > 1 || threshold[0] > threshold[1] {
		return result, infra.ContractError("Touch bone weight threshold is invalid")
	}
	mode := "bone"
	if input.Mode != nil {
		mode = *input.Mode
	}
	meshCache := map[string]touchMeshBuffers{}
	drafts := []TouchComponentDraft{}
	objectID := 1
	selectedComponents := make([]TouchComponentAnalysis, 0, len(input.ComponentIDs))
	for _, component := range session.Analysis.Components {
		if selected[component.ID] {
			selectedComponents = append(selectedComponents, component)
		}
	}
	lastProgress := .05
	emitProgress := func(stage string, progress float64, message, componentID string) {
		lastProgress = max(lastProgress, progress)
		t.emitTouchProgress(input.SessionID, stage, lastProgress, message, componentID)
	}
	defer func() {
		if err != nil {
			t.touchMu.Lock()
			delete(t.touchSessions, input.SessionID)
			t.touchMu.Unlock()
			t.protocol.CleanupMemorySession(session.ProtocolID)
			t.reportCleanup(os.RemoveAll(session.Dir), "TouchProfileAnalyzeComponents")
		}
	}()
	for index, component := range selectedComponents {
		emitProgress(
			"preview",
			.1+float64(index)/float64(max(len(selectedComponents), 1))*.25,
			"Loading mesh for "+component.Name,
			component.ID,
		)
		mesh, loadErr := loadTouchMeshBuffers(component)
		if loadErr != nil {
			return result, loadErr
		}
		meshCache[component.ID] = mesh
		var draft TouchComponentDraft
		if mode == "bone" {
			emitProgress(
				"vision",
				.35+float64(index)/float64(max(len(selectedComponents), 1))*.4,
				"Analyzing bone zones for "+component.Name,
				component.ID,
			)
			stride := 0
			if mesh.BlendStride != nil {
				stride = *mesh.BlendStride
			}
			draft = analyzeTouchComponentBones(
				component,
				mesh.Positions,
				mesh.BlendBytes,
				stride,
				selectionMap[component.ID],
				threshold,
				objectID,
			)
		} else {
			draft = TouchComponentDraft{
				ComponentID: component.ID,
				ObjectID:    objectID,
				Zones:       []TouchZoneSpec{},
				Warnings:    []string{"Vision LLM mode is disabled"},
			}
		}
		draft.ObjectID = objectID
		if draft.Interactive {
			objectID++
		}
		drafts = append(drafts, draft)
	}
	for _, component := range session.Analysis.Components {
		if selected[component.ID] {
			continue
		}
		drafts = append(
			drafts,
			TouchComponentDraft{
				ComponentID: component.ID,
				ObjectID:    objectID,
				Zones:       []TouchZoneSpec{},
				Warnings:    []string{"Component was not selected for touch analysis"},
			},
		)
	}
	warnings := []string{}
	for _, reason := range session.Analysis.SupportReasons {
		if !regexp.MustCompile(`(?i)position stride 40 with pn-t layout`).MatchString(reason) {
			warnings = appendUniqueString(warnings, reason)
		}
	}
	interactive := []TouchComponentDraft{}
	for _, draft := range drafts {
		if draft.Interactive {
			interactive = append(interactive, draft)
			for _, warning := range draft.Warnings {
				warnings = appendUniqueString(warnings, warning)
			}
		}
	}
	canApply := touchDraftAutoApplyable(interactive)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result = TouchDraft{
		SessionID:      input.SessionID,
		CreatedAt:      now,
		SourceModRoot:  session.Analysis.SourceRoot,
		Analysis:       session.Analysis,
		Components:     drafts,
		ModelName:      map[bool]string{true: "bone-weight", false: ""}[mode == "bone"],
		LLM:            TouchProfileLLMSettings{Protocol: "openai-compatible", Reasoning: "auto"},
		PromptVersion:  touchPromptVersion,
		RuntimeVersion: touchRuntimeVersion,
		CanAutoApply:   canApply,
		Warnings:       warnings,
	}
	if err = writeTouchDraft(session.Dir, result); err != nil {
		return result, err
	}
	session.Draft = &result
	session.Mesh = meshCache
	clearTouchPreviewBuffers(t.protocol, session)
	message := "Draft ready (manual review recommended)"
	if result.CanAutoApply {
		message = "Draft ready for apply"
	}
	t.emitTouchProgress(input.SessionID, "complete", 1, message, "")
	return result, nil
}

//wails:ignore
func (t *Service) TouchProfileSaveDraft(ctx context.Context, draft TouchDraft) (TouchDraft, error) {
	session, err := t.requireTouchSession(draft.SessionID)
	if err != nil {
		return TouchDraft{}, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return TouchDraft{}, err
	}
	next := draft
	interactive := make([]TouchComponentDraft, 0, len(next.Components))
	for _, component := range next.Components {
		if component.Interactive {
			interactive = append(interactive, component)
		}
	}
	next.CanAutoApply = touchDraftAutoApplyable(interactive)
	if err = writeTouchDraft(session.Dir, next); err != nil {
		return TouchDraft{}, err
	}
	session.Draft = &next
	clearTouchPreviewBuffers(t.protocol, session)
	session.DraftRevision++
	return next, nil
}

//wails:ignore
func (t *Service) TouchProfileUpdateZoneSettings(
	ctx context.Context,
	input TouchProfileUpdateZoneSettingsInput,
) (TouchDraft, error) {
	session, err := t.requireTouchSession(input.SessionID)
	if err != nil {
		return TouchDraft{}, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return TouchDraft{}, err
	}
	if session.Operation != "" {
		return TouchDraft{}, infra.ContractError(fmt.Sprintf("Touch profile is busy with %s", session.Operation))
	}
	if session.Draft == nil {
		return TouchDraft{}, infra.ContractError(fmt.Sprintf("Touch profile has no draft: %s", input.SessionID))
	}
	settings, err := normalizeTouchZoneSettings(input.Settings)
	if err != nil {
		return TouchDraft{}, err
	}
	next := *session.Draft
	next.Components = append([]TouchComponentDraft(nil), session.Draft.Components...)
	foundComponent, foundZone := false, false
	for i := range next.Components {
		if next.Components[i].ComponentID != input.ComponentID {
			continue
		}
		foundComponent = true
		next.Components[i].Zones = append([]TouchZoneSpec(nil), next.Components[i].Zones...)
		for z := range next.Components[i].Zones {
			if next.Components[i].Zones[z].ID == input.ZoneID {
				next.Components[i].Zones[z].Settings = settings
				foundZone = true
			}
		}
	}
	if !foundComponent {
		return TouchDraft{}, infra.ContractError(fmt.Sprintf("Touch component draft not found: %s", input.ComponentID))
	}
	if !foundZone {
		return TouchDraft{}, infra.ContractError(
			fmt.Sprintf("Touch zone not found: %s:%s", input.ComponentID, input.ZoneID),
		)
	}
	if err = writeTouchDraft(session.Dir, next); err != nil {
		return TouchDraft{}, err
	}
	session.Draft = &next
	clearTouchPreviewBuffers(t.protocol, session)
	session.DraftRevision++
	return next, nil
}

func (t *Service) TouchProfileUpdateZoneSettingsBatch(
	ctx context.Context,
	input TouchProfileUpdateZoneSettingsBatchInput,
) (TouchProfileUpdateResult, error) {
	session, err := t.requireTouchSession(input.SessionID)
	if err != nil {
		return TouchProfileUpdateResult{}, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return TouchProfileUpdateResult{}, err
	}
	if session.Operation != "" {
		return TouchProfileUpdateResult{}, infra.ContractError(
			fmt.Sprintf("Touch profile is busy with %s", session.Operation),
		)
	}
	if session.Draft == nil {
		return TouchProfileUpdateResult{}, infra.ContractError(
			fmt.Sprintf("Touch profile has no draft: %s", input.SessionID),
		)
	}
	normalized := make([]TouchProfileZoneSettingsChange, len(input.Changes))
	for index, change := range input.Changes {
		settings, normalizeErr := normalizeTouchZoneSettings(change.Settings)
		if normalizeErr != nil {
			return TouchProfileUpdateResult{}, normalizeErr
		}
		normalized[index] = change
		normalized[index].Settings = settings
	}
	next := *session.Draft
	next.Components = append([]TouchComponentDraft(nil), session.Draft.Components...)
	for index := range next.Components {
		next.Components[index].Zones = append([]TouchZoneSpec(nil), next.Components[index].Zones...)
	}
	previewChanged := false
	for _, change := range normalized {
		found := false
		for componentIndex := range next.Components {
			if next.Components[componentIndex].ComponentID != change.ComponentID {
				continue
			}
			for zoneIndex := range next.Components[componentIndex].Zones {
				zone := &next.Components[componentIndex].Zones[zoneIndex]
				if zone.ID != change.ZoneID {
					continue
				}
				previewChanged = previewChanged || touchMaskSettingsChanged(zone.Settings, change.Settings)
				zone.Settings = change.Settings
				found = true
			}
		}
		if !found {
			return TouchProfileUpdateResult{}, infra.ContractError(
				fmt.Sprintf("Touch zone not found: %s:%s", change.ComponentID, change.ZoneID),
			)
		}
	}
	if err = writeTouchDraft(session.Dir, next); err != nil {
		return TouchProfileUpdateResult{}, err
	}
	session.Draft = &next
	session.DraftRevision++
	if previewChanged {
		clearTouchPreviewBuffers(t.protocol, session)
	}
	return TouchProfileUpdateResult{OK: true, DraftRevision: session.DraftRevision, PreviewChanged: previewChanged}, nil
}

func (t *Service) TouchProfileGetPreviewDescriptor(
	ctx context.Context,
	input TouchProfilePreviewInput,
) (TouchProfilePreviewDescriptor, error) {
	session, err := t.requireTouchSession(input.SessionID)
	if err != nil {
		return TouchProfilePreviewDescriptor{}, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return TouchProfilePreviewDescriptor{}, err
	}
	if preview, ok := session.Preview[input.ComponentID]; ok {
		return preview.descriptor, nil
	}
	if session.Draft == nil {
		return TouchProfilePreviewDescriptor{}, infra.ContractError(
			fmt.Sprintf("Touch profile has no draft: %s", input.SessionID),
		)
	}
	component := findTouchComponent(session.Analysis.Components, input.ComponentID)
	if component == nil {
		return TouchProfilePreviewDescriptor{}, infra.ContractError(
			fmt.Sprintf("Touch component not found: %s", input.ComponentID),
		)
	}
	draft := findTouchDraft(session.Draft.Components, input.ComponentID)
	if draft == nil {
		return TouchProfilePreviewDescriptor{}, infra.ContractError(
			fmt.Sprintf("Touch component draft not found: %s", input.ComponentID),
		)
	}
	mesh, ok := session.Mesh[input.ComponentID]
	if !ok {
		mesh, err = loadTouchMeshBuffers(*component)
		if err != nil {
			return TouchProfilePreviewDescriptor{}, err
		}
		session.Mesh[input.ComponentID] = mesh
	}
	t.emitTouchProgress(input.SessionID, "preview", .4, "Building mask for "+component.Name, component.ID)
	masks, err := buildTouchVertexMasksContext(
		ctx,
		component.VertexCount,
		mesh.Positions,
		mesh.Indices,
		*component,
		draft.Zones,
	)
	if err != nil {
		return TouchProfilePreviewDescriptor{}, err
	}
	weights := make([]float32, 0, component.VertexCount*len(draft.Zones))
	zones := make([]TouchPreviewZoneDescriptor, 0, len(draft.Zones))
	for _, zone := range draft.Zones {
		offset := len(weights)
		weights = append(weights, extractTouchMaskChannel(masks, component.VertexCount, zone.Channel)...)
		zones = append(zones, TouchPreviewZoneDescriptor{TouchZoneSpec: zone, WeightOffset: offset})
	}
	session.PreviewRevision++
	bufferID := fmt.Sprintf("preview:%s:%d", component.ID, session.PreviewRevision)
	weightsURL, err := t.protocol.StoreMemoryBuffer(
		session.ProtocolID,
		bufferID,
		modmesh.Float32Bytes(weights),
		"application/octet-stream",
	)
	if err != nil {
		return TouchProfilePreviewDescriptor{}, err
	}
	descriptor := TouchProfilePreviewDescriptor{
		SessionID:       input.SessionID,
		ComponentID:     component.ID,
		PreviewRevision: session.PreviewRevision,
		VertexCount:     component.VertexCount,
		WeightsURL:      weightsURL,
		WeightsCount:    len(weights),
		Zones:           zones,
	}
	if previous, ok := session.Preview[input.ComponentID]; ok {
		t.protocol.RemoveMemoryBuffer(session.ProtocolID, previous.bufferID)
	}
	session.Preview[input.ComponentID] = touchCachedPreview{descriptor: descriptor, bufferID: bufferID}
	return descriptor, nil
}

func (t *Service) TouchProfileDiscardDraft(_ context.Context, sessionID string) (TouchProfileOK, error) {
	t.touchMu.Lock()
	session := t.touchSessions[sessionID]
	delete(t.touchSessions, sessionID)
	t.touchMu.Unlock()
	if session != nil {
		t.protocol.CleanupMemorySession(session.ProtocolID)
		t.reportCleanup(os.RemoveAll(session.Dir), "TouchProfileDiscardDraft")
	}
	return TouchProfileOK{OK: true}, nil
}

func (t *Service) TouchProfileCloseSession(ctx context.Context, sessionID string) (TouchProfileOK, error) {
	return t.TouchProfileDiscardDraft(ctx, sessionID)
}

func (t *Service) TouchProfileApply(ctx context.Context, input TouchProfileApplyInput) (TouchApplyResult, error) {
	session, err := t.requireTouchSession(input.SessionID)
	if err != nil {
		return TouchApplyResult{}, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.Draft == nil {
		return TouchApplyResult{}, infra.ContractError(
			fmt.Sprintf("Touch profile has no draft to apply: %s", input.SessionID),
		)
	}
	if err = assertTouchDraftCanApply(*session.Draft, input.Force); err != nil {
		return TouchApplyResult{}, err
	}
	if session.Applied != nil {
		return TouchApplyResult{}, infra.ContractError("Touch profile is already applied. Use regenerate instead.")
	}
	sourceRoot, err := filepath.Abs(session.Draft.SourceModRoot)
	if err != nil {
		return TouchApplyResult{}, err
	}
	if err = assertTouchProfileInputAllowed(sourceRoot); err != nil {
		return TouchApplyResult{}, err
	}
	if t.mod == nil {
		return TouchApplyResult{}, errors.New("tools service has no mod service")
	}
	parent := filepath.Dir(sourceRoot)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return TouchApplyResult{}, err
	}
	names := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	targetName := t.fs.GetUniqueName(touchFolderBaseName(filepath.Base(sourceRoot)), names)
	targetRoot := filepath.Join(parent, targetName)
	if !platform.SameOrChildPath(parent, targetRoot) || platform.SamePathFold(sourceRoot, targetRoot) {
		return TouchApplyResult{}, errors.New("invalid touch output path")
	}
	if err = claimTouchOperation(session, "apply"); err != nil {
		return TouchApplyResult{}, err
	}
	defer func() { session.Operation = "" }()
	validation, err := t.generateTouchOutput(ctx, session, sourceRoot, targetRoot, "apply")
	if err != nil {
		t.reportCleanup(os.RemoveAll(targetRoot), "TouchProfileApply")
		return TouchApplyResult{}, err
	}
	reenable := !touchDisabledPrefixRE.MatchString(filepath.Base(sourceRoot))
	disabledSource, err := t.mod.Disable(ctx, sourceRoot)
	if err != nil {
		t.reportCleanup(os.RemoveAll(targetRoot), "TouchProfileApply")
		return TouchApplyResult{}, err
	}
	session.Applied = &touchAppliedProfile{OutputRoot: targetRoot, SourceRoot: disabledSource, Reenable: reenable}
	t.emitTouchProgress(input.SessionID, "complete", 1, "Touch mod created", "")
	return TouchApplyResult{
		SessionID:                input.SessionID,
		OutputModRoot:            targetRoot,
		SourceModRoot:            disabledSource,
		ReenableSourceOnRollback: reenable,
		Validation:               validation,
		Warnings:                 session.Draft.Warnings,
	}, nil
}

func (t *Service) TouchProfileRegenerate(ctx context.Context, input TouchProfileApplyInput) (TouchApplyResult, error) {
	session, err := t.requireTouchSession(input.SessionID)
	if err != nil {
		return TouchApplyResult{}, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.Draft == nil {
		return TouchApplyResult{}, infra.ContractError(
			fmt.Sprintf("Touch profile has no draft to regenerate: %s", input.SessionID),
		)
	}
	if err = assertTouchDraftCanApply(*session.Draft, input.Force); err != nil {
		return TouchApplyResult{}, err
	}
	if session.Applied == nil {
		return TouchApplyResult{}, infra.ContractError("Touch profile has not been applied yet.")
	}
	if err = claimTouchOperation(session, "regenerate"); err != nil {
		return TouchApplyResult{}, err
	}
	defer func() { session.Operation = "" }()
	sourceRoot, outputRoot := session.Applied.SourceRoot, session.Applied.OutputRoot
	id, _ := newTouchID()
	if len(id) > 8 {
		id = id[:8]
	}
	staging := filepath.Join(filepath.Dir(outputRoot), "."+filepath.Base(outputRoot)+".regenerating-"+id)
	defer func() { t.reportCleanup(os.RemoveAll(staging), "TouchProfileRegenerate") }()
	if !pathIsDirectory(sourceRoot) {
		return TouchApplyResult{}, infra.ContractError(fmt.Sprintf("Touch source mod not found: %s", sourceRoot))
	}
	if !pathIsDirectory(outputRoot) {
		return TouchApplyResult{}, infra.ContractError(fmt.Sprintf("Touch output mod not found: %s", outputRoot))
	}
	if err = assertTouchProfileInputAllowed(sourceRoot); err != nil {
		return TouchApplyResult{}, err
	}
	if err = assertTouchSourceUnchanged(session.Analysis, sourceRoot); err != nil {
		return TouchApplyResult{}, err
	}
	validation, err := t.generateTouchOutput(ctx, session, sourceRoot, staging, "regenerate")
	if err != nil {
		return TouchApplyResult{}, err
	}
	if err = replaceTouchOutput(staging, outputRoot); err != nil {
		return TouchApplyResult{}, err
	}
	t.emitTouchProgress(input.SessionID, "complete", 1, "Touch mod regenerated", "")
	return TouchApplyResult{
		SessionID:                input.SessionID,
		OutputModRoot:            outputRoot,
		SourceModRoot:            sourceRoot,
		ReenableSourceOnRollback: session.Applied.Reenable,
		Validation:               validation,
		Warnings:                 session.Draft.Warnings,
	}, nil
}

func (t *Service) TouchProfileRollback(
	ctx context.Context,
	input TouchProfileRollbackInput,
) (TouchRollbackResult, error) {
	session, err := t.requireTouchSession(input.SessionID)
	if err != nil {
		return TouchRollbackResult{}, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.Applied == nil {
		return TouchRollbackResult{}, infra.ContractError("Touch profile has already been rolled back")
	}
	outputRoot, _ := filepath.Abs(input.OutputModRoot)
	sourceRoot, _ := filepath.Abs(input.SourceModRoot)
	if !platform.SamePathFold(session.Applied.OutputRoot, outputRoot) ||
		!platform.SamePathFold(session.Applied.SourceRoot, sourceRoot) ||
		session.Applied.Reenable != input.ReenableSourceOnRollback {
		return TouchRollbackResult{}, infra.ContractError(
			"Touch rollback paths do not match the active touch profile session",
		)
	}
	if platform.SamePathFold(outputRoot, sourceRoot) {
		return TouchRollbackResult{}, infra.ContractError(
			"Touch rollback refused: output and source paths are identical",
		)
	}
	if !pathIsDirectory(outputRoot) {
		return TouchRollbackResult{}, infra.ContractError(fmt.Sprintf("Touch output mod not found: %s", outputRoot))
	}
	if !pathIsDirectory(sourceRoot) {
		return TouchRollbackResult{}, infra.ContractError(fmt.Sprintf("Touch source mod not found: %s", sourceRoot))
	}
	if err = claimTouchOperation(session, "rollback"); err != nil {
		return TouchRollbackResult{}, err
	}
	defer func() { session.Operation = "" }()
	if err = os.RemoveAll(outputRoot); err != nil {
		return TouchRollbackResult{}, err
	}
	restored := sourceRoot
	reenabled := false
	if input.ReenableSourceOnRollback {
		restored, err = t.mod.Enable(ctx, sourceRoot)
		if err != nil {
			return TouchRollbackResult{}, err
		}
		reenabled = true
	}
	if session.Draft != nil {
		next := *session.Draft
		next.SourceModRoot = restored
		if err = writeTouchDraft(session.Dir, next); err != nil {
			return TouchRollbackResult{}, err
		}
		session.Draft = &next
	}
	session.Applied = nil
	return TouchRollbackResult{
		OutputModRoot:   outputRoot,
		SourceModRoot:   restored,
		RemovedOutput:   true,
		ReenabledSource: reenabled,
	}, nil
}
