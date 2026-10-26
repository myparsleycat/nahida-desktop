package modelviewer

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// DrawSource locates the INI draw call behind one viewer mesh so a tool can
// hook that draw. Face N of the mesh is triangle N of the draw: compaction
// renumbers vertices but keeps the triangle order.
type DrawSource struct {
	INIPath string
	Section string
	// IB is the resource bound as the draw's index buffer, as ib = names it
	// without the Resource prefix; empty when unknown.
	IB string
	// IBAliases names every index buffer resource of the INI that loads
	// IBPath, IB included, such as the one per frame of a frame animation.
	IBAliases  []string
	IBPath     string
	IBFormat   string
	StartIndex int
	IndexCount int
	BaseVertex int
	Auto       bool
	// Implicit is set when the override only binds the index buffer and has
	// no draw of its own: another override of the same hash, usually the one
	// with handling = skip, draws whatever buffer is bound with
	// drawindexed = auto.
	Implicit bool
	// Reversed is set when the viewer swapped the winding of every triangle.
	Reversed bool

	vertexBuffer string
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

// MatchesIB reports whether the INI value of an ib = assignment binds the
// draw's index buffer, under its own name or an alias. Any value matches a
// draw whose buffer is unknown.
func (d DrawSource) MatchesIB(value string) bool {
	if d.IB == "" {
		return true
	}
	name := modelViewerNormalizeKey(modelViewerTrimResourcePrefix(value))
	return name == modelViewerNormalizeKey(d.IB) || slices.ContainsFunc(d.IBAliases, func(alias string) bool {
		return name == modelViewerNormalizeKey(alias)
	})
}

// linkModelViewerDrawSources gives each draw the aliases of its index buffer
// and every mesh a DrawKey shared by the meshes of one draw. A frame animation
// that binds one index buffer resource per frame yields a mesh per frame,
// all drawing the same triangles. Aliases stay within one override section
// and one index range: another section loading the same file draws its own
// part, and a command list both run must still be refused as a shared draw.
// They also need the meshes to exclude each other by condition, as frames do;
// two parts drawn one after the other from the same index file are separate
// draws. Meshes that are not frames of one sequence share a DrawKey only when
// they draw over the same vertex buffer, and one frame binding several vertex
// buffers leaves the sequence unconfirmed.
func linkModelViewerDrawSources(transport *ModelViewerTransport, sources map[string]DrawSource) {
	conditions := make(map[string]ModelViewerDNF, len(transport.Meshes))
	for _, mesh := range transport.Meshes {
		conditions[mesh.ID] = mesh.Conditions
	}
	members := make(map[string][]string)
	for id, source := range sources {
		if source.IB == "" || source.IBPath == "" {
			continue
		}
		draw := fmt.Sprintf(
			"%s\x00%s\x00%s\x00%s\x00%d:%d:%d:%t",
			strings.ToLower(filepath.Clean(source.INIPath)), strings.ToLower(source.Section),
			strings.ToLower(filepath.Clean(source.IBPath)), strings.ToUpper(source.IBFormat),
			source.StartIndex, source.IndexCount, source.BaseVertex, source.Auto,
		)
		members[draw] = append(members[draw], id)
	}

	// An unknown vertex buffer cannot be told apart, so it stays its own draw.
	vertexBuffer := func(id string) string {
		if file := sources[id].vertexBuffer; file != "" {
			return strings.ToLower(filepath.Clean(file))
		}
		return "\x00" + id
	}
	meshDraws := make(map[string]string, len(sources))
	for draw, ids := range members {
		slices.Sort(ids)
		names, exclusive := modelViewerExclusiveIBNames(ids, sources, conditions)
		frameBuffers := make(map[string]string, len(names))
		frames := exclusive
		for _, id := range ids {
			name, buffer := modelViewerNormalizeKey(sources[id].IB), vertexBuffer(id)
			if known, exists := frameBuffers[name]; exists && known != buffer {
				frames = false
			}
			frameBuffers[name] = buffer
		}
		for _, id := range ids {
			if exclusive && len(names) > 1 {
				source := sources[id]
				source.IBAliases = names
				sources[id] = source
			}
			meshDraws[id] = draw
			if !frames {
				meshDraws[id] += "\x00" + modelViewerNormalizeKey(sources[id].IB) + "\x00" + vertexBuffer(id)
			}
		}
	}

	drawKeys := make(map[string]string)
	for index := range transport.Meshes {
		mesh := &transport.Meshes[index]
		draw, exists := meshDraws[mesh.ID]
		if !exists {
			continue
		}
		if _, exists := drawKeys[draw]; !exists {
			drawKeys[draw] = "draw" + strconv.Itoa(len(drawKeys))
		}
		mesh.DrawKey = drawKeys[draw]
	}
}

// modelViewerExclusiveIBNames lists the index buffer names of the draws ids,
// sorted, when every two draws under different names can never run together.
// A condition left empty proves nothing, so it keeps the draws apart.
func modelViewerExclusiveIBNames(
	ids []string,
	sources map[string]DrawSource,
	conditions map[string]ModelViewerDNF,
) ([]string, bool) {
	var names []string
	for index, id := range ids {
		name := modelViewerNormalizeKey(sources[id].IB)
		for _, other := range ids[index+1:] {
			left, right := conditions[id], conditions[other]
			if name != modelViewerNormalizeKey(sources[other].IB) &&
				(len(left) == 0 || len(right) == 0 || modelViewerDNFIntersects(left, right)) {
				return nil, false
			}
		}
		if !slices.ContainsFunc(names, func(alias string) bool { return modelViewerNormalizeKey(alias) == name }) {
			names = append(names, sources[id].IB)
		}
	}
	slices.Sort(names)
	return names, true
}

// TriangleVertices reads the vertex indices of one draw triangle in index
// buffer order, with BaseVertex already applied.
func (d DrawSource) TriangleVertices(face int) ([3]uint32, error) {
	first := d.StartIndex + face*3
	if face < 0 || (!d.Auto && face*3+3 > d.IndexCount) {
		return [3]uint32{}, fmt.Errorf("triangle %d is outside draw of %d indices", face, d.IndexCount)
	}
	width, err := modelViewerIndexBytes(d.IBFormat)
	if err != nil {
		return [3]uint32{}, fmt.Errorf("decode index buffer %q: %w", d.IBPath, err)
	}

	// Only the picked triangle is read; the whole buffer can be many megabytes.
	file, err := os.Open(d.IBPath)
	if err != nil {
		return [3]uint32{}, fmt.Errorf("read index buffer %q: %w", d.IBPath, err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return [3]uint32{}, fmt.Errorf("read index buffer %q: %w", d.IBPath, err)
	}
	if info.Size()%int64(width) != 0 {
		return [3]uint32{}, fmt.Errorf(
			"decode index buffer %q: byte length %d is not divisible by %d",
			d.IBPath,
			info.Size(),
			width,
		)
	}
	if first < 0 || int64(first+3)*int64(width) > info.Size() {
		return [3]uint32{}, fmt.Errorf("triangle %d is outside index buffer %q", face, d.IBPath)
	}
	var raw [12]byte
	if _, err := file.ReadAt(raw[:3*width], int64(first)*int64(width)); err != nil {
		return [3]uint32{}, fmt.Errorf("read index buffer %q: %w", d.IBPath, err)
	}
	indices, err := modelViewerDecodeIndices(raw[:3*width], d.IBFormat)
	if err != nil {
		return [3]uint32{}, fmt.Errorf("decode index buffer %q: %w", d.IBPath, err)
	}
	var vertices [3]uint32
	for corner := range vertices {
		vertices[corner] = indices[first+corner] + uint32(d.BaseVertex)
	}
	return vertices, nil
}
