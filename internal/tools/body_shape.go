package tools

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

const (
	bodyShapedSuffix   = " (Body Shaped)"
	defaultBlendStride = 16
	shaderFixMarker    = ".nahida-shader-fixes.json"
)

var bodyDisabledPrefixRE = regexp.MustCompile(`(?i)^(?:disabled[\s_]*)+[\s_]+`)

type BlendBoneInfo struct {
	ID          uint32 `json:"id"`
	VertexCount int    `json:"vertexCount"`
}

type BodyShapeMeshCandidate struct {
	ID                   string          `json:"id"`
	Name                 string          `json:"name"`
	PositionPath         string          `json:"positionPath"`
	PositionRelativePath string          `json:"positionRelativePath"`
	PositionStride       int             `json:"positionStride"`
	VertexCount          int             `json:"vertexCount"`
	Positions            []float32       `json:"positions"`
	Indices              []uint32        `json:"indices,omitempty"`
	IndexPath            *string         `json:"indexPath,omitempty"`
	IndexRelativePath    *string         `json:"indexRelativePath,omitempty"`
	VectorPath           *string         `json:"vectorPath,omitempty"`
	VectorRelativePath   *string         `json:"vectorRelativePath,omitempty"`
	VectorStride         *int            `json:"vectorStride,omitempty"`
	VectorLayout         *string         `json:"vectorLayout"`
	GLBMeshNames         []string        `json:"glbMeshNames"`
	BlendPath            *string         `json:"blendPath,omitempty"`
	BlendRelativePath    *string         `json:"blendRelativePath,omitempty"`
	BlendStride          *int            `json:"blendStride,omitempty"`
	BlendBytes           []byte          `json:"blendBytes,omitempty"`
	Bones                []BlendBoneInfo `json:"bones"`
}

type BodyShapeLoadResult struct {
	ModRoot string                   `json:"modRoot"`
	INIPath string                   `json:"iniPath"`
	Meshes  []BodyShapeMeshCandidate `json:"meshes"`
}

type BodyShapeMeshSummary struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	VertexCount    int    `json:"vertexCount"`
	IndexCount     int    `json:"indexCount"`
	PositionStride int    `json:"positionStride"`
}

type BodyShapeSessionDescriptor struct {
	SessionID string                 `json:"sessionId"`
	ModRoot   string                 `json:"modRoot"`
	INIPath   string                 `json:"iniPath"`
	Meshes    []BodyShapeMeshSummary `json:"meshes"`
}

type BodyShapeMeshInput struct {
	SessionID string `json:"sessionId"`
	MeshID    string `json:"meshId"`
}

type BodyShapeMeshDescriptor struct {
	SessionID      string          `json:"sessionId"`
	MeshID         string          `json:"meshId"`
	PositionsURL   string          `json:"positionsUrl"`
	PositionsCount int             `json:"positionsCount"`
	IndicesURL     *string         `json:"indicesUrl,omitempty"`
	IndexCount     int             `json:"indexCount"`
	BlendURL       *string         `json:"blendUrl,omitempty"`
	BlendBytes     int             `json:"blendBytes"`
	PositionStride int             `json:"positionStride"`
	VectorLayout   *string         `json:"vectorLayout"`
	BlendStride    *int            `json:"blendStride,omitempty"`
	Bones          []BlendBoneInfo `json:"bones"`
}

type BodyShapeBeginExportInput struct {
	SessionID      string `json:"sessionId"`
	MeshID         string `json:"meshId"`
	IncludeWeights bool   `json:"includeWeights"`
}

type BodyShapeExportUpload struct {
	ExportID           string  `json:"exportId"`
	PositionsUploadURL string  `json:"positionsUploadUrl"`
	WeightsUploadURL   *string `json:"weightsUploadUrl,omitempty"`
}

type BodyShapeOK struct {
	OK bool `json:"ok"`
}

type BodyShapeCommitExportInput struct {
	SessionID      string                  `json:"sessionId"`
	ExportID       string                  `json:"exportId"`
	Amount         *float64                `json:"amount,omitempty"`
	AxisScale      []float64               `json:"axisScale,omitempty"`
	WriteChangeLog *bool                   `json:"writeChangeLog,omitempty"`
	ChangeSummary  *BodyShapeChangeSummary `json:"changeSummary,omitempty"`
}

type bodyShapeExportSlot struct {
	meshID         string
	includeWeights bool
}

type bodyShapeSession struct {
	mu          sync.Mutex
	loaded      BodyShapeLoadResult
	descriptors map[string]BodyShapeMeshDescriptor
	exports     map[string]bodyShapeExportSlot
}

type BodyShapeChangeSummary struct {
	Amount          float64   `json:"amount"`
	AxisScale       []float64 `json:"axisScale"`
	MovedVertices   int       `json:"movedVertices"`
	MaxDisplacement float64   `json:"maxDisplacement"`
}

