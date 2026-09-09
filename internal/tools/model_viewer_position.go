package tools

import (
	"container/list"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"sync"

	"nahida.live/desktop/internal/infra"
)

const modelViewerPositionCacheBytes = 64 << 20

type modelViewerPositionCacheEntry struct {
	key  string
	data []byte
}

type modelViewerPositionCache struct {
	mu      sync.Mutex
	entries map[string]*list.Element
	order   list.List
	bytes   int
	limit   int
}

func (c *modelViewerPositionCache) load(
	ctx context.Context,
	key string,
	read func(context.Context) ([]byte, error),
) ([]byte, error) {
	// Serialize misses to bound transient decode memory as well as retained bytes.
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if hit := c.entries[key]; hit != nil {
		c.order.MoveToFront(hit)
		return hit.Value.(modelViewerPositionCacheEntry).data, nil
	}
	data, err := read(ctx)
	if err != nil {
		return nil, err
	}
	if len(data) > c.limit {
		return data, nil
	}
	for c.bytes+len(data) > c.limit {
		oldest := c.order.Back()
		entry := oldest.Value.(modelViewerPositionCacheEntry)
		delete(c.entries, entry.key)
		c.bytes -= len(entry.data)
		c.order.Remove(oldest)
	}
	if c.entries == nil {
		c.entries = make(map[string]*list.Element)
	}
	c.entries[key] = c.order.PushFront(modelViewerPositionCacheEntry{key: key, data: data})
	c.bytes += len(data)
	return data, nil
}

func (t *Tools) registerModelViewerPosition(
	sessionID, meshID string,
	variantIndex int,
	source modelViewerDirectPositionAssignment,
	indices, sources []uint32,
	vertexCount int,
	cache *modelViewerPositionCache,
) (string, error) {
	key := fmt.Sprintf("%s.variant.%d", meshID, variantIndex)
	return t.protocol.StoreMemoryLoader(sessionID, key, func(ctx context.Context) (data []byte, err error) {
		defer func() {
			if err != nil && ctx.Err() == nil {
				err = infra.ReportError(
					t.log,
					err,
					"Tools.LoadModelViewerPosition",
					infra.Diagnostic{
						Operation: "load-position-variant",
						Stage:     "prepare-geometry",
						Fields: map[string]any{
							"memorySessionId": sessionID,
							"meshId":          meshID,
							"variantIndex":    variantIndex,
							"sourcePath":      source.sourcePath,
							"sourceBytes":     source.sourceBytes,
							"stride":          source.stride,
						},
					},
				)
			}
		}()
		return cache.load(ctx, key, func(ctx context.Context) ([]byte, error) {
			positions, err := readModelViewerVariantPositions(ctx, source, sources, vertexCount)
			if err != nil {
				return nil, err
			}
			bounds, err := modelViewerGeometryBounds(ctx, positions)
			if err != nil {
				return nil, err
			}
			normals, err := modelViewerVertexNormals(ctx, positions, indices)
			if err != nil {
				return nil, err
			}
			return modelViewerVariantBytes(bounds, positions, normals), nil
		})
	})
}

func readModelViewerVariantPositions(
	ctx context.Context,
	source modelViewerDirectPositionAssignment,
	sources []uint32,
	vertexCount int,
) ([]float32, error) {
	if source.stride < 12 || source.sourceBytes <= 0 || source.sourceBytes%int64(source.stride) != 0 ||
		vertexCount < 0 {
		return nil, fmt.Errorf("invalid position source layout")
	}
	file, err := os.Open(source.sourcePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() != source.sourceBytes {
		return nil, fmt.Errorf(
			"position source size changed: expected %d, received %d",
			source.sourceBytes,
			info.Size(),
		)
	}
	count := vertexCount
	if sources != nil {
		count = len(sources)
	}
	positions := make([]float32, count*3)
	// Read nearby records together without materializing an entire interleaved file.
	chunk := make([]byte, 64<<10)
	chunkStart, chunkEnd := int64(-1), int64(-1)
	for vertex := range count {
		if vertex%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		index := uint64(vertex)
		if sources != nil {
			index = uint64(sources[vertex])
		}
		if index >= uint64(source.sourceBytes/int64(source.stride)) {
			return nil, fmt.Errorf("position source index %d exceeds available records", index)
		}
		offset := int64(index) * int64(source.stride)
		if offset < chunkStart || offset+12 > chunkEnd {
			n, readErr := file.ReadAt(chunk, offset)
			if readErr != nil && readErr != io.EOF {
				return nil, readErr
			}
			chunkStart, chunkEnd = offset, offset+int64(n)
			if n < 12 {
				return nil, io.ErrUnexpectedEOF
			}
		}
		for axis := range 3 {
			positions[vertex*3+axis] = math.Float32frombits(
				binary.LittleEndian.Uint32(chunk[int(offset-chunkStart)+axis*4:]),
			)
		}
	}
	return positions, ctx.Err()
}
