package modelviewer

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
)

// modelViewerEncodeChunkBytes bounds the scratch buffer of a streamed encoding, so writing a mesh to the spool
// never holds a second copy of the whole mesh.
const modelViewerEncodeChunkBytes = 64 << 10

type modelViewerChunkWriter struct {
	w   io.Writer
	buf []byte
	err error
}

func newModelViewerChunkWriter(w io.Writer) *modelViewerChunkWriter {
	return &modelViewerChunkWriter{w: w, buf: make([]byte, 0, modelViewerEncodeChunkBytes)}
}

func (c *modelViewerChunkWriter) uint32(value uint32) {
	if len(c.buf)+4 > cap(c.buf) {
		c.flush()
	}
	c.buf = binary.LittleEndian.AppendUint32(c.buf, value)
}

func (c *modelViewerChunkWriter) float32s(values []float32) {
	for _, value := range values {
		c.uint32(math.Float32bits(value))
	}
}

func (c *modelViewerChunkWriter) uint32s(values []uint32) {
	for _, value := range values {
		c.uint32(value)
	}
}

func (c *modelViewerChunkWriter) bytes(data []byte) {
	for len(data) > 0 {
		if len(c.buf) == cap(c.buf) {
			c.flush()
		}
		n := min(len(data), cap(c.buf)-len(c.buf))
		c.buf = append(c.buf, data[:n]...)
		data = data[n:]
	}
}

// flush writes the buffered chunk; after a failed write it discards the rest and close reports the failure.
func (c *modelViewerChunkWriter) flush() {
	if c.err == nil && len(c.buf) > 0 {
		_, c.err = c.w.Write(c.buf)
	}
	c.buf = c.buf[:0]
}

func (c *modelViewerChunkWriter) close() error {
	c.flush()
	return c.err
}

// MVG3 stores seven element counts followed by position, normal, tangent, and
// two UV float32 arrays, RGBA8 vertex colors, and uint32 indices. All fields are
// little-endian; colors hold four bytes per vertex, so every array stays
// four-byte aligned.
func modelViewerMeshBytes(payload modelViewerMeshPayload) []byte {
	data := bytes.NewBuffer(make([]byte, 0, modelViewerMeshByteLength(payload)))
	_ = writeModelViewerMeshBytes(data, payload) // A bytes.Buffer write cannot fail.
	return data.Bytes()
}

func modelViewerMeshAttributes(payload modelViewerMeshPayload) [][]float32 {
	return [][]float32{payload.Positions, payload.Normals, payload.Tangents, payload.UVs, payload.UVs1}
}

func modelViewerMeshByteLength(payload modelViewerMeshPayload) int {
	count := len(payload.Indices)
	for _, values := range modelViewerMeshAttributes(payload) {
		count += len(values)
	}
	return 32 + count*4 + len(payload.Colors)
}

func writeModelViewerMeshBytes(w io.Writer, payload modelViewerMeshPayload) error {
	attributes := modelViewerMeshAttributes(payload)
	out := newModelViewerChunkWriter(w)
	out.bytes([]byte{'M', 'V', 'G', '3'})
	for _, values := range attributes {
		out.uint32(uint32(len(values)))
	}
	out.uint32(uint32(len(payload.Colors)))
	out.uint32(uint32(len(payload.Indices)))

	for _, values := range attributes {
		out.float32s(values)
	}
	out.bytes(payload.Colors)
	out.uint32s(payload.Indices)
	return out.close()
}

func writeModelViewerFloat32s(w io.Writer, values []float32) error {
	out := newModelViewerChunkWriter(w)
	out.float32s(values)
	return out.close()
}

func writeModelViewerUint32s(w io.Writer, values []uint32) error {
	out := newModelViewerChunkWriter(w)
	out.uint32s(values)
	return out.close()
}

func modelViewerColorBytes(colors []float32) []uint8 {
	if len(colors) == 0 {
		return nil
	}
	data := make([]uint8, len(colors))
	for index, value := range colors {
		data[index] = uint8(math.Round(float64(min(max(value, 0), 1)) * 255))
	}
	return data
}