type BodyShapeExportInput struct {
	ModRoot        string                  `json:"modRoot"`
	PositionPath   string                  `json:"positionPath"`
	PositionStride int                     `json:"positionStride"`
	Positions      []float32               `json:"positions"`
	VectorPath     *string                 `json:"vectorPath,omitempty"`
	VectorLayout   *string                 `json:"vectorLayout"`
	Weights        []float32               `json:"weights,omitempty"`
	Amount         *float64                `json:"amount,omitempty"`
	AxisScale      []float64               `json:"axisScale,omitempty"`
	WriteChangeLog *bool                   `json:"writeChangeLog,omitempty"`
	ChangeSummary  *BodyShapeChangeSummary `json:"changeSummary,omitempty"`
}

type BodyShapeExportResult struct {
	PositionPath  string  `json:"positionPath"`
	PositionBytes int     `json:"positionBytes"`
	VectorPath    *string `json:"vectorPath,omitempty"`
	VectorBytes   *int    `json:"vectorBytes,omitempty"`
	ChangeLogPath *string `json:"changeLogPath,omitempty"`
	ModRoot       *string `json:"modRoot,omitempty"`
	SourceModPath *string `json:"sourceModPath,omitempty"`
}

func (t *Tools) BodyShapeLoadMod(ctx context.Context, modPath string) (BodyShapeSessionDescriptor, error) {
	if err := ctx.Err(); err != nil {
		return BodyShapeSessionDescriptor{}, err
	}
	loaded, err := loadBodyShapeMod(modPath, func(message string) {
		if t.log != nil {
			t.log.Warn(message, "BodyShapeEditor")
		}
	})
	if err != nil {
		return BodyShapeSessionDescriptor{}, err
	}
	if t.protocol == nil {
		return BodyShapeSessionDescriptor{}, errors.New("tools service has no protocol service")
	}
	sessionID := t.protocol.CreateMemorySession()
	session := &bodyShapeSession{loaded: loaded, descriptors: make(map[string]BodyShapeMeshDescriptor), exports: make(map[string]bodyShapeExportSlot)}
	t.bodyShapeMu.Lock()
	t.bodyShapeSessions[sessionID] = session
	t.bodyShapeMu.Unlock()
	descriptor := BodyShapeSessionDescriptor{SessionID: sessionID, ModRoot: loaded.ModRoot, INIPath: loaded.INIPath, Meshes: make([]BodyShapeMeshSummary, 0, len(loaded.Meshes))}
	for _, mesh := range loaded.Meshes {
		descriptor.Meshes = append(descriptor.Meshes, BodyShapeMeshSummary{ID: mesh.ID, Name: mesh.Name, VertexCount: mesh.VertexCount, IndexCount: len(mesh.Indices), PositionStride: mesh.PositionStride})
	}
	return descriptor, nil
}

//wails:ignore
func (t *Tools) BodyShapeExport(ctx context.Context, input BodyShapeExportInput) (result BodyShapeExportResult, err error) {
	sourceRoot, err := filepath.Abs(input.ModRoot)
	if err != nil {
		return result, err
	}
	info, err := os.Stat(sourceRoot)
	if err != nil || !info.IsDir() {
		return result, contractError(fmt.Sprintf("Mod path does not exist: %s", sourceRoot))
	}
	if t.mod == nil {
		return result, errors.New("tools service has no mod service")
	}
	parent := filepath.Dir(sourceRoot)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return result, err
	}
	existing := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			existing = append(existing, entry.Name())
		}
	}
	baseName := bodyShapedFolderBaseName(filepath.Base(sourceRoot))
	targetName := t.fs.GetUniqueName(baseName, existing)
	targetRoot := filepath.Join(parent, targetName)
	if !sameOrChildPath(parent, targetRoot) || samePathFold(parent, targetRoot) || samePathFold(sourceRoot, targetRoot) {
		return result, errors.New("invalid body shape target path")
	}
	copied := false
	defer func() {
		if err != nil && copied {
			if cleanupErr := os.RemoveAll(targetRoot); cleanupErr != nil {
				t.logError(cleanupErr, "BodyShapeEditor:exportMesh:cleanup:"+targetRoot)
			}
		}
	}()
	if err = copyBodyShapeTree(ctx, sourceRoot, targetRoot); err != nil {
		return result, err
	}
	copied = true
	if removeErr := os.Remove(filepath.Join(targetRoot, shaderFixMarker)); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return result, removeErr
	}
	remappedPosition, err := remapBodyShapePath(input.PositionPath, sourceRoot, targetRoot)
	if err != nil {
		return result, err
	}
	remapped := input
	remapped.ModRoot, remapped.PositionPath = targetRoot, remappedPosition
	if input.VectorPath != nil {
		vector, remapErr := remapBodyShapePath(*input.VectorPath, sourceRoot, targetRoot)
		if remapErr != nil {
			return result, remapErr
		}
		remapped.VectorPath = &vector
	}
	result, err = exportBodyShapeMesh(remapped, func(message string) {
		if t.log != nil {
			t.log.Warn(message, "BodyShapeEditor")
		}
	})
	if err != nil {
		return result, err
	}
	sourceModPath, err := disableBodyShapeSource(ctx, t.mod, sourceRoot)
	if err != nil {
		return result, err
	}
	result.ModRoot, result.SourceModPath = &targetRoot, &sourceModPath
	return result, nil
}

