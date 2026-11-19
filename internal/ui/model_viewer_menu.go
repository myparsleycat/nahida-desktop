package ui

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"maps"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

// viewerRotations are the Model menu's turns of the model, in degrees of roll, pitch and yaw.
var viewerRotations = []struct {
	label string
	delta vec3
}{
	{"Left 90°", vec3{0, 0, -90}},
	{"Right 90°", vec3{0, 0, 90}},
	{"Up 90°", vec3{0, -90, 0}},
	{"Down 90°", vec3{0, 90, 0}},
	{"Flip 180°", vec3{0, 0, 180}},
}

// orientationMatrix turns the model by roll, pitch and yaw in degrees, as a three.js Euler in YXZ order.
func orientationMatrix(o vec3) mat4 {
	const degree = math.Pi / 180
	return rotationY(o[2] * degree).mul(rotationX(o[1] * degree)).mul(rotationZ(o[0] * degree))
}

func (v *ModelViewer) world() mat4 {
	if v.scene == nil {
		return identity4()
	}
	return orientationMatrix(v.orientation).mul(v.scene.world)
}

// rotate turns the model and moves the camera with the centre of its bounds, so it stays in view.
func (v *ModelViewer) rotate(delta vec3) {
	if v.scene == nil {
		return
	}
	before := v.boundsCenter()
	v.orientation = v.orientation.add(delta)
	v.camera.follow(v.boundsCenter().sub(before))
}

func (v *ModelViewer) boundsCenter() vec3 {
	minimum, maximum, ok := viewerBounds(v.scene.transport.Meshes, v.world())
	if !ok {
		return vec3{}
	}
	return minimum.add(maximum).scale(0.5)
}

func (v *ModelViewer) resetView() {
	v.orientation = vec3{}
	v.camera = v.home
	v.motion.stop()
}

// renderSettings returns the stored tone mapping, environment and exposure, or the defaults until they load.
// An exposure being typed shows at once.
func (v *ModelViewer) renderSettings() (tone, environment string, exposure float64) {
	tone, environment, exposure = viewerToneNeutral, viewerEnvironmentStudio, viewerDefaultExposure
	s := v.shell
	if s != nil && s.settingsLoaded(
		setting.KeyModelViewerToneMapping, setting.KeyModelViewerEnvironment, setting.KeyModelViewerExposure,
	) {
		if value := s.stringSetting(setting.KeyModelViewerToneMapping); value != "" {
			tone = value
		}
		if value := s.stringSetting(setting.KeyModelViewerEnvironment); value != "" {
			environment = value
		}
		if value, ok := s.settings.values[setting.KeyModelViewerExposure].(float64); ok {
			exposure = clampViewerExposure(value)
		}
	}
	if v.exposureDraft != nil {
		if value, err := strconv.ParseFloat(*v.exposureDraft, 64); err == nil {
			exposure = clampViewerExposure(value)
		}
	}
	return tone, environment, exposure
}

func (v *ModelViewer) setRenderSetting(key string, value any) {
	if v.shell != nil {
		v.shell.setSetting(key, value)
	}
}

func (v *ModelViewer) commitExposure(value float64) {
	v.exposureDraft = nil
	if !math.IsNaN(value) {
		v.setRenderSetting(setting.KeyModelViewerExposure, clampViewerExposure(value))
	}
}

// formatViewerNumber shows a number as the React viewer's sliders do: at most two decimals, without trailing
// zeros.
func formatViewerNumber(value float64) string {
	return strconv.FormatFloat(math.Round(value*100)/100, 'f', -1, 64)
}

