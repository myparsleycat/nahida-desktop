package ui

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"maps"
	"strings"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/modname"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/tools"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

const modelViewerSurfaceKey = "model-viewer.surface"

// ModelViewer is the screen of one model viewer window: a mod rendered with an orbit camera beside the
// toggles its INI declares. Its state belongs to the UI thread.
type ModelViewer struct {
	shell    *Shell
	tools    *tools.Tools
	platform *platform.Shell
	log      *infra.Log
	dark     func() bool
	font     string
	path     string

	ctx    context.Context
	cancel context.CancelFunc
	closed bool

	loading bool
	loadErr string
	scene   *viewerScene
	// opened is set once a scene installs. Until then openFailed, when set, takes a load failure instead of the
	// viewer showing it, as the mod screen keeps its dialog closed until the model loads.
	opened     bool
	openFailed func(error)
	// state holds the transport's default values with the user's selections and their effects over them.
	state map[string]any
	// preview replaces state while a choice in an open list is highlighted, and previewForm the form.
	preview     map[string]any
	previewForm *string
	// resolved is the shown toggle state with the transport's state rules applied, and toggleStates the mesh
	// states it gives without an animation frame.
	resolved     map[string]any
	toggleStates []modelviewer.ViewerMeshState
	form         string
	anim         viewerAnimation
	// compute is the deformer of the clip being played, or nil for a clip without one.
	compute      *viewerCompute
	ineffective  viewerIneffective
	overlayMenus viewerOverlayMenus
	// highlightGameTextures tints the meshes the mod binds no diffuse texture to.
	highlightGameTextures bool
	camera                orbitCamera
	home                  orbitCamera
	normalMaps            bool
	// orientation is the user's turn of the model in degrees of roll, pitch and yaw.
	orientation vec3
	doubleSided bool
	// exposureDraft is the exposure being typed, until it is committed.
	exposureDraft *string
	renderer      viewerRenderer
	// surfaceSize is the device-pixel size of the last draw, which a preview capture renders at.
	surfaceSize [2]int
	// revision counts scene changes the camera does not show, for the surface's Retain version.
	revision    uint64
	uploading   bool
	drawErr     string
	dragStart   orbitCamera
	dragPanning bool
	// dragLast is the movement of the drag already handed to motion.
	dragLast [2]float32
	motion   cameraMotion
	// axisHover is the head of the axis gizmo under the pointer.
	axisHover string
	width     float32
	height    float32
	// tool is the tool that edits the model in this viewer, or nil for the plain viewer.
	tool *viewerTool
	// pressAt is where the last primary press on the model began, which a click picks at.
	pressAt [2]float32
	// pickSeq counts surface picks, so a pick that waited for its source indices reports only while latest.
	pickSeq     uint64
	togglesOpen bool
	// splitWidth is the width the viewer and the toggles share; sideWidth is the toggles' share of it, which
	// sideClosed hides, and sideDragStart their width when a drag of the splitter began.
	splitWidth    float32
	sideWidth     float32
	sideClosed    bool
	sideDragStart float32
	// sourceIndices caches the source index buffers read for picks, by URL.
	sourceIndices map[string][]uint32
	// shownDark is the theme the window's colours were last set for.
	shownDark *bool
	// pendingVariants holds the position variants being loaded, by mesh and variant index.
	pendingVariants map[[2]int]bool
	// variantFailureShown is set once the scene's first unreadable position variant was shown.
	variantFailureShown bool
	toonAssets          viewerToonAssets

	// toasts shows the viewer's notices: the main window's toasts in the dialog, its own in a window.
	toasts         *kit.Toaster
	windowToasts   kit.Toaster
	previewPath    string
	savingPreview  bool
	confirmPreview bool
	// gameLinked is set while a running game that loads the mod takes toggle changes through the desktop
	// channel; applyingToGame while those changes are being sent.
	gameLinked     bool
	gameLinkCheck  int
	applyingToGame bool
}

