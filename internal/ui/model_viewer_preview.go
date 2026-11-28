package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/modname"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

const (
	modelPreviewSize         = 512
	modelPreviewCacheEntries = 64
	modelPreviewTimeout      = 30 * time.Second
	// Failures can be transient, such as a timeout or a lost device, so a failed preview is retried once after
	// this delay; a second failure is kept until the render settings change.
	modelPreviewRetryAfter = time.Minute
	modelPreviewRetries    = 1
	// A queued preview whose card has not been built for this long has left the view and waits until its card
	// asks again.
	modelPreviewWantedFor = 2 * time.Second
)

// modelPreviewSettings are the viewer settings a grid preview is rendered with.
type modelPreviewSettings struct {
	ToneMapping string  `json:"toneMapping"`
	Environment string  `json:"environment"`
	Exposure    float64 `json:"exposure"`
	ToonShadows bool    `json:"toonShadows"`
	ToonStyles  string  `json:"toonStyles"`
}

// modelPreviews renders the 3D previews of mod cards without a media preview, one at a time, and keeps the
// latest ones. Every field is used on the UI thread except renderer, which only the running render uses.
type modelPreviews struct {
	images   map[string]*modelPreviewImage
	queue    map[string]*modelPreviewJob
	failures map[string][]modelPreviewFailure
	// current is the preview being rendered; a card built meanwhile must not queue it again.
	current *modelPreviewJob
	cancel  context.CancelFunc
	// pumpPosted is set while a pump waits for the build that asked for previews to finish.
	pumpPosted bool
	used       uint64
	renderer   viewerRenderer
}

type modelPreviewImage struct {
	image      *fairy.Image
	generation uint64
	used       uint64
}

type modelPreviewJob struct {
	key  string
	info mod.ModInfo
	// preset poses the model in a toggle preset's values instead of the INIs' current ones. A preset preview is
	// asked for while the pointer rests on the preset, so it takes the free render slot before the cards asking in
	// the same build.
	preset   *mod.TogglePreset
	settings modelPreviewSettings
	// queued is when a card of the folder first asked for the preview.
	queued time.Time
	wanted time.Time
	// stopped is set on the UI thread when the render is canceled because the folder or the screen was left, or,
	// for a preset preview, because the pointer moved to another preset.
	stopped bool
}

// modelPreviewFailure counts the failed renders of one folder. The key outlives a toggle, so each name of the
// folder keeps its own count: a render of the old name fails without saying anything about the renamed one.
type modelPreviewFailure struct {
	path  string
	at    time.Time
	count int
}

// modelPreviewKey names a preview by everything it shows: the folder, its content signature, the INI toggle
// values the image is posed in and the render settings, so a changed mod is rendered again.
func modelPreviewKey(info mod.ModInfo, settings modelPreviewSettings) string {
	version, path := any(info.Signature), info.Path
	if info.Signature == "" {
		version = info.Mtime
	} else {
		// The signature tells the folder apart from a disabled copy beside it, so toggling keeps the render.
		path = modname.StripDisabledPath(filepath.ToSlash(path))
	}
	var toggles [][3]any
	for _, ini := range info.Inis {
		for _, toggle := range ini.ToggleKeys {
			var current any
			if toggle.CurrentValue != nil {
				current = *toggle.CurrentValue
			}
			toggles = append(toggles, [3]any{
				strings.ToLower(toggle.IniFileName), strings.ToLower(toggle.Variable), current,
			})
		}
	}
	key, _ := json.Marshal([]any{strings.ToLower(path), version, toggles, settings, modelPreviewSize})
	return string(key)
}

// modelPreviewState poses the model in the toggle values the mod's INIs hold now. A variable is matched in its
// own INI first, as "<ini stem>::<name>", so same-named variables of other INIs keep their values.
func modelPreviewState(transport modelviewer.ModelViewerTransport, info mod.ModInfo) map[string]any {
	variables := map[string]modelviewer.ModelViewerVariable{}
	for _, variable := range transport.Variables {
		variables[strings.ToLower(variable.ID)] = variable
	}
	state := maps.Clone(transport.DefaultState)
	if state == nil {
		state = map[string]any{}
	}
	for _, ini := range info.Inis {
		for _, toggle := range ini.ToggleKeys {
			if toggle.CurrentValue == nil {
				continue
			}
			name := strings.TrimLeft(toggle.Variable, "$")
			base := filepath.Base(strings.ReplaceAll(toggle.IniFileName, "\\", "/"))
			stem := strings.TrimSuffix(base, filepath.Ext(base))
			variable, ok := variables[strings.ToLower(stem+"::"+name)]
			if !ok {
				if variable, ok = variables[strings.ToLower(name)]; !ok {
					continue
				}
			}
			state = modelviewer.ApplyViewerVariableSelection(state, variable, *toggle.CurrentValue)
		}
	}
	return state
}