func (v *ModelViewer) menuBar() *fairy.El {
	tone, environment, exposure := v.renderSettings()
	radio := func(label, value, current, key string) kit.MenuItem {
		return kit.MenuItem{Label: label, Radio: true, Checked: value == current, OnClick: func() {
			v.setRenderSetting(key, value)
		}}
	}
	menus := []kit.MenubarMenu{
		{Label: T("page.tools.model_viewer.menu.model"), Items: func() []kit.MenuItem {
			items := []kit.MenuItem{{Heading: true, Label: T("page.tools.model_viewer.menu.rotate")}}
			for _, rotation := range viewerRotations {
				items = append(items, kit.MenuItem{
					Label:   T("page.tools.model_viewer.rotate_actions." + rotation.label),
					OnClick: func() { v.rotate(rotation.delta) },
				})
			}
			items = append(items, kit.MenuItem{Separator: true}, kit.MenuItem{
				Label: T("page.tools.model_viewer.menu.reset"), Icon: lucide.RotateCcw, OnClick: v.resetView,
			})
			if v.characterMotionAvailable() {
				wanted, _ := v.motionWanted()
				checked := wanted && v.motionFailed != v.motionKey()
				items = append(items, kit.MenuItem{Separator: true}, kit.MenuItem{
					Label: T("page.tools.model_viewer.menu.character_motion"), Checkbox: true,
					Checked: checked, Loading: v.motionCancel != nil,
					OnClick: func() { v.setCharacterMotion(!checked) },
				})
			}
			items = append(items, v.originalMenuItems()...)
			return items
		}},
		{Label: T("page.tools.model_viewer.menu.texture"), Items: func() []kit.MenuItem {
			return []kit.MenuItem{{
				Label: T("page.tools.model_viewer.menu.double_sided"), Checkbox: true, Checked: v.doubleSided,
				OnClick: func() { v.doubleSided = !v.doubleSided },
			}}
		}},
		{Label: T("page.tools.model_viewer.menu.rendering"), Items: func() []kit.MenuItem {
			items := []kit.MenuItem{
				{Heading: true, Label: T("page.tools.model_viewer.menu.tone_mapping")},
				radio(
					T("page.tools.model_viewer.menu.tone_neutral"),
					viewerToneNeutral,
					tone,
					setting.KeyModelViewerToneMapping,
				),
				radio(
					T("page.tools.model_viewer.menu.tone_aces"),
					viewerToneACES,
					tone,
					setting.KeyModelViewerToneMapping,
				),
				radio(T("page.tools.model_viewer.menu.none"), viewerToneNone, tone, setting.KeyModelViewerToneMapping),
				{Separator: true},
				{Heading: true, Label: T("page.tools.model_viewer.menu.environment")},
				radio(
					T("page.tools.model_viewer.menu.environment_studio"),
					viewerEnvironmentStudio,
					environment,
					setting.KeyModelViewerEnvironment,
				),
				radio(
					T("page.tools.model_viewer.menu.environment_soft"),
					viewerEnvironmentSoft,
					environment,
					setting.KeyModelViewerEnvironment,
				),
				radio(
					T("page.tools.model_viewer.menu.none"),
					viewerEnvironmentNone,
					environment,
					setting.KeyModelViewerEnvironment,
				),
				{Separator: true},
				{Heading: true, Label: T("page.tools.model_viewer.menu.exposure")},
				{Content: v.exposureControl(exposure)},
			}
			if v.hasToon() {
				toonShadows := v.toonShadows()
				items = append(items, kit.MenuItem{Separator: true}, kit.MenuItem{
					Label: T("page.tools.model_viewer.menu.toon_shadows"), Checkbox: true, Checked: toonShadows,
					OnClick: func() { v.setRenderSetting(setting.KeyModelViewerToonShadows, !toonShadows) },
				}, kit.MenuItem{
					Label:   T("page.tools.model_viewer.menu." + viewerToonProfiles[v.toonProfile()].label),
					Submenu: v.toonMenu(),
				})
			}
			if v.hasNormalMaps() {
				items = append(items, kit.MenuItem{Separator: true}, kit.MenuItem{
					Label: T("page.tools.model_viewer.menu.normal_maps"), Checkbox: true, Checked: v.normalMaps,
					OnClick: func() { v.normalMaps = !v.normalMaps },
				})
			}
			return items
		}},
	}
	if v.hasToggles() {
		menus = append(
			menus,
			kit.MenubarMenu{Label: T("page.tools.model_viewer.menu.toggle"), Items: func() []kit.MenuItem {
				items := []kit.MenuItem{
					{
						Label:   T("page.tools.model_viewer.menu.save_to_ini"),
						Icon:    lucide.Save,
						OnClick: v.saveTogglesToINI,
					},
				}
				items = append(items, v.togglePresetMenuItems()...)
				items = append(items, []kit.MenuItem{
					{Separator: true},
					{Label: T("page.tools.model_viewer.menu.reset"), Icon: lucide.RotateCcw, OnClick: v.resetToggles},
				}...)
				gamePreview := v.gamePreviewEnabled()
				linkable, _ := v.desktopChannel()
				items = append(items, kit.MenuItem{Separator: true}, kit.MenuItem{
					Label: T("page.tools.model_viewer.menu.game_preview"), Checkbox: true, Checked: gamePreview,
					Disabled: !linkable,
					OnClick:  func() { v.setRenderSetting(setting.KeyModelViewerGamePreview, !gamePreview) },
				})

				if len(v.shownOverlayMenus()) == 0 {
					return items
				}
				inViewer := v.overlayInViewer()
				items = append(items, kit.MenuItem{Separator: true}, kit.MenuItem{
					Label: T("page.tools.model_viewer.menu.overlay_in_viewer"), Checkbox: true, Checked: inViewer,
					OnClick: func() { v.setRenderSetting(setting.KeyModelViewerOverlayInViewer, !inViewer) },
				})
				if inViewer {
					items = append(items, kit.MenuItem{
						Label:    T("page.tools.model_viewer.menu.overlay_reset_placement"),
						Icon:     lucide.RotateCcw,
						Disabled: !v.hasOverlayMenuPlacement(),
						OnClick:  v.resetOverlayMenuPlacement,
					})
				}
				return items
			}},
		)
	}
	menus = append(menus, kit.MenubarMenu{Label: T("page.tools.model_viewer.menu.misc"), Items: func() []kit.MenuItem {
		return []kit.MenuItem{{
			Label: T("page.tools.model_viewer.menu.capture_set_preview"), Icon: lucide.Camera,
			Disabled: v.scene == nil || v.savingPreview, OnClick: v.requestPreviewCapture,
		}, {
			Label: T("page.tools.model_viewer.menu.save_png"), Icon: lucide.ImageDown,
			Disabled: v.scene == nil || v.savingPNG, Submenu: v.pngMenu(),
		}}
	}})
	return kit.Menubar("model-viewer", menus)
}