// NewModelViewer makes the viewer of the mod at path and starts loading it. Call it on the UI thread.
func (s *Shell) NewModelViewer(path string) *ModelViewer {
	ctx, cancel := context.WithCancel(context.Background())
	v := &ModelViewer{
		shell:       s,
		tools:       s.deps.Tools,
		platform:    s.deps.Platform,
		log:         s.deps.Log,
		dark:        s.Dark,
		font:        s.font,
		path:        path,
		ctx:         ctx,
		cancel:      cancel,
		normalMaps:  true,
		doubleSided: true,
		sideWidth:   viewerSideWidth,
		toasts:      &s.toasts,
	}
	v.refreshGameLinked()
	v.load()
	return v
}

// Title is the window title for the viewer.
func (v *ModelViewer) Title() string {
	return modname.DisplayName(v.path) + " - " + T("page.tools.model_viewer.title")
}

// Colors returns the window colours of the current theme.
func (v *ModelViewer) Colors() (dark bool, background, text fairy.Color) {
	dark = v.dark()
	t := Theme(dark, v.font)
	return dark, t.Background, t.Foreground
}

// Close cancels loading and frees the model. Call it on the UI thread when the window closes.
func (v *ModelViewer) Close() {
	if v.closed {
		return
	}
	v.closed = true
	v.cancel()
	v.releaseScene()
	v.toonAssets.release()
	_ = fairy.UseDevice(func(fairy.GPU) error {
		v.renderer.release()
		return nil
	})
}

func (v *ModelViewer) releaseScene() {
	v.stopCompute(false)
	scene := v.scene
	v.scene = nil
	if scene == nil {
		return
	}
	scene.gpu.release()
	if id := scene.transport.MemorySessionID; id != "" && v.tools != nil {
		go func() { _, _ = v.tools.CleanupModelViewer(context.Background(), id) }()
	}
}

func (v *ModelViewer) read(ctx context.Context, url string) ([]byte, error) {
	return v.tools.ReadModelViewerResource(ctx, url)
}

// load reads the mod on a background goroutine and installs it on the UI thread.
func (v *ModelViewer) load() {
	v.loading, v.loadErr = true, ""
	v.releaseScene()
	ctx := v.ctx
	go func() {
		scene, err := v.loadScene(ctx)
		fairy.Go(func() {
			if v.closed || ctx.Err() != nil {
				if scene != nil {
					v.scene = scene
					v.releaseScene()
				}
				return
			}
			v.loading = false
			if err != nil {
				if !v.opened && v.openFailed != nil {
					v.openFailed(err)
					return
				}
				v.loadErr = err.Error()
				return
			}
			v.opened = true
			v.install(scene)
		})
	}()
}

func (v *ModelViewer) loadScene(ctx context.Context) (scene *viewerScene, err error) {
	stage := "wait-startup"
	defer func() {
		if err != nil && !infra.IsCancellationError(err) && ctx.Err() == nil {
			err = infra.ReportError(v.log, err, "ModelViewer.load", infra.Diagnostic{
				Operation: "load", Stage: stage, Fields: map[string]any{"modPath": v.path},
			})
		}
	}()
	if v.tools == nil {
		return nil, fmt.Errorf("%s", T("page.tools.model_viewer.model_data_unavailable"))
	}
	// A --model-viewer launch opens the viewer while startup maintenance still runs.
	if s := v.shell; s != nil && s.deps.Ready != nil {
		if err := s.deps.Ready(ctx); err != nil {
			return nil, err
		}
	}
	stage = "load-transport"
	transport, err := v.tools.LoadModViewer(ctx, v.path)
	if err != nil {
		return nil, err
	}
	release := func() {
		if transport.MemorySessionID != "" {
			_, _ = v.tools.CleanupModelViewer(context.Background(), transport.MemorySessionID)
		}
	}
	stage = "read-meshes"
	_, states := modelviewer.EvaluateViewerState(transport, nil, "")
	scene, err = buildViewerScene(ctx, v.read, transport, states)
	if err != nil {
		release()
		return nil, err
	}
	stage = "upload"
	scene.gpu, err = uploadViewerScene(ctx, v.read, v.log, transport, viewerMeshUploads(scene))
	if err != nil {
		release()
		return nil, err
	}
	return scene, nil
}

