package ui

import (
	"encoding/binary"
	"fmt"
	"slices"

	"github.com/myparsleycat/fairy"

	"nahida.live/desktop/internal/blenderlink"
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
		if mesh.unloaded {
			coverageChanged := (mesh.info.IndexCount > 0) != (info.IndexCount > 0)
			mesh.info, mesh.failed = info, false
			mesh.variants, mesh.unreadable = map[int]*viewerPositionGeometry{}, nil
			if coverageChanged {
				v.rematchOriginal(scene)
			}
			continue
		}
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
	ctx, read, log, normalXY := v.ctx, v.read, v.log, scene.transport.NormalXYReconstruct
	entries := map[string]modelviewer.ModelViewerTextureTransport{key: entry}
	go func() {
		textures, generations, err := uploadViewerTextures(ctx, read, log, entries, normalXY)
		fairy.Go(func() {
			texture, uploaded := textures[key]
			current := v.scene == scene && v.liveTextureWanted[key] == entry.URL
			if !current || !uploaded || len(generations) != 1 || scene.gpu == nil ||
				!generations[scene.gpu.generation] ||
				v.uploading {
				if uploaded {
					_ = fairy.UseDevice(func(fairy.GPU) error {
						texture.view.Release()
						return nil
					})
				}
				if current && err != nil && ctx.Err() == nil {
					v.log.Warn(fmt.Sprintf("Model viewer could not show live texture %s: %v", key, err),
						"ModelViewer.loadLiveTexture")
				}
				return
			}
			delete(v.liveTextureWanted, key)
			old, replaced := scene.gpu.textures[key]
			scene.gpu.textures[key] = texture
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

// loadLiveMesh reads and uploads the new geometry of loaded mesh i, then swaps it in, unless a newer one was
// asked for meanwhile.
func (v *ModelViewer) loadLiveMesh(scene *viewerScene, i int, info modelviewer.ModelViewerMeshTransport) {
	if v.liveWanted == nil {
		v.liveWanted = map[int]string{}
	}
	v.liveWanted[i] = info.GeometryURL
	ctx, read, averagedOutline := v.ctx, v.read, scene.meshes[i].averagedOutline
	go func() {
		var buffers viewerMeshGPU
		var generation uint64
		var blend []uint16
		decoded, err := readViewerMesh(ctx, read, info, averagedOutline)
		if err == nil && info.BlendURL != "" {
			var data []byte
			if data, err = read(ctx, info.BlendURL); err == nil {
				blend = make([]uint16, len(data)/2)
				for k := range blend {
					blend[k] = binary.LittleEndian.Uint16(data[k*2:])
				}
			}
		}
		if err == nil {
			buffers, generation, err = uploadViewerMesh(decoded.upload())
		}
		fairy.Go(func() {
			current := v.scene == scene && v.liveWanted[i] == info.GeometryURL
			if !current || err != nil || scene.gpu == nil || scene.gpu.generation != generation || v.uploading {
				releaseViewerMeshGPU(&buffers)
				if current && err != nil && ctx.Err() == nil {
					v.log.Warn(fmt.Sprintf("Model viewer could not show live mesh %s: %v", info.ID, err),
						"ModelViewer.loadLiveMesh")
				}
				return
			}
			delete(v.liveWanted, i)
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
			if m := v.activeMotion(); m != nil && blend != nil {
				m.replaceBlend(i, info.BlendPalette, blend)
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

// replaceBlend poses scene mesh i with new bone weights through the palette mapping made when the motion
// loaded. A palette the motion did not map leaves the mesh unposed.
func (m *viewerMotion) replaceBlend(i int, palette string, blend []uint16) {
	mapping, ok := m.palettes[palette]
	m.meshes = slices.DeleteFunc(m.meshes, func(mesh viewerMotionMesh) bool { return mesh.index == i })
	if ok {
		m.meshes = append(m.meshes, viewerMotionMesh{
			index: i, vertices: len(blend) / 8, blend: packMotionBlend(blend, mapping),
		})
	}
	if m.gpu != nil {
		_ = fairy.UseDevice(func(fairy.GPU) error {
			m.gpu.release()
			return nil
		})
		m.gpu = nil
	}
}
