package modelviewer

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type modelViewerTextureSettings struct {
	TextureFormat   string
	JPEGQuality     int
	MaterialProfile string
}

const modelViewerTextureConcurrency = 8

type modelViewerINITextureWork struct {
	meshes   []modelViewerDirectMesh
	bindings []modelViewerTextureBinding
	shapes   []modelViewerShapeKey
	jobs     []modelViewerTextureJob
}

type modelViewerTextureJob struct {
	batchIndex   int
	path         string
	resourceName string
	keys         []string
	role         string
	canonicalKey string
}

type modelViewerTextureRunStats struct {
	Jobs            int
	UniquePaths     int
	UniqueContents  int
	LogicalTextures int
	Decodes         int
	Encodes         int
	HashBytes       int64
	HashWallMs      int64
	PrepareWallMs   int64
	TotalWallMs     int64
	DirectDDS       int
	PreparedImages  int
}

func collectModelViewerTextureJobs(
	batchIndex int,
	modDir string,
	resources []modelViewerResource,
	bindings []modelViewerTextureBinding,
	meshes []modelViewerDirectMesh,
) []modelViewerTextureJob {
	resourceMap := make(map[string]modelViewerResource)
	for _, resource := range resources {
		key := modelViewerNormalizeKey(resource.Name)
		resourceMap[key] = resource
	}
	var resourceNames []string
	bindingRoles := make(map[string]string)
	for _, binding := range bindings {
		for _, name := range appendUniqueModelViewer(binding.TextureResourceNames, binding.DiffuseResourceName) {
			resourceNames = appendUniqueModelViewer(resourceNames, name)
		}
		for key, role := range binding.TextureRoles {
			bindingRoles[key] = role
		}
	}
	for _, mesh := range meshes {
		for _, assignment := range mesh.textureAssignments {
			resourceNames = appendUniqueModelViewer(resourceNames, assignment.resource)
		}
	}
	jobs := make([]modelViewerTextureJob, 0, len(resourceNames))
	seen := make(map[string]bool)
	for _, name := range resourceNames {
		resourceKey := modelViewerNormalizeKey(name)
		if seen[resourceKey] {
			continue
		}
		seen[resourceKey] = true
		resource, ok := resourceMap[resourceKey]
		if !ok || resource.Filename == "" {
			continue
		}
		role := bindingRoles[resourceKey]
		if role == "" {
			role = classifyModelViewerTextureRole(name)
		}
		for _, mesh := range meshes {
			for _, assignment := range mesh.textureAssignments {
				if modelViewerNormalizeKey(assignment.resource) == resourceKey && assignment.role != "" {
					role = assignment.role
				}
			}
		}
		fileKey := modelViewerTextureKey(resource.Filename, role)
		texturePath, pathErr := resolveModelViewerResourcePath(modDir, modDir, resource.Filename)
		if pathErr != nil {
			continue
		}
		keys := []string{resourceKey}
		if fileKey != "" {
			keys = append(keys, fileKey)
			seen[fileKey] = true
		}
		jobs = append(jobs, modelViewerTextureJob{
			batchIndex:   batchIndex,
			path:         texturePath,
			resourceName: name,
			keys:         keys,
			role:         role,
			canonicalKey: fileKey,
		})
	}
	for _, mesh := range meshes {
		for _, assignment := range mesh.textureAssignments {
			key := modelViewerAssignmentTextureKey(assignment)
			if key == "" || seen[key] || assignment.file == "" {
				continue
			}
			texturePath, pathErr := resolveModelViewerResourcePath(modDir, modDir, assignment.file)
			if pathErr != nil {
				continue
			}
			seen[key] = true
			jobs = append(jobs, modelViewerTextureJob{
				batchIndex:   batchIndex,
				path:         texturePath,
				resourceName: assignment.resource,
				keys:         []string{key},
				role:         assignment.role,
				canonicalKey: key,
			})
		}
	}
	return jobs
}