func (v *ModelViewer) exposureControl(exposure float64) *fairy.El {
	t := kit.T()
	text := formatViewerNumber(exposure)
	if v.exposureDraft != nil {
		text = *v.exposureDraft
	}
	commit := func() {
		if v.exposureDraft == nil {
			return
		}
		value, err := strconv.ParseFloat(*v.exposureDraft, 64)
		if err != nil {
			value = math.NaN()
		}
		v.commitExposure(value)
	}
	return fairy.Col(
		fairy.Row(
			kit.Button("-0.1", func() { v.commitExposure(exposure - 0.1) }, kit.Outline, kit.Small),
			kit.Button(
				T("page.tools.model_viewer.menu.reset"),
				func() { v.commitExposure(viewerDefaultExposure) },
				kit.Outline,
				kit.Small,
			),
			kit.Button("+0.1", func() { v.commitExposure(exposure + 0.1) }, kit.Outline, kit.Small),
		).Gap(4),
		kit.TextField(
			text,
			func(value string) { v.exposureDraft = &value },
			kit.Label(T("page.tools.model_viewer.menu.exposure")),
		).
			Key("model-viewer.exposure").
			OnBlur(commit).
			OnSubmit(func(string) { commit() }),
		fairy.Row(
			fairy.Text(formatViewerNumber(viewerMinExposure)),
			fairy.Text(formatViewerNumber(viewerMaxExposure)),
		).Justify(fairy.JustifyBetween).FontSize(11).Color(t.MutedForeground),
	).Gap(8)
}

func (v *ModelViewer) hasNormalMaps() bool {
	if v.scene == nil {
		return false
	}
	for _, mesh := range v.scene.transport.Meshes {
		if (mesh.NormalMapKey != nil && *mesh.NormalMapKey != "") || len(mesh.NormalMapVariants) > 0 {
			return true
		}
	}
	return false
}

func (v *ModelViewer) hasToggles() bool {
	if v.scene == nil {
		return false
	}
	return len(v.shownOverlayMenus()) > 0 || slices.ContainsFunc(v.scene.transport.Variables, v.showsVariable)
}

func (v *ModelViewer) resetToggles() {
	if v.scene == nil {
		return
	}
	v.setToggleState(v.defaultState())
}

// saveTogglesToINI writes the current toggle values into each INI's persist variables, and sets them in the
// running games that load the mod, so the game shows the values the INI now starts with.
func (v *ModelViewer) saveTogglesToINI() {
	scene := v.scene
	if scene == nil || v.tools == nil || v.shell == nil {
		return
	}
	state := maps.Clone(v.state)
	inis := scene.transport.INIs
	toasts := v.toasts
	v.shell.background(func(ctx context.Context) {
		var updated, repaired bool
		var applied, missing int
		var gameErr error
		for _, ini := range inis {
			values := iniToggleValues(state, ini.Prefix)
			result, err := v.tools.PersistModelViewerToggleState(ini.Path, values)
			if err != nil {
				err = infra.ReportError(v.log, err, "ModelViewer.saveTogglesToINI", infra.Diagnostic{
					Operation: "save-to-ini", Fields: map[string]any{"modPath": v.path, "iniPath": ini.Path},
				})
				toasts.Show(kit.ToastError, T("page.tools.model_viewer.toast.save_to_ini_error"), err.Error())
				return
			}
			updated = updated || len(result.UpdatedVariables) > 0
			repaired = repaired || len(result.RepairedVariables) > 0

			if gameErr != nil {
				continue
			}
			game, err := v.tools.ApplyModelViewerToggleStateToGame(ctx, ini.Path, values, true)
			if err != nil {
				gameErr = infra.ReportError(v.log, err, "ModelViewer.saveTogglesToINI", infra.Diagnostic{
					Operation: "apply-to-game", Fields: map[string]any{"modPath": v.path, "iniPath": ini.Path},
				})
				continue
			}
			applied += len(game.Applied)
			missing += len(game.Missing)
		}

		body := ""
		switch {
		case missing > 0:
			body = T("page.tools.model_viewer.toast.apply_to_game_missing")
		case applied > 0:
			body = T("page.tools.model_viewer.toast.save_to_ini_game_applied")
		}
		switch {
		case updated && repaired:
			toasts.Show(kit.ToastSuccess, T("page.tools.model_viewer.toast.save_to_ini_success_repaired"), body)
		case updated:
			toasts.Show(kit.ToastSuccess, T("page.tools.model_viewer.toast.save_to_ini_success"), body)
		case repaired:
			toasts.Show(kit.ToastWarning, T("page.tools.model_viewer.toast.save_to_ini_repaired"), body)
		// The INI already held these values; only the game differed.
		case missing > 0:
			toasts.Show(kit.ToastWarning, T("page.tools.model_viewer.toast.apply_to_game_missing"), "")
		case applied > 0:
			toasts.Show(kit.ToastSuccess, T("page.tools.model_viewer.toast.apply_to_game_success"), "")
		default:
			toasts.Show(kit.ToastWarning, T("page.tools.model_viewer.toast.save_to_ini_no_changes"), "")
		}
		if gameErr != nil {
			toasts.Show(kit.ToastWarning, T("page.tools.model_viewer.toast.apply_to_game_error"), gameErr.Error())
		}
	})
}

