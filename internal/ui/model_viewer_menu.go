package ui

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
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
			return append(items, kit.MenuItem{Separator: true}, kit.MenuItem{
				Label: T("page.tools.model_viewer.menu.reset"), Icon: lucide.RotateCcw, OnClick: v.resetView,
			})
		}},
		{Label: T("page.tools.model_viewer.menu.texture"), Items: func() []kit.MenuItem {
			return []kit.MenuItem{{
				Label: "Double Sided", Checkbox: true, Checked: v.doubleSided,
				OnClick: func() { v.doubleSided = !v.doubleSided },
			}}
		}},
		{Label: T("page.tools.model_viewer.menu.rendering"), Items: func() []kit.MenuItem {
			items := []kit.MenuItem{
				{Heading: true, Label: "Tone Mapping"},
				radio("Neutral", viewerToneNeutral, tone, setting.KeyModelViewerToneMapping),
				radio("ACES Filmic", viewerToneACES, tone, setting.KeyModelViewerToneMapping),
				radio("None", viewerToneNone, tone, setting.KeyModelViewerToneMapping),
				{Separator: true},
				{Heading: true, Label: "Environment"},
				radio("Studio", viewerEnvironmentStudio, environment, setting.KeyModelViewerEnvironment),
				radio("Soft", viewerEnvironmentSoft, environment, setting.KeyModelViewerEnvironment),
				radio("None", viewerEnvironmentNone, environment, setting.KeyModelViewerEnvironment),
				{Separator: true},
				{Heading: true, Label: "Exposure"},
				{Content: v.exposureControl(exposure)},
			}
			if v.hasToon() {
				toonShadows := v.toonShadows()
				items = append(items, kit.MenuItem{Separator: true}, kit.MenuItem{
					Label: "Toon Shadows", Checkbox: true, Checked: toonShadows,
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
					{Separator: true},
					{Label: T("page.tools.model_viewer.menu.reset"), Icon: lucide.RotateCcw, OnClick: v.resetToggles},
				}
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
			kit.Button("Reset", func() { v.commitExposure(viewerDefaultExposure) }, kit.Outline, kit.Small),
			kit.Button("+0.1", func() { v.commitExposure(exposure + 0.1) }, kit.Outline, kit.Small),
		).Gap(4),
		kit.TextField(text, func(value string) { v.exposureDraft = &value }, kit.Label("Exposure")).
			Key("model-viewer.exposure").OnBlur(commit).OnSubmit(func(string) { commit() }),
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

// applyTogglesToGame sets every variable of the current toggle state in the running games that load the mod,
// without changing the INI files.
func (v *ModelViewer) applyTogglesToGame() {
	scene := v.scene
	if scene == nil || v.tools == nil || v.shell == nil || v.applyingToGame {
		return
	}
	state := maps.Clone(v.state)
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
			if v.gameLinkCheck == check {
				v.gameLinked = linked
			}
		})
	})
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
	shot, err := v.capture()
	if err != nil {
		_ = infra.ReportError(v.log, err, "ModelViewer.capturePreview", infra.Diagnostic{
			Operation: "capture-preview", Stage: "render", Fields: map[string]any{"modPath": v.path},
		})
		v.toasts.Show(kit.ToastError, T("page.tools.model_viewer.toast.capture_preview_error"), err.Error())
		return
	}
	v.savingPreview = true
	s, path, existing, toasts := v.shell, v.path, v.previewPath, v.toasts
	s.background(func(ctx context.Context) {
		var encoded bytes.Buffer
		err := png.Encode(&encoded, shot)
		var saved string
		if err == nil {
			var previous *string
			if existing != "" {
				previous = &existing
			}
			data := "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())
			saved, err = s.deps.Mod.PastePreview(ctx, path, data, "base64", previous)
		}
		if err != nil {
			err = infra.ReportError(v.log, err, "ModelViewer.capturePreview", infra.Diagnostic{
				Operation: "capture-preview", Stage: "save", Fields: map[string]any{"modPath": path},
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

// capture renders the current view at the surface's size and crops its centre square.
func (v *ModelViewer) capture() (image.Image, error) {
	width, height := v.surfaceSize[0], v.surfaceSize[1]
	if v.scene == nil || width <= 0 || height <= 0 {
		return nil, errors.New("the viewer has not drawn the model yet")
	}
	opts := v.drawOptions([4]float32{})
	var drawErr error
	shot, err := fairy.SurfaceImage(width, height, 1, func(f fairy.SurfaceFrame) {
		drawErr = v.renderer.draw(f, v.scene, opts)
	})
	if err != nil {
		return nil, err
	}
	defer shot.Release()
	if drawErr != nil {
		return nil, drawErr
	}
	pixels, err := shot.NRGBA()
	if err != nil {
		return nil, err
	}
	size := min(width, height)
	x, y := (width-size)/2, (height-size)/2
	return pixels.SubImage(image.Rect(x, y, x+size, y+size)), nil
}

// toonMenu is the toon style submenu: the style's switches and, for GIMI, the texture folder and, once it is
// listed, the shadow ramp and face shadow map choices.
func (v *ModelViewer) toonMenu() []kit.MenuItem {
	style := v.toonStyle()
	update := func(change func(*viewerToonStyle)) func() {
		return func() {
			latest := v.toonStyle()
			change(&latest)
			v.setToonStyle(latest)
		}
	}
	flag := func(label string, on bool, change func(*viewerToonStyle)) kit.MenuItem {
		return kit.MenuItem{
			Label: T("page.tools.model_viewer.menu." + label), Checkbox: true, Checked: on, OnClick: update(change),
		}
	}
	items := []kit.MenuItem{
		flag("toon_outlines", style.Outlines, func(s *viewerToonStyle) { s.Outlines = !s.Outlines }),
		flag("toon_self_shadow", style.SelfShadow, func(s *viewerToonStyle) { s.SelfShadow = !s.SelfShadow }),
		flag("toon_light_follows_camera", style.LightFollowsCamera, func(s *viewerToonStyle) {
			s.LightFollowsCamera = !s.LightFollowsCamera
		}),
	}
	if v.toonProfile() == efmiProfile {
		items = append(
			items,
			flag("toon_flat_face", style.FlatFace, func(s *viewerToonStyle) { s.FlatFace = !s.FlatFace }),
		)
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
	if listed == nil || v.toonAssets.dir != style.AssetDir {
		return items
	}

	items = append(items, kit.MenuItem{Separator: true},
		kit.MenuItem{Heading: true, Label: T("page.tools.model_viewer.menu.gimi_toon_ramp")},
		kit.MenuItem{
			Label: T("page.tools.model_viewer.menu.gimi_toon_ramp_builtin"), Radio: true, Checked: style.Ramp == "",
			OnClick: update(func(s *viewerToonStyle) { s.Ramp = "" }),
		})
	for _, ramp := range listed.Ramps {
		label := ramp.Name
		if len(label) > len("avatar_") && strings.EqualFold(label[:len("avatar_")], "avatar_") {
			label = label[len("avatar_"):]
		}
		items = append(items, kit.MenuItem{
			Label: label, Radio: true, Checked: style.Ramp == ramp.Name,
			OnClick: update(func(s *viewerToonStyle) { s.Ramp = ramp.Name }),
		})
	}
	items = append(items, kit.MenuItem{Separator: true},
		kit.MenuItem{Heading: true, Label: T("page.tools.model_viewer.menu.gimi_toon_face_type")})
	for _, faceType := range append([]string{"auto"}, genshinFaceTypes...) {
		items = append(items, kit.MenuItem{
			Label:    T("page.tools.model_viewer.menu.gimi_toon_face_" + faceType),
			Radio:    true,
			Checked:  style.FaceType == faceType,
			Disabled: faceType != "auto" && listed.FaceLightmaps[faceType] == "",
			OnClick:  update(func(s *viewerToonStyle) { s.FaceType = faceType }),
		})
	}
	return items
}