// presetINIValues keys a toggle preset's values, which name each INI by its path in the mod, by absolute path, as
// viewerStateWithValues takes them.
func presetINIValues(modPath string, inis map[string]map[string]string) map[string]map[string]string {
	values := make(map[string]map[string]string, len(inis))
	for relative, vars := range inis {
		values[filepath.Join(modPath, filepath.FromSlash(relative))] = vars
	}
	return values
}

// modelPreviewEnabled reports whether the mod's card shows a model preview: the setting is on and the mod has
// INIs, no media preview, and no download writing to it.
func (s *Shell) modelPreviewEnabled(info mod.ModInfo) bool {
	if s.deps.Tools == nil || !s.settingsLoaded(setting.KeyModGridModelPreview) ||
		!s.boolSetting(setting.KeyModGridModelPreview) {
		return false
	}
	if info.Preview != nil && isPreviewMedia(*info.Preview) {
		return false
	}
	return !s.isNteGame() && !s.isModDownloading(info.Path) && !s.isDownloadPlaceholder(info.Path) &&
		len(info.Inis) > 0
}

// cardModelPreview is the preview area of a card that shows a model preview, and false for any other card:
// the rendered image, a placeholder while it is made, or fallback when it could not be made.
func (s *Shell) cardModelPreview(info mod.ModInfo, fallback *fairy.El) (*fairy.El, bool) {
	if !s.modelPreviewEnabled(info) {
		return nil, false
	}
	img, loading := s.modelPreview(info)
	switch {
	case img != nil:
		return fairy.ImageEl(img).WFull().HFull().Fit(fairy.FitContain).PassThrough(), true
	case loading:
		return kit.Skeleton().WFull().HFull().Radius(0), true
	}
	return fallback, true
}

func (s *Shell) modelPreviewMenuItems(info mod.ModInfo) []kit.MenuItem {
	return []kit.MenuItem{
		{
			Label: T("page.mod.context-menu.open-model-viewer"), Icon: lucide.Box,
			OnClick: func() { s.openModelViewer(info.Path) },
		},
		{Separator: true},
		{
			Label: T("page.mod.context-menu.paste-preview"), Icon: lucide.Clipboard,
			OnClick: func() { s.pastePreview(info) },
		},
	}
}

func (s *Shell) modelPreviewSettings() modelPreviewSettings {
	v := &ModelViewer{shell: s}
	var settings modelPreviewSettings
	settings.ToneMapping, settings.Environment, settings.Exposure = v.renderSettings()
	settings.ToonShadows = v.toonShadows()
	if s.settingsLoaded(setting.KeyModelViewerToonStyles) {
		settings.ToonStyles = s.stringSetting(setting.KeyModelViewerToonStyles)
	}
	return settings
}

// modelPreview returns the mod's rendered preview, or whether it is still being made. Call it on the UI thread
// each time the card is built: a queued preview is only made while its card keeps asking for it.
func (s *Shell) modelPreview(info mod.ModInfo) (*fairy.Image, bool) {
	return s.requestModelPreview(info, nil)
}

// togglePresetPreview returns the mod's model posed in preset's values, or whether it is still being made. Call
// it on the UI thread each time the preview is built, as modelPreview.
func (s *Shell) togglePresetPreview(info mod.ModInfo, preset mod.TogglePreset) (*fairy.Image, bool) {
	if s.deps.Tools == nil || s.isNteGame() || s.isModDownloading(info.Path) || s.isDownloadPlaceholder(info.Path) ||
		len(info.Inis) == 0 {
		return nil, false
	}
	return s.requestModelPreview(info, &preset)
}