func (v *ModelViewer) install(scene *viewerScene) {
	v.scene = scene
	markViewerMeshes(scene)
	v.state = v.defaultState()
	v.preview, v.previewForm = nil, nil
	v.form = ""
	v.highlightGameTextures = false
	v.pendingVariants = nil
	v.variantFailureShown = false
	v.sourceIndices = nil
	v.orientation = vec3{}
	v.camera = frameOrbit(scene.minimum, scene.maximum)
	v.home = v.camera
	v.motion.stop()
	v.previewPath = ""
	if scene.transport.PreviewPath != nil {
		v.previewPath = *scene.transport.PreviewPath
	}
	v.overlayMenus = newViewerOverlayMenus()
	v.anim = viewerAnimation{waves: modelviewer.NewShapeWavePlayback(scene.transport.Meshes)}
	v.evaluate()
	v.startAnimation("")
	v.refreshIneffective()
}

func (v *ModelViewer) defaultState() map[string]any {
	state := maps.Clone(v.scene.transport.DefaultState)
	if state == nil {
		state = map[string]any{}
	}
	return state
}

// shownForm is the form the viewer draws: the one highlighted in the open form list, or the selected one.
func (v *ModelViewer) shownForm() string {
	if v.previewForm != nil {
		return *v.previewForm
	}
	return v.form
}

// evaluate resolves the toggle state being shown and applies it to the scene.
func (v *ModelViewer) evaluate() {
	if v.scene == nil {
		return
	}
	state := v.state
	if v.preview != nil {
		state = v.preview
	}
	v.resolved, v.toggleStates = modelviewer.EvaluateViewerState(v.scene.transport, state, v.shownForm())
	v.observeCompute()
	v.present()
}

// present applies the toggle state, under the values of the animation frame, to the scene and loads the
// position variants it and the next frames newly need.
func (v *ModelViewer) present() {
	scene := v.scene
	if scene == nil {
		return
	}
	form := v.shownForm()
	states := v.toggleStates
	if len(v.anim.values) > 0 {
		state := maps.Clone(v.resolved)
		maps.Copy(state, v.anim.values)
		_, states = modelviewer.EvaluateViewerState(scene.transport, state, form)
	}
	v.revision++
	for i, index := range scene.apply(states) {
		v.loadVariant(i, index)
	}
	for _, upcoming := range v.upcomingAnimationStates(form) {
		for i, state := range upcoming {
			mesh := scene.meshes[i]
			if state.Visible && state.PositionVariant >= 0 && mesh.variants[state.PositionVariant] == nil &&
				mesh.geometryKey != variantGeometryKey(state.PositionVariant) {
				v.loadVariant(i, state.PositionVariant)
			}
		}
	}
}

// loadVariant reads position variant index of mesh i unless it is loading or could not be read before. The
// scene is presented again once a variant it waits to show arrives.
func (v *ModelViewer) loadVariant(i, index int) {
	scene := v.scene
	pending := [2]int{i, index}
	if v.pendingVariants[pending] || scene.meshes[i].unreadable[index] {
		return
	}
	if v.pendingVariants == nil {
		v.pendingVariants = map[[2]int]bool{}
	}
	v.pendingVariants[pending] = true
	ctx := v.ctx
	go func() {
		variant, err := scene.loadVariant(ctx, v.read, i, index)
		fairy.Go(func() {
			if v.scene != scene {
				return
			}
			delete(v.pendingVariants, pending)
			if err != nil {
				if ctx.Err() == nil {
					v.failVariant(scene, i, index, err)
				}
				return
			}
			scene.storeVariant(i, index, variant)
			if scene.waitsFor(i, index) {
				v.present()
			}
		})
	}()
}