// applyTogglesToGame sets the toggle variables of the current state in the running games that load the mod,
// without changing the INI files.
func (v *ModelViewer) applyTogglesToGame() {
	scene := v.scene
	if scene == nil || v.tools == nil || v.shell == nil || v.applyingToGame {
		return
	}
	state := toggleState(v.state, &scene.transport)
	inis := scene.transport.INIs
	toasts := v.toasts
	v.applyingToGame = true
	v.shell.background(func(ctx context.Context) {
		defer fairy.Go(func() { v.applyingToGame = false })
		var games, applied, missing int
		for _, ini := range inis {
			result, err := v.tools.ApplyModelViewerToggleStateToGame(
				ctx,
				ini.Path,
				iniToggleValues(state, ini.Prefix),
				false,
			)
			if err != nil {
				err = infra.ReportError(v.log, err, "ModelViewer.applyTogglesToGame", infra.Diagnostic{
					Operation: "apply-to-game", Fields: map[string]any{"modPath": v.path, "iniPath": ini.Path},
				})
				toasts.Show(kit.ToastError, T("page.tools.model_viewer.toast.apply_to_game_error"), err.Error())
				return
			}
			games = max(games, result.Games)
			applied += len(result.Applied)
			missing += len(result.Missing)
		}
		switch {
		case games == 0:
			toasts.Show(kit.ToastWarning, T("page.tools.model_viewer.toast.apply_to_game_not_connected"), "")
		case missing > 0 || applied == 0:
			toasts.Show(kit.ToastWarning, T("page.tools.model_viewer.toast.apply_to_game_missing"), "")
		default:
			toasts.Show(kit.ToastSuccess, T("page.tools.model_viewer.toast.apply_to_game_success"), "")
		}
	})
}

// refreshGameLinked rechecks whether a running game takes the mod's toggle changes. Call it on the UI thread; the
// check resolves paths on disk, so it runs in the background and only the latest one sets gameLinked.
func (v *ModelViewer) refreshGameLinked() {
	if v.tools == nil || v.shell == nil {
		return
	}
	v.gameLinkCheck++
	check := v.gameLinkCheck
	v.shell.background(func(context.Context) {
		linked := v.tools.ModelViewerGameLinked(v.path)
		fairy.Go(func() {
			if v.gameLinkCheck != check || v.closed || v.gameLinked == linked {
				return
			}
			v.gameLinked = linked
			v.syncGamePreview()
		})
	})
}

// toggleState keeps the variables the viewer's controls and overlay menus set. The rest of the state holds every
// variable the INIs assign, including runtime counters and handshake values that a running game must keep.
func toggleState(state map[string]any, transport *modelviewer.ModelViewerTransport) map[string]any {
	ids := map[string]bool{}
	for _, variable := range transport.Variables {
		ids[variable.ID] = true
		for _, effect := range variable.Effects {
			ids[effect.Var] = true
		}
	}
	for _, menu := range transport.OverlayMenus {
		for _, page := range menu.Pages {
			for _, button := range page.Buttons {
				ids[button.Variable] = true
			}
			for _, slider := range page.Sliders {
				for _, mode := range slider.Modes {
					ids[mode.Variable] = true
				}
			}
		}
	}
	toggles := map[string]any{}
	for id, value := range state {
		if ids[id] {
			toggles[id] = value
		}
	}
	return toggles
}

// iniToggleValues picks the values of the variables one INI declares from the viewer state, by their declared
// names.
func iniToggleValues(state map[string]any, prefix string) map[string]any {
	values := map[string]any{}
	for id, value := range state {
		if name, ok := strings.CutPrefix(id, prefix); ok {
			values[name] = value
		}
	}
	return values
}