func runModelViewerTextureJobs(
	ctx context.Context,
	settings modelViewerTextureSettings,
	batchCount int,
	jobs []modelViewerTextureJob,
) ([]map[string]modelViewerTexturePayload, modelViewerTextureRunStats, error) {
	outputs := make([]map[string]modelViewerTexturePayload, batchCount)
	for index := range outputs {
		outputs[index] = make(map[string]modelViewerTexturePayload)
	}
	stats := modelViewerTextureRunStats{Jobs: len(jobs)}
	if ctx.Err() != nil || len(jobs) == 0 {
		return outputs, stats, ctx.Err()
	}
	startedAt := time.Now()
	imageJobs := make([]modelViewerTextureJob, 0, len(jobs))
	directPaths := make(map[string]modelViewerDDSMetadata)
	failedDirectPaths := make(map[string]bool)
	for _, job := range jobs {
		if !strings.EqualFold(filepath.Ext(job.path), ".dds") {
			imageJobs = append(imageJobs, job)
			continue
		}
		pathKey := strings.ToLower(filepath.Clean(job.path))
		metadata, exists := directPaths[pathKey]
		if !exists && !failedDirectPaths[pathKey] {
			var err error
			metadata, err = inspectModelViewerDDS(job.path)
			if err != nil {
				failedDirectPaths[pathKey] = true
				continue
			}
			directPaths[pathKey] = metadata
		}
		if failedDirectPaths[pathKey] {
			continue
		}
		item := modelViewerTexturePayload{
			Key:          job.canonicalKey,
			Role:         job.role,
			Path:         job.path,
			ResourceName: job.resourceName,
			DDS:          &metadata,
			InvertAlpha:  modelViewerTextureNameRequestsAlphaInvert(job.resourceName),
		}
		for _, key := range job.keys {
			outputs[job.batchIndex][key] = item
		}
	}
	for _, metadata := range directPaths {
		if metadata.Format != "" {
			stats.DirectDDS++
		}
	}
	stats.UniquePaths = len(directPaths) + len(failedDirectPaths)
	if len(imageJobs) == 0 {
		stats.UniqueContents = len(directPaths)
		stats.LogicalTextures = countModelViewerLogicalTextures(outputs)
		stats.TotalWallMs = time.Since(startedAt).Milliseconds()
		return outputs, stats, ctx.Err()
	}
	type pathGroup struct {
		key         string
		path        string
		jobs        []modelViewerTextureJob
		contentKey  string
		hashedBytes int64
	}
	pathIndexes := make(map[string]int, len(imageJobs))
	pathGroups := make([]pathGroup, 0, len(imageJobs))
	for _, job := range imageJobs {
		pathKey := strings.ToLower(filepath.Clean(job.path))
		index, ok := pathIndexes[pathKey]
		if !ok {
			index = len(pathGroups)
			pathIndexes[pathKey] = index
			pathGroups = append(pathGroups, pathGroup{key: pathKey, path: job.path})
		}
		pathGroups[index].jobs = append(pathGroups[index].jobs, job)
	}
	stats.UniquePaths += len(pathGroups)
	hashStartedAt := time.Now()
	hashWork := make(chan int)
	hashWorkers := min(modelViewerTextureConcurrency, runtime.GOMAXPROCS(0), len(pathGroups))
	if hashWorkers < 1 {
		hashWorkers = 1
	}
	var hashGroup sync.WaitGroup
	for range hashWorkers {
		hashGroup.Add(1)
		go func() {
			defer hashGroup.Done()
			for index := range hashWork {
				group := &pathGroups[index]
				if ctx.Err() != nil {
					group.contentKey = "path:" + group.key
					continue
				}
				hash, size, err := modelViewerTextureFileHash(ctx, group.path)
				if err != nil {
					group.contentKey = "path:" + group.key
					continue
				}
				group.contentKey = hash
				group.hashedBytes = size
			}
		}()
	}
hashDispatch:
	for index := range pathGroups {
		select {
		case hashWork <- index:
		case <-ctx.Done():
			break hashDispatch
		}
	}
	close(hashWork)
	hashGroup.Wait()
	if err := ctx.Err(); err != nil {
		return outputs, stats, err
	}
	stats.HashWallMs = time.Since(hashStartedAt).Milliseconds()
	for _, group := range pathGroups {
		stats.HashBytes += group.hashedBytes
	}
	type preparedJob struct {
		job     modelViewerTextureJob
		texture *modelViewerPreparedTexture
	}
	type contentGroup struct {
		path     string
		jobs     []modelViewerTextureJob
		prepared []preparedJob
		decodes  int
		encodes  int
	}
	contentIndexes := make(map[string]int, len(pathGroups))
	contentGroups := make([]contentGroup, 0, len(pathGroups))
	for _, path := range pathGroups {
		index, ok := contentIndexes[path.contentKey]
		if !ok {
			index = len(contentGroups)
			contentIndexes[path.contentKey] = index
			contentGroups = append(contentGroups, contentGroup{path: path.path})
		}
		contentGroups[index].jobs = append(contentGroups[index].jobs, path.jobs...)
	}
	stats.UniqueContents = len(directPaths) + len(contentGroups)
	format := normalizeModelViewerFormat(settings.TextureFormat)
	quality := normalizeJPEGQuality(settings.JPEGQuality)
	type encodeVariant struct {
		invert    bool
		transform modelViewerTextureTransform
		format    string
		quality   int
		profile   string
		role      string
	}
	prepareStartedAt := time.Now()
	prepareWork := make(chan int)
	prepareWorkers := min(modelViewerTextureConcurrency, runtime.GOMAXPROCS(0), len(contentGroups))
	if prepareWorkers < 1 {
		prepareWorkers = 1
	}
	var prepareGroup sync.WaitGroup
	for range prepareWorkers {
		prepareGroup.Add(1)
		go func() {
			defer prepareGroup.Done()
			for index := range prepareWork {
				group := &contentGroups[index]
				if ctx.Err() != nil {
					continue
				}
				group.decodes++
				decoded, err := decodeModelViewerTextureSource(ctx, group.path)
				if err != nil {
					continue
				}
				variants := make(map[encodeVariant]*modelViewerPreparedTexture, 2)
				for _, job := range group.jobs {
					if ctx.Err() != nil {
						break
					}
					variant := encodeVariant{
						invert:    modelViewerTextureShouldInvertAlpha(job.resourceName, decoded),
						transform: modelViewerTextureTransformFor(settings.MaterialProfile, job.role),
						format:    modelViewerTextureFormatFor(settings.MaterialProfile, job.role, format),
						quality:   quality,
						profile:   settings.MaterialProfile,
						role:      job.role,
					}
					texture, exists := variants[variant]
					if !exists {
						group.encodes++
						texture, err = encodeModelViewerPreparedTexture(
							ctx,
							decoded,
							job.path,
							job.resourceName,
							variant.transform,
							variant.format,
							variant.quality,
						)
						if err != nil {
							texture = nil
						}
						if ctx.Err() != nil {
							break
						}
						variants[variant] = texture
					}
					if texture != nil {
						group.prepared = append(group.prepared, preparedJob{job: job, texture: texture})
					}
				}
			}
		}()
	}
prepareDispatch:
	for index := range contentGroups {
		select {
		case prepareWork <- index:
		case <-ctx.Done():
			break prepareDispatch
		}
	}
	close(prepareWork)
	prepareGroup.Wait()
	if err := ctx.Err(); err != nil {
		return outputs, stats, err
	}
	stats.PrepareWallMs = time.Since(prepareStartedAt).Milliseconds()
	for _, group := range contentGroups {
		stats.Decodes += group.decodes
		stats.Encodes += group.encodes
		stats.PreparedImages += len(group.prepared)
		for _, prepared := range group.prepared {
			if prepared.job.batchIndex < 0 || prepared.job.batchIndex >= len(outputs) {
				continue
			}
			item := modelViewerTexturePayload{
				Key:      prepared.job.canonicalKey,
				Role:     prepared.job.role,
				Bytes:    prepared.texture.bytes,
				MIMEType: prepared.texture.mimeType,
			}
			for _, key := range prepared.job.keys {
				outputs[prepared.job.batchIndex][key] = item
			}
		}
	}
	stats.LogicalTextures = countModelViewerLogicalTextures(outputs)
	stats.TotalWallMs = time.Since(startedAt).Milliseconds()
	return outputs, stats, ctx.Err()
}

func countModelViewerLogicalTextures(outputs []map[string]modelViewerTexturePayload) int {
	count := 0
	for _, output := range outputs {
		seen := make(map[string]bool, len(output))
		for _, item := range output {
			if item.Key == "" || seen[item.Key] {
				continue
			}
			seen[item.Key] = true
			count++
		}
	}
	return count
}

func modelViewerAssignmentTextureKey(assignment modelViewerDirectTextureAssignment) string {
	if assignment.file != "" && assignment.role != "" {
		return modelViewerTextureKey(assignment.file, assignment.role)
	}
	return modelViewerNormalizeKey(assignment.resource)
}

func classifyModelViewerTextureRole(name string) string {
	key := modelViewerNormalizeKey(name)
	switch {
	case strings.Contains(key, "normal") || strings.Contains(key, "bump"):
		return "normal_map"
	case strings.Contains(key, "light"):
		return "light_map"
	case strings.Contains(key, "material") || strings.Contains(key, "metal") || strings.Contains(key, "rough"):
		return "material_map"
	default:
		return "diffuse"
	}
}
