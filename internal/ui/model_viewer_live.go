package ui

import (
	"fmt"
	"slices"

	"github.com/myparsleycat/fairy"

	"nahida.live/desktop/internal/blenderlink"
	"nahida.live/desktop/internal/infra"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

func (s *Shell) registerBlenderLiveEvents(on func(string, func([]any))) {
	on(blenderlink.EventState, func(data []any) {
		if state, ok := first(data).(blenderlink.State); ok {
			s.tools.blenderLink.state, s.tools.blenderLink.read = state, true
			s.blenderLinksChanged(state)
		}
	})
	on(modelviewer.EventLiveMeshes, func(data []any) {
		update, ok := first(data).(modelviewer.ModelViewerLiveUpdate)
		if !ok {
			return
		}
		for v := range s.viewers {
			v.applyLiveMeshes(update)
		}
	})
}

// viewerBlenderSource is a viewer's Blender live link scene: the connection, the game to export for when the
// scene names none, and the character the user chose.
type viewerBlenderSource struct {
	link      uint64
	importer  string
	character string
}

// applyLiveMeshes shows meshes and textures a Blender live link rebuilt in place of the scene's. A mesh that is
// not loaded takes the new buffers when it next loads; a loaded mesh or a texture keeps drawing until its
// replacement is uploaded. A mesh with geometry the scene does not have, or an update that asks for it, opens the
// scene again; an empty one the scene does not have is ignored. While a Blender scene loads, the updates wait for
// install, since the session they name may be the one loading.
func (v *ModelViewer) applyLiveMeshes(update modelviewer.ModelViewerLiveUpdate) {
	scene := v.scene
	if v.loading && v.blender != nil {
		v.pendingLive = append(v.pendingLive, update)
		return
	}
	if scene == nil || scene.transport.MemorySessionID != update.MemorySessionID {
		return
	}
	index := func(id string) int {
		return slices.IndexFunc(scene.meshes, func(mesh *viewerMesh) bool { return mesh.info.ID == id })
	}
	unknown := slices.ContainsFunc(update.Meshes, func(live modelviewer.ModelViewerLiveMesh) bool {
		return live.IndexCount > 0 && index(live.ID) < 0
	})
	if update.Reload || unknown {
		v.load()
		return
	}
	for _, live := range update.Meshes {
		i := index(live.ID)
		if i < 0 {
			continue
		}
		mesh := scene.meshes[i]
		info := mesh.info
		info.GeometryURL, info.BlendURL, info.BlendPalette = live.GeometryURL, live.BlendURL, live.BlendPalette
		info.VertexCount, info.IndexCount, info.Bounds = live.VertexCount, live.IndexCount, live.Bounds
		// The rebuilt mesh may have other vertices than the targets and variants of the exported one.
		info.ShapeTargets, info.PositionVariants = nil, nil
		scene.transport.Meshes[i] = info
		v.loadLiveMesh(scene, i, info)
	}
	for _, live := range update.Textures {
		if scene.transport.Textures == nil {
			scene.transport.Textures = map[string]modelviewer.ModelViewerTextureTransport{}
		}
		scene.transport.Textures[live.Key] = live.Texture
		v.loadLiveTexture(scene, live.Key, live.Texture)
	}
}

// loadLiveTexture reads and uploads a texture a Blender live link replaced, then swaps it in, unless a newer one
// was asked for meanwhile.
func (v *ModelViewer) loadLiveTexture(scene *viewerScene, key string, entry modelviewer.ModelViewerTextureTransport) {
	if v.liveTextureWanted == nil {
		v.liveTextureWanted = map[string]string{}
	}
	v.liveTextureWanted[key] = entry.URL
	gpu := scene.gpu
	if !v.liveUploadReady(scene, gpu, fairy.DeviceGeneration()) {
		return
	}
	ctx, read, log, normalXY := v.ctx, v.read, v.log, scene.transport.NormalXYReconstruct
	entries := map[string]modelviewer.ModelViewerTextureTransport{key: entry}
	go func() {
		textures, generations, err := uploadViewerTextures(ctx, read, log, entries, normalXY)
		fairy.Go(func() {
			texture, uploaded := textures[key]
			if v.liveTextureWanted[key] != entry.URL || !v.liveUploadReady(scene, gpu, fairy.DeviceGeneration()) ||
				uploaded && !generations[gpu.generation] {
				if uploaded {
					texture.view.Release()
				}
				return
			}
			delete(v.liveTextureWanted, key)
			if !uploaded {
				if err != nil && ctx.Err() == nil {
					v.log.Warn(fmt.Sprintf("Model viewer could not show live texture %s: %v", key, err),
						"ModelViewer.loadLiveTexture")
				}
				return
			}
			old, replaced := gpu.textures[key]
			gpu.textures[key] = texture
			if replaced {
				_ = fairy.UseDevice(func(fairy.GPU) error {
					old.view.Release()
					return nil
				})
			}
			v.revision++
			v.present()
		})
	}()
}

// loadLiveMesh swaps in new geometry for a resident mesh. For an unloaded mesh, the next regular load uses the
// new geometry, including when recovery could not restore the mesh's previous buffers.
func (v *ModelViewer) loadLiveMesh(scene *viewerScene, i int, info modelviewer.ModelViewerMeshTransport) {
	mesh := scene.meshes[i]
	if mesh.unloaded {
		coverageChanged := (mesh.info.IndexCount > 0) != (info.IndexCount > 0)
		mesh.info, mesh.failed = info, false
		mesh.variants, mesh.unreadable = map[int]*viewerPositionGeometry{}, nil
		delete(v.liveWanted, i)
		if m := v.activeMotion(); m != nil {
			m.replaceBlend(i, "", nil, nil)
			v.reloadMotionBlend(scene, m, i)
		}
		if coverageChanged {
			v.rematchOriginal(scene)
		}
		return
	}

	if v.liveWanted == nil {
		v.liveWanted = map[int]string{}
	}
	v.liveWanted[i] = info.GeometryURL
	gpu := scene.gpu
	if !v.liveUploadReady(scene, gpu, fairy.DeviceGeneration()) {
		return
	}
	ctx, read, averagedOutline := v.ctx, v.read, mesh.averagedOutline
	go func() {
		var buffers viewerMeshGPU
		var generation uint64
		var blend []uint16
		var blendErr error
		decoded, err := readViewerMesh(ctx, read, info, averagedOutline)
		if err == nil && info.BlendURL != "" {
			// Bone weights that cannot be read leave the mesh unposed rather than holding back its new geometry.
			var data []byte
			if data, blendErr = read(ctx, info.BlendURL); blendErr == nil {
				blend, blendErr = decodeMotionBlend(info.ID, data, decoded.vertexCount)
			}
		}
		if err == nil {
			buffers, generation, err = uploadViewerMesh(decoded.upload())
		}
		fairy.Go(func() {
			if v.liveWanted[i] != info.GeometryURL || !v.liveUploadReady(scene, gpu, fairy.DeviceGeneration()) ||
				err == nil && gpu.generation != generation {
				buffers.release()
				return
			}
			delete(v.liveWanted, i)
			if err != nil {
				if ctx.Err() == nil {
					v.log.Warn(fmt.Sprintf("Model viewer could not show live mesh %s: %v", info.ID, err),
						"ModelViewer.loadLiveMesh")
				}
				return
			}
			mesh := scene.meshes[i]
			old := scene.gpu.meshes[i]
			if !mesh.unloaded {
				scene.forgetMesh(i)
			}
			scene.gpu.meshes[i] = buffers
			coverageChanged := (mesh.info.IndexCount > 0) != (info.IndexCount > 0)
			mesh.info, mesh.failed = info, false
			mesh.variants, mesh.unreadable = map[int]*viewerPositionGeometry{}, nil
			mesh.fill(decoded)
			v.dropCPU(mesh)
			unloaded := scene.storeMesh(i)
			_ = fairy.UseDevice(func(fairy.GPU) error {
				old.release()
				for _, j := range unloaded {
					scene.gpu.meshes[j].release()
				}
				return nil
			})
			if info.BlendURL != "" {
				v.reportLiveBlend(blendErr, "ModelViewer.loadLiveMesh", "read-blend", info)
			}
			if m := v.activeMotion(); m != nil {
				v.applyMotionBlend(scene, m, i, info, decoded.positions, blend)
			}
			// The original model's match reads the new positions, so it runs after the swap.
			if coverageChanged {
				v.rematchOriginal(scene)
			}
			v.revision++
			v.present()
		})
	}()
}

// A GPU set replaced by recovery cannot accept an upload, even if both were made on the same device.
func (v *ModelViewer) liveUploadReady(scene *viewerScene, gpu *viewerGPUSet, generation uint64) bool {
	return v.scene == scene && !v.uploading && v.uploadErr == "" && gpu.matchesGeneration(generation) &&
		scene.gpu == gpu
}

func (v *ModelViewer) resumeLiveUploads(
	scene *viewerScene,
	recoveryTextures map[string]modelviewer.ModelViewerTextureTransport,
) {
	for i := range v.liveWanted {
		v.loadLiveMesh(scene, i, scene.transport.Meshes[i])
	}
	for key, url := range v.liveTextureWanted {
		if _, uploaded := scene.gpu.textures[key]; uploaded && recoveryTextures[key].URL == url {
			delete(v.liveTextureWanted, key)
			continue
		}
		v.loadLiveTexture(scene, key, scene.transport.Textures[key])
	}
}

// applyMotionBlend poses scene mesh i with bone weights blend through its palette. When blend weights groups the
// palette has not mapped to more vertices than MapPalettes has seen for them, the mesh draws unposed while they are
// mapped onto the skeleton off the UI thread.
func (v *ModelViewer) applyMotionBlend(
	scene *viewerScene,
	m *viewerMotion,
	i int,
	info modelviewer.ModelViewerMeshTransport,
	positions []float32,
	blend []uint16,
) {
	m.cancelBlendLoad(i)
	palette := info.BlendPalette
	if palette == "" || len(blend) == 0 || m.motion == nil || m.motion.NodeCount() == 0 ||
		!paletteNeedsMapping(blend, m.palettes[palette], m.paletteEvidence[palette]) {
		m.replaceBlend(i, palette, positions, blend)
		return
	}

	m.replaceBlend(i, "", nil, nil)
	ctx, post, motion := m.beginBlendLoad(v.ctx, i), v.poster(), m.motion
	go func() {
		palettes, err := motion.MapPalettes(ctx, []modelviewer.MotionPalette{{Positions: positions, Blend: blend}})
		post(func() {
			// A cancelled mapping records nothing, so the next update maps again.
			if ctx.Err() != nil {
				return
			}
			m.cancelBlendLoad(i)
			if !v.motionBlendCurrent(scene, m, i, info) {
				return
			}
			v.reportLiveBlend(err, "ModelViewer.applyMotionBlend", "map-palette", info)
			if err == nil && len(palettes) > 0 {
				if m.palettes == nil {
					m.palettes = map[string][]int32{}
				}
				m.palettes[palette] = mergeMotionPalette(m.palettes[palette], palettes[0])
			}
			m.recordMappingEvidence(palette, blend)
			m.replaceBlend(i, palette, positions, blend)
			v.revision++
		})
	}()
}

// reloadMotionBlend reads the bone weights scene mesh i now names, and its base positions when the mesh holds none
// on the CPU, then poses the mesh with them.
func (v *ModelViewer) reloadMotionBlend(scene *viewerScene, m *viewerMotion, i int) {
	m.cancelBlendLoad(i)
	mesh := scene.meshes[i]
	info := mesh.info
	if info.BlendURL == "" || info.BlendPalette == "" {
		return
	}
	var held []float32
	if !mesh.unloaded && !mesh.cpuDropped {
		held = mesh.positions
	}
	if v.meshLoads == nil {
		v.meshLoads = make(chan struct{}, viewerLoadConcurrency)
	}
	ctx, loads, read, post, averaged := m.beginBlendLoad(v.ctx, i), v.meshLoads, v.read, v.poster(),
		mesh.averagedOutline
	go func() {
		positions := held
		var err error
		if positions == nil {
			select {
			case loads <- struct{}{}:
				var decoded *viewerMesh
				if decoded, err = readViewerMesh(ctx, read, info, averaged); err == nil {
					positions = decoded.positions
				}
				<-loads
			case <-ctx.Done():
				err = ctx.Err()
			}
		}
		var blend []uint16
		if err == nil {
			var data []byte
			if data, err = read(ctx, info.BlendURL); err == nil {
				blend, err = decodeMotionBlend(info.ID, data, len(positions)/3)
			}
		}
		post(func() {
			if ctx.Err() != nil {
				return
			}
			m.cancelBlendLoad(i)
			if !v.motionBlendCurrent(scene, m, i, info) {
				return
			}
			v.reportLiveBlend(err, "ModelViewer.reloadMotionBlend", "reload-blend", info)
			if err != nil {
				return
			}
			v.applyMotionBlend(scene, m, i, info, positions, blend)
			v.revision++
		})
	}()
}

// reportLiveBlend logs bone weights of a Blender live mesh that could not be read or mapped at stage, once per
// episode of the same failure for the mesh. A nil err ends the mesh's episode.
func (v *ModelViewer) reportLiveBlend(err error, where, stage string, info modelviewer.ModelViewerMeshTransport) {
	throttle := v.liveBlendFailures[info.ID]
	if throttle == nil {
		if err == nil {
			return
		}
		throttle = &infra.DiagnosticThrottle{}
		if v.liveBlendFailures == nil {
			v.liveBlendFailures = map[string]*infra.DiagnosticThrottle{}
		}
		v.liveBlendFailures[info.ID] = throttle
	}
	throttle.Report(v.log, err, where, infra.Diagnostic{
		Severity:  infra.DiagnosticWarn,
		Operation: "live-motion-blend",
		Stage:     stage,
		Fields: map[string]any{
			"path": info.ID, "modPath": v.path, "blendURL": infra.SanitizeLogURL(info.BlendURL),
		},
	})
}

// motionBlendCurrent reports whether motion m still plays on scene and scene mesh i still has the geometry and bone
// weights info names.
func (v *ModelViewer) motionBlendCurrent(
	scene *viewerScene,
	m *viewerMotion,
	i int,
	info modelviewer.ModelViewerMeshTransport,
) bool {
	if v.closed || v.scene != scene || v.activeMotion() != m || i >= len(scene.meshes) {
		return false
	}
	now := scene.meshes[i].info
	return now.GeometryURL == info.GeometryURL && now.BlendURL == info.BlendURL &&
		now.BlendPalette == info.BlendPalette
}

// paletteNeedsMapping reports whether blend weights a group that mapping maps to no node to more vertices than
// evidence records MapPalettes has seen for it.
func paletteNeedsMapping(blend []uint16, mapping, evidence []int32) bool {
	for group, count := range motionGroupVertices(blend) {
		if count == 0 || group < len(mapping) && mapping[group] >= 0 {
			continue
		}
		if group >= len(evidence) || count > evidence[group] {
			return true
		}
	}
	return false
}

// mergeMotionPalette keeps the groups existing maps and takes the rest from mapped.
func mergeMotionPalette(existing, mapped []int32) []int32 {
	out := make([]int32, max(len(existing), len(mapped)))
	for i := range out {
		out[i] = -1
		if i < len(existing) && existing[i] >= 0 {
			out[i] = existing[i]
		} else if i < len(mapped) && mapped[i] >= 0 {
			out[i] = mapped[i]
		}
	}
	return out
}

// replaceBlend poses scene mesh i with new bone weights through the palette mapping, or clears its skinning state
// when no weights remain, then updates anchorNodes and bounds.
func (m *viewerMotion) replaceBlend(i int, palette string, positions []float32, blend []uint16) {
	m.meshes = slices.DeleteFunc(m.meshes, func(mesh viewerMotionMesh) bool { return mesh.index == i })
	if i >= 0 && i < len(m.anchorNodes) {
		m.anchorNodes[i] = -1
	}

	mapping := m.palettes[palette]
	vertices := len(blend) / 8
	if palette != "" && len(mapping) > 0 && vertices > 0 && len(blend) == vertices*8 {
		nodes := len(m.bones)
		if m.motion != nil {
			nodes = m.motion.NodeCount()
		}
		totals := make([]float64, nodes)
		boxes := buildMotionMeshBoxes(nodes, totals, positions, blend, mapping)
		m.meshes = append(m.meshes, viewerMotionMesh{
			index: i, vertices: vertices, blend: packMotionBlend(blend, mapping), boxes: boxes,
		})
		if i >= 0 && i < len(m.anchorNodes) && len(m.parents) == nodes {
			m.anchorNodes[i] = anchorNode(totals, m.parents)
		}
	}
	m.rebuildBoxes()

	if m.gpu != nil {
		_ = fairy.UseDevice(func(fairy.GPU) error {
			m.gpu.release()
			return nil
		})
		m.gpu = nil
	}
}