// failVariant reports a position variant that could not be read and stops playback, as the React viewer did.
// The variant is not read again: the states that select it show without it, and only the scene's first failure
// is shown, since one cause usually fails many variants.
func (v *ModelViewer) failVariant(scene *viewerScene, i, index int, err error) {
	mesh := scene.meshes[i]
	if mesh.unreadable == nil {
		mesh.unreadable = map[int]bool{}
	}
	mesh.unreadable[index] = true
	err = infra.ReportError(v.log, err, "ModelViewer.loadPositionVariant", infra.Diagnostic{
		Operation: "load-position-variant",
		Fields:    map[string]any{"modPath": v.path, "meshId": mesh.info.ID, "variant": index},
	})
	v.setAnimationPlaying(false)
	if !v.variantFailureShown {
		v.variantFailureShown = true
		v.toasts.Show(kit.ToastError, T("page.tools.model_viewer.toast.load_error"), err.Error())
	}
	v.present()
}

func (v *ModelViewer) selectValue(variable modelviewer.ModelViewerVariable, value any) {
	v.setToggleState(modelviewer.ApplyViewerVariableSelection(v.state, variable, value))
}

// reupload makes the scene's GPU resources again after the device was lost.
func (v *ModelViewer) reupload() {
	scene := v.scene
	if scene == nil || v.uploading {
		return
	}
	v.uploading = true
	uploads := viewerMeshUploads(scene)
	ctx := v.ctx
	go func() {
		set, err := uploadViewerScene(ctx, v.read, v.log, scene.transport, uploads)
		fairy.Go(func() {
			v.uploading = false
			if err != nil || v.scene != scene {
				set.release()
				return
			}
			scene.gpu.release()
			scene.gpu = set
			for _, mesh := range scene.meshes {
				mesh.dirty = true
			}
			v.revision++
		})
	}()
}

// version identifies the picture for the surface's Retain.
func (v *ModelViewer) version(opts viewerDrawOptions) uint64 {
	h := fnv.New64a()
	c := opts.camera
	values := []float64{
		c.target[0], c.target[1], c.target[2], c.distance, c.yaw, c.pitch, c.lens(), float64(v.revision),
		float64(opts.background[0]), float64(opts.background[1]), float64(opts.background[2]),
		opts.exposure, opts.metalness, opts.roughness,
	}
	values = append(values, opts.world[:]...)
	for _, on := range []bool{opts.normalMaps, opts.doubleSided, opts.untextured != nil} {
		if on {
			values = append(values, 1)
		} else {
			values = append(values, 0)
		}
	}
	_ = binary.Write(h, binary.LittleEndian, values)
	_, _ = h.Write([]byte(opts.toneMapping + "|" + opts.environment))
	_ = binary.Write(h, binary.LittleEndian, opts.overlay.version())
	if toon := opts.toon; toon != nil {
		_, _ = fmt.Fprintf(h, "|toon:%v:%p:%p:%p:%p:%p", toon.style, toon.textures.bodyRamp.P,
			toon.textures.hairRamp.P, toon.textures.metalMap.P, toon.textures.faceLightMap.P,
			toon.textures.faceShadow.P)
	}
	return h.Sum64()
}