func (t *Tools) BodyShapeGetMesh(ctx context.Context, input BodyShapeMeshInput) (BodyShapeMeshDescriptor, error) {
	if err := ctx.Err(); err != nil {
		return BodyShapeMeshDescriptor{}, err
	}
	session, err := t.requireBodyShapeSession(input.SessionID)
	if err != nil {
		return BodyShapeMeshDescriptor{}, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if descriptor, ok := session.descriptors[input.MeshID]; ok {
		return descriptor, nil
	}
	mesh := findBodyShapeMesh(session.loaded.Meshes, input.MeshID)
	if mesh == nil {
		return BodyShapeMeshDescriptor{}, contractError(fmt.Sprintf("Body shape mesh not found: %s", input.MeshID))
	}
	positionsURL, err := t.protocol.StoreMemoryBuffer(input.SessionID, "mesh:"+mesh.ID+":positions", float32Bytes(mesh.Positions), "application/octet-stream")
	if err != nil {
		return BodyShapeMeshDescriptor{}, err
	}
	descriptor := BodyShapeMeshDescriptor{SessionID: input.SessionID, MeshID: mesh.ID, PositionsURL: positionsURL, PositionsCount: len(mesh.Positions), IndexCount: len(mesh.Indices), BlendBytes: len(mesh.BlendBytes), PositionStride: mesh.PositionStride, VectorLayout: mesh.VectorLayout, BlendStride: mesh.BlendStride, Bones: mesh.Bones}
	if len(mesh.Indices) > 0 {
		value, storeErr := t.protocol.StoreMemoryBuffer(input.SessionID, "mesh:"+mesh.ID+":indices", uint32Bytes(mesh.Indices), "application/octet-stream")
		if storeErr != nil {
			return BodyShapeMeshDescriptor{}, storeErr
		}
		descriptor.IndicesURL = &value
	}
	if len(mesh.BlendBytes) > 0 {
		value, storeErr := t.protocol.StoreMemoryBuffer(input.SessionID, "mesh:"+mesh.ID+":blend", mesh.BlendBytes, "application/octet-stream")
		if storeErr != nil {
			return BodyShapeMeshDescriptor{}, storeErr
		}
		descriptor.BlendURL = &value
	}
	session.descriptors[input.MeshID] = descriptor
	return descriptor, nil
}

func (t *Tools) BodyShapeBeginExport(ctx context.Context, input BodyShapeBeginExportInput) (BodyShapeExportUpload, error) {
	if err := ctx.Err(); err != nil {
		return BodyShapeExportUpload{}, err
	}
	session, err := t.requireBodyShapeSession(input.SessionID)
	if err != nil {
		return BodyShapeExportUpload{}, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	mesh := findBodyShapeMesh(session.loaded.Meshes, input.MeshID)
	if mesh == nil {
		return BodyShapeExportUpload{}, contractError(fmt.Sprintf("Body shape mesh not found: %s", input.MeshID))
	}
	exportID, err := newToolsID()
	if err != nil {
		return BodyShapeExportUpload{}, err
	}
	positionsID := "export:" + exportID + ":positions"
	positionsURL, err := t.protocol.CreateMemoryUpload(input.SessionID, positionsID, int64(len(mesh.Positions))*4)
	if err != nil {
		return BodyShapeExportUpload{}, err
	}
	result := BodyShapeExportUpload{ExportID: exportID, PositionsUploadURL: positionsURL}
	if input.IncludeWeights {
		weightsID := "export:" + exportID + ":weights"
		weightsURL, uploadErr := t.protocol.CreateMemoryUpload(input.SessionID, weightsID, int64(mesh.VertexCount)*4)
		if uploadErr != nil {
			t.protocol.RemoveMemoryBuffer(input.SessionID, positionsID)
			return BodyShapeExportUpload{}, uploadErr
		}
		result.WeightsUploadURL = &weightsURL
	}
	session.exports[exportID] = bodyShapeExportSlot{meshID: mesh.ID, includeWeights: input.IncludeWeights}
	return result, nil
}

func (t *Tools) BodyShapeCommitExport(ctx context.Context, input BodyShapeCommitExportInput) (BodyShapeExportResult, error) {
	session, err := t.requireBodyShapeSession(input.SessionID)
	if err != nil {
		return BodyShapeExportResult{}, err
	}
	session.mu.Lock()
	slot, exists := session.exports[input.ExportID]
	if exists {
		delete(session.exports, input.ExportID)
	}
	mesh := findBodyShapeMesh(session.loaded.Meshes, slot.meshID)
	session.mu.Unlock()
	if !exists || mesh == nil {
		return BodyShapeExportResult{}, contractError("Body shape export slot not found")
	}
	positionsID := "export:" + input.ExportID + ":positions"
	weightsID := "export:" + input.ExportID + ":weights"
	defer t.protocol.RemoveMemoryBuffer(input.SessionID, positionsID)
	defer t.protocol.RemoveMemoryBuffer(input.SessionID, weightsID)
	positionsRaw, err := t.protocol.TakeMemoryUpload(input.SessionID, positionsID)
	if err != nil {
		return BodyShapeExportResult{}, err
	}
	positions, err := decodeFloat32Bytes(positionsRaw)
	if err != nil {
		return BodyShapeExportResult{}, err
	}
	var weights []float32
	if slot.includeWeights {
		weightsRaw, takeErr := t.protocol.TakeMemoryUpload(input.SessionID, weightsID)
		if takeErr != nil {
			return BodyShapeExportResult{}, takeErr
		}
		weights, err = decodeFloat32Bytes(weightsRaw)
		if err != nil {
			return BodyShapeExportResult{}, err
		}
	}
	return t.BodyShapeExport(ctx, BodyShapeExportInput{ModRoot: session.loaded.ModRoot, PositionPath: mesh.PositionPath, PositionStride: mesh.PositionStride, Positions: positions, VectorPath: mesh.VectorPath, VectorLayout: mesh.VectorLayout, Weights: weights, Amount: input.Amount, AxisScale: input.AxisScale, WriteChangeLog: input.WriteChangeLog, ChangeSummary: input.ChangeSummary})
}

func (t *Tools) BodyShapeCloseSession(_ context.Context, sessionID string) (BodyShapeOK, error) {
	t.bodyShapeMu.Lock()
	delete(t.bodyShapeSessions, sessionID)
	t.bodyShapeMu.Unlock()
	if t.protocol != nil {
		t.protocol.CleanupMemorySession(sessionID)
	}
	return BodyShapeOK{OK: true}, nil
}

func (t *Tools) requireBodyShapeSession(id string) (*bodyShapeSession, error) {
	t.bodyShapeMu.Lock()
	session := t.bodyShapeSessions[id]
	t.bodyShapeMu.Unlock()
	if session == nil {
		return nil, contractError(fmt.Sprintf("Body shape session not found: %s", id))
	}
	return session, nil
}

func findBodyShapeMesh(meshes []BodyShapeMeshCandidate, id string) *BodyShapeMeshCandidate {
	for index := range meshes {
		if meshes[index].ID == id {
			return &meshes[index]
		}
	}
	return nil
}

func float32Bytes(values []float32) []byte {
	data := make([]byte, len(values)*4)
	for index, value := range values {
		binary.LittleEndian.PutUint32(data[index*4:], math.Float32bits(value))
	}
	return data
}

func uint32Bytes(values []uint32) []byte {
	data := make([]byte, len(values)*4)
	for index, value := range values {
		binary.LittleEndian.PutUint32(data[index*4:], value)
	}
	return data
}

func decodeFloat32Bytes(data []byte) ([]float32, error) {
	if len(data)%4 != 0 {
		return nil, errors.New("float32 buffer is not 4-byte aligned")
	}
	values := make([]float32, len(data)/4)
	for index := range values {
		values[index] = math.Float32frombits(binary.LittleEndian.Uint32(data[index*4:]))
	}
	return values, nil
}

func (t *Tools) shutdownBodyShape() error {
	t.bodyShapeMu.Lock()
	sessions := t.bodyShapeSessions
	t.bodyShapeSessions = make(map[string]*bodyShapeSession)
	t.bodyShapeMu.Unlock()
	if t.protocol != nil {
		for id := range sessions {
			t.protocol.CleanupMemorySession(id)
		}
	}
	return nil
}

type unmanagedModDisabler interface {
	DisableUnmanaged(context.Context, string) (string, error)
}

func disableBodyShapeSource(ctx context.Context, disabler ModDisabler, path string) (string, error) {
	if unmanaged, ok := disabler.(unmanagedModDisabler); ok {
		return unmanaged.DisableUnmanaged(ctx, path)
	}
	return disabler.Disable(ctx, path)
}

func loadBodyShapeMod(modPath string, warn func(string)) (BodyShapeLoadResult, error) {
	resolved, err := filepath.Abs(modPath)
	if err != nil {
		return BodyShapeLoadResult{}, err
	}
	if _, err := os.Stat(resolved); err != nil {
		return BodyShapeLoadResult{}, contractError(fmt.Sprintf("Path does not exist: %s", resolved))
	}
	iniPath, sections, err := loadModINIBundle(resolved)
	if err != nil {
		return BodyShapeLoadResult{}, err
	}
	modRoot := filepath.Dir(iniPath)
	resources := collectModResources(sections)
	positions := collectPositionResources(resources)
	indices := collectIndexResources(resources)
	vectors := collectNamedResources(resources, "vector", false)
	blends := collectNamedResources(resources, "blend", true)
	indicesByPosition := matchIndexResources(positions, indices, sections)
	if len(positions) == 0 {
		return BodyShapeLoadResult{}, contractError("No position buffer resources found in mod.ini")
	}
	result := BodyShapeLoadResult{ModRoot: modRoot, INIPath: iniPath, Meshes: []BodyShapeMeshCandidate{}}
	for _, position := range positions {
		positionPath, pathErr := resolveBodyShapeResource(modRoot, position.Filename)
		if pathErr != nil {
			warn(fmt.Sprintf("Missing position buffer: %s", position.Filename))
			continue
		}
		data, readErr := os.ReadFile(positionPath)
		if readErr != nil {
			warn(fmt.Sprintf("Missing position buffer: %s", positionPath))
			continue
		}
		vertexCount, validationErr := validatePositionBuffer(len(data), position.Stride, nil)
		if validationErr != nil {
			warn(fmt.Sprintf("Skipping position buffer %s: %s", positionPath, validationErr))
			continue
		}
		meshPositions, _ := extractBodyPositions(data, position.Stride)
		mesh := BodyShapeMeshCandidate{
			ID: position.Name, Name: position.Name, PositionPath: positionPath,
			PositionRelativePath: position.Filename, PositionStride: position.Stride,
			VertexCount: vertexCount, Positions: meshPositions, GLBMeshNames: []string{}, Bones: []BlendBoneInfo{},
		}
		for _, index := range indicesByPosition[strings.ToLower(position.Name)] {
			indexPath, resolveErr := resolveBodyShapeResource(modRoot, index.Filename)
			if resolveErr != nil {
				warn(fmt.Sprintf("Missing index buffer: %s", index.Filename))
				continue
			}
			values, readErr := readIndexBuffer(indexPath, index.Format)
			if readErr != nil {
				warn(fmt.Sprintf("Missing index buffer: %s", indexPath))
				continue
			}
			valid := true
			for _, value := range values {
				if value >= uint32(vertexCount) {
					valid = false
					break
				}
			}
			if !valid {
				warn(fmt.Sprintf("Skipping index buffer %s: index exceeds %d", indexPath, vertexCount-1))
				continue
			}
			mesh.Indices = append(mesh.Indices, values...)
			if mesh.IndexPath == nil {
				mesh.IndexPath, mesh.IndexRelativePath = &indexPath, stringPtr(index.Filename)
			}
			mesh.GLBMeshNames = append(mesh.GLBMeshNames, strings.TrimSuffix(filepath.Base(filepath.FromSlash(index.Filename)), filepath.Ext(index.Filename)))
		}
		if vector, ok := matchCompanionResource(position, vectors); ok {
			if vectorPath, resolveErr := resolveBodyShapeResource(modRoot, vector.Filename); resolveErr == nil {
				if stat, statErr := os.Stat(vectorPath); statErr == nil && stat.Mode().IsRegular() {
					stride := vector.Stride
					if stride == 0 {
						stride = 8
					}
					mesh.VectorPath, mesh.VectorRelativePath, mesh.VectorStride = &vectorPath, stringPtr(vector.Filename), &stride
					if stride == 8 && stat.Size() == int64(vertexCount*8) {
						mesh.VectorLayout = stringPtr("snorm8-tangent-normal")
					}
				}
			}
		}
		if blend, ok := matchCompanionResource(position, blends); ok {
			if blendPath, resolveErr := resolveBodyShapeResource(modRoot, blend.Filename); resolveErr == nil {
				if raw, readErr := os.ReadFile(blendPath); readErr == nil {
					stride := blend.Stride
					if stride == 0 {
						stride = defaultBlendStride
					}
					if validationErr := validateBlendBuffer(len(raw), vertexCount, stride); validationErr != nil {
						warn(fmt.Sprintf("Skipping blend buffer %s: %s", blendPath, validationErr))
					} else {
						mesh.BlendPath, mesh.BlendRelativePath, mesh.BlendStride = &blendPath, stringPtr(blend.Filename), &stride
						mesh.BlendBytes = raw
						mesh.Bones = listBlendBones(raw, vertexCount, stride)
					}
				}
			}
		}
		result.Meshes = append(result.Meshes, mesh)
	}
	if len(result.Meshes) == 0 {
		return BodyShapeLoadResult{}, contractError("No readable position buffers found in the selected mod")
	}
	return result, nil
}

func exportBodyShapeMesh(input BodyShapeExportInput, warn func(string)) (BodyShapeExportResult, error) {
	positionPath, err := filepath.Abs(input.PositionPath)
	if err != nil {
		return BodyShapeExportResult{}, err
	}
	original, err := os.ReadFile(positionPath)
	if err != nil {
		return BodyShapeExportResult{}, contractError(fmt.Sprintf("Position buffer not found: %s", positionPath))
	}
	expected := len(input.Positions) / 3
	if len(input.Positions)%3 != 0 {
		return BodyShapeExportResult{}, errors.New("position float count is not divisible by 3")
	}
	if _, err := validatePositionBuffer(len(original), input.PositionStride, &expected); err != nil {
		return BodyShapeExportResult{}, err
	}
	written, err := writeBodyPositions(original, input.PositionStride, input.Positions)
	if err != nil {
		return BodyShapeExportResult{}, err
	}
	if len(written) != len(original) {
		return BodyShapeExportResult{}, contractError(fmt.Sprintf("Refusing to write position buffer: size would change from %d to %d", len(original), len(written)))
	}
	if err := writeBodyFileAtomic(positionPath, written, 0o600); err != nil {
		return BodyShapeExportResult{}, err
	}
	result := BodyShapeExportResult{PositionPath: positionPath, PositionBytes: len(written)}
	if input.VectorPath != nil && input.VectorLayout != nil && *input.VectorLayout == "snorm8-tangent-normal" && len(input.Weights) > 0 && input.Amount != nil && len(input.AxisScale) == 3 {
		vectorPath, absErr := filepath.Abs(*input.VectorPath)
		if absErr != nil {
			return result, absErr
		}
		vectors, readErr := os.ReadFile(vectorPath)
		if readErr != nil {
			return result, readErr
		}
		if len(vectors) != expected*8 {
			warn(fmt.Sprintf("Skipping vector rewrite for %s: size mismatch", vectorPath))
		} else {
			corrected, correctionErr := correctBodyVectors(vectors, input.Weights, *input.Amount, input.AxisScale)
			if correctionErr != nil {
				return result, correctionErr
			}
			if err := writeBodyFileAtomic(vectorPath, corrected, 0o600); err != nil {
				return result, err
			}
			bytes := len(corrected)
			result.VectorPath, result.VectorBytes = &vectorPath, &bytes
		}
	}
	writeLog := input.WriteChangeLog == nil || *input.WriteChangeLog
	if writeLog {
		root, err := filepath.Abs(input.ModRoot)
		if err != nil {
			return result, err
		}
		if !sameOrChildPath(root, positionPath) {
			return result, errors.New("position path is outside mod root")
		}
		changeLogPath := filepath.Join(root, "변경사항.txt")
		relativePosition, _ := filepath.Rel(root, positionPath)
		lines := []string{
			"[체형 수정 내역]", "", "- 수정 대상 파일: " + relativePosition,
			"- 방향 버퍼: 수정하지 않음 (레이아웃 미검증 또는 균일 스케일)",
			"- 유지한 파일: Index, Blend, UV, Color 및 기타 원본 파일",
			"- 변경 방식: 본/부위 가중치 + 피벗 기준 축별 스케일 (원본 정점 기준 재계산)",
		}
		if result.VectorPath != nil {
			relativeVector, _ := filepath.Rel(root, *result.VectorPath)
			lines[3] = "- 방향 버퍼: " + relativeVector
		}
		if summary := input.ChangeSummary; summary != nil && len(summary.AxisScale) == 3 {
			lines = append(lines,
				fmt.Sprintf("- 강도: %v", summary.Amount),
				fmt.Sprintf("- 축 스케일: X=%v, Y=%v, Z=%v", summary.AxisScale[0], summary.AxisScale[1], summary.AxisScale[2]),
				fmt.Sprintf("- 이동 정점 수: %d", summary.MovedVertices),
				fmt.Sprintf("- 최대 이동 거리: %.6f", summary.MaxDisplacement),
			)
		}
		lines = append(lines, "- 참고: 실제 애니메이션과 모든 의상 조합에서 미세 클리핑이 발생할 수 있음", "")
		if err := writeBodyFileAtomic(changeLogPath, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
			return result, err
		}
		result.ChangeLogPath = &changeLogPath
	}
	return result, nil
}

func validatePositionBuffer(size, stride int, expected *int) (int, error) {
	if stride < 12 {
		return 0, contractError(fmt.Sprintf("Unsupported position stride: %d", stride))
	}
	if size <= 0 || size%stride != 0 {
		return 0, contractError(fmt.Sprintf("Position file size %d is not divisible by stride %d", size, stride))
	}
	vertices := size / stride
	if expected != nil && vertices != *expected {
		return 0, contractError(fmt.Sprintf("Vertex count mismatch: file has %d, expected %d", vertices, *expected))
	}
	return vertices, nil
}

func extractBodyPositions(data []byte, stride int) ([]float32, error) {
	count, err := validatePositionBuffer(len(data), stride, nil)
	if err != nil {
		return nil, err
	}
	out := make([]float32, count*3)
	for vertex := range count {
		base, offset := vertex*stride, vertex*3
		out[offset] = math.Float32frombits(binary.LittleEndian.Uint32(data[base:]))
		out[offset+1] = math.Float32frombits(binary.LittleEndian.Uint32(data[base+4:]))
		out[offset+2] = math.Float32frombits(binary.LittleEndian.Uint32(data[base+8:]))
	}
	return out, nil
}

func writeBodyPositions(original []byte, stride int, positions []float32) ([]byte, error) {
	count, err := validatePositionBuffer(len(original), stride, nil)
	if err != nil {
		return nil, err
	}
	if len(positions) != count*3 {
		return nil, contractError(fmt.Sprintf("Position count %v does not match vertex count %d", float64(len(positions))/3, count))
	}
	out := append([]byte(nil), original...)
	for index, value := range positions {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return nil, contractError(fmt.Sprintf("Non-finite position at float index %d", index))
		}
	}
	for vertex := range count {
		base, offset := vertex*stride, vertex*3
		binary.LittleEndian.PutUint32(out[base:], math.Float32bits(positions[offset]))
		binary.LittleEndian.PutUint32(out[base+4:], math.Float32bits(positions[offset+1]))
		binary.LittleEndian.PutUint32(out[base+8:], math.Float32bits(positions[offset+2]))
	}
	return out, nil
}

func validateBlendBuffer(size, vertexCount, stride int) error {
	if stride != 4 && stride != 8 && stride != 12 && stride != 32 && stride < 16 {
		return contractError(fmt.Sprintf("Unsupported blend stride: %d", stride))
	}
	if vertexCount <= 0 {
		return contractError("Blend vertex count must be positive")
	}
	if size < vertexCount*stride {
		return contractError(fmt.Sprintf("Blend buffer too small: %d < %d", size, vertexCount*stride))
	}
	return nil
}

func listBlendBones(data []byte, vertexCount, stride int) []BlendBoneInfo {
	counts := make(map[uint32]int)
	limit := min(vertexCount, len(data)/stride)
	for vertex := range limit {
		seen := make(map[uint32]bool)
		visitBlendInfluences(data, vertex*stride, stride, func(id uint32, weight float32) {
			if weight > 0 && !seen[id] {
				seen[id] = true
				counts[id]++
			}
		})
	}
	bones := make([]BlendBoneInfo, 0, len(counts))
	for id, count := range counts {
		bones = append(bones, BlendBoneInfo{ID: id, VertexCount: count})
	}
	sort.Slice(bones, func(i, j int) bool { return bones[i].ID < bones[j].ID })
	return bones
}

func visitBlendInfluences(data []byte, base, stride int, visit func(uint32, float32)) {
	if base+stride > len(data) {
		return
	}
	switch stride {
	case 4:
		visit(binary.LittleEndian.Uint32(data[base:]), 1)
	case 12:
		for index := range 4 {
			weight := float32(binary.LittleEndian.Uint16(data[base+index*2:])) / 65535
			if weight > 0 {
				visit(uint32(data[base+8+index]), weight)
			}
		}
	case 32:
		for index := range 4 {
			weight := math.Float32frombits(binary.LittleEndian.Uint32(data[base+index*4:]))
			if weight > 0 && !math.IsNaN(float64(weight)) && !math.IsInf(float64(weight), 0) {
				visit(binary.LittleEndian.Uint32(data[base+16+index*4:]), weight)
			}
		}
	default:
		weightsOffset := 8
		if stride == 8 {
			weightsOffset = 4
		}
		for index := range 4 {
			if weight := float32(data[base+weightsOffset+index]) / 255; weight > 0 {
				visit(uint32(data[base+index]), weight)
			}
		}
	}
}

func correctBodyVectors(original []byte, weights []float32, amount float64, axis []float64) ([]byte, error) {
	if len(original)%8 != 0 {
		return nil, contractError("Vector buffer length is not divisible by 8")
	}
	out := append([]byte(nil), original...)
	limit := min(len(weights), len(original)/8)
	for vertex := range limit {
		weight := float64(weights[vertex])
		if weight <= 0 || amount == 0 {
			continue
		}
		sx, sy, sz := 1+amount*axis[0]*weight, 1+amount*axis[1]*weight, 1+amount*axis[2]*weight
		if math.Abs(sx-1) < 1e-8 && math.Abs(sy-1) < 1e-8 && math.Abs(sz-1) < 1e-8 {
			continue
		}
		correctBodyVertexVectors(out, vertex, sx, sy, sz)
	}
	return out, nil
}

func correctBodyVertexVectors(data []byte, vertex int, sx, sy, sz float64) {
	base := vertex * 8
	tx, ty, tz := float64(int8(data[base]))/127, float64(int8(data[base+1]))/127, float64(int8(data[base+2]))/127
	nx, ny, nz := float64(int8(data[base+4]))/127, float64(int8(data[base+5]))/127, float64(int8(data[base+6]))/127
	tx, ty, tz = tx*sx, ty*sy, tz*sz
	if length := math.Sqrt(tx*tx + ty*ty + tz*tz); length > 1e-8 {
		tx, ty, tz = tx/length, ty/length, tz/length
	}
	if sx == 0 {
		sx = 1
	}
	if sy == 0 {
		sy = 1
	}
	if sz == 0 {
		sz = 1
	}
	nx, ny, nz = nx/sx, ny/sy, nz/sz
	if length := math.Sqrt(nx*nx + ny*ny + nz*nz); length > 1e-8 {
		nx, ny, nz = nx/length, ny/length, nz/length
	}
	dot := tx*nx + ty*ny + tz*nz
	tx, ty, tz = tx-nx*dot, ty-ny*dot, tz-nz*dot
	if length := math.Sqrt(tx*tx + ty*ty + tz*tz); length > 1e-8 {
		tx, ty, tz = tx/length, ty/length, tz/length
	}
	data[base], data[base+1], data[base+2] = byte(snorm8(tx)), byte(snorm8(ty)), byte(snorm8(tz))
	data[base+4], data[base+5], data[base+6] = byte(snorm8(nx)), byte(snorm8(ny)), byte(snorm8(nz))
}

func snorm8(value float64) int8 {
	rounded := int(math.Floor(value*127 + 0.5))
	return int8(max(-127, min(127, rounded)))
}

func resolveBodyShapeResource(root, relative string) (string, error) {
	if strings.TrimSpace(relative) == "" {
		return "", errors.New("resource filename is empty")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(filepath.Join(rootAbs, filepath.FromSlash(relative)))
	if err != nil || !sameOrChildPath(rootAbs, path) || samePathFold(rootAbs, path) {
		return "", errors.New("resource path is outside mod root")
	}
	realRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", err
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil || !sameOrChildPath(realRoot, realPath) || samePathFold(realRoot, realPath) {
		return "", errors.New("resource path is outside mod root")
	}
	info, err := os.Stat(realPath)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("resource is not a regular file")
	}
	return realPath, nil
}

func remapBodyShapePath(path, sourceRoot, targetRoot string) (string, error) {
	return remapBodyShapePathWithResolver(path, sourceRoot, targetRoot, filepath.EvalSymlinks)
}

func remapBodyShapePathWithResolver(
	path, sourceRoot, targetRoot string,
	resolve func(string) (string, error),
) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	root, err := filepath.Abs(sourceRoot)
	if err != nil {
		return "", err
	}
	comparisonRoot, comparisonPath := root, absolute
	if realRoot, rootErr := resolve(root); rootErr == nil {
		if realPath, pathErr := resolve(absolute); pathErr == nil {
			comparisonRoot, comparisonPath = realRoot, realPath
		}
	}
	relative, err := filepath.Rel(comparisonRoot, comparisonPath)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", contractError(fmt.Sprintf("Path is outside mod root: %s", path))
	}
	return filepath.Join(targetRoot, relative), nil
}

func copyBodyShapeTree(ctx context.Context, source, target string) error {
	if err := os.Mkdir(target, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if samePathFold(path, source) {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if !sameOrChildPath(target, destination) {
			return errors.New("copy target escaped body shape directory")
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic links are not supported in body shape mods: %s", path)
		}
		if entry.IsDir() {
			return os.Mkdir(destination, 0o755)
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported mod entry: %s", path)
		}
		return copyBodyShapeFile(path, destination, info.Mode().Perm())
	})
}

func copyBodyShapeFile(source, target string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	return errors.Join(copyErr, closeErr)
}

func writeBodyFileAtomic(target string, data []byte, mode os.FileMode) error {
	temp, err := os.CreateTemp(filepath.Dir(target), ".body-shape-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return replaceAtomic(tempPath, target)
}

func stringPtr(value string) *string { return &value }

func bodyShapedFolderBaseName(name string) string {
	return strings.TrimSpace(bodyDisabledPrefixRE.ReplaceAllString(strings.TrimSpace(name), "")) + bodyShapedSuffix
}