func (s *Shell) requestModelPreview(info mod.ModInfo, preset *mod.TogglePreset) (*fairy.Image, bool) {
	p := &s.mods.modelPreviews
	settings := s.modelPreviewSettings()
	key := modelPreviewKey(info, settings)
	if preset != nil {
		values, _ := json.Marshal(preset.INIs)
		key += string(values)
	}
	if entry := p.images[key]; entry != nil && entry.generation == fairy.DeviceGeneration() {
		p.used++
		entry.used = p.used
		return entry.image, false
	}
	if failure := p.failure(key, info.Path); failure != nil &&
		(failure.count > modelPreviewRetries || time.Since(failure.at) < modelPreviewRetryAfter) {
		return nil, false
	}
	if p.current != nil && !p.current.stopped && p.current.key == key {
		return nil, true
	}
	if preset != nil {
		// The pointer has left every other preset, so their previews are no longer wanted.
		maps.DeleteFunc(p.queue, func(other string, job *modelPreviewJob) bool {
			return job.preset != nil && other != key
		})
		if p.current != nil && p.current.preset != nil && !p.current.stopped {
			p.current.stopped = true
			p.cancel()
		}
	}

	if p.queue == nil {
		p.queue = map[string]*modelPreviewJob{}
	}
	job := p.queue[key]
	if job == nil {
		job = &modelPreviewJob{key: key, preset: preset, settings: settings, queued: time.Now()}
		p.queue[key] = job
	}
	job.info, job.wanted = info, time.Now()
	s.postModelPreviewPump()
	return nil, true
}

// postModelPreviewPump runs pumpModelPreviews after the current build, once every card and preset of the build has
// asked; the view builds the cards first. Starting a render changes nothing on screen, so it does not rebuild.
func (s *Shell) postModelPreviewPump() {
	p := &s.mods.modelPreviews
	if p.pumpPosted {
		return
	}
	p.pumpPosted = true
	fairy.GoQuiet(func() {
		p.pumpPosted = false
		s.pumpModelPreviews()
	})
}

// pumpModelPreviews starts the preset preview asked for last, else the card preview asked for last, skipping the
// ones nothing wants now.
func (s *Shell) pumpModelPreviews() {
	p := &s.mods.modelPreviews
	if p.current != nil {
		return
	}
	var next *modelPreviewJob
	for _, job := range p.queue {
		if time.Since(job.wanted) > modelPreviewWantedFor {
			continue
		}
		switch {
		case next == nil:
			next = job
		case (job.preset != nil) != (next.preset != nil):
			if job.preset != nil {
				next = job
			}
		case job.wanted.After(next.wanted):
			next = job
		}
	}
	if next == nil {
		return
	}
	delete(p.queue, next.key)
	p.current = next
	jobCtx, cancelJob := context.WithCancel(context.Background())
	p.cancel = cancelJob
	s.background(func(runCtx context.Context) {
		defer cancelJob()
		defer context.AfterFunc(runCtx, cancelJob)()
		ctx, cancel := context.WithTimeout(jobCtx, modelPreviewTimeout)
		defer cancel()
		img, generation, err := s.renderModelPreview(ctx, p, next)
		if err != nil && jobCtx.Err() == nil {
			_, service, operation := next.diagnostics()
			fields := map[string]any{"modName": next.info.Name, "modPath": next.info.Path}
			if next.preset != nil {
				fields["presetId"], fields["presetName"] = next.preset.ID, next.preset.Name
			}
			err = infra.ReportError(s.deps.Log, err, service, infra.Diagnostic{Operation: operation, Fields: fields})
		}
		fairy.Go(func() { s.finishModelPreview(next, img, generation, err) })
	})
}

// cancelRunning stops the running preview and drops the queued ones, as when another folder is selected or the
// mod screen is left.
func (p *modelPreviews) cancelRunning() {
	if p.current != nil && !p.current.stopped {
		p.current.stopped = true
		p.cancel()
	}
	clear(p.queue)
}

func (s *Shell) finishModelPreview(job *modelPreviewJob, img *fairy.Image, generation uint64, err error) {
	p := &s.mods.modelPreviews
	p.current, p.cancel = nil, nil
	switch {
	case err != nil && job.stopped:
		// A render stopped because its folder or the screen was left says nothing about the mod, so it is not
		// counted.
	case err != nil:
		if p.failures == nil {
			p.failures = map[string][]modelPreviewFailure{}
		}
		failure := p.failure(job.key, job.info.Path)
		if failure == nil {
			p.failures[job.key] = append(p.failures[job.key], modelPreviewFailure{path: job.info.Path})
			failure = &p.failures[job.key][len(p.failures[job.key])-1]
		}
		failure.at, failure.count = time.Now(), failure.count+1
		if failure.count <= modelPreviewRetries {
			// A card that stays in view is built again only when something changes, so it is asked to be.
			time.AfterFunc(modelPreviewRetryAfter, func() { fairy.Go(func() {}) })
		}
	default:
		delete(p.failures, job.key)
		p.store(job.key, img, generation)
	}
	s.pumpModelPreviews()
}

