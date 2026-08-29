package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"nahida.live/desktop/internal/appdata"
)

const touchProgressEventName = "tools:touchProfileProgress"

type TouchProfileLoadInput struct {
	ModPath string `json:"modPath"`
}
type TouchProfilePreviewInput struct {
	SessionID   string `json:"sessionId"`
	ComponentID string `json:"componentId"`
}
type TouchProfileAnalyzeInput struct {
	SessionID       string                        `json:"sessionId"`
	ComponentIDs    []string                      `json:"componentIds"`
	Mode            *string                       `json:"mode,omitempty"`
	BoneSelections  []TouchBoneComponentSelection `json:"boneSelections,omitempty"`
	WeightThreshold *[2]float64                   `json:"weightThreshold,omitempty"`
}
type TouchProfileUpdateZoneSettingsInput struct {
	SessionID   string            `json:"sessionId"`
	ComponentID string            `json:"componentId"`
	ZoneID      string            `json:"zoneId"`
	Settings    TouchZoneSettings `json:"settings"`
}
type TouchProfileApplyInput struct {
	SessionID string `json:"sessionId"`
	Force     bool   `json:"force,omitempty"`
}
type TouchProfileRollbackInput struct {
	SessionID                string `json:"sessionId"`
	OutputModRoot            string `json:"outputModRoot"`
	SourceModRoot            string `json:"sourceModRoot"`
	ReenableSourceOnRollback bool   `json:"reenableSourceOnRollback"`
}
type TouchProfileOK struct {
	OK bool `json:"ok"`
}

type touchAppliedProfile struct {
	OutputRoot, SourceRoot string
	Reenable               bool
}
type touchSession struct {
	mu        sync.Mutex
	Analysis  TouchModAnalysis
	Dir       string
	Mesh      map[string]touchMeshBuffers
	Preview   map[string]TouchProfilePreview
	Draft     *TouchDraft
	Applied   *touchAppliedProfile
	Operation string
}

func (t *Tools) TouchProfilePrepare(ctx context.Context, input TouchProfileLoadInput) (result TouchModInspection, err error) {
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if strings.TrimSpace(input.ModPath) == "" {
		return result, contractError("Touch profile mod path is required")
	}
	id, err := newToolsID()
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
			_ = os.RemoveAll(sessionDir)
		}
	}()
	t.emitTouchProgress(id, "scan", .05, "Scanning mod structure", "")
	analysis, err := analyzeTouchMod(input.ModPath, func(message string) {
		if t.log != nil {
			t.log.Warn(message, "TouchProfile")
		}
	})
	if err != nil {
		t.logError(err, "TouchProfile:prepareMod:"+input.ModPath)
		return result, err
	}
	session := &touchSession{Analysis: analysis, Dir: sessionDir, Mesh: map[string]touchMeshBuffers{}, Preview: map[string]TouchProfilePreview{}}
	t.touchMu.Lock()
	t.touchSessions[id] = session
	t.touchMu.Unlock()
	result = TouchModInspection{SessionID: id, ModRoot: analysis.ModRoot, INIRelativePath: analysis.INIRelativePath, SourceFilesRelativePaths: analysis.SourceFilesRelativePaths, SupportGrade: analysis.SupportGrade, SupportReasons: analysis.SupportReasons, Components: []TouchComponentInspection{}}
	for _, component := range analysis.Components {
		result.Components = append(result.Components, TouchComponentInspection{ID: component.ID, Name: component.Name, Kind: component.Kind, SupportGrade: component.SupportGrade, InteractiveCandidate: component.InteractiveCandidate, VertexCount: component.VertexCount, IndexCount: component.IndexCount, VariantKey: component.VariantKey, VariantCondition: component.VariantCondition, ObjectMaps: component.ObjectMaps, HasBlend: component.BlendPath != nil, Bones: component.Bones})
	}
	return result, nil
}

