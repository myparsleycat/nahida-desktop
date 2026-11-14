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
	// A queued preview whose card has not been built for this long has left the view and is dropped.
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
	running  bool
	used     uint64
	renderer viewerRenderer
}

type modelPreviewImage struct {
	image      *fairy.Image
	generation uint64
	used       uint64
}

type modelPreviewJob struct {
	key      string
	info     mod.ModInfo
	settings modelPreviewSettings
	wanted   time.Time
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
	p := &s.mods.modelPreviews
	settings := s.modelPreviewSettings()
	key := modelPreviewKey(info, settings)
	if entry := p.images[key]; entry != nil && entry.generation == fairy.DeviceGeneration() {
		p.used++
		entry.used = p.used
		return entry.image, false
	}
	if failure := p.failure(key, info.Path); failure != nil &&
		(failure.count > modelPreviewRetries || time.Since(failure.at) < modelPreviewRetryAfter) {
		return nil, false
	}

	if p.queue == nil {
		p.queue = map[string]*modelPreviewJob{}
	}
	job := p.queue[key]
	if job == nil {
		job = &modelPreviewJob{key: key, settings: settings}
		p.queue[key] = job
	}
	job.info, job.wanted = info, time.Now()
	s.pumpModelPreviews()
	return nil, true
}

// pumpModelPreviews starts the preview whose card asked for it last, after dropping the ones no card wants now.
func (s *Shell) pumpModelPreviews() {
	p := &s.mods.modelPreviews
	if p.running {
		return
	}
	var next *modelPreviewJob
	for key, job := range p.queue {
		if time.Since(job.wanted) > modelPreviewWantedFor {
			delete(p.queue, key)
			continue
		}
		if next == nil || job.wanted.After(next.wanted) {
			next = job
		}
	}
	if next == nil {
		return
	}
	delete(p.queue, next.key)
	p.running = true
	s.background(func(ctx context.Context) {
		ctx, cancel := context.WithTimeout(ctx, modelPreviewTimeout)
		defer cancel()
		img, generation, err := s.renderModelPreview(ctx, p, next)
		if err != nil {
			err = infra.ReportError(s.deps.Log, err, "Mod.gridModelPreview", infra.Diagnostic{
				Operation: "grid-model-preview",
				Fields:    map[string]any{"modName": next.info.Name, "modPath": next.info.Path},
			})
		}
		fairy.Go(func() { s.finishModelPreview(next, img, generation, err) })
	})
}

func (s *Shell) finishModelPreview(job *modelPreviewJob, img *fairy.Image, generation uint64, err error) {
	p := &s.mods.modelPreviews
	p.running = false
	if err != nil {
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
	} else {
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
	tools := s.deps.Tools
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

	stage = "read-meshes"
	_, states := modelviewer.EvaluateViewerState(transport, modelPreviewState(transport, job.info), "")
	transport, states = visiblePreviewTransport(transport, states)
	if len(transport.Meshes) == 0 {
		return nil, 0, errors.New("the mod draws no mesh in its current toggle state")
	}
	scene, err := buildViewerScene(ctx, tools.ReadModelViewerResource, transport, states)
	if err != nil {
		return nil, 0, err
	}
	markViewerMeshes(scene)

	stage = "upload"
	scene.gpu, err = uploadViewerScene(ctx, tools.ReadModelViewerResource, s.deps.Log, transport,
		viewerMeshUploads(scene))
	if err != nil {
		return nil, 0, err
	}
	defer scene.gpu.release()

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
	return img, generation, nil
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
			mesh.HighlightMapKey, mesh.RampMapKey,
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
		opts.minimum, opts.maximum, _ = viewerBounds(scene.transport.Meshes, opts.world)
	}
	opts.camera = camera
	return opts
}