func (p *modelPreviews) failure(key, path string) *modelPreviewFailure {
	failures := p.failures[key]
	if len(failures) == 0 {
		return nil
	}
	// Each card asks on every build. The paths come from the same scanner, so an exact match spares the UI thread
	// the system calls SamePathFold makes.
	i := slices.IndexFunc(failures, func(failure modelPreviewFailure) bool { return failure.path == path })
	if i < 0 {
		i = slices.IndexFunc(failures, func(failure modelPreviewFailure) bool {
			return platform.SamePathFold(failure.path, path)
		})
	}
	if i < 0 {
		return nil
	}
	return &failures[i]
}

// store keeps img and drops the least recently shown previews past the limit. A dropped image is freed when
// it is collected, since a card built this frame may still show it.
func (p *modelPreviews) store(key string, img *fairy.Image, generation uint64) {
	if p.images == nil {
		p.images = map[string]*modelPreviewImage{}
	}
	p.used++
	p.images[key] = &modelPreviewImage{image: img, generation: generation, used: p.used}
	for len(p.images) > modelPreviewCacheEntries {
		oldest := ""
		for key, entry := range p.images {
			if oldest == "" || entry.used < p.images[oldest].used {
				oldest = key
			}
		}
		delete(p.images, oldest)
	}
}

// renderModelPreview loads the mod's grid preview model, posed as its INIs are, and renders it into a square
// image with a transparent background. It runs off the UI thread.
func (s *Shell) renderModelPreview(
	ctx context.Context,
	p *modelPreviews,
	job *modelPreviewJob,
) (img *fairy.Image, generation uint64, err error) {
	go warmViewerRenderer()
	tools := s.deps.Tools
	started := time.Now()
	stage := "load"
	defer func() {
		if err != nil {
			err = infra.AnnotateError(err, infra.Diagnostic{Stage: stage})
		}
	}()
	transport, err := tools.LoadModGridPreview(ctx, job.info.Path)
	if err != nil {
		return nil, 0, err
	}
	if id := transport.MemorySessionID; id != "" {
		defer func() { _, _ = tools.CleanupModelViewer(context.Background(), id) }()
	}
	// Choices that cannot be read leave the parts with the game's textures.
	if mods := s.deps.Mod; mods != nil {
		if saved, err := mods.GetViewerTextureChoices(ctx, job.info.Path); err == nil {
			applySavedTextureChoices(&transport, saved)
		}
	}
	loaded := time.Now()

	stage = "read-meshes"
	state := modelPreviewState(transport, job.info)
	if job.preset != nil {
		state = viewerStateWithValues(state, transport.INIs, presetINIValues(job.info.Path, job.preset.INIs))
	}
	_, states := modelviewer.EvaluateViewerState(transport, state, "")
	transport, states = visiblePreviewTransport(transport, states)
	if len(transport.Meshes) == 0 {
		return nil, 0, errors.New("the mod draws no mesh in this toggle state")
	}
	scene, err := buildViewerScene(ctx, tools.ReadModelViewerResource, transport, states)
	if err != nil {
		return nil, 0, err
	}
	markViewerMeshes(scene)
	built := time.Now()

	stage = "upload"
	scene.gpu, err = uploadViewerScene(ctx, tools.ReadModelViewerResource, s.deps.Log, transport,
		viewerMeshUploads(scene))
	if err != nil {
		return nil, 0, err
	}
	defer scene.gpu.release()
	// The upload can finish without an error after a cancellation, since only some texture reads check ctx.
	if ctx.Err() != nil {
		return nil, 0, ctx.Err()
	}
	uploaded := time.Now()

	stage = "render"
	opts := modelPreviewDrawOptions(scene, job.settings)
	var drawErr error
	img, err = fairy.SurfaceImage(modelPreviewSize, modelPreviewSize, 1, func(f fairy.SurfaceFrame) {
		if scene.gpu.generation != f.Generation {
			drawErr = errors.New("the graphics device was reset while the preview loaded")
			return
		}
		generation = f.Generation
		drawErr = p.renderer.draw(f, scene, opts)
	})
	if err == nil && drawErr != nil {
		img.Release()
		img, err = nil, drawErr
	}
	if err != nil {
		return nil, 0, fmt.Errorf("render %dpx model preview: %w", modelPreviewSize, err)
	}

	done := time.Now()
	title, service, _ := job.diagnostics()
	s.deps.Log.Info(
		fmt.Sprintf(
			"%s rendered in %dms "+
				"(wait=%dms load=%dms scene=%dms upload=%dms render=%dms meshes=%d textures=%d)",
			title,
			done.Sub(started).Milliseconds(),
			started.Sub(job.queued).Milliseconds(),
			loaded.Sub(started).Milliseconds(),
			built.Sub(loaded).Milliseconds(),
			uploaded.Sub(built).Milliseconds(),
			done.Sub(uploaded).Milliseconds(),
			len(transport.Meshes),
			len(transport.Textures),
		),
		service,
	)
	return img, generation, nil
}

