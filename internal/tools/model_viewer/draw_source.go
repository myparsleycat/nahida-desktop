package modelviewer

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// DrawSource locates the INI draw call behind one viewer mesh so a tool can
// hook that draw. Removed invalid triangles retain their original draw indices
// through sourceFaces; vertex compaction does not change that mapping.
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
	sourceFaces  []uint32
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
// buffers leaves the sequence unconfirmed. Surface tracking can share a face
// index only when the meshes kept the same original triangles after recovery.
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

	type surfaceKey struct {
		draw  string
		faces [sha256.Size]byte
	}
	drawKeys := make(map[surfaceKey]string)
	for index := range transport.Meshes {
		mesh := &transport.Meshes[index]
		draw, exists := meshDraws[mesh.ID]
		if !exists {
			continue
		}
		key := surfaceKey{draw: draw}
		if faces := sources[mesh.ID].sourceFaces; len(faces) != 0 {
			key.faces = sha256.Sum256(modelViewerUint32Bytes(faces))
		}
		if _, exists := drawKeys[key]; !exists {
			drawKeys[key] = "draw" + strconv.Itoa(len(drawKeys))
		}
		mesh.DrawKey = drawKeys[key]
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

// SourceFaceIndex maps a viewer face to the original draw before invalid
// triangles were removed. The result is relative to StartIndex.
func (d DrawSource) SourceFaceIndex(viewerFace int) (int, error) {
	if viewerFace < 0 || d.sourceFaces != nil && viewerFace >= len(d.sourceFaces) {
		return 0, fmt.Errorf("triangle %d is outside viewer mesh", viewerFace)
	}
	face := viewerFace
	if d.sourceFaces != nil {
		face = int(d.sourceFaces[viewerFace])
	}
	if !d.Auto && face >= d.IndexCount/3 {
		return 0, fmt.Errorf("triangle %d is outside draw of %d indices", face, d.IndexCount)
	}
	return face, nil
}

// TriangleVertices reads a viewer triangle in original index buffer order,
// with BaseVertex already applied.
func (d DrawSource) TriangleVertices(face int) ([3]uint32, error) {
	face, err := d.SourceFaceIndex(face)
	if err != nil {
		return [3]uint32{}, err
	}
	raw, err := os.ReadFile(d.IBPath)
	if err != nil {
		return [3]uint32{}, fmt.Errorf("read index buffer %q: %w", d.IBPath, err)
	}
	indices, err := modelViewerDecodeIndices(raw, d.IBFormat)
	if err != nil {
		return [3]uint32{}, fmt.Errorf("decode index buffer %q: %w", d.IBPath, err)
	}
	if d.StartIndex < 0 || d.StartIndex > len(indices) || face >= (len(indices)-d.StartIndex)/3 {
		return [3]uint32{}, fmt.Errorf("triangle %d is outside index buffer %q", face, d.IBPath)
	}
	first := d.StartIndex + face*3
	var vertices [3]uint32
	for corner := range vertices {
		vertices[corner] = indices[first+corner] + uint32(d.BaseVertex)
	}
	return vertices, nil
}
