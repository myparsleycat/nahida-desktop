package modelviewer

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"sort"
	"sync"
	"time"
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
			if mesh.drawSource != nil {
				if prepared.drawSources == nil {
					prepared.drawSources = make(map[string]DrawSource)
				}
				source := *mesh.drawSource
				source.vertexBuffer = mesh.positionFile
				source.sourceFaces = mesh.geometry.sourceFaces
				prepared.drawSources[item.ID] = source
			}
		}
	}
	linkModelViewerDrawSources(transport, prepared.drawSources)
	return meshPayloads, texturePayloads, textureStats, ctx.Err()
}

const modelViewerEffectCardUVArea = 2

type modelViewerTexturePayload struct {
	Key          string
	Role         string
	Bytes        []byte
	MIMEType     string
	Path         string
	ResourceName string
	DDS          *modelViewerDDSMetadata
	InvertAlpha  bool
	CoverageMask bool
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

type modelViewerPayloadOptions struct {
	ddsPreviewMaxDimension uint32
	// Smaller target reached through a stored mip, or else by decoding and
	// scaling the texture into the fallback image.
	ddsPreviewPreferredDimension uint32
	includeAllStateVariables     bool
}

// modelViewerEffectCardTextures returns the textures of meshes whose triangles
// cover the texture at least twice over. An unwrapped surface stays near one
// even with mirrored parts such as eyes, while effect cards reuse the whole map.
func modelViewerEffectCardTextures(
	transportMeshes []ModelViewerMeshTransport,
	meshes []modelViewerMeshPayload,
) map[string]bool {
	result := make(map[string]bool)
	for index := range min(len(transportMeshes), len(meshes)) {
		if modelViewerMeshUVArea(meshes[index]) < modelViewerEffectCardUVArea {
			continue
		}
		mesh := &transportMeshes[index]
		if mesh.TexKey != nil {
			result[*mesh.TexKey] = true
		}
		for _, variant := range mesh.TextureVariants {
			result[variant.TexKey] = true
		}
	}
	return result
}

func modelViewerMeshUVArea(mesh modelViewerMeshPayload) float64 {
	vertices := uint32(len(mesh.UVs) / 2)
	area := 0.0
	for offset := 0; offset+2 < len(mesh.Indices); offset += 3 {
		a, b, c := mesh.Indices[offset], mesh.Indices[offset+1], mesh.Indices[offset+2]
		if a >= vertices || b >= vertices || c >= vertices {
			continue
		}
		edgeU1, edgeV1 := mesh.UVs[b*2]-mesh.UVs[a*2], mesh.UVs[b*2+1]-mesh.UVs[a*2+1]
		edgeU2, edgeV2 := mesh.UVs[c*2]-mesh.UVs[a*2], mesh.UVs[c*2+1]-mesh.UVs[a*2+1]
		area += math.Abs(float64(edgeU1*edgeV2-edgeU2*edgeV1)) / 2
	}
	return area
}

func writeModelViewerPayload(
	ctx context.Context,
	t *Service,
	sessionID string,
	transport *ModelViewerTransport,
	meshes []modelViewerMeshPayload,
	textures map[string]modelViewerTexturePayload,
	options modelViewerPayloadOptions,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	textureKeys := make([]string, 0, len(textures))
	for key := range textures {
		textureKeys = append(textureKeys, key)
	}
	sort.Strings(textureKeys)
	effectCardTextures := modelViewerEffectCardTextures(transport.Meshes, meshes)
	for _, key := range textureKeys {
		if err := ctx.Err(); err != nil {
			return err
		}
		texture := textures[key]
		if texture.DDS != nil {
			materialProfile := transport.MaterialProfile
			directURL := t.protocol.LocalFileURL(texture.Path, true)
			directMetadata := *texture.DDS
			previewLimit, resample := modelViewerDDSPreviewLimit(*texture.DDS, options)
			if plan, needed := modelViewerDDSPreviewPlanFor(
				*texture.DDS,
				previewLimit,
			); needed && texture.DDS.Format != "" && !resample {
				previewURL, previewErr := t.protocol.StoreMemoryLoader(
					sessionID,
					"tex-dds-preview:"+key,
					func(loadCtx context.Context) ([]byte, error) {
						startedAt := time.Now()
						data, loadErr := prepareModelViewerDDSPreviewWithLimit(
							loadCtx,
							texture.Path,
							*texture.DDS,
							previewLimit,
						)
						if loadErr != nil {
							t.log.Warn(
								fmt.Sprintf(
									"DDS preview failed path=%q role=%q format=%q elapsed=%dms error=%v",
									texture.Path,
									texture.Role,
									texture.DDS.Format,
									time.Since(startedAt).Milliseconds(),
									loadErr,
								),
								"StaticGlb.loadForViewer",
							)
							return nil, fmt.Errorf(
								"prepare DDS preview path=%q role=%q format=%q: %w",
								texture.Path,
								texture.Role,
								texture.DDS.Format,
								loadErr,
							)
						}
						t.log.Info(
							fmt.Sprintf(
								"DDS preview prepared path=%q role=%q format=%q dimensions=%dx%d elapsed=%dms",
								texture.Path,
								texture.Role,
								texture.DDS.Format,
								plan.width,
								plan.height,
								time.Since(startedAt).Milliseconds(),
							),
							"StaticGlb.loadForViewer",
						)
						return data, nil
					},
				)
				if previewErr != nil {
					return previewErr
				}
				directURL = previewURL
				directMetadata.Width = plan.width
				directMetadata.Height = plan.height
				directMetadata.MipCount = 1
			}
			fallbackURL, err := t.protocol.StoreMemoryLoaderWithContentType(
				sessionID,
				"tex-fallback:"+key,
				"image/png",
				func(loadCtx context.Context) ([]byte, error) {
					startedAt := time.Now()
					data, loadErr := prepareModelViewerDDSFallback(
						loadCtx,
						texture.Path,
						options.ddsPreviewPreferredDimension,
					)
					if loadErr != nil {
						t.log.Warn(
							fmt.Sprintf(
								"DDS fallback failed path=%q role=%q format=%q materialProfile=%q elapsed=%dms error=%v",
								texture.Path,
								texture.Role,
								texture.DDS.Format,
								materialProfile,
								time.Since(startedAt).Milliseconds(),
								loadErr,
							),
							"StaticGlb.loadForViewer",
						)
						return nil, fmt.Errorf(
							"prepare DDS fallback path=%q role=%q format=%q: %w",
							texture.Path,
							texture.Role,
							texture.DDS.Format,
							loadErr,
						)
					}
					t.log.Info(
						fmt.Sprintf(
							"DDS fallback prepared path=%q role=%q format=%q materialProfile=%q elapsed=%dms",
							texture.Path,
							texture.Role,
							texture.DDS.Format,
							materialProfile,
							time.Since(startedAt).Milliseconds(),
						),
						"StaticGlb.loadForViewer",
					)
					return data, nil
				},
			)
			if err != nil {
				return err
			}
			if resample {
				// Without dimensions the renderer skips the compressed upload and
				// loads the scaled fallback, while Format still drives its shader.
				directURL = fallbackURL
				directMetadata.Width, directMetadata.Height, directMetadata.MipCount = 0, 0, 0
			}
			transport.Textures[key] = ModelViewerTextureTransport{
				URL:          directURL,
				FallbackURL:  fallbackURL,
				Role:         texture.Role,
				Encoding:     "dds",
				Format:       directMetadata.Format,
				Width:        directMetadata.Width,
				Height:       directMetadata.Height,
				MipCount:     directMetadata.MipCount,
				InvertAlpha:  texture.InvertAlpha,
				CoverageMask: texture.CoverageMask && effectCardTextures[key],
			}
			continue
		}
		url, err := t.protocol.StoreMemoryBuffer(sessionID, "tex:"+key, texture.Bytes, texture.MIMEType)
		if err != nil {
			return err
		}
		transport.Textures[key] = ModelViewerTextureTransport{URL: url, Role: texture.Role, Encoding: "image"}
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