func (t *Tools) TouchProfileGetMeshPreview(ctx context.Context, input TouchProfilePreviewInput) (TouchMeshPreview, error) {
	session, err := t.requireTouchSession(input.SessionID)
	if err != nil {
		return TouchMeshPreview{}, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return TouchMeshPreview{}, err
	}
	mesh, ok := session.Mesh[input.ComponentID]
	if !ok {
		component := findTouchComponent(session.Analysis.Components, input.ComponentID)
		if component == nil {
			return TouchMeshPreview{}, contractError(fmt.Sprintf("Touch component not found: %s", input.ComponentID))
		}
		mesh, err = loadTouchMeshBuffers(*component)
		if err != nil {
			return TouchMeshPreview{}, err
		}
		session.Mesh[input.ComponentID] = mesh
	}
	return TouchMeshPreview{SessionID: input.SessionID, ComponentID: input.ComponentID, VertexCount: len(mesh.Positions) / 3, Positions: mesh.Positions, Indices: mesh.Indices, Bones: mesh.Bones, BlendStride: mesh.BlendStride, BlendBytes: mesh.BlendBytes}, nil
}

func (t *Tools) TouchProfileAnalyzeComponents(ctx context.Context, input TouchProfileAnalyzeInput) (result TouchDraft, err error) {
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
		return result, contractError("Touch bone weight threshold is invalid")
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
			_ = os.RemoveAll(session.Dir)
		}
	}()
	for index, component := range selectedComponents {
		emitProgress("preview", .1+float64(index)/float64(max(len(selectedComponents), 1))*.25, "Loading mesh for "+component.Name, component.ID)
		mesh, loadErr := loadTouchMeshBuffers(component)
		if loadErr != nil {
			return result, loadErr
		}
		meshCache[component.ID] = mesh
		var draft TouchComponentDraft
		if mode == "bone" {
			emitProgress("vision", .35+float64(index)/float64(max(len(selectedComponents), 1))*.4, "Analyzing bone zones for "+component.Name, component.ID)
			stride := 0
			if mesh.BlendStride != nil {
				stride = *mesh.BlendStride
			}
			draft = analyzeTouchComponentBones(component, mesh.Positions, mesh.BlendBytes, stride, selectionMap[component.ID], threshold, objectID)
		} else {
			draft = TouchComponentDraft{ComponentID: component.ID, ObjectID: objectID, Zones: []TouchZoneSpec{}, Warnings: []string{"Vision LLM mode is disabled"}}
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
		drafts = append(drafts, TouchComponentDraft{ComponentID: component.ID, ObjectID: objectID, Zones: []TouchZoneSpec{}, Warnings: []string{"Component was not selected for touch analysis"}})
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
	result = TouchDraft{SessionID: input.SessionID, CreatedAt: now, SourceModRoot: session.Analysis.SourceRoot, Analysis: session.Analysis, Components: drafts, ModelName: map[bool]string{true: "bone-weight", false: ""}[mode == "bone"], LLM: TouchProfileLLMSettings{Protocol: "openai-compatible", Reasoning: "auto"}, PromptVersion: touchPromptVersion, RuntimeVersion: touchRuntimeVersion, CanAutoApply: canApply, Warnings: warnings}
	if err = writeTouchDraft(session.Dir, result); err != nil {
		return result, err
	}
	session.Draft = &result
	session.Mesh = meshCache
	session.Preview = map[string]TouchProfilePreview{}
	message := "Draft ready (manual review recommended)"
	if result.CanAutoApply {
		message = "Draft ready for apply"
	}
	t.emitTouchProgress(input.SessionID, "complete", 1, message, "")
	return result, nil
}

func (t *Tools) TouchProfileSaveDraft(ctx context.Context, draft TouchDraft) (TouchDraft, error) {
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
	session.Preview = map[string]TouchProfilePreview{}
	return next, nil
}

func (t *Tools) TouchProfileUpdateZoneSettings(ctx context.Context, input TouchProfileUpdateZoneSettingsInput) (TouchDraft, error) {
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
		return TouchDraft{}, contractError(fmt.Sprintf("Touch profile is busy with %s", session.Operation))
	}
	if session.Draft == nil {
		return TouchDraft{}, contractError(fmt.Sprintf("Touch profile has no draft: %s", input.SessionID))
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
		return TouchDraft{}, contractError(fmt.Sprintf("Touch component draft not found: %s", input.ComponentID))
	}
	if !foundZone {
		return TouchDraft{}, contractError(fmt.Sprintf("Touch zone not found: %s:%s", input.ComponentID, input.ZoneID))
	}
	if err = writeTouchDraft(session.Dir, next); err != nil {
		return TouchDraft{}, err
	}
	session.Draft = &next
	session.Preview = map[string]TouchProfilePreview{}
	return next, nil
}