// diagnostics names the job's kind in the log.
func (job *modelPreviewJob) diagnostics() (title, service, operation string) {
	if job.preset != nil {
		return "Toggle preset preview", "Mod.togglePresetPreview", "toggle-preset-preview"
	}
	return "Grid model preview", "Mod.gridModelPreview", "grid-model-preview"
}

// visiblePreviewTransport keeps only the meshes the state draws and the textures they use: a still image shows
// one toggle state, so the rest is never read.
func visiblePreviewTransport(
	transport modelviewer.ModelViewerTransport,
	states []modelviewer.ViewerMeshState,
) (modelviewer.ModelViewerTransport, []modelviewer.ViewerMeshState) {
	var meshes []modelviewer.ModelViewerMeshTransport
	var shown []modelviewer.ViewerMeshState
	used := map[string]bool{}
	for i, mesh := range transport.Meshes {
		if i >= len(states) || !states[i].Visible {
			continue
		}
		meshes, shown = append(meshes, mesh), append(shown, states[i])
		state := states[i]
		for _, key := range []string{
			state.TexKey, state.NormalMapKey, state.LightMapKey, state.MaterialMapKey, state.CutoutMapKey,
		} {
			used[key] = true
		}
		for _, key := range []*string{
			mesh.TexKey, mesh.NormalMapKey, mesh.LightMapKey, mesh.MaterialMapKey, mesh.CutoutMapKey,
			mesh.HighlightMapKey, mesh.RampMapKey, mesh.FaceSDFMapKey,
		} {
			if key != nil {
				used[*key] = true
			}
		}
	}
	transport.Meshes = meshes
	textures := maps.Clone(transport.Textures)
	maps.DeleteFunc(textures, func(key string, _ modelviewer.ModelViewerTextureTransport) bool { return !used[key] })
	transport.Textures = textures
	return transport, shown
}

// modelPreviewDrawOptions frames the scene as the viewer opens it and draws it with settings over a
// transparent background.
func modelPreviewDrawOptions(scene *viewerScene, settings modelPreviewSettings) viewerDrawOptions {
	profile := scene.transport.MaterialProfile
	camera := frameOrbit(scene.minimum, scene.maximum)
	opts := viewerDrawOptions{
		world:       scene.world,
		normalMaps:  true,
		doubleSided: true,
		toneMapping: settings.ToneMapping,
		environment: settings.Environment,
		exposure:    settings.Exposure,
		profile:     string(profile),
	}
	opts.metalness, opts.roughness = viewerBaseMaterial(profile)
	if toon := string(profile); viewerToonProfiles[toon].mode != 0 && settings.ToonShadows {
		// The toon lens is narrower; the camera dollies back so the model keeps its size, as the viewer does.
		camera.distance *= math.Tan(camera.lens()/2) / math.Tan(toonFieldOfView/2)
		camera.fov = toonFieldOfView
		opts.toon = &viewerToonDraw{profile: toon, style: parseToonStyle(settings.ToonStyles, toon)}
		if toon == efmiProfile {
			opts.toon.efmi = efmiPreset("")
		}
		opts.minimum, opts.maximum, _ = viewerBounds(scene.transport.Meshes, opts.world)
	}
	opts.camera = camera
	return opts
}
