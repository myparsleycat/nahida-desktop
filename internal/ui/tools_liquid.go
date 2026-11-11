package ui

import (
	"context"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/modruntime"
	"nahida.live/desktop/internal/tools"
	liquidemitter "nahida.live/desktop/internal/tools/liquid_emitter"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

const liquidPrefix = "page.tools.liquid_emitter."

const (
	liquidHistoryLimit = 100
	// Within this tilt of straight down or up, the azimuth no longer turns the direction.
	liquidPoleDegrees = 0.05
	// The runtime rejects a whole profile file when an offset axis exceeds 25 cm.
	liquidMaxOffsetMeters = 0.25
	// How far the runtime's slot count goes up when a mod needs more slots than it has.
	liquidSlotRaise        = 4
	liquidSlotCountSetting = "slotCount"
	liquidRuntimeName      = "LiquidGravityProbe"
)

var (
	liquidAxes = [3]string{"X", "Y", "Z"}
	// Applying repairs an outdated or changed runtime, so only these leave editing open.
	liquidEditableStates = map[string]bool{
		modruntime.StateCurrent: true, modruntime.StateOutdated: true, modruntime.StateModified: true,
	}
	liquidErrorPattern  = regexp.MustCompile(`LIQUID_([A-Z_]+)(?::([^\n]*))?`)
	liquidDefaultDown   = vec3{0, 0, -1}
	liquidRuntimeLook   = liquidemitter.Look{Force: 1, Amount: 1, Viscosity: 1, Nozzle: liquidemitter.NozzleSpray}
	liquidNozzles       = []string{liquidemitter.NozzleSpray, liquidemitter.NozzleJet, liquidemitter.NozzleOoze}
	liquidMaterialNames = []string{"clear", "translucent", "cloudy"}
	// A preset is not stored: it only sets the scattering and roughness.
	liquidMaterials = map[string][2]float64{"clear": {0, 0}, "translucent": {0.35, 0.2}, "cloudy": {0.85, 0.35}}
)

// liquidAnchor is an emitter's place on a viewer mesh face in source mesh units.
type liquidAnchor struct {
	meshID      string
	faceIndex   int
	vertices    [3]uint32
	triangle    [3]vec3
	barycentric vec3
	point       vec3
	offset      vec3
	direction   vec3
}

// liquidHistory is the undo history of the emitter being edited; each emitter keeps its own, so undoing never
// moves one emitter onto another's anchor.
type liquidHistory struct {
	// pid is the saved emitter the entries edit, or nil while they place a new one.
	pid     *int
	entries []*liquidAnchor
	index   int
	// lastKey makes consecutive edits with the same key, such as one slider drag, share one undo step.
	lastKey string
}

// liquidTool is the Liquid Emitter editing one mod folder; moving to another folder makes a new one.
type liquidTool struct {
	shell   *Shell
	modPath string
	modName string
	viewer  *ModelViewer

	inspection *tools.LiquidEmitterInspection
	inspectErr error
	// busy names the request running; only one runs at a time.
	busy string

	mode           string
	history        liquidHistory
	seeded         bool
	picking        bool
	showTrajectory bool
	notice         string
	// poleAzimuth keeps the last azimuth set, since straight down or up has none of its own.
	poleAzimuth float64
	// lookDraft is the unsaved look of the emitter lookKey names, one emitter of one mod.
	lookDraft *liquidemitter.Look
	lookKey   string
	// positionDrafts hold the position fields being typed.
	positionDrafts map[int]string
	sections       map[string]bool
	// ask names the question shown over the tool: "slots", "remove-all", "restore" or "close".
	ask       string
	slotsNext int
	slotsNow  int
	closed    bool
}

// openLiquidEmitterDialog shows the Liquid Emitter for the mod at modPath over the main window.
func (s *Shell) openLiquidEmitterDialog(modPath, modName string) {
	if s.tools.liquidDialog != nil || s.deps.Tools == nil {
		return
	}
	s.tools.liquidDialog = newLiquidTool(s, modPath, modName, nil)
}

func newLiquidTool(s *Shell, modPath, modName string, inspection *tools.LiquidEmitterInspection) *liquidTool {
	lt := &liquidTool{
		shell:          s,
		modPath:        modPath,
		modName:        modName,
		mode:           liquidemitter.ApplyInPlace,
		history:        liquidHistory{entries: []*liquidAnchor{nil}},
		picking:        true,
		positionDrafts: map[int]string{},
		sections:       map[string]bool{"emitters": true},
		inspection:     inspection,
	}
	lt.viewer = s.NewModelViewer(modPath)
	lt.viewer.tool = &viewerTool{overlay: lt.overlay, inspector: lt.inspector, footer: lt.footer}
	if inspection == nil {
		lt.inspect()
	}
	return lt
}

// requestClose asks before dropping changes that were not applied.
func (lt *liquidTool) requestClose() {
	if lt.unsaved() {
		lt.ask = "close"
		return
	}
	lt.shell.closeLiquidEmitterDialog()
}

func (s *Shell) closeLiquidEmitterDialog() {
	if lt := s.tools.liquidDialog; lt != nil {
		s.tools.liquidDialog = nil
		lt.release()
	}
}

func (lt *liquidTool) release() {
	lt.closed = true
	lt.viewer.Close()
}

// moveTo follows the tool to the new mod it created or the original it restored.
func (lt *liquidTool) moveTo(result tools.LiquidEmitterInspection) {
	s := lt.shell
	s.refreshListing()
	if lt.closed || s.tools.liquidDialog != lt {
		return
	}
	lt.release()
	s.tools.liquidDialog = newLiquidTool(s, result.ModPath, result.ModName, &result)
}

func (lt *liquidTool) inspect() {
	path := lt.modPath
	lt.shell.background(func(ctx context.Context) {
		result, err := lt.shell.deps.Tools.LiquidEmitterInspect(ctx, path)
		fairy.Go(func() {
			if err != nil {
				lt.inspectErr = lt.report(err, "inspect")
				return
			}
			lt.inspection, lt.inspectErr = &result, nil
		})
	})
}

func (lt *liquidTool) report(err error, action string) error {
	if infra.IsCancellationError(err) {
		return err
	}
	return infra.ReportError(lt.shell.deps.Log, err, "LiquidEmitter."+action, infra.Diagnostic{
		Operation: "liquid-emitter-" + action, Fields: map[string]any{"modPath": lt.modPath},
	})
}

// run sends one request and shows its failure; done runs on the UI thread with the new inspection.
func (lt *liquidTool) run(
	action string,
	work func(ctx context.Context, t *tools.Tools) (tools.LiquidEmitterInspection, error),
	done func(result tools.LiquidEmitterInspection),
) {
	if lt.busy != "" {
		return
	}
	lt.busy = action
	s := lt.shell
	s.background(func(ctx context.Context) {
		result, err := work(ctx, s.deps.Tools)
		fairy.Go(func() {
			lt.busy = ""
			if err != nil {
				s.toasts.Show(kit.ToastError, liquidErrorMessage(lt.report(err, action)), "")
				return
			}
			done(result)
		})
	})
}

func liquidErrorMessage(err error) string {
	message := err.Error()
	match := liquidErrorPattern.FindStringSubmatch(message)
	if match == nil {
		if runtime := modRuntimeMessage(err); runtime != "" {
			return runtime
		}
		return message
	}
	key := liquidPrefix + "error." + strings.ToLower(match[1])
	if !catalog.Has(key) {
		return message
	}
	return T(key, "detail", strings.TrimSpace(match[2]))
}

func (lt *liquidTool) emitters() []liquidemitter.Emitter {
	if lt.inspection == nil {
		return nil
	}
	return lt.inspection.Emitters
}

func (lt *liquidTool) meshUnit() float64 {
	if lt.inspection == nil || lt.inspection.MeshUnit == 0 {
		return 1
	}
	return lt.inspection.MeshUnit
}

func (lt *liquidTool) down() vec3 {
	if lt.inspection == nil {
		return liquidDefaultDown
	}
	return vec3(lt.inspection.MeshDown)
}

func (lt *liquidTool) anchor() *liquidAnchor {
	return lt.history.entries[lt.history.index]
}

// current is the saved emitter being edited, or nil while a new one is placed.
func (lt *liquidTool) current() *liquidemitter.Emitter {
	if lt.history.pid == nil {
		return nil
	}
	emitters := lt.emitters()
	if i := slices.IndexFunc(emitters, func(e liquidemitter.Emitter) bool { return e.PID == *lt.history.pid }); i >= 0 {
		return &emitters[i]
	}
	return nil
}

func (lt *liquidTool) editable() bool {
	return lt.inspection != nil && liquidEditableStates[lt.inspection.Runtime.State]
}

func (lt *liquidTool) locked() bool { return !lt.editable() || lt.anchor() == nil }

// applyMode edits a mod this tool generated in place.
func (lt *liquidTool) applyMode() string {
	if lt.inspection != nil && lt.inspection.SourcePath != "" {
		return liquidemitter.ApplyInPlace
	}
	return lt.mode
}

func (lt *liquidTool) atLimit() bool {
	return lt.inspection != nil && len(lt.inspection.Emitters) >= lt.inspection.MaxEmitters
}

func (lt *liquidTool) lookDraftKey() string {
	key := lt.modPath + ":"
	if lt.inspection != nil {
		key = lt.inspection.ModPath + ":"
	}
	if current := lt.current(); current != nil {
		key += strconv.Itoa(current.PID)
	}
	return key
}

// draftLook is the unsaved look of the emitter being edited; a draft of another emitter does not carry over.
func (lt *liquidTool) draftLook() *liquidemitter.Look {
	if lt.lookDraft == nil || lt.lookKey != lt.lookDraftKey() {
		return nil
	}
	return lt.lookDraft
}

func (lt *liquidTool) setDraftLook(look liquidemitter.Look) {
	lt.lookDraft, lt.lookKey = &look, lt.lookDraftKey()
}

// savedLook is the emitter's stored look, or the one a new emitter starts from.
func (lt *liquidTool) savedLook() *liquidemitter.Look {
	if current := lt.current(); current != nil {
		return &current.Look
	}
	if lt.inspection != nil {
		return &lt.inspection.DefaultLook
	}
	return nil
}

func (lt *liquidTool) lookDirty() bool {
	draft, saved := lt.draftLook(), lt.savedLook()
	return draft != nil && (saved == nil || *draft != *saved)
}

func (lt *liquidTool) anchorDirty() bool {
	anchor := lt.anchor()
	var saved *liquidAnchor
	if current := lt.current(); current != nil {
		saved = liquidAnchorFromEmitter(*current)
	}
	if anchor == nil || saved == nil {
		return anchor != saved
	}
	return *anchor != *saved
}

func (lt *liquidTool) unsaved() bool { return lt.anchorDirty() || lt.lookDirty() }

func (lt *liquidTool) commit(next *liquidAnchor, key string) {
	h := &lt.history
	if key != "" && key == h.lastKey && h.index == len(h.entries)-1 {
		h.entries[h.index] = next
		return
	}
	entries := append(slices.Clone(h.entries[:h.index+1]), next)
	if len(entries) > liquidHistoryLimit {
		entries = entries[len(entries)-liquidHistoryLimit:]
	}
	h.entries, h.index, h.lastKey = entries, len(entries)-1, key
}

func (lt *liquidTool) step(delta int) {
	h := &lt.history
	h.index = min(max(h.index+delta, 0), len(h.entries)-1)
	h.lastKey = ""
	clear(lt.positionDrafts)
}

func (lt *liquidTool) update(key string, change func(a *liquidAnchor)) {
	anchor := lt.anchor()
	if anchor == nil {
		return
	}
	next := *anchor
	change(&next)
	lt.commit(&next, key)
}

func (lt *liquidTool) selectEmitter(pid *int, next *liquidAnchor) {
	lt.history = liquidHistory{pid: pid, entries: []*liquidAnchor{next}}
	lt.notice = ""
	clear(lt.positionDrafts)
}

// follow moves the selection to the emitter a request answered with, only while it still edits the emitter the
// request was for; a nil next drops the selection and its anchor together.
func (lt *liquidTool) follow(requested, next *int) {
	if !samePID(lt.history.pid, requested) {
		return
	}
	if next == nil {
		lt.history = liquidHistory{entries: []*liquidAnchor{nil}}
		return
	}
	pid := *next
	lt.history.pid = &pid
}

func samePID(a, b *int) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func (lt *liquidTool) pick(pick viewerSurfacePick, ok bool) {
	if !ok {
		lt.notice = T(liquidPrefix + "pick.missed")
		return
	}
	lt.notice = ""
	direction := lt.down()
	if anchor := lt.anchor(); anchor != nil {
		direction = anchor.direction
	}
	clear(lt.positionDrafts)
	lt.commit(&liquidAnchor{
		meshID: pick.meshID, faceIndex: pick.faceIndex, vertices: pick.vertices, triangle: pick.triangle,
		barycentric: pick.barycentric, point: pick.point, direction: direction,
	}, "")
}

func (lt *liquidTool) setOffsetAxis(axis int, meters float64, key string) {
	unit := lt.meshUnit()
	lt.update(key, func(a *liquidAnchor) {
		a.offset[axis] = meters / unit
		a.offset = liquidLimitOffsetAxis(a.offset, axis, liquidMaxOffsetMeters/unit)
	})
}

// angles are the direction's angles, keeping the last azimuth set while it points straight down or up.
func (lt *liquidTool) angles() (liquidAngles, bool, bool) {
	anchor := lt.anchor()
	if anchor == nil {
		return liquidAngles{}, false, false
	}
	angles := liquidDirectionToAngles(anchor.direction, lt.down())
	pole := angles.tilt < liquidPoleDegrees || angles.tilt > 180-liquidPoleDegrees
	if pole {
		angles.azimuth = lt.poleAzimuth
	}
	return angles, pole, true
}

func (lt *liquidTool) setTilt(value float64) {
	angles, _, ok := lt.angles()
	if !ok {
		return
	}
	angles.tilt = value
	lt.poleAzimuth = angles.azimuth
	lt.update("direction-tilt", func(a *liquidAnchor) { a.direction = liquidAnglesToDirection(angles, lt.down()) })
}

// setAzimuth leaves a vertical direction as it is.
func (lt *liquidTool) setAzimuth(value float64) {
	angles, pole, ok := lt.angles()
	if !ok {
		return
	}
	angles.azimuth = value
	lt.poleAzimuth = value
	if pole {
		return
	}
	lt.update("direction-azimuth", func(a *liquidAnchor) { a.direction = liquidAnglesToDirection(angles, lt.down()) })
}

// sync applies what follows from the inspection and the loaded model before the view is built.
func (lt *liquidTool) sync() {
	v := lt.viewer
	v.tool.pick = nil
	if lt.picking && lt.editable() {
		v.tool.pick = lt.pick
	}
	if lt.seeded || lt.inspection == nil || v.scene == nil {
		return
	}
	lt.seeded = true

	// Start from the first saved emitter whose mesh the viewer still has.
	savedForm := ""
	for _, emitter := range lt.inspection.Emitters {
		saved := liquidAnchorFromEmitter(emitter)
		if saved == nil {
			continue
		}
		if mesh := lt.mesh(saved.meshID); mesh != nil {
			pid := emitter.PID
			lt.selectEmitter(&pid, saved)
			savedForm = mesh.Form
			break
		}
	}

	// A bundle of forms opens on the form being edited, or its first, so the forms drawn over one another do
	// not hide each other's surfaces.
	if form := lt.formShowing(savedForm); v.form != form {
		v.form = form
		v.evaluate()
	}
}

// showForm switches the viewer to a form that shows meshForm when the selected one hides it.
func (lt *liquidTool) showForm(meshForm string) {
	v := lt.viewer
	if v.scene == nil || meshForm == "" || v.form == "" || formVisible(meshForm, v.form) {
		return
	}
	v.form = lt.formShowing(meshForm)
	v.evaluate()
}

// formShowing is the first form of the viewer that shows meshForm, or all forms when none does.
func (lt *liquidTool) formShowing(meshForm string) string {
	forms := lt.viewer.scene.transport.Forms
	if i := slices.IndexFunc(forms, func(f modelviewer.ModelViewerForm) bool {
		return formVisible(meshForm, f.ID)
	}); i >= 0 {
		return forms[i].ID
	}
	return ""
}

func formVisible(meshForm, selected string) bool {
	return selected == "" || meshForm == "" || meshForm == selected || hasFormPrefix(selected, meshForm)
}

func liquidAnchorFromEmitter(emitter liquidemitter.Emitter) *liquidAnchor {
	saved := emitter.Anchor
	vec := func(values []float64) (vec3, bool) {
		if len(values) != 3 || slices.ContainsFunc(values, func(x float64) bool {
			return math.IsNaN(x) || math.IsInf(x, 0)
		}) {
			return vec3{}, false
		}
		return vec3(values), true
	}
	barycentric, okB := vec(saved.Barycentric)
	point, okP := vec(saved.Point)
	offset, okO := vec(saved.Offset)
	direction, okD := vec(saved.Direction)
	corners := saved.TrianglePositions
	if len(corners) != 9 || len(saved.FaceVertices) != 3 || !okB || !okP || !okO || !okD {
		return nil
	}
	return &liquidAnchor{
		meshID: saved.MeshID, faceIndex: saved.FaceIndex, vertices: [3]uint32(saved.FaceVertices),
		triangle:    [3]vec3{vec3(corners[0:3]), vec3(corners[3:6]), vec3(corners[6:9])},
		barycentric: barycentric, point: point, offset: offset, direction: direction,
	}
}

// handleApply saves the look alone when only the look of a hooked emitter changed, and offers more runtime
// slots when a new emitter would exceed them: every emitter of a drawn character takes one, so a new one past
// the count would stay dry.
func (lt *liquidTool) handleApply() {
	current := lt.current()
	draft := lt.draftLook()
	if current != nil && current.Hooked && draft != nil && lt.lookDirty() && !lt.anchorDirty() &&
		lt.applyMode() == liquidemitter.ApplyInPlace {
		requested, look := current.PID, *draft
		lt.run("set-look", func(ctx context.Context, t *tools.Tools) (tools.LiquidEmitterInspection, error) {
			return t.LiquidEmitterSetLook(ctx, tools.LiquidEmitterLookInput{
				ModPath: lt.modPath, PID: requested, Look: look,
			})
		}, func(result tools.LiquidEmitterInspection) {
			lt.inspection = &result
			if result.Applied != nil {
				lt.follow(&requested, result.Applied)
			}
			lt.shell.toasts.Show(kit.ToastSuccess, T(liquidPrefix+"applied"), "")
		})
		return
	}

	if setting := lt.slotSetting(); current == nil && setting != nil {
		slots, err := strconv.ParseFloat(strings.TrimSpace(setting.Value), 64)
		if err == nil && !math.IsInf(slots, 0) && float64(len(lt.emitters())+1) > slots && slots < setting.Max {
			lt.slotsNow, lt.slotsNext = int(slots), int(math.Min(slots+liquidSlotRaise, setting.Max))
			lt.ask = "slots"
			return
		}
	}
	lt.applyAnchor()
}

func (lt *liquidTool) slotSetting() *modruntime.SettingValue {
	if lt.inspection == nil {
		return nil
	}
	settings := lt.inspection.Runtime.Settings
	if i := slices.IndexFunc(settings, func(s modruntime.SettingValue) bool {
		return s.ID == liquidSlotCountSetting
	}); i >= 0 {
		return &settings[i]
	}
	return nil
}

func (lt *liquidTool) applyAnchor() {
	anchor, v := lt.anchor(), lt.viewer
	if anchor == nil || v.scene == nil {
		return
	}
	mode := lt.applyMode()
	input := tools.LiquidEmitterApplyInput{
		ModPath:           lt.modPath,
		ViewerSessionID:   v.scene.transport.MemorySessionID,
		MeshID:            anchor.meshID,
		FaceIndex:         anchor.faceIndex,
		FaceVertices:      anchor.vertices[:],
		Barycentric:       anchor.barycentric[:],
		TrianglePositions: slices.Concat(anchor.triangle[0][:], anchor.triangle[1][:], anchor.triangle[2][:]),
		Point:             anchor.point[:],
		Offset:            anchor.offset[:],
		Direction:         anchor.direction[:],
		Mode:              mode,
	}
	requested := lt.history.pid
	if current := lt.current(); current != nil {
		pid := current.PID
		input.PID = &pid
	}
	if draft := lt.draftLook(); draft != nil && lt.lookDirty() {
		look := *draft
		input.Look = &look
	}
	lt.run("apply", func(ctx context.Context, t *tools.Tools) (tools.LiquidEmitterInspection, error) {
		return t.LiquidEmitterApply(ctx, input)
	}, func(result tools.LiquidEmitterInspection) {
		if mode == liquidemitter.ApplyNewMod {
			lt.shell.toasts.Show(kit.ToastSuccess, T(liquidPrefix+"created"), result.ModPath)
			lt.moveTo(result)
			return
		}
		lt.inspection = &result
		// A mod copied with its marker gets new slots on its first change, so the edited emitter is followed by
		// the slot the backend reports.
		lt.follow(requested, result.Applied)
		lt.shell.toasts.Show(kit.ToastSuccess, T(liquidPrefix+"applied"), "")
	})
}

// raiseSlots writes the runtime's slot count and then applies. The manager writes every setting it is given
// and resets the rest, so the others go along unchanged.
func (lt *liquidTool) raiseSlots(next int) {
	inspection := lt.inspection
	if inspection == nil || lt.shell.deps.ModRuntime == nil {
		return
	}
	values := map[string]string{}
	for _, setting := range inspection.Runtime.Settings {
		values[setting.ID] = setting.Value
	}
	values[liquidSlotCountSetting] = strconv.Itoa(next)
	importer := inspection.Importer
	runtimes := lt.shell.deps.ModRuntime
	var status modruntime.Status
	lt.run("raise-slots", func(ctx context.Context, _ *tools.Tools) (tools.LiquidEmitterInspection, error) {
		var err error
		status, err = runtimes.SetSettings(ctx, importer, liquidRuntimeName, values)
		return *inspection, err
	}, func(result tools.LiquidEmitterInspection) {
		result.Runtime = status
		lt.inspection = &result
		lt.refreshRuntimes(importer)
		lt.shell.toasts.Show(kit.ToastSuccess, T(liquidPrefix+"slots.raised", "count", next), "")
		lt.applyAnchor()
	})
}

// refreshRuntimes reloads the XXMI screen's runtime list for importer if it was loaded.
func (lt *liquidTool) refreshRuntimes(importer string) {
	if listing := lt.shell.xxmi.modRuntimes[importer]; listing != nil {
		listing.invalidate()
	}
}

func (lt *liquidTool) remove(pids []int) {
	requested := lt.history.pid
	path := lt.modPath
	lt.run("remove", func(ctx context.Context, t *tools.Tools) (tools.LiquidEmitterInspection, error) {
		return t.LiquidEmitterRemove(ctx, tools.LiquidEmitterRemoveInput{ModPath: path, PIDs: pids})
	}, func(result tools.LiquidEmitterInspection) {
		lt.inspection = &result
		// A new emitter still being placed is none of the removed ones, so its anchor stays.
		if requested != nil {
			if len(pids) == 0 || slices.Contains(pids, *requested) {
				lt.follow(requested, nil)
			} else {
				next := *requested
				if i := slices.IndexFunc(result.Renumbered, func(c liquidemitter.PIDChange) bool {
					return c.From == next
				}); i >= 0 {
					next = result.Renumbered[i].To
				}
				lt.follow(requested, &next)
			}
		}
		lt.shell.toasts.Show(kit.ToastSuccess, T(liquidPrefix+"removed"), "")
	})
}

func (lt *liquidTool) install() {
	path := lt.modPath
	lt.run("install-runtime", func(ctx context.Context, t *tools.Tools) (tools.LiquidEmitterInspection, error) {
		return t.LiquidEmitterInstallRuntime(ctx, path)
	}, func(result tools.LiquidEmitterInspection) {
		lt.inspection = &result
		lt.refreshRuntimes(result.Importer)
		lt.shell.toasts.Show(kit.ToastSuccess, T(liquidPrefix+"runtime.installed"), "")
	})
}

func (lt *liquidTool) restore() {
	path := lt.modPath
	lt.run("restore", func(ctx context.Context, t *tools.Tools) (tools.LiquidEmitterInspection, error) {
		return t.LiquidEmitterRestoreSource(ctx, path)
	}, func(result tools.LiquidEmitterInspection) {
		lt.shell.toasts.Show(kit.ToastSuccess, T(liquidPrefix+"restored"), result.ModPath)
		lt.moveTo(result)
	})
}
