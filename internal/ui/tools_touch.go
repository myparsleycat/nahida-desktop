package ui

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit"
	"github.com/samber/lo"

	"nahida.live/desktop/internal/tools"
	touchprofile "nahida.live/desktop/internal/tools/touch_profile"
)

const (
	allTouchZones          = "__all__"
	touchSettingsDebounce  = 120 * time.Millisecond
	touchTopologyCacheSize = 2
	touchPrefix            = "page.tools.touch_profile."
)

type touchPhase uint8

const (
	touchSelect touchPhase = iota
	touchReview
)

type touchApplied struct {
	sessionID, output, source string
	reenable                  bool
}

// touchMeshView is a loaded component mesh; its address identifies it to the preview.
type touchMeshView struct {
	mesh tools.TouchMesh
}

type touchPreviewData struct {
	preview tools.TouchPreview
	mesh    *touchMeshView
}

// touchProfile is one Touch Profile session screen: the tools page, or the dialog bound to one mod.
type touchProfile struct {
	// key prefixes the window-local state of the screen's controls.
	key string
	// fixedPath is the mod the dialog converts; the tools page leaves it empty and asks for a folder.
	fixedPath string
	modName   string
	// changed runs after applying or rolling back moved mod folders.
	changed func()

	modPath                        string
	loading, applying, rollingBack bool
	// generation counts mod loads, so a load or apply that finishes after another load leaves the screen alone.
	generation  int
	inspection  *tools.TouchModInspection
	draft       *tools.TouchDraft
	phase       touchPhase
	selected    map[string]bool
	focusMesh   string
	applied     *touchApplied
	inputError  string
	threshold   [2]float64
	assignments map[string][]touchprofile.TouchBoneZoneSelection
	componentID string
	zoneID      string
	unlinked    map[string]bool
	batch       *touchprofile.SettingsBatch
	savePending bool
	// hoverBone, browseBone and channelBone are bones the selection preview may show, or -1: the assignment row
	// under the pointer, the bone highlighted in a bone picker, and the bone whose channel list is open.
	hoverBone, browseBone, channelBone int64

	topology    []*touchMeshView
	meshPreview *touchMeshView
	meshLoading bool
	meshError   string
	meshKey     string
	meshCancel  context.CancelFunc

	preview        *touchPreviewData
	lastPreview    *touchPreviewData
	previewLoading bool
	previewError   string
	previewVersion int
	previewKey     string
	previewCancel  context.CancelFunc

	view touchViewport
}

func newTouchProfile(key string) touchProfile {
	return touchProfile{key: key, threshold: [2]float64{.01, 1}, hoverBone: -1, browseBone: -1, channelBone: -1}
}

// highlightBone is the bone whose weights the selection preview shows, or -1. An open channel list wins over a
// bone highlighted in a picker, which wins over a hovered assignment row.
func (tp *touchProfile) highlightBone() int64 {
	switch {
	case tp.channelBone >= 0:
		return tp.channelBone
	case tp.browseBone >= 0:
		return tp.browseBone
	}
	return tp.hoverBone
}

func (tp *touchProfile) sessionID() string {
	switch {
	case tp.draft != nil:
		return tp.draft.SessionID
	case tp.inspection != nil:
		return tp.inspection.SessionID
	}
	return ""
}

func (tp *touchProfile) interactiveComponents() []*touchprofile.TouchComponentDraft {
	if tp.draft == nil {
		return nil
	}
	var components []*touchprofile.TouchComponentDraft
	for i := range tp.draft.Components {
		if component := &tp.draft.Components[i]; component.Interactive && len(component.Zones) > 0 {
			components = append(components, component)
		}
	}
	return components
}

func (tp *touchProfile) selectedComponent() *touchprofile.TouchComponentDraft {
	components := tp.interactiveComponents()
	if len(components) == 0 {
		return nil
	}
	for _, component := range components {
		if component.ComponentID == tp.componentID {
			return component
		}
	}
	return components[0]
}

func (tp *touchProfile) activeZoneID() string {
	component := tp.selectedComponent()
	if component != nil && slices.ContainsFunc(component.Zones, func(zone touchprofile.TouchZoneSpec) bool {
		return zone.ID == tp.zoneID
	}) {
		return tp.zoneID
	}
	return allTouchZones
}

func (tp *touchProfile) linked(componentID string) bool { return !tp.unlinked[componentID] }

func (tp *touchProfile) activePreview() *touchPreviewData {
	component := tp.selectedComponent()
	if tp.preview == nil || component == nil || tp.preview.preview.ComponentID != component.ComponentID {
		return nil
	}
	return tp.preview
}