func (v *ModelViewer) requestPreviewCapture() {
	if v.scene == nil || v.savingPreview {
		return
	}
	if v.previewPath != "" {
		v.confirmPreview = true
		return
	}
	v.savePreview()
}

// dialogs shows the viewer's confirmations. Call it after the overlay that holds the viewer, so they show above it.
func (v *ModelViewer) dialogs(cx *fairy.Cx) {
	v.togglePresetDialogs(cx)
	if !v.confirmPreview {
		return
	}
	close := func() { v.confirmPreview = false }
	name := v.path
	if v.scene != nil && v.scene.transport.Name != "" {
		name = v.scene.transport.Name
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title:       T("page.tools.model_viewer.dialog.overwrite_preview.title"),
		Alert:       true,
		Description: T("page.tools.model_viewer.dialog.overwrite_preview.description", "name", name),
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), close, kit.Outline),
			kit.Button(T("page.tools.model_viewer.dialog.overwrite_preview.confirm"), func() {
				close()
				v.savePreview()
			}),
		},
		OnClose: close,
		Key:     "model-viewer.overwrite-preview",
	})
}

// savePreview renders the view again with a transparent background and saves its centre square as the mod's
// preview, as the React viewer's capture did.
func (v *ModelViewer) savePreview() {
	if v.shell == nil || v.shell.deps.Mod == nil {
		return
	}
	shot, err := v.capture(1)
	if errors.Is(err, errViewerCaptureNotReady) {
		v.toasts.Show(kit.ToastWarning, T("page.tools.model_viewer.toast.capture_not_ready"), "")
		return
	}
	if err != nil {
		_ = infra.ReportError(v.log, err, "ModelViewer.capturePreview", infra.Diagnostic{
			Operation: "capture-preview", Stage: "render", Fields: map[string]any{"modPath": v.path},
		})
		v.toasts.Show(kit.ToastError, T("page.tools.model_viewer.toast.capture_preview_error"), err.Error())
		return
	}
	v.savingPreview = true
	s, path, existing, toasts, log := v.shell, v.path, v.previewPath, v.toasts, v.log
	s.background(func(ctx context.Context) {
		frame, err := shot.NRGBA()
		shot.Release()
		stage := "readback"
		var saved string
		if err == nil {
			stage = "save"
			bounds := frame.Bounds()
			size := min(bounds.Dx(), bounds.Dy())
			x, y := (bounds.Dx()-size)/2, (bounds.Dy()-size)/2
			var encoded bytes.Buffer
			err = png.Encode(&encoded, frame.SubImage(image.Rect(x, y, x+size, y+size)))
			if err == nil {
				var previous *string
				if existing != "" {
					previous = &existing
				}
				data := "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())
				saved, err = s.deps.Mod.PastePreview(ctx, path, data, "base64", previous)
			}
		}
		if err != nil {
			err = infra.ReportError(log, err, "ModelViewer.capturePreview", infra.Diagnostic{
				Operation: "capture-preview", Stage: stage, Fields: map[string]any{"modPath": path},
			})
		}
		fairy.Go(func() {
			v.savingPreview = false
			if err != nil {
				toasts.Show(kit.ToastError, T("page.tools.model_viewer.toast.capture_preview_error"), err.Error())
				return
			}
			v.previewPath = saved
			toasts.Show(kit.ToastSuccess, T("page.tools.model_viewer.toast.capture_preview_success"), "")
			if group := s.mods.group; group != nil && platform.SameOrChildPath(group.Path, path) {
				s.refreshListing()
			}
		})
	})
}

// errViewerCaptureNotReady is returned while the scene is not on the current device, so a capture would miss it.
var errViewerCaptureNotReady = errors.New("model viewer is still loading the model")

// maxCaptureSize is the longest texture side Direct3D 11 allows.
const maxCaptureSize = 16384

// pngScales are the PNG export's resolutions as multiples of the viewport's device pixels.
var pngScales = []int{1, 2, 4}

// maxCapturePixels keeps a capture's colour and depth targets, four bytes a pixel each, at 512 MiB: the
// per-resource limit Direct3D 11 sets for an adapter with 2 GiB of memory.
const maxCapturePixels = 1 << 27

// captureSize is the surface size times scale, shrunk in proportion so neither side passes maxCaptureSize and the
// area stays within maxCapturePixels.
func (v *ModelViewer) captureSize(scale int) (width, height int) {
	width, height = v.surfaceSize[0]*scale, v.surfaceSize[1]*scale
	shrink := max(
		float64(max(width, height))/maxCaptureSize,
		math.Sqrt(float64(width)*float64(height)/maxCapturePixels),
	)
	if shrink > 1 {
		width, height = max(int(float64(width)/shrink), 1), max(int(float64(height)/shrink), 1)
	}
	return width, height
}

