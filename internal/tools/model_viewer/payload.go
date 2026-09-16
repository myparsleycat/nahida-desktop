package modelviewer

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"sync"
)

func prepareModelViewerPayload(
	ctx context.Context,
	prepared *modelViewerPreparedGeometry,
	transport *ModelViewerTransport,
) ([]modelViewerMeshPayload, map[string]modelViewerTexturePayload, modelViewerTextureRunStats, error) {
	settings := modelViewerTextureSettings{TextureFormat: "jpeg-safe", JPEGQuality: 85}
	meshPayloads := make([]modelViewerMeshPayload, 0)
	texturePayloads := make(map[string]modelViewerTexturePayload)
	textureJobs := make([]modelViewerTextureJob, 0)
	for _, work := range prepared.textures {
		textureJobs = append(textureJobs, work.jobs...)
	}
	transport.MaterialProfile = detectModelViewerMaterialProfile(prepared.sections)
	settings.MaterialProfile = transport.MaterialProfile
	texturesByBatch, textureStats, err := runModelViewerTextureJobs(ctx, settings, len(prepared.textures), textureJobs)

	if err != nil {
		return nil, nil, textureStats, err
	}
	for batchIndex, work := range prepared.textures {
		if err := ctx.Err(); err != nil {
			return nil, nil, textureStats, err
		}
		textures := texturesByBatch[batchIndex]
		for _, value := range textures {
			if value.Key != "" {
				texturePayloads[value.Key] = value
			}
		}
		for _, mesh := range work.meshes {
			if err := ctx.Err(); err != nil {
				return nil, nil, textureStats, err
			}
			item, payload := buildModelViewerDirectMeshPayload(
				mesh,
				work.bindings,
				textures,
				work.shapes,
				prepared.cache,
			)
			transport.Meshes = append(transport.Meshes, item)
			meshPayloads = append(meshPayloads, payload)
		}
	}
	return meshPayloads, texturePayloads, textureStats, ctx.Err()
}

type modelViewerTexturePayload struct {
	Key      string
	Role     string
	Bytes    []byte
	MIMEType string
}

type modelViewerMeshPayload struct {
	Positions         []float32
	Normals           []float32
	Tangents          []float32
	UVs               []float32
	Indices           []uint32
	ShapePositions    [][]float32
	ShapeLowPositions [][]float32
	SourceIndices     []uint32
	PositionSources   []modelViewerDirectPositionAssignment
}

func writeModelViewerPayload(
	ctx context.Context,
	t *Service,
	sessionID string,
	transport *ModelViewerTransport,
	meshes []modelViewerMeshPayload,
	textures map[string]modelViewerTexturePayload,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if t == nil || t.protocol == nil || transport == nil {
		return fmt.Errorf("protocol service is unavailable")
	}
	textureKeys := make([]string, 0, len(textures))
	for key := range textures {
		textureKeys = append(textureKeys, key)
	}
	sort.Strings(textureKeys)
	for _, key := range textureKeys {
		if err := ctx.Err(); err != nil {
			return err
		}
		texture := textures[key]
		url, err := t.protocol.StoreMemoryBuffer(sessionID, "tex:"+key, texture.Bytes, texture.MIMEType)
		if err != nil {
			return err
		}
		transport.Textures[key] = ModelViewerTextureTransport{URL: url, Role: texture.Role}
	}
	positionCache := &modelViewerPositionCache{limit: modelViewerPositionCacheBytes}
	if err := t.prepareModelViewerComputeSources(ctx, sessionID, transport, meshes, positionCache); err != nil {
		return err
	}

	if len(meshes) != len(transport.Meshes) {
		return fmt.Errorf("model viewer payload mesh count mismatch")
	}
	writeMesh := func(mesh *ModelViewerMeshTransport, payload modelViewerMeshPayload) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		write := func(suffix string, data []byte) (string, error) {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			return t.protocol.StoreMemoryBuffer(sessionID, mesh.ID+suffix, data, "application/octet-stream")
		}
		var err error
		mesh.Bounds, err = modelViewerGeometryBounds(ctx, payload.Positions)
		if err != nil {
			return fmt.Errorf("mesh %s bounds: %w", mesh.ID, err)
		}
		if payload.Normals == nil {
			payload.Normals, err = modelViewerVertexNormals(ctx, payload.Positions, payload.Indices)
			if err != nil {
				return fmt.Errorf("mesh %s normals: %w", mesh.ID, err)
			}
		}
		mesh.GeometryURL, err = write(".geometry", modelViewerMeshBytes(payload))
		if err != nil {
			return err
		}
		if !modelViewerSourceIndicesAreIdentity(payload.SourceIndices) {
			mesh.SourceIndicesURL, err = write(".source-idx", modelViewerUint32Bytes(payload.SourceIndices))
			if err != nil {
				return err
			}
		}
		if len(payload.ShapePositions) != len(mesh.ShapeTargets) ||
			len(payload.ShapeLowPositions) != len(mesh.ShapeTargets) {
			return fmt.Errorf("model viewer shape payload count mismatch for %s", mesh.ID)
		}
		for targetIndex := range mesh.ShapeTargets {
			target := &mesh.ShapeTargets[targetIndex]
			target.PositionsURL, err = write(
				fmt.Sprintf(".shape.%d", targetIndex),
				modelViewerFloat32Bytes(payload.ShapePositions[targetIndex]),
			)
			if err != nil {
				return err
			}
			target.LowPositionsURL, err = write(
				fmt.Sprintf(".shape.%d.low", targetIndex),
				modelViewerFloat32Bytes(payload.ShapeLowPositions[targetIndex]),
			)
			if err != nil {
				return err
			}
		}
		if len(payload.PositionSources) != len(mesh.PositionVariants) {
			return fmt.Errorf("model viewer position variant payload count mismatch for %s", mesh.ID)
		}
		for variantIndex := range mesh.PositionVariants {
			mesh.PositionVariants[variantIndex].GeometryURL, err = t.registerModelViewerPosition(
				sessionID,
				mesh.ID,
				variantIndex,
				payload.PositionSources[variantIndex],
				payload.Indices,
				payload.SourceIndices,
				len(payload.Positions)/3,
				positionCache,
			)
			if err != nil {
				return err
			}
		}
		return nil
	}
	meshErrors := make([]error, len(transport.Meshes))
	if workers := min(len(transport.Meshes), runtime.GOMAXPROCS(0)); workers > 1 {
		work := make(chan int)
		var workerGroup sync.WaitGroup
		for range workers {
			workerGroup.Add(1)
			go func() {
				defer workerGroup.Done()
				for meshIndex := range work {
					meshErrors[meshIndex] = writeMesh(&transport.Meshes[meshIndex], meshes[meshIndex])
				}
			}()
		}
		for meshIndex := range transport.Meshes {
			work <- meshIndex
		}
		close(work)
		workerGroup.Wait()
	} else {
		for meshIndex := range transport.Meshes {
			meshErrors[meshIndex] = writeMesh(&transport.Meshes[meshIndex], meshes[meshIndex])
		}
	}
	for _, err := range meshErrors {
		if err != nil {
			return err
		}
	}
	return nil
}