// displayPreview keeps the last mesh on screen while the next component's preview loads.
func (tp *touchProfile) displayPreview() *touchPreviewData {
	if active := tp.activePreview(); active != nil {
		return active
	}
	if tp.previewLoading {
		return tp.lastPreview
	}
	return nil
}

func (tp *touchProfile) cachedTopology(componentID string) *touchMeshView {
	for _, view := range tp.topology {
		if view.mesh.ComponentID == componentID {
			return view
		}
	}
	return nil
}

func (tp *touchProfile) cacheTopology(view *touchMeshView) {
	tp.topology = lo.Reject(tp.topology, func(cached *touchMeshView, _ int) bool {
		return cached.mesh.ComponentID == view.mesh.ComponentID
	})
	tp.topology = append(tp.topology, view)
	if len(tp.topology) > touchTopologyCacheSize {
		tp.topology = tp.topology[len(tp.topology)-touchTopologyCacheSize:]
	}
}

func (tp *touchProfile) stopMeshLoad() {
	if tp.meshCancel != nil {
		tp.meshCancel()
		tp.meshCancel = nil
	}
	tp.meshLoading = false
}

// cancelPreview stops a preview load; the next build loads it again once no settings save is pending.
func (tp *touchProfile) cancelPreview() {
	if tp.previewCancel != nil {
		tp.previewCancel()
		tp.previewCancel = nil
	}
	if tp.previewLoading {
		tp.previewLoading = false
		tp.previewKey = ""
	}
}

func (tp *touchProfile) resetPreview() {
	tp.cancelPreview()
	tp.preview, tp.lastPreview, tp.previewError, tp.previewKey = nil, nil, "", ""
}

func (tp *touchProfile) resetMeshPreview() {
	tp.stopMeshLoad()
	tp.meshPreview, tp.meshError, tp.meshKey = nil, "", ""
}

// dropSession forgets the open session and everything loaded from it, without closing it in the backend.
func (tp *touchProfile) dropSession() {
	if tp.batch != nil {
		tp.batch.Dispose()
		tp.batch = nil
	}
	tp.savePending = false
	tp.topology = nil
	tp.resetPreview()
	tp.resetMeshPreview()
	tp.draft, tp.inspection = nil, nil
	tp.phase = touchSelect
	tp.componentID, tp.zoneID = "", allTouchZones
	tp.hoverBone, tp.browseBone, tp.channelBone = -1, -1, -1
}

func newTouchMeshView(mesh tools.TouchMesh) *touchMeshView { return &touchMeshView{mesh: mesh} }

func (s *Shell) touchFailed(err error, operation, title string, fields map[string]any) {
	err = s.toolError(err, operation, fields)
	s.toasts.Show(kit.ToastError, T(touchPrefix+title), err.Error())
}

// touchInputFailed shows why a mod cannot be converted, or title with the error.
func (s *Shell) touchInputFailed(tp *touchProfile, err error, operation string, fields map[string]any) {
	code := touchprofile.InputErrorCode(err)
	if code == "" {
		s.touchFailed(err, operation, "toast.load_failed", fields)
		return
	}
	tp.inputError = code
	prefix := touchPrefix + "input_error." + code
	s.toasts.Show(kit.ToastError, T(prefix+".title"), T(prefix+".description"))
}

func (s *Shell) closeTouchSession(sessionID string) {
	if sessionID == "" {
		return
	}
	go func() {
		if _, err := s.deps.Tools.TouchProfileCloseSession(context.Background(), sessionID); err != nil {
			_ = s.toolError(err, "touch-profile.close", map[string]any{"sessionId": sessionID})
		}
	}()
}

func (s *Shell) touchLoad(tp *touchProfile, path string) {
	if path == "" {
		return
	}
	tp.generation++
	generation := tp.generation
	tp.modPath = path
	previous := tp.sessionID()
	tp.dropSession()
	tp.loading = true
	tp.applied, tp.inputError = nil, ""
	tp.unlinked = map[string]bool{}
	tp.assignments = map[string][]touchprofile.TouchBoneZoneSelection{}
	s.closeTouchSession(previous)

	s.background(func(ctx context.Context) {
		next, err := s.deps.Tools.TouchProfilePrepare(ctx, tools.TouchProfileLoadInput{ModPath: path})
		fairy.Go(func() {
			if generation != tp.generation {
				if err == nil {
					s.closeTouchSession(next.SessionID)
				}
				return
			}
			tp.loading = false
			if err != nil {
				s.touchInputFailed(tp, err, "touch-profile.prepare", map[string]any{"modPath": path})
				return
			}
			tp.inspection = &next
			tp.selected = map[string]bool{}
			for _, component := range next.Components {
				if component.InteractiveCandidate {
					tp.selected[component.ID] = true
				}
			}
			tp.phase = touchSelect
			tp.batch = s.newTouchSettingsBatch(tp, next.SessionID)
		})
	})
}

