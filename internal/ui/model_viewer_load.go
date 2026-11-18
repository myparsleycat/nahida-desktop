package ui

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/myparsleycat/fairy"
	"golang.org/x/sync/errgroup"

	"nahida.live/desktop/internal/infra"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

// viewerLoadConcurrency overlaps transfers and decoding without starting every request at once, as the React
// viewer's MODEL_PAYLOAD_LOAD_CONCURRENCY did.
const viewerLoadConcurrency = 8

type viewerReader func(context.Context, string) ([]byte, error)

// buildViewerScene reads the meshes the initial state shows and those a compute deformer reads, and evaluates
// that state, including the position variants it selects. The other meshes stay unloaded until a state shows
// them. It does not touch the GPU.
func buildViewerScene(
	ctx context.Context,
	read viewerReader,
	transport modelviewer.ModelViewerTransport,
	states []modelviewer.ViewerMeshState,
) (*viewerScene, error) {
	scene := &viewerScene{transport: transport, meshes: make([]*viewerMesh, len(transport.Meshes))}
	scene.world, scene.minimum, scene.maximum = viewerWorld(transport)
	averagedOutline := averagedOutlineProfiles[string(transport.MaterialProfile)]
	deformed := map[string]bool{}
	for _, deformer := range transport.ComputeDeformers {
		for _, id := range deformer.MeshIDs {
			deformed[id] = true
		}
	}
	for i, info := range transport.Meshes {
		scene.meshes[i] = newViewerStub(info, averagedOutline)
		scene.meshes[i].pinned = deformed[info.ID]
	}

	decoded := make([]*viewerMesh, len(scene.meshes))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(viewerLoadConcurrency)
	for i, mesh := range scene.meshes {
		if !mesh.pinned && (i >= len(states) || !states[i].Visible) {
			continue
		}
		group.Go(func() error {
			var err error
			if decoded[i], err = readViewerMesh(groupCtx, read, mesh.info, averagedOutline); err != nil {
				return fmt.Errorf("mesh %s: %w", mesh.info.ID, err)
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	for i, mesh := range decoded {
		if mesh != nil {
			scene.meshes[i].fill(mesh)
		}
	}
	scene.trackLoaded()

	missing := scene.apply(states)
	if missing.empty() {
		return scene, nil
	}
	variants := make([]*viewerPositionGeometry, len(scene.meshes))
	group, groupCtx = errgroup.WithContext(ctx)
	group.SetLimit(viewerLoadConcurrency)
	for i, index := range missing.variants {
		mesh := scene.meshes[i]
		if mesh.unloaded {
			continue
		}
		info, base := mesh.info, mesh.variantBase()
		group.Go(func() error {
			variant, err := readViewerVariant(groupCtx, read, info, base, index)
			if err != nil {
				return fmt.Errorf("mesh %s position variant %d: %w", scene.meshes[i].info.ID, index, err)
			}
			variants[i] = variant
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	for i, index := range missing.variants {
		if variants[i] != nil {
			scene.storeVariant(i, index, variants[i])
		}
	}
	scene.apply(states)
	return scene, nil
}

func readViewerMesh(
	ctx context.Context,
	read viewerReader,
	info modelviewer.ModelViewerMeshTransport,
	averagedOutline bool,
) (*viewerMesh, error) {
	data, err := read(ctx, info.GeometryURL)
	if err != nil {
		return nil, err
	}
	mesh, err := decodeViewerMesh(data, averagedOutline)
	if err != nil {
		return nil, err
	}
	mesh.info = info
	for _, target := range info.ShapeTargets {
		data, err := read(ctx, target.PositionsURL)
		if err != nil {
			return nil, fmt.Errorf("shape target %s: %w", target.Var, err)
		}
		shape := viewerShapeTarget{positions: float32s(data), mode: string(target.Mode)}
		if target.LowPositionsURL != "" {
			if data, err = read(ctx, target.LowPositionsURL); err != nil {
				return nil, fmt.Errorf("shape target %s: %w", target.Var, err)
			}
			shape.lowPositions = float32s(data)
		}
		mesh.shapes = append(mesh.shapes, shape)
	}
	return mesh, nil
}

// viewerMeshUpload is what the GPU copy of a mesh is made from, taken on the UI thread.
type viewerMeshUpload struct {
	current []float32
	uvs     []float32
	extra   []byte
	indices []uint32
}

func (m *viewerMesh) upload() viewerMeshUpload {
	return viewerMeshUpload{current: m.current, uvs: m.uvs, extra: m.extra, indices: m.indices}
}

// viewerMeshUploads takes the uploads of a scene's meshes. An unloaded mesh, or one that dropped its arrays, gives
// an empty upload and no GPU buffers.
func viewerMeshUploads(scene *viewerScene) []viewerMeshUpload {
	uploads := make([]viewerMeshUpload, len(scene.meshes))
	for i, mesh := range scene.meshes {
		uploads[i] = mesh.upload()
	}
	return uploads
}

// uploadViewerMesh makes the GPU buffers of one mesh and reports the device generation they belong to.
func uploadViewerMesh(upload viewerMeshUpload) (viewerMeshGPU, uint64, error) {
	var buffers viewerMeshGPU
	var generation uint64
	err := fairy.UseDevice(func(g fairy.GPU) error {
		generation = g.Generation
		var err error
		buffers, err = newViewerMeshGPU(g.Device, upload)
		return err
	})
	return buffers, generation, err
}

// uploadViewerScene makes the GPU resources of a scene. Textures load through read and are released from
// memory once uploaded; one that fails is logged and left out, so its meshes draw untextured. Each upload
// holds the device lock only for itself, so other windows keep painting.
func uploadViewerScene(
	ctx context.Context,
	read viewerReader,
	log *infra.Log,
	transport modelviewer.ModelViewerTransport,
	meshes []viewerMeshUpload,
) (*viewerGPUSet, error) {
	set := &viewerGPUSet{meshes: make([]viewerMeshGPU, len(meshes)), textures: map[string]viewerTextureGPU{}}
	var mu sync.Mutex
	generations := map[uint64]bool{}
	record := func(generation uint64) {
		mu.Lock()
		generations[generation] = true
		mu.Unlock()
	}
	fail := func(err error) (*viewerGPUSet, error) {
		set.release()
		return nil, err
	}

	for i, mesh := range meshes {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		err := fairy.UseDevice(func(g fairy.GPU) error {
			record(g.Generation)
			var err error
			set.meshes[i], err = newViewerMeshGPU(g.Device, mesh)
			return err
		})
		if err != nil {
			return fail(fmt.Errorf("upload mesh %s: %w", transport.Meshes[i].ID, err))
		}
	}

	// The original model loads the face textures with itself, since only its face shows them.
	entries := maps.Clone(transport.Textures)
	faceOnly := viewerFaceOnlyTextures(&transport)
	maps.DeleteFunc(entries, func(key string, _ modelviewer.ModelViewerTextureTransport) bool { return faceOnly[key] })
	textures, textureGenerations, err := uploadViewerTextures(ctx, read, log, entries, transport.NormalXYReconstruct)
	if err != nil {
		return fail(err)
	}
	set.textures = textures
	maps.Copy(generations, textureGenerations)
	// A set that spans a lost device matches no generation, so the viewer uploads it again.
	switch len(generations) {
	case 0:
		set.generation = fairy.DeviceGeneration()
	case 1:
		for generation := range generations {
			set.generation = generation
		}
	}
	return set, nil
}

// uploadViewerTextures loads entries through read and uploads them. One that fails is logged and left out; the upload
// fails only when cancelled or the device cannot be used, and then releases what it uploaded.
func uploadViewerTextures(
	ctx context.Context,
	read viewerReader,
	log *infra.Log,
	entries map[string]modelviewer.ModelViewerTextureTransport,
	normalXYReconstruct bool,
) (map[string]viewerTextureGPU, map[uint64]bool, error) {
	textures, generations := make(map[string]viewerTextureGPU, len(entries)), map[uint64]bool{}
	var mu sync.Mutex
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(viewerLoadConcurrency)
	for _, key := range slices.Sorted(maps.Keys(entries)) {
		entry := entries[key]
		group.Go(func() error {
			data, err := loadViewerTexture(groupCtx, read, entry, normalXYReconstruct)
			if err != nil {
				if groupCtx.Err() != nil {
					return groupCtx.Err()
				}
				log.Warn(fmt.Sprintf("Model viewer texture %q (%s, %s) could not be loaded: %v",
					key, entry.Role, entry.Format, err), "ModelViewer.loadTexture")
				return nil
			}
			return fairy.UseDevice(func(g fairy.GPU) error {
				mu.Lock()
				generations[g.Generation] = true
				mu.Unlock()
				view, err := data.upload(g.Device, g.Context)
				if err != nil {
					log.Warn(fmt.Sprintf("Model viewer texture %q could not be uploaded: %v", key, err),
						"ModelViewer.loadTexture")
					return nil
				}
				mu.Lock()
				textures[key] = viewerTextureGPU{
					view: view, flags: data.flags, normalMode: data.normalMode, format: data.sourceFormat,
					outlineMask: data.outlineMask,
				}
				mu.Unlock()
				return nil
			})
		})
	}
	if err := group.Wait(); err != nil {
		_ = fairy.UseDevice(func(fairy.GPU) error {
			for _, texture := range textures {
				texture.view.Release()
			}
			return nil
		})
		return nil, nil, err
	}
	return textures, generations, nil
}