func (t *Tools) TouchProfileGetPreview(ctx context.Context, input TouchProfilePreviewInput) (TouchProfilePreview, error) {
	session, err := t.requireTouchSession(input.SessionID)
	if err != nil {
		return TouchProfilePreview{}, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return TouchProfilePreview{}, err
	}
	if preview, ok := session.Preview[input.ComponentID]; ok {
		return preview, nil
	}
	if session.Draft == nil {
		return TouchProfilePreview{}, contractError(fmt.Sprintf("Touch profile has no draft: %s", input.SessionID))
	}
	component := findTouchComponent(session.Analysis.Components, input.ComponentID)
	if component == nil {
		return TouchProfilePreview{}, contractError(fmt.Sprintf("Touch component not found: %s", input.ComponentID))
	}
	draft := findTouchDraft(session.Draft.Components, input.ComponentID)
	if draft == nil {
		return TouchProfilePreview{}, contractError(fmt.Sprintf("Touch component draft not found: %s", input.ComponentID))
	}
	mesh, ok := session.Mesh[input.ComponentID]
	if !ok {
		mesh, err = loadTouchMeshBuffers(*component)
		if err != nil {
			return TouchProfilePreview{}, err
		}
		session.Mesh[input.ComponentID] = mesh
	}
	t.emitTouchProgress(input.SessionID, "preview", .4, "Building mask for "+component.Name, component.ID)
	masks := buildTouchVertexMasks(component.VertexCount, mesh.Positions, mesh.Indices, *component, draft.Zones)
	preview := TouchProfilePreview{SessionID: input.SessionID, ComponentID: component.ID, VertexCount: component.VertexCount, Positions: mesh.Positions, Indices: mesh.Indices, Zones: []TouchPreviewZone{}}
	for _, zone := range draft.Zones {
		preview.Zones = append(preview.Zones, TouchPreviewZone{TouchZoneSpec: zone, Weights: extractTouchMaskChannel(masks, component.VertexCount, zone.Channel)})
	}
	session.Preview[input.ComponentID] = preview
	return preview, nil
}

func (t *Tools) TouchProfileDiscardDraft(_ context.Context, sessionID string) (TouchProfileOK, error) {
	t.touchMu.Lock()
	session := t.touchSessions[sessionID]
	delete(t.touchSessions, sessionID)
	t.touchMu.Unlock()
	if session != nil {
		_ = os.RemoveAll(session.Dir)
	}
	return TouchProfileOK{OK: true}, nil
}

