package modelviewer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/platform"
)

type SurfaceGeometry struct {
	Draw        DrawSource `json:"-"`
	Positions   []float32
	VertexIDs   []uint32
	Indices     []uint32
	Fingerprint string
}

func readSurfaceFile(ctx context.Context, name string) ([]byte, error) {
	release, err := diskio.Acquire(ctx, name)
	if err != nil {
		return nil, err
	}
	defer release()
	return os.ReadFile(name)
}

// Capture provenance before scratch buffers are released, using only hashes of
// the bytes read for decoding. Index hashes belong to the cached decoded format.
func (c *modelViewerBufferCache) surfaceSourceFiles(
	root, ibPath, ibFormat string,
	positionFiles ...string,
) (map[string][32]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	digest, ok := c.sourceHashes[modelViewerIndicesKey(ibPath, ibFormat)]
	if !ok {
		return nil, fmt.Errorf("surface index provenance unavailable: %s", ibPath)
	}
	files := map[string][32]byte{ibPath: digest}
	for _, name := range positionFiles {
		if name == "" {
			continue
		}
		if !filepath.IsAbs(name) {
			name = filepath.Join(root, filepath.FromSlash(name))
		}
		digest, ok := c.sourceHashes[strings.ToLower(name)]
		if !ok {
			return nil, fmt.Errorf("surface vertex provenance unavailable: %s", name)
		}
		files[name] = digest
	}
	return files, nil
}

func prepareSurfaceSnapshots(
	ctx context.Context,
	sources map[string]DrawSource,
	meshes []ModelViewerMeshTransport,
	payloads []modelViewerMeshPayload,
	spool *modelViewerSpool,
) error {
	for i, mesh := range meshes {
		source, ok := sources[mesh.ID]
		if !ok {
			continue
		}
		payload := payloads[i]
		ids := slices.Clone(payload.SourceIndices)
		if len(ids) == 0 {
			ids = make([]uint32, len(payload.Positions)/3)
			for k := range ids {
				ids[k] = uint32(k)
			}
		}
		length := 4 + len(payload.Positions)*4 + len(ids)*4
		encode := func(w io.Writer) error {
			out := newModelViewerChunkWriter(w)
			out.uint32(uint32(len(ids)))
			out.float32s(payload.Positions)
			out.uint32s(ids)
			return out.close()
		}
		if spool == nil {
			var buffer bytes.Buffer
			if err := encode(&buffer); err != nil {
				return err
			}
			raw := buffer.Bytes()
			source.surfaceRead = func(context.Context) ([]byte, error) { return raw, nil }
		} else {
			offset, err := spool.appendStream(ctx, length, encode)
			if err != nil {
				return err
			}
			source.surfaceRead = func(ctx context.Context) ([]byte, error) { return spool.read(ctx, offset, length) }
		}
		sources[mesh.ID] = source
	}
	return ctx.Err()
}

func (t *Service) SurfaceGeometry(ctx context.Context, sessionID, meshID string) (SurfaceGeometry, error) {
	source, ok := t.DrawSource(sessionID, meshID)
	if !ok || source.surfaceRead == nil {
		return SurfaceGeometry{}, fmt.Errorf("surface mesh session is unavailable")
	}

	var ib []byte
	fingerprint := sha256.New()
	for _, name := range slices.Sorted(maps.Keys(source.surfaceFiles)) {
		raw, err := readSurfaceFile(ctx, name)
		if err != nil {
			return SurfaceGeometry{}, err
		}
		if sha256.Sum256(raw) != source.surfaceFiles[name] {
			return SurfaceGeometry{}, fmt.Errorf("surface source changed: %s", name)
		}
		_, _ = fingerprint.Write(raw)
		if platform.SamePathFold(name, source.IBPath) {
			ib = raw
		}
	}

	raw, err := source.surfaceRead(ctx)
	if err != nil {
		return SurfaceGeometry{}, err
	}
	var geometry SurfaceGeometry
	if len(raw) < 4 {
		return geometry, fmt.Errorf("truncated surface snapshot")
	}
	vertices := int(binary.LittleEndian.Uint32(raw))
	if vertices > (len(raw)-4)/16 || len(raw) != 4+vertices*16 {
		return geometry, fmt.Errorf("invalid surface snapshot length")
	}
	geometry.Positions = make([]float32, vertices*3)
	geometry.VertexIDs = make([]uint32, vertices)
	for i := range geometry.Positions {
		geometry.Positions[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[4+i*4:]))
	}
	for i := range geometry.VertexIDs {
		geometry.VertexIDs[i] = binary.LittleEndian.Uint32(raw[4+vertices*12+i*4:])
	}

	indices, err := modelViewerDecodeIndices(ib, source.IBFormat)
	if err != nil {
		return geometry, err
	}
	faces := source.IndexCount / 3
	if source.Auto {
		faces = (len(indices) - source.StartIndex) / 3
	}
	if source.sourceFaces != nil {
		faces = len(source.sourceFaces)
	}
	if source.StartIndex < 0 || source.StartIndex > len(indices) || faces <= 0 {
		return geometry, fmt.Errorf("invalid surface draw range")
	}
	for face := range faces {
		original, err := source.SourceFaceIndex(face)
		if err != nil {
			return geometry, err
		}
		start := source.StartIndex + original*3
		if start < 0 || start+3 > len(indices) {
			return geometry, fmt.Errorf("surface triangle outside index buffer")
		}
		geometry.Indices = append(geometry.Indices, indices[start:start+3]...)
	}

	if _, ok = t.DrawSource(sessionID, meshID); !ok {
		return geometry, fmt.Errorf("surface mesh session closed")
	}
	geometry.Draw = source
	geometry.Fingerprint = fmt.Sprintf("%x", fingerprint.Sum(nil))
	return geometry, ctx.Err()
}