// drawOptions are the settings the surface draws with over background.
func (v *ModelViewer) drawOptions(background [4]float32) viewerDrawOptions {
	tone, environment, exposure := v.renderSettings()
	opts := viewerDrawOptions{
		camera:      v.camera,
		world:       v.world(),
		background:  background,
		normalMaps:  v.normalMaps,
		doubleSided: v.doubleSided,
		toneMapping: tone,
		environment: environment,
		exposure:    exposure,
	}
	if v.highlightGameTextures {
		opts.untextured = &[3]float32{
			float32(srgbToLinear(viewerGameTextureHighlight >> 16 & 0xff / 255.0)),
			float32(srgbToLinear(viewerGameTextureHighlight >> 8 & 0xff / 255.0)),
			float32(srgbToLinear(viewerGameTextureHighlight & 0xff / 255.0)),
		}
	}
	opts.metalness, opts.roughness = viewerBaseMaterial("")
	if v.scene == nil {
		return opts
	}
	opts.profile = string(v.scene.transport.MaterialProfile)
	opts.metalness, opts.roughness = viewerBaseMaterial(v.scene.transport.MaterialProfile)
	if v.toonActive() {
		style := v.toonStyle()
		opts.toon = &viewerToonDraw{profile: v.toonProfile(), style: style, textures: v.toonTextures(style)}
		opts.minimum, opts.maximum, _ = viewerBounds(v.scene.transport.Meshes, opts.world)
	}
	return opts
}

// drag orbits with the left button and pans with the others or with Shift. The camera follows with the
// damping of the React viewer's OrbitControls.
func (v *ModelViewer) drag(d fairy.DragEvent) {
	if d.Phase == fairy.DragStart {
		v.dragStart = v.camera
		v.dragPanning = d.Button != 0 || d.Mods&keyboard.ModShift != 0
		v.dragLast = [2]float32{}
		v.motion.turn = nil
	}
	if d.Canceled {
		v.camera.target, v.camera.yaw, v.camera.pitch = v.dragStart.target, v.dragStart.yaw, v.dragStart.pitch
		v.motion.yaw, v.motion.pitch, v.motion.shift = 0, 0, vec3{}
		return
	}

	dx, dy := float64(d.DX-v.dragLast[0]), float64(d.DY-v.dragLast[1])
	v.dragLast = [2]float32{d.DX, d.DY}
	if v.dragPanning {
		v.motion.pan(v.camera, dx, dy, float64(v.height))
	} else {
		v.motion.rotate(dx, dy, float64(v.height))
	}
	v.runCamera()
}

// viewAlong turns the camera to the yaw and pitch of an axis view.
func (v *ModelViewer) viewAlong(view [2]float64) {
	v.motion.turn = &view
	v.runCamera()
}

// runCamera steps the camera's motion until none is left.
func (v *ModelViewer) runCamera() {
	if v.motion.running {
		return
	}
	v.motion.running = true
	v.motion.last = time.Now()
	time.AfterFunc(cameraTick, func() { fairy.Go(v.cameraTick) })
}

func (v *ModelViewer) cameraTick() {
	now := time.Now()
	elapsed := now.Sub(v.motion.last).Seconds()
	v.motion.last = now
	if v.closed || !v.motion.step(&v.camera, elapsed) {
		v.motion.running = false
		return
	}
	time.AfterFunc(cameraTick, func() { fairy.Go(v.cameraTick) })
}

// View builds the content of a viewer window of its own.
func (v *ModelViewer) View(cx *fairy.Cx) *fairy.El {
	dark := v.dark()
	t := Theme(dark, v.font)
	// The viewer window keeps the system title bar, so dialogs centre on the whole window.
	t.DialogTopInset = 0
	if v.shownDark == nil || *v.shownDark != dark {
		v.shownDark = &dark
		cx.Window().SetColors(dark, t.Background, t.Foreground)
	}
	kit.Use(t)
	v.toasts = &v.windowToasts
	content := v.content(cx, t, v.header(t, nil))
	v.dialogs(cx)
	cx.Overlay(v.windowToasts.View(), nil)
	return content.Pad(12)
}

