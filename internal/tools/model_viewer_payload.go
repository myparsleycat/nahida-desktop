package tools

import "context"

func prepareModelViewerPayload(ctx context.Context, prepared *modelViewerPreparedGeometry, transport *ModelViewerTransport) ([]modelViewerMeshPayload, map[string]modelViewerTexturePayload, modelViewerTextureRunStats, error) {
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
			item, payload := buildModelViewerDirectMeshPayload(mesh, work.bindings, textures, work.shapes, prepared.cache)
			transport.Meshes = append(transport.Meshes, item)
			meshPayloads = append(meshPayloads, payload)
		}
	}
	return meshPayloads, texturePayloads, textureStats, ctx.Err()
}