func (t *Tools) TouchProfileApply(ctx context.Context, input TouchProfileApplyInput) (TouchApplyResult, error) {
	session, err := t.requireTouchSession(input.SessionID)
	if err != nil {
		return TouchApplyResult{}, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.Draft == nil {
		return TouchApplyResult{}, contractError(fmt.Sprintf("Touch profile has no draft to apply: %s", input.SessionID))
	}
	if err = assertTouchDraftCanApply(*session.Draft, input.Force); err != nil {
		return TouchApplyResult{}, err
	}
	if session.Applied != nil {
		return TouchApplyResult{}, contractError("Touch profile is already applied. Use regenerate instead.")
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
	if !sameOrChildPath(parent, targetRoot) || samePathFold(sourceRoot, targetRoot) {
		return TouchApplyResult{}, errors.New("invalid touch output path")
	}
	if err = claimTouchOperation(session, "apply"); err != nil {
		return TouchApplyResult{}, err
	}
	defer func() { session.Operation = "" }()
	validation, err := t.generateTouchOutput(ctx, session, sourceRoot, targetRoot, "apply")
	if err != nil {
		_ = os.RemoveAll(targetRoot)
		return TouchApplyResult{}, err
	}
	reenable := !touchDisabledPrefixRE.MatchString(filepath.Base(sourceRoot))
	disabledSource, err := t.mod.Disable(ctx, sourceRoot)
	if err != nil {
		_ = os.RemoveAll(targetRoot)
		return TouchApplyResult{}, err
	}
	session.Applied = &touchAppliedProfile{OutputRoot: targetRoot, SourceRoot: disabledSource, Reenable: reenable}
	t.emitTouchProgress(input.SessionID, "complete", 1, "Touch mod created", "")
	return TouchApplyResult{SessionID: input.SessionID, OutputModRoot: targetRoot, SourceModRoot: disabledSource, ReenableSourceOnRollback: reenable, Validation: validation, Warnings: session.Draft.Warnings}, nil
}

func (t *Tools) TouchProfileRegenerate(ctx context.Context, input TouchProfileApplyInput) (TouchApplyResult, error) {
	session, err := t.requireTouchSession(input.SessionID)
	if err != nil {
		return TouchApplyResult{}, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.Draft == nil {
		return TouchApplyResult{}, contractError(fmt.Sprintf("Touch profile has no draft to regenerate: %s", input.SessionID))
	}
	if err = assertTouchDraftCanApply(*session.Draft, input.Force); err != nil {
		return TouchApplyResult{}, err
	}
	if session.Applied == nil {
		return TouchApplyResult{}, contractError("Touch profile has not been applied yet.")
	}
	if err = claimTouchOperation(session, "regenerate"); err != nil {
		return TouchApplyResult{}, err
	}
	defer func() { session.Operation = "" }()
	sourceRoot, outputRoot := session.Applied.SourceRoot, session.Applied.OutputRoot
	id, _ := newToolsID()
	if len(id) > 8 {
		id = id[:8]
	}
	staging := filepath.Join(filepath.Dir(outputRoot), "."+filepath.Base(outputRoot)+".regenerating-"+id)
	defer func() { _ = os.RemoveAll(staging) }()
	if !pathIsDirectory(sourceRoot) {
		return TouchApplyResult{}, contractError(fmt.Sprintf("Touch source mod not found: %s", sourceRoot))
	}
	if !pathIsDirectory(outputRoot) {
		return TouchApplyResult{}, contractError(fmt.Sprintf("Touch output mod not found: %s", outputRoot))
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
	return TouchApplyResult{SessionID: input.SessionID, OutputModRoot: outputRoot, SourceModRoot: sourceRoot, ReenableSourceOnRollback: session.Applied.Reenable, Validation: validation, Warnings: session.Draft.Warnings}, nil
}

func (t *Tools) TouchProfileRollback(ctx context.Context, input TouchProfileRollbackInput) (TouchRollbackResult, error) {
	session, err := t.requireTouchSession(input.SessionID)
	if err != nil {
		return TouchRollbackResult{}, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.Applied == nil {
		return TouchRollbackResult{}, contractError("Touch profile has already been rolled back")
	}
	outputRoot, _ := filepath.Abs(input.OutputModRoot)
	sourceRoot, _ := filepath.Abs(input.SourceModRoot)
	if !samePathFold(session.Applied.OutputRoot, outputRoot) || !samePathFold(session.Applied.SourceRoot, sourceRoot) || session.Applied.Reenable != input.ReenableSourceOnRollback {
		return TouchRollbackResult{}, contractError("Touch rollback paths do not match the active touch profile session")
	}
	if samePathFold(outputRoot, sourceRoot) {
		return TouchRollbackResult{}, contractError("Touch rollback refused: output and source paths are identical")
	}
	if !pathIsDirectory(outputRoot) {
		return TouchRollbackResult{}, contractError(fmt.Sprintf("Touch output mod not found: %s", outputRoot))
	}
	if !pathIsDirectory(sourceRoot) {
		return TouchRollbackResult{}, contractError(fmt.Sprintf("Touch source mod not found: %s", sourceRoot))
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
	return TouchRollbackResult{OutputModRoot: outputRoot, SourceModRoot: restored, RemovedOutput: true, ReenabledSource: reenabled}, nil
}

func (t *Tools) generateTouchOutput(ctx context.Context, session *touchSession, sourceRoot, targetRoot, operation string) (TouchValidationResult, error) {
	draft := session.Draft
	if draft == nil {
		return TouchValidationResult{}, contractError("Touch profile has no draft for output generation")
	}
	namespace := sanitizeTouchNamespace(sourceRoot)
	varPrefix := "nhd_touch_" + strings.ToLower(namespace)
	analysis, err := rebaseTouchAnalysis(draft.Analysis, sourceRoot, targetRoot)
	if err != nil {
		return TouchValidationResult{}, err
	}
	interactive := []TouchComponentAnalysis{}
	for _, component := range analysis.Components {
		componentDraft := findTouchDraft(draft.Components, component.ID)
		if componentDraft != nil && componentDraft.Interactive && len(componentDraft.Zones) > 0 {
			interactive = append(interactive, component)
		}
	}
	if len(interactive) == 0 {
		return TouchValidationResult{}, contractError("No interactive components selected for touch conversion")
	}
	message := "Copying mod to touch output folder"
	if operation == "regenerate" {
		message = "Preparing regenerated touch output"
	}
	t.emitTouchProgress(draft.SessionID, "assets", .2, message, "")
	if err = copyBodyShapeTree(ctx, sourceRoot, targetRoot); err != nil {
		return TouchValidationResult{}, err
	}
	assets := []TouchGeneratedAssets{}
	for index, component := range interactive {
		componentDraft := findTouchDraft(draft.Components, component.ID)
		t.emitTouchProgress(draft.SessionID, "assets", .3+float64(index)/float64(len(interactive))*.3, "Generating touch assets for "+component.Name, component.ID)
		mesh, ok := session.Mesh[component.ID]
		if !ok {
			if operation == "regenerate" {
				return TouchValidationResult{}, contractError(fmt.Sprintf("Touch mesh cache is missing for %s; analyze the mod again before regenerating", component.ID))
			}
			mesh, err = loadTouchMeshBuffers(component)
			if err != nil {
				return TouchValidationResult{}, err
			}
		}
		asset, assetErr := writeTouchComponentAssets(targetRoot, component, *componentDraft, mesh.Positions, mesh.Indices, touchAssetPrefix(component, namespace))
		if assetErr != nil {
			return TouchValidationResult{}, assetErr
		}
		assets = append(assets, asset)
	}
	if err = copyTouchRuntimeShaders(targetRoot); err != nil {
		return TouchValidationResult{}, err
	}
	t.emitTouchProgress(draft.SessionID, "ini", .75, "Patching touch INI", "")
	analysisRoot := filepath.Join(sourceRoot, draft.Analysis.ModRootRelativeToSource)
	sourceINI, err := resolveTouchRelative(analysisRoot, draft.Analysis.INIRelativePath)
	if err != nil {
		return TouchValidationResult{}, err
	}
	targetINI, err := remapTouchPath(sourceINI, sourceRoot, targetRoot)
	if err != nil {
		return TouchValidationResult{}, err
	}
	analysis.Components = interactive
	if _, _, err = compileTouchINI(sourceINI, targetINI, analysis, draft.Components, assets, namespace, varPrefix, t.touchUseFrameGuard(ctx)); err != nil {
		return TouchValidationResult{}, err
	}
	t.emitTouchProgress(draft.SessionID, "validate", .9, "Validating generated touch mod", "")
	validation, err := validateTouchOutput(targetRoot, targetINI, interactive, draft.Components, assets)
	if err != nil {
		return validation, err
	}
	if !validation.OK {
		messages := []string{}
		for _, issue := range validation.Issues {
			if issue.Level == "error" {
				messages = append(messages, issue.Message)
			}
		}
		return validation, contractError("Touch validation failed: " + strings.Join(messages, "; "))
	}
	if err = writeTouchManifest(targetRoot, draft.RuntimeVersion); err != nil {
		return validation, err
	}
	return validation, nil
}

var touchDisabledPrefixRE = regexp.MustCompile(`(?i)^(?:disabled[\s_]*)+[\s_]+`)

func touchFolderBaseName(name string) string {
	return touchDisabledPrefixRE.ReplaceAllString(strings.TrimSpace(name), "") + touchFolderSuffix
}
func (t *Tools) requireTouchSession(id string) (*touchSession, error) {
	t.touchMu.Lock()
	session := t.touchSessions[id]
	t.touchMu.Unlock()
	if session == nil {
		return nil, contractError(fmt.Sprintf("Touch profile session not found: %s", id))
	}
	return session, nil
}
func claimTouchOperation(session *touchSession, operation string) error {
	if session.Operation != "" {
		return contractError(fmt.Sprintf("Touch profile is busy with %s", session.Operation))
	}
	session.Operation = operation
	return nil
}
func (t *Tools) emitTouchProgress(sessionID, stage string, progress float64, message, componentID string) {
	event := TouchProgressEvent{Stage: stage, Progress: progress, Message: message}
	if sessionID != "" {
		event.SessionID = &sessionID
	}
	if componentID != "" {
		event.ComponentID = &componentID
	}
	t.emitEvent(touchProgressEventName, event)
}
func (t *Tools) shutdownTouchProfiles() error {
	t.touchMu.Lock()
	sessions := t.touchSessions
	t.touchSessions = map[string]*touchSession{}
	t.touchMu.Unlock()
	var err error
	for _, session := range sessions {
		session.mu.Lock()
		if session.Operation != "" {
			err = errors.Join(err, fmt.Errorf("touch profile is busy with %s", session.Operation))
		} else {
			err = errors.Join(err, os.RemoveAll(session.Dir))
		}
		session.mu.Unlock()
	}
	return err
}

func findTouchComponent(components []TouchComponentAnalysis, id string) *TouchComponentAnalysis {
	for i := range components {
		if components[i].ID == id {
			return &components[i]
		}
	}
	return nil
}
func findTouchDraft(drafts []TouchComponentDraft, id string) *TouchComponentDraft {
	for i := range drafts {
		if drafts[i].ComponentID == id {
			return &drafts[i]
		}
	}
	return nil
}
func appendUniqueString(values []string, value string) []string {
	for _, entry := range values {
		if entry == value {
			return values
		}
	}
	return append(values, value)
}
func touchDraftAutoApplyable(interactive []TouchComponentDraft) bool {
	if len(interactive) == 0 {
		return false
	}
	minimum, total := 1.0, 0.0
	for _, entry := range interactive {
		minimum = min(minimum, entry.Confidence)
		total += entry.Confidence
	}
	return minimum >= touchConfidenceAutoMin && total/float64(len(interactive)) >= touchConfidenceAutoAverage
}
func assertTouchDraftCanApply(draft TouchDraft, force bool) error {
	if !force && !draft.CanAutoApply {
		return contractError("Touch draft confidence is too low for automatic apply. Review zones or pass force=true.")
	}
	return nil
}

func writeTouchDraft(dir string, draft TouchDraft) error {
	raw, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		return err
	}
	return writeBodyFileAtomic(filepath.Join(dir, "draft.json"), raw, 0600)
}

func rebaseTouchAnalysis(analysis TouchModAnalysis, sourceRoot, targetRoot string) (TouchModAnalysis, error) {
	analysisRoot := filepath.Join(sourceRoot, analysis.ModRootRelativeToSource)
	ini, err := resolveTouchRelative(analysisRoot, analysis.INIRelativePath)
	if err != nil {
		return analysis, err
	}
	analysis.INIPath, err = remapTouchPath(ini, sourceRoot, targetRoot)
	if err != nil {
		return analysis, err
	}
	analysis.ModRoot = targetRoot
	for i := range analysis.Components {
		component := &analysis.Components[i]
		path, resolveErr := resolveTouchRelative(analysisRoot, component.PositionRelativePath)
		if resolveErr != nil {
			return analysis, resolveErr
		}
		component.PositionPath, err = remapTouchPath(path, sourceRoot, targetRoot)
		if err != nil {
			return analysis, err
		}
		component.IndexPaths = []string{}
		for _, relative := range component.IndexRelativePaths {
			path, resolveErr = resolveTouchRelative(analysisRoot, relative)
			if resolveErr != nil {
				return analysis, resolveErr
			}
			path, err = remapTouchPath(path, sourceRoot, targetRoot)
			if err != nil {
				return analysis, err
			}
			component.IndexPaths = append(component.IndexPaths, path)
		}
		if component.IndexRelativePath != nil {
			path, resolveErr = resolveTouchRelative(analysisRoot, *component.IndexRelativePath)
			if resolveErr != nil {
				return analysis, resolveErr
			}
			path, err = remapTouchPath(path, sourceRoot, targetRoot)
			if err != nil {
				return analysis, err
			}
			component.IndexPath = &path
		}
		if component.BlendRelativePath != nil {
			path, resolveErr = resolveTouchRelative(analysisRoot, *component.BlendRelativePath)
			if resolveErr != nil {
				return analysis, resolveErr
			}
			path, err = remapTouchPath(path, sourceRoot, targetRoot)
			if err != nil {
				return analysis, err
			}
			component.BlendPath = &path
		}
	}
	return analysis, nil
}
func resolveTouchRelative(root, relative string) (string, error) {
	root, _ = filepath.Abs(root)
	absolute, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil || samePathFold(root, absolute) || !sameOrChildPath(root, absolute) {
		return "", contractError(fmt.Sprintf("Path is outside mod root: %s", relative))
	}
	return absolute, nil
}
func remapTouchPath(path, sourceRoot, targetRoot string) (string, error) {
	absolute, _ := filepath.Abs(path)
	relative, err := filepath.Rel(sourceRoot, absolute)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", contractError(fmt.Sprintf("Path is outside mod root: %s", path))
	}
	return filepath.Join(targetRoot, relative), nil
}
func assertTouchSourceUnchanged(analysis TouchModAnalysis, sourceRoot string) error {
	root := filepath.Join(sourceRoot, analysis.ModRootRelativeToSource)
	meshPaths := []string{}
	for _, component := range analysis.Components {
		path, err := resolveTouchRelative(root, component.PositionRelativePath)
		if err != nil {
			return err
		}
		meshPaths = append(meshPaths, path)
		for _, relative := range component.IndexRelativePaths {
			path, err = resolveTouchRelative(root, relative)
			if err != nil {
				return err
			}
			meshPaths = append(meshPaths, path)
		}
		if len(component.IndexRelativePaths) == 0 && component.IndexRelativePath != nil {
			path, err = resolveTouchRelative(root, *component.IndexRelativePath)
			if err != nil {
				return err
			}
			meshPaths = append(meshPaths, path)
		}
		if component.BlendRelativePath != nil {
			path, err = resolveTouchRelative(root, *component.BlendRelativePath)
			if err != nil {
				return err
			}
			meshPaths = append(meshPaths, path)
		}
	}
	sourcePaths := analysis.SourceFilesRelativePaths
	if len(sourcePaths) == 0 {
		sourcePaths = []string{analysis.INIRelativePath}
	}
	iniPaths := []string{}
	for _, relative := range sourcePaths {
		path, err := resolveTouchRelative(root, relative)
		if err != nil {
			return err
		}
		iniPaths = append(iniPaths, path)
	}
	meshHash, err := hashTouchFiles(meshPaths, root)
	if err != nil {
		return err
	}
	iniHash, err := hashTouchFiles(iniPaths, root)
	if err != nil {
		return err
	}
	if meshHash != analysis.MeshHash || iniHash != analysis.INIHash {
		return contractError("Touch source mod changed since analysis; analyze the mod again")
	}
	return nil
}

func replaceTouchOutput(staging, output string) (err error) {
	id, _ := newToolsID()
	if len(id) > 8 {
		id = id[:8]
	}
	backup := output + ".backup-" + id
	oldMoved, newMoved := false, false
	defer func() { _ = os.RemoveAll(staging) }()
	if err = os.Rename(output, backup); err != nil {
		return err
	}
	oldMoved = true
	if err = os.Rename(staging, output); err != nil {
		if restoreErr := os.Rename(backup, output); restoreErr != nil {
			return errors.Join(err, restoreErr)
		}
		return err
	}
	newMoved = true
	if err = os.RemoveAll(backup); err != nil {
		if newMoved {
			_ = os.RemoveAll(output)
		}
		if oldMoved {
			if restoreErr := os.Rename(backup, output); restoreErr != nil {
				return errors.Join(err, restoreErr)
			}
		}
		return err
	}
	return nil
}
func sanitizeTouchNamespace(root string) string {
	base := filepath.Base(root)
	if regexp.MustCompile(`(?i)^(body|face|hair|leg|legs|outfit|parts?)$`).MatchString(touchDisabledPrefixRE.ReplaceAllString(base, "")) {
		base = filepath.Base(filepath.Dir(root))
	}
	base = regexp.MustCompile(`[^a-zA-Z0-9]+`).ReplaceAllString(touchDisabledPrefixRE.ReplaceAllString(base, ""), "")
	if len(base) > 24 {
		base = base[:24]
	}
	if base == "" {
		return "Mod"
	}
	return base
}
func copyTouchRuntimeShaders(outputRoot string) error {
	target := filepath.Join(outputRoot, "Resources", "IM")
	if err := os.MkdirAll(target, 0755); err != nil {
		return err
	}
	for _, name := range touchShaderFiles {
		raw, err := touchRuntimeShaders.ReadFile("touch_runtime/" + name)
		if err != nil {
			return contractError(fmt.Sprintf("Bundled touch runtime shader missing: %s", name))
		}
		if err = os.WriteFile(filepath.Join(target, name), raw, 0600); err != nil {
			return err
		}
	}
	return nil
}
func writeTouchManifest(root, version string) error {
	raw, err := json.MarshalIndent(map[string]string{"kind": touchProfileManifestKind, "runtimeVersion": version, "createdAt": time.Now().UTC().Format(time.RFC3339Nano)}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, touchProfileManifestFile), raw, 0600)
}
func pathIsDirectory(path string) bool { info, err := os.Stat(path); return err == nil && info.IsDir() }

func (t *Tools) touchUseFrameGuard(ctx context.Context) bool {
	if t.xxmi == nil {
		return false
	}
	config, err := t.xxmi.GetXXMIConfig(ctx)
	if err != nil {
		return false
	}
	packages, _ := config["Packages"].(map[string]any)
	packageEntries, _ := packages["packages"].(map[string]any)
	xxmiPackage, _ := packageEntries["XXMI"].(map[string]any)
	version, _ := xxmiPackage["deployed_version"].(string)
	return supportsTouchFrameNumberGuard(version)
}
