package modelviewer

import (
	"fmt"
	"os"
	"strings"
)

// DrawSource locates the INI draw call behind one viewer mesh so a tool can
// hook that draw. Face N of the mesh is triangle N of the draw: compaction
// renumbers vertices but keeps the triangle order.
type DrawSource struct {
	INIPath    string
	Section    string
	IBPath     string
	IBFormat   string
	StartIndex int
	IndexCount int
	BaseVertex int
	Auto       bool
	// Reversed is set when the viewer swapped the winding of every triangle.
	Reversed bool
}

// DrawSource returns the draw behind meshID of a loaded viewer session.
//
//wails:ignore
func (t *Service) DrawSource(sessionID, meshID string) (DrawSource, bool) {
	t.modelViewerMu.Lock()
	defer t.modelViewerMu.Unlock()
	session, exists := t.modelViewerSessions[strings.TrimSpace(sessionID)]
	if !exists {
		return DrawSource{}, false
	}
	source, exists := session.drawSources[meshID]
	return source, exists
}

// TriangleVertices reads the vertex indices of one draw triangle in index
// buffer order, with BaseVertex already applied.
func (d DrawSource) TriangleVertices(face int) ([3]uint32, error) {
	first := d.StartIndex + face*3
	if face < 0 || (!d.Auto && face*3+3 > d.IndexCount) {
		return [3]uint32{}, fmt.Errorf("triangle %d is outside draw of %d indices", face, d.IndexCount)
	}
	raw, err := os.ReadFile(d.IBPath)
	if err != nil {
		return [3]uint32{}, fmt.Errorf("read index buffer %q: %w", d.IBPath, err)
	}
	indices, err := modelViewerDecodeIndices(raw, d.IBFormat)
	if err != nil {
		return [3]uint32{}, fmt.Errorf("decode index buffer %q: %w", d.IBPath, err)
	}
	if first < 0 || first+3 > len(indices) {
		return [3]uint32{}, fmt.Errorf("triangle %d is outside index buffer %q", face, d.IBPath)
	}
	var vertices [3]uint32
	for corner := range vertices {
		vertices[corner] = indices[first+corner] + uint32(d.BaseVertex)
	}
	return vertices, nil
}