// content builds the viewer with header above its menu bar and viewport; a tool's dialog passes nil and titles
// the dialog itself, as React's ModelViewerWorkspace has no header of its own.
func (v *ModelViewer) content(cx *fairy.Cx, t kit.Theme, header *fairy.El) *fairy.El {
	background := t.Background.Mix(t.Muted, 0.5)

	scene := v.scene
	v.syncFieldOfView()
	v.syncToonAssets(v.toonStyle())
	opts := v.drawOptions([4]float32{background.R, background.G, background.B, 1})
	if v.tool != nil && v.tool.overlay != nil {
		opts.overlay = v.tool.overlay()
	}
	animated := opts.overlay != nil && opts.overlay.animated != nil
	surface := fairy.Surface(func(f fairy.SurfaceFrame) bool {
		v.surfaceSize = [2]int{f.Width, f.Height}
		if scene != nil && scene.gpu != nil && scene.gpu.generation != f.Generation {
			v.reupload()
		}
		err := v.renderer.draw(f, scene, opts)
		if message := fmt.Sprint(err); err != nil && message != v.drawErr {
			v.drawErr = message
			_ = infra.ReportError(v.log, err, "ModelViewer.draw", infra.Diagnostic{
				Operation: "draw", Fields: map[string]any{"modPath": v.path},
			})
		}
		return animated
	}).Retain(v.version(opts)).Key(modelViewerSurfaceKey).WFull().HFull().
		Cursor(fairy.CursorMove).DragButtons(fairy.MouseLeft | fairy.MouseRight | fairy.MouseMiddle).
		OnDrag(v.drag).
		OnWheel(func(e fairy.WheelEvent) bool {
			v.motion.zoom(v.camera, float64(e.DY))
			v.runCamera()
			return true
		}).
		OnLayout(func(w, h float32) { v.width, v.height = w, h })
	if v.tool != nil && v.tool.pick != nil {
		surface.Cursor(fairy.CursorCrosshair).
			OnPointerDown(func(e fairy.PointerEvent) {
				if e.Button == 0 {
					v.pressAt = [2]float32{e.LocalX, e.LocalY}
				}
			}).
			OnClick(func() { v.pickAt(v.pressAt[0], v.pressAt[1]) })
	}

	viewport := fairy.Box(surface).Grow(1).MinH(320).MinW(0).Radius(6).Clip()
	if scene != nil && !v.loading {
		viewport.Child(v.axisGizmo(t))
		if v.overlayInViewer() {
			for _, index := range v.shownOverlayMenus() {
				if menu := v.floatingOverlayMenu(t, index, float64(v.width), float64(v.height)); menu != nil {
					viewport.Child(menu)
				}
			}
		}
		if len(scene.transport.Forms) > 1 {
			viewport.Child(v.formSwitcher())
		}
		if count := v.gameTextureCount(); count > 0 {
			viewport.Child(v.gameTextureNotice(t, count))
		}
	}
	switch {
	case v.loading:
		viewport.Child(
			fairy.Col(kit.Spinner(32), fairy.Text(T("page.tools.model_viewer.loading")).FontSize(14).
				LineHeight(20).Color(t.MutedForeground)).
				Absolute().Inset(0).Center().Gap(12).Radius(6).Fill(t.Muted.Alpha(0.7)).PassThrough(),
		)
	case v.loadErr != "":
		viewport.Child(fairy.Col(
			fairy.Text(T("page.tools.model_viewer.toast.load_error")).FontSize(14).LineHeight(20).SemiBold(),
			fairy.Text(v.loadErr).Color(t.Destructive).FontSize(12).LineHeight(16).MaxW(560).Wrap(),
			kit.Button(T("page.tools.model_viewer.retry"), v.load),
		).Absolute().Inset(0).Center().Gap(10).Pad(24))
	}

	main := fairy.Col().Gap(12).WFull().HFull().Fill(t.Background).Color(t.Foreground)
	if header != nil {
		main.Child(header)
	}
	if scene == nil {
		return main.Child(fairy.Row(viewport).Grow(1).MinH(0))
	}
	main.Child(v.menuBar().Shrink(0))
	width, _ := cx.Size()
	main.Child(v.sideSplit(t, viewport, width))
	if bar := v.animationBar(t); bar != nil {
		main.Child(bar)
	}
	return main
}