func (s *Shell) newTouchSettingsBatch(tp *touchProfile, sessionID string) *touchprofile.SettingsBatch {
	var batch *touchprofile.SettingsBatch
	batch = touchprofile.NewSettingsBatch(touchprofile.SettingsBatchOptions{
		Debounce: touchSettingsDebounce,
		Save: func(changes []touchprofile.TouchProfileZoneSettingsChange) (tools.TouchProfileUpdateResult, error) {
			return s.deps.Tools.TouchProfileUpdateZoneSettingsBatch(context.Background(),
				tools.TouchProfileUpdateZoneSettingsBatchInput{SessionID: sessionID, Changes: changes})
		},
		OnPendingChange: func(pending bool) {
			fairy.Go(func() {
				if tp.batch == batch {
					tp.savePending = pending
				}
			})
		},
		OnPreviewRequired: func() {
			fairy.Go(func() {
				if tp.batch == batch {
					tp.previewVersion++
				}
			})
		},
		OnError: func(err error) {
			fairy.Go(func() {
				s.touchFailed(err, "touch-profile.save-settings", "toast.settings_save_failed",
					map[string]any{"sessionId": sessionID})
			})
		},
	})
	return batch
}

func (s *Shell) touchSelectFolder(tp *touchProfile) {
	go func() {
		result, err := s.deps.Dialog.SelectDirectory("touch-profile-mod")
		fairy.Go(func() {
			switch {
			case err != nil:
				err = s.toolError(err, "touch-profile.select-folder", nil)
				s.toasts.Show(kit.ToastError, err.Error(), "")
			case !result.Canceled && result.FilePath != "":
				s.touchLoad(tp, result.FilePath)
			}
		})
	}()
}

// touchCanAnalyze is the Next button's condition: a mesh is selected, a selected mesh has a bone, and every
// selected mesh's bones have a channel.
func (tp *touchProfile) touchCanAnalyze() bool {
	if tp.loading || len(tp.selected) == 0 {
		return false
	}
	anyBone := false
	for id := range tp.selected {
		assignments := tp.assignments[id]
		anyBone = anyBone || len(assignments) > 0
		if slices.ContainsFunc(assignments, func(a touchprofile.TouchBoneZoneSelection) bool {
			return a.Channel == nil
		}) {
			return false
		}
	}
	return anyBone
}

func (s *Shell) touchAnalyze(tp *touchProfile) {
	inspection := tp.inspection
	if inspection == nil || len(inspection.Components) == 0 {
		return
	}
	if len(tp.selected) == 0 {
		s.toasts.Show(kit.ToastError, T(touchPrefix+"toast.no_components_selected"), "")
		return
	}
	tp.loading, tp.inputError = true, ""
	mode := "bone"
	threshold := tp.threshold
	input := tools.TouchProfileAnalyzeInput{SessionID: inspection.SessionID, Mode: &mode, WeightThreshold: &threshold}
	for _, component := range inspection.Components {
		if !tp.selected[component.ID] {
			continue
		}
		input.ComponentIDs = append(input.ComponentIDs, component.ID)
		if zones := tp.assignments[component.ID]; len(zones) > 0 {
			input.BoneSelections = append(input.BoneSelections, touchprofile.TouchBoneComponentSelection{
				ComponentID: component.ID, Zones: slices.Clone(zones),
			})
		}
	}
	generation := tp.generation
	s.background(func(ctx context.Context) {
		next, err := s.deps.Tools.TouchProfileAnalyzeComponents(ctx, input)
		fairy.Go(func() {
			if generation != tp.generation {
				return
			}
			tp.loading = false
			if err != nil {
				s.touchInputFailed(tp, err, "touch-profile.analyze", map[string]any{
					"sessionId": input.SessionID, "componentIds": input.ComponentIDs,
				})
				return
			}
			tp.draft = &next
			tp.phase = touchReview
		})
	})
}

func (tp *touchProfile) backToSelect() {
	tp.draft = nil
	tp.phase = touchSelect
	tp.resetPreview()
	tp.componentID, tp.zoneID = "", allTouchZones
	tp.assignments = map[string][]touchprofile.TouchBoneZoneSelection{}
	tp.hoverBone, tp.browseBone, tp.channelBone = -1, -1, -1
}