// capture renders the current view again with a transparent background at captureSize(scale). The caller
// releases the image; reading its pixels back may run on any goroutine.
func (v *ModelViewer) capture(scale int) (*fairy.Image, error) {
	width, height := v.captureSize(scale)
	scene := v.scene
	// While the scene waits for a mesh it keeps drawing the frame it shows, so only a scene without its GPU set is
	// incomplete.
	if scene == nil || width <= 0 || height <= 0 || v.uploading || scene.gpu == nil ||
		scene.gpu.generation != fairy.DeviceGeneration() {
		return nil, errViewerCaptureNotReady
	}
	opts := v.drawOptions([4]float32{})
	opts.morph = float32(v.interpolation(trackMod, time.Now()))
	opts.interpolating = v.interpolating()
	opts.singleSample = scale > 1
	opts.pixelScale = float32(width) / float32(v.surfaceSize[0])
	// The live surface may not draw again soon to bring the targets back to its size and sample count, so the
	// capture's are not kept until then.
	release := opts.singleSample || [2]int{width, height} != v.surfaceSize
	var drawErr error
	shot, err := fairy.SurfaceImage(width, height, 1, func(f fairy.SurfaceFrame) {
		drawErr = v.renderer.draw(f, v.scene, opts)
		if release {
			v.renderer.releaseTargets()
		}
	})
	if err != nil {
		return nil, err
	}
	if drawErr != nil {
		shot.Release()
		return nil, drawErr
	}
	return shot, nil
}

// savePNG asks for a destination and then saves the view rendered at the scale there as a PNG file.
func (v *ModelViewer) savePNG(scale int) {
	if v.shell == nil || v.shell.deps.Dialog == nil || v.savingPNG {
		return
	}
	name := filepath.Base(v.path)
	if v.scene != nil && v.scene.transport.Name != "" {
		name = v.scene.transport.Name
	}
	if v.shell.deps.FS != nil {
		name = v.shell.deps.FS.SanitizeWindowsFilename(name, "_")
	}
	v.savingPNG = true
	s, path, toasts, log := v.shell, v.path, v.toasts, v.log
	go func() {
		selection, err := s.deps.Dialog.SaveFile(platform.SaveFileOptions{
			SuggestedName: name + ".png",
			Filters:       []platform.FileFilter{{Name: "PNG", Extensions: []string{"png"}}},
			Role:          "model-viewer-png",
		})
		if err != nil {
			err = infra.ReportError(log, err, "ModelViewer.savePNG", infra.Diagnostic{
				Operation: "save-png", Stage: "dialog", Fields: map[string]any{"modPath": path, "scale": scale},
			})
		}
		fairy.Go(func() {
			switch {
			case err != nil:
				v.savingPNG = false
				toasts.Show(kit.ToastError, T("page.tools.model_viewer.toast.save_png_error"), err.Error())
			case selection.Canceled || selection.FilePath == "":
				v.savingPNG = false
			default:
				v.writePNG(scale, selection.FilePath)
			}
		})
	}()
}

// writePNG renders the view at the scale and writes it to destination, reading the pixels back and encoding them
// off the UI thread.
func (v *ModelViewer) writePNG(scale int, destination string) {
	if v.closed {
		v.savingPNG = false
		return
	}
	fields := map[string]any{"modPath": v.path, "scale": scale, "destinationPath": destination}
	shot, err := v.capture(scale)
	if errors.Is(err, errViewerCaptureNotReady) {
		v.savingPNG = false
		v.toasts.Show(kit.ToastWarning, T("page.tools.model_viewer.toast.capture_not_ready"), "")
		return
	}
	if err != nil {
		v.savingPNG = false
		width, height := v.captureSize(scale)
		fields["size"] = [2]int{width, height}
		_ = infra.ReportError(v.log, err, "ModelViewer.savePNG", infra.Diagnostic{
			Operation: "save-png", Stage: "render", Fields: fields,
		})
		v.toasts.Show(kit.ToastError, T("page.tools.model_viewer.toast.save_png_error"), err.Error())
		return
	}

	toasts, log := v.toasts, v.log
	go func() {
		frame, err := shot.NRGBA()
		shot.Release()
		stage := "readback"
		if err == nil {
			stage = "write"
			err = platform.WriteFileAtomicFunc(destination, 0o644, func(w io.Writer) error {
				return png.Encode(w, frame)
			})
		}
		if err != nil {
			err = infra.ReportError(log, err, "ModelViewer.savePNG", infra.Diagnostic{
				Operation: "save-png", Stage: stage, Fields: fields,
			})
		}
		fairy.Go(func() {
			v.savingPNG = false
			if err != nil {
				toasts.Show(kit.ToastError, T("page.tools.model_viewer.toast.save_png_error"), err.Error())
				return
			}
			bounds := frame.Bounds()
			toasts.Show(kit.ToastSuccess, T("page.tools.model_viewer.toast.save_png_success"),
				fmt.Sprintf("%s · %d×%d", filepath.Base(destination), bounds.Dx(), bounds.Dy()))
		})
	}()
}