// header names the mod and its folder. With onClose set it shows a close button, as in the main window's dialog.
func (v *ModelViewer) header(t kit.Theme, onClose func()) *fairy.El {
	name := modname.DisplayName(v.path)
	if v.scene != nil && v.scene.transport.Name != "" {
		name = v.scene.transport.Name
	} else if v.scene == nil {
		name = T("page.tools.model_viewer.title")
	}
	if onClose != nil {
		return fairy.Row(
			fairy.Text(name).FontSize(16).LineHeight(24).SemiBold().Ellipsis().Grow(1).MinW(0),
			kit.IconButton(lucide.X, onClose, kit.Ghost, kit.Small, kit.Tooltip(T("g.close"))).Label(T("g.close")),
		).Gap(12).Align(fairy.Center).Shrink(0)
	}
	row := fairy.Row(
		fairy.Col(
			fairy.Text(name).FontSize(14).LineHeight(20).SemiBold().Ellipsis(),
			fairy.Text(v.path).FontSize(12).LineHeight(16).Color(t.MutedForeground).Ellipsis(),
		).Grow(1).MinW(0),
		kit.Button(T("page.tools.model_viewer.open_folder"), v.openFolder,
			kit.Small, kit.Outline).Shrink(0),
	).Gap(12).Align(fairy.Center).Shrink(0)
	return row
}

func (v *ModelViewer) openFolder() {
	if v.platform == nil {
		return
	}
	if err := v.platform.OpenPath(v.path); err != nil {
		_ = infra.ReportError(v.log, err, "ModelViewer.openFolder", infra.Diagnostic{
			Operation: "open-folder", Fields: map[string]any{"modPath": v.path},
		})
	}
}

func hasFormPrefix(selected, form string) bool {
	return len(selected) > len(form)+2 && selected[:len(form)+2] == form+"::"
}

// openModelViewer shows the viewer of the mod at path in a dialog over the main window.
func (s *Shell) openModelViewer(path string) {
	if s.mods.viewer != nil {
		return
	}
	v := s.NewModelViewer(path)
	v.openFailed = func(err error) {
		if s.mods.viewer != v {
			return
		}
		s.closeModelViewer()
		s.toasts.Show(kit.ToastError, T("page.mod.toast.model-viewer-open-failed"), modelViewerFailure(err))
	}
	s.mods.viewer = v
}

// modelViewerOpening reports whether the viewer of the mod at path is still loading its first scene, while the
// mod screen shows the button that opened it as busy.
func (s *Shell) modelViewerOpening(path string) bool {
	v := s.mods.viewer
	return v != nil && !v.opened && platform.SamePathFold(v.path, path)
}

// modelViewerFailure is the part of a load error after its last "Error", as the React mod screen showed it.
func modelViewerFailure(err error) string {
	message := err.Error()
	if index := strings.LastIndex(message, "Error"); index >= 0 {
		message = strings.TrimSpace(strings.TrimPrefix(strings.TrimLeft(message[index+len("Error"):], " "), ":"))
	}
	return message
}

func (s *Shell) closeModelViewer() {
	if v := s.mods.viewer; v != nil {
		s.mods.viewer = nil
		v.Close()
	}
}

func (s *Shell) modelViewerDialog(cx *fairy.Cx) {
	v := s.mods.viewer
	if v == nil || !v.opened {
		return
	}
	t := kit.T()
	panel := v.content(cx, *t, v.header(*t, s.closeModelViewer)).Pad(12).Radius(t.RadiusXl()).
		Border(1, t.Border).Clip().Shadow(0, 20, 50, 0, shadowColor(t)).Role(fairy.RoleDialog).Label(v.Title())
	showWindowDialog(cx, "mod.model-viewer", panel, s.closeModelViewer)
	v.dialogs(cx)
}