func (s *Shell) touchApply(tp *touchProfile, force bool) {
	draft := tp.draft
	if draft == nil || tp.applying || tp.savePending {
		return
	}
	regenerating := tp.applied != nil
	generation := tp.generation
	tp.applying = true
	operation, done, failed := "touch-profile.apply", "toast.created", "toast.create_failed"
	if regenerating {
		operation, done, failed = "touch-profile.regenerate", "toast.regenerated", "toast.regenerate_failed"
	}
	input := tools.TouchProfileApplyInput{SessionID: draft.SessionID, Force: force}
	s.background(func(ctx context.Context) {
		run := s.deps.Tools.TouchProfileApply
		if regenerating {
			run = s.deps.Tools.TouchProfileRegenerate
		}
		next, err := run(ctx, input)
		fairy.Go(func() {
			if generation == tp.generation {
				tp.applying = false
			}
			if err != nil {
				s.touchFailed(err, operation, failed, map[string]any{"sessionId": input.SessionID, "force": force})
				return
			}
			if generation == tp.generation {
				tp.applied = &touchApplied{
					sessionID: next.SessionID, output: next.OutputModRoot, source: next.SourceModRoot,
					reenable: next.ReenableSourceOnRollback,
				}
			}
			s.toasts.Show(kit.ToastSuccess, T(touchPrefix+done), next.OutputModRoot)
			if !regenerating && tp.changed != nil {
				tp.changed()
			}
		})
	})
}

func (s *Shell) touchRollback(tp *touchProfile) {
	applied := tp.applied
	if applied == nil || tp.rollingBack {
		return
	}
	tp.rollingBack = true
	generation := tp.generation
	input := tools.TouchProfileRollbackInput{
		SessionID: applied.sessionID, OutputModRoot: applied.output, SourceModRoot: applied.source,
		ReenableSourceOnRollback: applied.reenable,
	}
	s.background(func(ctx context.Context) {
		next, err := s.deps.Tools.TouchProfileRollback(ctx, input)
		fairy.Go(func() {
			tp.rollingBack = false
			if err != nil {
				s.touchFailed(err, "touch-profile.rollback", "toast.rollback_failed", map[string]any{
					"sessionId": input.SessionID, "output": input.OutputModRoot, "source": input.SourceModRoot,
				})
				return
			}
			if generation == tp.generation {
				tp.applied = nil
				tp.previewError = ""
				tp.previewVersion++
				tp.modPath = next.SourceModRoot
				if tp.draft != nil {
					tp.draft.SourceModRoot = next.SourceModRoot
				}
			}
			s.toasts.Show(kit.ToastSuccess, T(touchPrefix+"toast.rolled_back"), next.SourceModRoot)
			if tp.changed != nil {
				tp.changed()
			}
		})
	})
}

func (s *Shell) touchDiscard(tp *touchProfile) {
	sessionID := tp.sessionID()
	if sessionID == "" {
		return
	}
	s.closeTouchSession(sessionID)
	tp.dropSession()
	tp.inputError = ""
}

// touchUpdateZones sets the settings of zoneIDs and queues the save.
func (tp *touchProfile) touchUpdateZones(
	componentID string,
	zoneIDs []string,
	settings touchprofile.TouchZoneSettings,
	refreshPreview bool,
) {
	if tp.draft == nil || tp.batch == nil || tp.applying || tp.rollingBack || len(zoneIDs) == 0 {
		return
	}
	changes := make([]touchprofile.TouchProfileZoneSettingsChange, 0, len(zoneIDs))
	for i := range tp.draft.Components {
		component := &tp.draft.Components[i]
		if component.ComponentID != componentID {
			continue
		}
		for z := range component.Zones {
			if slices.Contains(zoneIDs, component.Zones[z].ID) {
				component.Zones[z].Settings = settings
			}
		}
	}
	for _, zoneID := range zoneIDs {
		changes = append(changes, touchprofile.TouchProfileZoneSettingsChange{
			ComponentID: componentID, ZoneID: zoneID, Settings: settings,
		})
	}
	tp.cancelPreview()
	tp.savePending = true
	tp.batch.Enqueue(changes, refreshPreview)
}

func (tp *touchProfile) setLinked(component *touchprofile.TouchComponentDraft, linked bool) {
	if tp.unlinked == nil {
		tp.unlinked = map[string]bool{}
	}
	tp.unlinked[component.ComponentID] = !linked
	if !linked || len(component.Zones) < 2 {
		return
	}
	ids := lo.Map(component.Zones, func(zone touchprofile.TouchZoneSpec, _ int) string { return zone.ID })
	tp.touchUpdateZones(component.ComponentID, ids, component.Zones[0].Settings, true)
}