// pngMenu lists the PNG export's scales with the size each one saves.
func (v *ModelViewer) pngMenu() []kit.MenuItem {
	items := make([]kit.MenuItem, 0, len(pngScales))
	for _, scale := range pngScales {
		width, height := v.captureSize(scale)
		items = append(items, kit.MenuItem{
			Label: T("page.tools.model_viewer.menu.save_png_scale",
				"scale", strconv.Itoa(scale), "width", strconv.Itoa(width), "height", strconv.Itoa(height)),
			OnClick: func() { v.savePNG(scale) },
		})
	}
	return items
}

// toonMenu is the toon style submenu: the style's switches and, for GIMI, the texture folder, the shadow ramps
// it lists and the face light map choice.
func (v *ModelViewer) toonMenu() []kit.MenuItem {
	style := v.toonStyle()
	update := func(change func(*viewerToonStyle)) func() {
		return func() { v.updateToonStyle(change) }
	}
	flag := func(label string, on bool, change func(*viewerToonStyle)) kit.MenuItem {
		return kit.MenuItem{
			Label: T("page.tools.model_viewer.menu." + label), Checkbox: true, Checked: on, OnClick: update(change),
		}
	}
	var items []kit.MenuItem
	c := v.toonCharacter()
	if c != nil {
		items = append(items, kit.MenuItem{
			Heading: true, Label: T("page.tools.model_viewer.menu.toon_character", "name", c.Name(catalog.Language())),
		})
	}
	items = append(items,
		flag("toon_outlines", style.Outlines, func(s *viewerToonStyle) { s.Outlines = !s.Outlines }),
		flag("toon_self_shadow", style.SelfShadow, func(s *viewerToonStyle) { s.SelfShadow = !s.SelfShadow }),
		flag("toon_light_follows_camera", style.LightFollowsCamera, func(s *viewerToonStyle) {
			s.LightFollowsCamera = !s.LightFollowsCamera
		}),
	)
	if v.toonProfile() == efmiProfile {
		items = append(
			items,
			flag("toon_flat_face", style.FlatFace, func(s *viewerToonStyle) { s.FlatFace = !s.FlatFace }),
		)
	}
	items = append(items, kit.MenuItem{
		Label: T("page.tools.model_viewer.menu.toon_shading"), Submenu: v.toonShadingMenu(style), SubmenuWidth: 288,
	})
	if c != nil {
		items = append(items, kit.MenuItem{
			Label: T("page.tools.model_viewer.menu.toon_character_clear"), Icon: lucide.RotateCcw,
			Disabled: !v.hasCharacterToonStyle(), OnClick: v.clearCharacterToonStyle,
		})
	}
	if v.toonProfile() != gimiProfile {
		return items
	}

	items = append(items, kit.MenuItem{Separator: true}, kit.MenuItem{
		Label: T("page.tools.model_viewer.menu.gimi_toon_assets_folder"), Icon: lucide.FolderOpen,
		OnClick: func() { v.pickToonAssetDir(style) },
	})
	if style.AssetDir != "" {
		items = append(items, kit.MenuItem{Heading: true, Label: style.AssetDir}, kit.MenuItem{
			Label: T("page.tools.model_viewer.menu.gimi_toon_assets_clear"), Icon: lucide.X,
			OnClick: update(func(s *viewerToonStyle) { s.AssetDir = "" }),
		})
	}
	listed := v.toonAssets.listed
	if listed != nil && v.toonAssets.dir != style.AssetDir {
		listed = nil
	}
	bundled := c != nil && c.ramps != nil
	if listed != nil || bundled {
		items = append(items, kit.MenuItem{Separator: true},
			kit.MenuItem{Heading: true, Label: T("page.tools.model_viewer.menu.gimi_toon_ramp")})
		if bundled || (listed != nil && c != nil && c.Genshin != nil && c.Genshin.Avatar != "") {
			items = append(items, kit.MenuItem{
				Label: T("page.tools.model_viewer.menu.gimi_toon_ramp_auto", "name", c.Name(catalog.Language())),
				Radio: true, Checked: style.rampAuto,
				OnClick: update(func(s *viewerToonStyle) { s.Ramp = toonRampAuto }),
			})
		}
		items = append(items, kit.MenuItem{
			Label: T("page.tools.model_viewer.menu.gimi_toon_ramp_builtin"), Radio: true,
			Checked: style.Ramp == "" && !style.rampAuto,
			OnClick: update(func(s *viewerToonStyle) { s.Ramp, s.rampAuto = "", false }),
		})
		var ramps []modelviewer.GenshinToonRampTransport
		if listed != nil {
			ramps = listed.Ramps
		}
		for _, ramp := range ramps {
			label := ramp.Name
			if len(label) > len("avatar_") && strings.EqualFold(label[:len("avatar_")], "avatar_") {
				label = label[len("avatar_"):]
			}
			items = append(items, kit.MenuItem{
				Label: label, Radio: true, Checked: style.Ramp == ramp.Name && !style.rampAuto,
				OnClick: update(func(s *viewerToonStyle) { s.Ramp, s.rampAuto = ramp.Name, false }),
			})
		}
	}

	if v.scene == nil || !v.scene.genshinFace {
		return items
	}
	items = append(items, kit.MenuItem{Separator: true},
		kit.MenuItem{Heading: true, Label: T("page.tools.model_viewer.menu.gimi_toon_face_type")})
	for _, faceType := range append([]string{"auto"}, genshinFaceTypes...) {
		items = append(items, kit.MenuItem{
			Label:   T("page.tools.model_viewer.menu.gimi_toon_face_" + faceType),
			Radio:   true,
			Checked: style.FaceType == faceType,
			OnClick: update(func(s *viewerToonStyle) { s.FaceType = faceType }),
		})
	}
	return items
}

// toonShadingMenu holds a slider or a colour picker for each shading value of the toon style, and a reset.
func (v *ModelViewer) toonShadingMenu(style viewerToonStyle) []kit.MenuItem {
	t := kit.T()
	profile := v.toonProfile()
	var body *characterMaterial
	var use uint32
	if c := v.toonCharacter(); c != nil && c.materials != nil && c.materials.body != nil {
		body = c.materials.body
		use, _ = characterMaterialUse(profile, style.characterShading, style.Shading)
	}
	var rows []*fairy.El
	for _, param := range viewerToonParams[toonStyleKey(profile)] {
		if param.hidden {
			continue
		}
		value, changed := style.Shading[param.key]
		if !changed {
			value = param.def
		}
		// A value the character's material sets shows the material's until the user changes it for the character.
		fromCharacter := false
		if bit := characterMenuKeys[profile][param.key]; body != nil && use&bit != 0 {
			value, fromCharacter = characterShadingValue(profile, param.key, body)
		}
		label := T("page.tools.model_viewer.menu.toon_param_" + toonParamLabel(param.key))
		heading := []*fairy.El{fairy.Text(label).Grow(1).MinW(0)}
		if fromCharacter {
			heading = append(heading,
				fairy.Text(T("page.tools.model_viewer.menu.toon_param_character")).Color(t.MutedForeground))
		}
		key := "model-viewer.toon." + param.key
		if param.color {
			color := fairy.Color{A: 1}
			for i, channel := range []*float32{&color.R, &color.G, &color.B} {
				*channel = float32(min(max(linearToSRGB(value[i]), 0), 1))
			}
			rows = append(rows, fairy.Row(append(heading,
				kit.ColorPicker(key, color, func(c fairy.Color, _ bool) {
					v.setToonShading(param.key, [3]float64{
						srgbToLinear(float64(c.R)), srgbToLinear(float64(c.G)), srgbToLinear(float64(c.B)),
					})
				}, kit.Label(label)),
			)...).Gap(8).Align(fairy.Center))
			continue
		}
		rows = append(rows, fairy.Col(
			fairy.Row(append(heading,
				fairy.Text(formatToonParam(value[0], param.step)).Color(t.MutedForeground),
			)...).Gap(8),
			kit.Slider(value[0], param.min, param.max, param.step, func(x float64) {
				v.setToonShading(param.key, [3]float64{x})
			}, kit.Label(label)).Key(key),
		).Gap(4))
	}
	return []kit.MenuItem{
		{Content: fairy.Col(rows...).Gap(12).FontSize(13)},
		{Separator: true},
		{
			Label: T("page.tools.model_viewer.menu.toon_shading_reset"), Icon: lucide.RotateCcw,
			Disabled: len(style.Shading) == 0, OnClick: v.resetToonShading,
		},
	}
}

// toonParamLabel is the snake case of a toonParam key, as its label key ends.
func toonParamLabel(key string) string {
	var label strings.Builder
	for _, r := range key {
		if unicode.IsUpper(r) {
			label.WriteByte('_')
			r = unicode.ToLower(r)
		}
		label.WriteRune(r)
	}
	return label.String()
}

// formatToonParam shows a value with as many decimals as its step has.
func formatToonParam(value, step float64) string {
	decimals := 0
	for step > 0 && step < 1 && decimals < 4 {
		step *= 10
		decimals++
		if math.Abs(step-math.Round(step)) < 1e-9 {
			break
		}
	}
	return strconv.FormatFloat(value, 'f', decimals, 64)
}