// zoneTargets is the zones an edit of zone applies to: every zone of a linked component, or zone alone.
func (tp *touchProfile) zoneTargets(component *touchprofile.TouchComponentDraft, zone string) []string {
	if !tp.linked(component.ComponentID) {
		return []string{zone}
	}
	return lo.Map(component.Zones, func(zone touchprofile.TouchZoneSpec, _ int) string { return zone.ID })
}

// touchEffects starts the preview loads that the screen's state calls for, replacing any that no longer apply.
func (s *Shell) touchEffects(tp *touchProfile) {
	if tp.phase == touchSelect && tp.inspection != nil &&
		slices.ContainsFunc(tp.inspection.Components, func(c touchprofile.TouchComponentInspection) bool {
			return c.ID == tp.focusMesh
		}) {
		if key := tp.inspection.SessionID + "\x00" + tp.focusMesh; key != tp.meshKey {
			tp.meshKey = key
			s.touchLoadMesh(tp, tp.inspection.SessionID, tp.focusMesh)
		}
	}

	component := tp.selectedComponent()
	if tp.draft == nil || component == nil || tp.savePending {
		return
	}
	key := fmt.Sprintf("%s\x00%s\x00%d", tp.draft.SessionID, component.ComponentID, tp.previewVersion)
	if key != tp.previewKey {
		tp.previewKey = key
		s.touchLoadPreview(tp, tp.draft.SessionID, component.ComponentID)
	}
}

func (s *Shell) touchLoadMesh(tp *touchProfile, sessionID, componentID string) {
	tp.stopMeshLoad()
	if cached := tp.cachedTopology(componentID); cached != nil {
		tp.meshPreview, tp.meshError = cached, ""
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	tp.meshCancel, tp.meshLoading, tp.meshError = cancel, true, ""
	input := tools.TouchProfilePreviewInput{SessionID: sessionID, ComponentID: componentID}
	go func() {
		mesh, err := s.deps.Tools.TouchProfileMesh(ctx, input)
		var view *touchMeshView
		if err == nil {
			view = newTouchMeshView(mesh)
		}
		fairy.Go(func() {
			if ctx.Err() != nil {
				return
			}
			tp.meshLoading, tp.meshCancel = false, nil
			cancel()
			if err != nil {
				tp.meshError = s.toolError(err, "touch-profile.mesh", map[string]any{
					"sessionId": sessionID, "componentId": componentID,
				}).Error()
				return
			}
			tp.cacheTopology(view)
			tp.meshPreview = view
		})
	}()
}

func (s *Shell) touchLoadPreview(tp *touchProfile, sessionID, componentID string) {
	if tp.previewCancel != nil {
		tp.previewCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	tp.previewCancel, tp.previewLoading, tp.previewError = cancel, true, ""
	cached := tp.cachedTopology(componentID)
	input := tools.TouchProfilePreviewInput{SessionID: sessionID, ComponentID: componentID}
	go func() {
		view, err := cached, error(nil)
		if view == nil {
			var mesh tools.TouchMesh
			if mesh, err = s.deps.Tools.TouchProfileMesh(ctx, input); err == nil {
				view = newTouchMeshView(mesh)
			}
		}
		var preview tools.TouchPreview
		if err == nil {
			preview, err = s.deps.Tools.TouchProfilePreview(ctx, input, view.mesh)
		}
		fairy.Go(func() {
			if ctx.Err() != nil {
				return
			}
			tp.previewLoading, tp.previewCancel = false, nil
			cancel()
			if err != nil {
				tp.previewError = s.toolError(err, "touch-profile.preview", map[string]any{
					"sessionId": sessionID, "componentId": componentID,
				}).Error()
				return
			}
			if cached == nil {
				tp.cacheTopology(view)
			}
			data := &touchPreviewData{preview: preview, mesh: view}
			tp.preview, tp.lastPreview = data, data
		})
	}()
}

// openTouchProfileDialog opens Touch Profile bound to the mod at modPath, as the mod menu does.
func (s *Shell) openTouchProfileDialog(modPath, modName string) {
	if s.tools.touchDialog != nil {
		return
	}
	tp := newTouchProfile("mod.touch")
	tp.fixedPath, tp.modName = modPath, modName
	tp.changed = s.refreshListing
	s.tools.touchDialog = &tp
	s.touchLoad(&tp, modPath)
}

func (s *Shell) closeTouchProfileDialog() {
	tp := s.tools.touchDialog
	if tp == nil {
		return
	}
	s.tools.touchDialog = nil
	tp.generation++
	s.closeTouchSession(tp.sessionID())
	tp.dropSession()
	tp.view.close()
}
