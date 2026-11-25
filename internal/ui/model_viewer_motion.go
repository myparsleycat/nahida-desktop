package ui

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/d3d"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/setting"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

var errMotionNoBlend = errors.New("no mesh has bone weights")

// viewerMotionNoNode is the node index of a bone weight that moves with no node of the skeleton.
const viewerMotionNoNode = 0xffff

// viewerMotion plays the game motions of the mod's character. The vertex shaders skin whatever geometry the mod's
// own animation shows with the meshes' bone weights and the pose of the motion's frame, as the game skins the mod's
// buffers. Everything belongs to the UI thread.
type viewerMotion struct {
	scene  *viewerScene
	motion *modelviewer.CharacterMotion
	clips  []modelviewer.ModelViewerAnimationClip
	play   viewerPlayback
	meshes []viewerMotionMesh
	cancel context.CancelFunc
	pose   *modelviewer.CharacterMotionPose
	// bones holds the pose shown, and posed numbers it for the GPU copy and the shadow map. posedAt is where in
	// the clips bones was evaluated.
	bones   [][12]float32
	posed   uint64
	posedAt viewerMotionPoseAt
	parents []int32
	// boxes bounds the rest positions each node moves, the last one those that move with no node. clipBoxes bounds
	// them over every pose of each clip and clipBox over the clip playing, so the shadow volume stays still while it
	// plays, widened to the poses shown while it fades in; restBox bounds the meshes the motion does not pose. All
	// are in the mod's space.
	boxes            []viewerMotionBox
	clipBoxes        []viewerMotionBox
	clipBoxesValid   []bool
	clipBox, restBox viewerMotionBox
	// fadeStart is when the pose shown at the last change of clip began fading into the new clip, or zero once the
	// fade is over. fadeLength is how long the fade takes at the clip's own rate. fadeStowed marks the nodes the
	// clips faded from put out of sight, hidden until the fade ends.
	fadeStart  time.Time
	fadeLength time.Duration
	fadeStowed []bool
	// palettes maps each blend palette's groups to skeleton nodes, as packMotionBlend takes them; a negative entry
	// maps a group to no node. paletteEvidence counts, by palette and group, the most weighted vertices MapPalettes has
	// seen for the group.
	palettes        map[string][]int32
	paletteEvidence map[string][]int32
	// blendLoads cancels, by scene mesh, the bone weight read or palette mapping still running for it.
	blendLoads map[int]context.CancelFunc
	// anchorNodes holds, by scene mesh, the node that turns the mesh's face direction, or -1.
	anchorNodes []int32
	// gpu is the motion's buffers on the device the scene draws on, made by the first draw, and gpuErr why they
	// could not be made or written, at gpuStage.
	gpu      *viewerSkinGPU
	gpuErr   error
	gpuStage string
}

// viewerMotionNodeBox holds a mesh's bounding box for one skeleton node, or -1 for vertices weighted to no node.
type viewerMotionNodeBox struct {
	node int32
	box  viewerMotionBox
}

// viewerMotionMesh is a mesh the motion poses: its bone weights as the shaders read them, four uint32 per vertex
// (see skinBlend in model_viewer.hlsl), and its per-node rest boxes.
type viewerMotionMesh struct {
	index    int
	vertices int
	blend    []uint32
	boxes    []viewerMotionNodeBox
}

// viewerMotionBox is an axis-aligned box; ok is false while it holds no point.
type viewerMotionBox struct {
	minimum, maximum vec3
	ok               bool
}

func (b *viewerMotionBox) add(p vec3) {
	if !b.ok {
		b.minimum, b.maximum, b.ok = p, p, true
		return
	}
	for axis := range 3 {
		b.minimum[axis] = min(b.minimum[axis], p[axis])
		b.maximum[axis] = max(b.maximum[axis], p[axis])
	}
}

func (b *viewerMotionBox) merge(other viewerMotionBox) {
	if !other.ok {
		return
	}
	b.add(other.minimum)
	b.add(other.maximum)
}

var viewerMotionIdentity = [12]float32{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0}

// viewerMotionHidden is the bone matrix of a node put out of sight; the shaders drop the vertices it mostly carries
// (see skin in model_viewer.hlsl).
var viewerMotionHidden [12]float32

// hideStowed sets the bones of the nodes stowed marks to viewerMotionHidden.
func hideStowed(bones [][12]float32, stowed []bool) {
	for i, hidden := range stowed {
		if hidden && i < len(bones) {
			bones[i] = viewerMotionHidden
		}
	}
}

// unionStowed marks the nodes a or b marks; it returns a or b itself when the other marks none.
func unionStowed(a, b []bool) []bool {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	out := make([]bool, max(len(a), len(b)))
	for i := range out {
		out[i] = i < len(a) && a[i] || i < len(b) && b[i]
	}
	return out
}

// motionNode is the skeleton node palette maps a bone weight's group to, or viewerMotionNoNode.
func motionNode(group uint16, palette []int32) uint32 {
	if int(group) < len(palette) && palette[group] >= 0 && palette[group] < viewerMotionNoNode {
		return uint32(palette[group])
	}
	return viewerMotionNoNode
}

// packMotionBlend lays out a mesh's bone weights, eight uint16 per vertex (four palette groups, then their four
// weights), for the shaders: each group becomes the skeleton node palette maps it to, or viewerMotionNoNode.
func packMotionBlend(blend []uint16, palette []int32) []uint32 {
	vertices := len(blend) / 8
	packed := make([]uint32, vertices*4)
	for vertex := range vertices {
		var nodes [4]uint32
		for k := range 4 {
			nodes[k] = motionNode(blend[vertex*8+k], palette)
		}
		weights := blend[vertex*8+4 : vertex*8+8]
		packed[vertex*4] = nodes[0] | nodes[1]<<16
		packed[vertex*4+1] = nodes[2] | nodes[3]<<16
		packed[vertex*4+2] = uint32(weights[0]) | uint32(weights[1])<<16
		packed[vertex*4+3] = uint32(weights[2]) | uint32(weights[3])<<16
	}
	return packed
}

// decodeMotionBlend reads a mesh's bone weights file, eight little-endian uint16 per vertex.
func decodeMotionBlend(id string, data []byte, vertices int) ([]uint16, error) {
	if len(data) != vertices*16 {
		return nil, fmt.Errorf("mesh %s bone weights do not match its %d vertices", id, vertices)
	}
	blend := make([]uint16, len(data)/2)
	for i := range blend {
		blend[i] = binary.LittleEndian.Uint16(data[i*2:])
	}
	return blend, nil
}

// motionGroupVertices counts, by palette group, the vertices blend weights to the group.
func motionGroupVertices(blend []uint16) []int32 {
	var counts []int32
	for vertex := range len(blend) / 8 {
		slots := blend[vertex*8 : vertex*8+8]
		for k := range 4 {
			group := int(slots[k])
			if slots[4+k] == 0 || group >= viewerMotionNoNode {
				continue
			}
			repeated := false
			for j := range k {
				repeated = repeated || slots[4+j] != 0 && int(slots[j]) == group
			}
			if repeated {
				continue
			}
			for len(counts) <= group {
				counts = append(counts, 0)
			}
			counts[group]++
		}
	}
	return counts
}

// recordMappingEvidence notes that one MapPalettes call has seen the vertices blends weight to each group of palette,
// so that a group it could not place is mapped again only once more vertices are weighted to it.
func (m *viewerMotion) recordMappingEvidence(palette string, blends ...[]uint16) {
	var seen []int32
	for _, blend := range blends {
		for group, count := range motionGroupVertices(blend) {
			for len(seen) <= group {
				seen = append(seen, 0)
			}
			seen[group] += count
		}
	}

	if m.paletteEvidence == nil {
		m.paletteEvidence = map[string][]int32{}
	}
	evidence := m.paletteEvidence[palette]
	for group, count := range seen {
		for len(evidence) <= group {
			evidence = append(evidence, 0)
		}
		evidence[group] = max(evidence[group], count)
	}
	m.paletteEvidence[palette] = evidence
}

// beginBlendLoad cancels the bone weight work still running for scene mesh i and returns the context of the next.
func (m *viewerMotion) beginBlendLoad(parent context.Context, i int) context.Context {
	m.cancelBlendLoad(i)
	ctx, cancel := context.WithCancel(parent)
	if m.blendLoads == nil {
		m.blendLoads = map[int]context.CancelFunc{}
	}
	m.blendLoads[i] = cancel
	return ctx
}

func (m *viewerMotion) cancelBlendLoad(i int) {
	if cancel, ok := m.blendLoads[i]; ok {
		cancel()
		delete(m.blendLoads, i)
	}
}

// characterMotionAvailable reports whether game motions exist for the mod's character and a mesh of the scene has
// bone weights to play them with. A tool edits and picks the geometry the mod shows, so a viewer with one plays none.
func (v *ModelViewer) characterMotionAvailable() bool {
	c := v.character
	if c == nil || v.scene == nil || v.tools == nil || v.tool != nil || !modelviewer.HasCharacterMotion(c.game, c.ID) {
		return false
	}
	return slices.ContainsFunc(v.scene.transport.Meshes, func(mesh modelviewer.ModelViewerMeshTransport) bool {
		return mesh.BlendURL != "" && mesh.BlendPalette != ""
	})
}

// motionWanted reports whether the viewer plays the character's motions: the setting, unless they were dismissed
// in this viewer. known is false while the setting loads. Settings that cannot be read leave the motions on.
func (v *ModelViewer) motionWanted() (wanted, known bool) {
	if v.motionDismissed {
		return false, true
	}
	s := v.shell
	if s == nil {
		return true, true
	}
	if s.settingsLoaded(setting.KeyModelViewerCharacterMotion) {
		return s.boolSetting(setting.KeyModelViewerCharacterMotion), true
	}
	return s.settings.loadErr != nil, s.settings.loadErr != nil
}

// motionKey names the motions of the character shown for the scene shown. Call it while both are set.
func (v *ModelViewer) motionKey() viewerCharacterKey {
	return viewerCharacterKey{scene: v.scene, character: v.character.styleKey()}
}

// setCharacterMotion turns the character's motions on or off for every viewer. Turning them on also retries
// motions that failed and shows them again where they were dismissed.
func (v *ModelViewer) setCharacterMotion(enabled bool) {
	v.motionFailed, v.motionDismissed = viewerCharacterKey{}, false
	v.setRenderSetting(setting.KeyModelViewerCharacterMotion, enabled)
	if !enabled {
		v.stopMotion()
	}
}

// dismissMotion turns the character's motions off in this viewer only.
func (v *ModelViewer) dismissMotion() {
	v.motionDismissed = true
	v.stopMotion()
}

// syncMotion starts the character's motions for the scene shown when they are wanted, or stops them when they are
// not or no longer apply. Call it on each build.
func (v *ModelViewer) syncMotion() {
	wanted, known := v.motionWanted()
	if !known {
		return
	}
	if !wanted || !v.characterMotionAvailable() {
		v.stopMotion()
		return
	}
	if v.charMotion != nil || v.motionCancel != nil || v.motionFailed == v.motionKey() {
		return
	}
	v.startCharacterMotion(v.motionKey())
}

// viewerMotionSource is a UI-thread snapshot of a scene mesh used when loading character motions off-thread.
type viewerMotionSource struct {
	info      modelviewer.ModelViewerMeshTransport
	averaged  bool
	positions []float32
}

// changedMotionSources lists the meshes whose geometry or bone weights differ from sources; ok is false when the
// scene has another number of meshes.
func changedMotionSources(meshes []*viewerMesh, sources []viewerMotionSource) (changed []int, ok bool) {
	if len(meshes) != len(sources) {
		return nil, false
	}
	for i, mesh := range meshes {
		if mesh.info.GeometryURL != sources[i].info.GeometryURL ||
			mesh.info.BlendURL != sources[i].info.BlendURL ||
			mesh.info.BlendPalette != sources[i].info.BlendPalette {
			changed = append(changed, i)
		}
	}
	return changed, true
}

// startCharacterMotion loads the character's motions into their own lane of the playback controller. A load that
// fails marks key so that it does not load again by itself.
func (v *ModelViewer) startCharacterMotion(key viewerCharacterKey) {
	scene, c := v.scene, v.character
	sources := make([]viewerMotionSource, len(scene.meshes))
	for i, mesh := range scene.meshes {
		sources[i] = viewerMotionSource{info: mesh.info, averaged: mesh.averagedOutline}
		if mesh.info.BlendURL != "" && !mesh.unloaded && !mesh.cpuDropped && mesh.positions != nil {
			sources[i].positions = mesh.positions
		}
	}
	ctx, cancel := context.WithCancel(v.ctx)
	v.motionCancel = cancel
	v.revision++
	post := v.poster()
	run := v.downloads.begin(downloadMotion, post)
	tools, read := v.tools, v.read
	go func() {
		m, err := loadViewerMotion(ctx, tools, read, sources, c.game, c.ID, run.report)
		post(func() {
			run.end()
			if v.motionCancel == nil || ctx.Err() != nil || v.scene != scene {
				cancel()
				return
			}
			v.motionCancel = nil
			v.revision++
			changed, sameMeshes := changedMotionSources(scene.meshes, sources)
			if err != nil || !sameMeshes {
				cancel()
				if (len(changed) > 0 || !sameMeshes) && v.characterMotionAvailable() {
					v.startCharacterMotion(key)
					return
				}
				if err != nil {
					v.motionFailed = key
					v.reportMotionError(err, c)
				}
				return
			}
			m.scene = scene
			m.cancel = cancel
			v.charMotion = m
			v.startMotion(m.defaultClip())
			if v.sequence != nil {
				v.restartShot()
			} else if v.wallpaper != nil {
				v.restoreWallpaperTrack(trackMotion, v.wallpaper.Motion)
			}
			// A Blender live link replaced these meshes while the motion loaded.
			for _, i := range changed {
				m.replaceBlend(i, "", nil, nil)
				v.reloadMotionBlend(scene, m, i)
			}
		})
	}()
}

// defaultClip is the first idle clip. Star Rail motions have no groups, so their idle clip is found by name.
func (m *viewerMotion) defaultClip() string {
	clips := m.motion.Clips
	index := slices.IndexFunc(clips, func(clip modelviewer.CharacterMotionClip) bool { return clip.Group == "idle" })
	if index < 0 {
		index = max(slices.IndexFunc(clips, func(clip modelviewer.CharacterMotionClip) bool {
			return strings.EqualFold(clip.Name, "idle")
		}), 0)
	}
	return m.clips[index].ID
}

func (v *ModelViewer) reportMotionError(err error, c *viewerCharacter) {
	if infra.IsCancellationError(err) {
		return
	}
	if errors.Is(err, errMotionNoBlend) {
		v.toasts.Show(kit.ToastError, T("page.tools.model_viewer.toast.motion_no_blend"), "")
		return
	}
	err = infra.ReportError(v.log, err, "ModelViewer.characterMotion", infra.Diagnostic{
		Operation: "load-character-motion",
		Fields:    map[string]any{"modPath": v.path, "game": c.game, "character": c.ID},
	})
	v.toasts.Show(kit.ToastError, T("page.tools.model_viewer.toast.motion_error"), err.Error())
}

// loadViewerMotion reads the character's motion and the bone weights of every mesh that has them, and maps each
// bone palette onto the skeleton by the meshes' base positions from sources. progress receives the motion file's
// download progress.
func loadViewerMotion(
	ctx context.Context,
	tools interface {
		LoadCharacterMotion(
			context.Context, string, string, modelviewer.CharacterFileProgress,
		) (*modelviewer.CharacterMotion, error)
	},
	read viewerReader,
	sources []viewerMotionSource,
	game, id string,
	progress modelviewer.CharacterFileProgress,
) (*viewerMotion, error) {
	motion, err := tools.LoadCharacterMotion(ctx, game, id, progress)
	if err != nil {
		return nil, err
	}
	if len(motion.Clips) == 0 {
		return nil, fmt.Errorf("character motion %s/%s has no clips", game, id)
	}

	type weighted struct {
		index     int
		positions []float32
		blend     []uint16
	}
	palettes := map[string][]weighted{}
	var order []string
	for index, source := range sources {
		info := source.info
		if info.BlendURL == "" || info.BlendPalette == "" {
			continue
		}
		data, err := read(ctx, info.BlendURL)
		if err != nil {
			return nil, fmt.Errorf("mesh %s bone weights: %w", info.ID, err)
		}
		positions := source.positions
		if positions == nil {
			decoded, err := readViewerMesh(ctx, read, info, source.averaged)
			if err != nil {
				return nil, fmt.Errorf("mesh %s: %w", info.ID, err)
			}
			positions = decoded.positions
		}
		blend, err := decodeMotionBlend(info.ID, data, len(positions)/3)
		if err != nil {
			return nil, err
		}
		if _, ok := palettes[info.BlendPalette]; !ok {
			order = append(order, info.BlendPalette)
		}
		palettes[info.BlendPalette] = append(palettes[info.BlendPalette],
			weighted{index: index, positions: positions, blend: blend})
	}
	if len(order) == 0 {
		return nil, errMotionNoBlend
	}

	nodes := motion.NodeCount()
	parents := make([]int32, nodes)
	for i := range parents {
		parents[i] = int32(motion.NodeParent(i))
	}
	m := &viewerMotion{
		motion: motion, pose: motion.NewPose(), bones: make([][12]float32, nodes), parents: parents,
		boxes: make([]viewerMotionBox, nodes+1), clipBoxes: make([]viewerMotionBox, len(motion.Clips)),
		clipBoxesValid: make([]bool, len(motion.Clips)), anchorNodes: make([]int32, len(sources)),
	}
	for i := range m.bones {
		m.bones[i] = viewerMotionIdentity
	}
	for i := range m.anchorNodes {
		m.anchorNodes[i] = -1
	}
	paletteSources := make([]modelviewer.MotionPalette, len(order))
	for i, name := range order {
		for _, mesh := range palettes[name] {
			paletteSources[i].Positions = append(paletteSources[i].Positions, mesh.positions...)
			paletteSources[i].Blend = append(paletteSources[i].Blend, mesh.blend...)
		}
	}
	mapped, err := motion.MapPalettes(ctx, paletteSources)
	if err != nil {
		return nil, err
	}

	totals := make([]float64, nodes)
	m.palettes = make(map[string][]int32, len(order))
	for i, name := range order {
		palette := mapped[i]
		m.palettes[name] = palette
		blends := make([][]uint16, len(palettes[name]))
		for k, mesh := range palettes[name] {
			blends[k] = mesh.blend
		}
		m.recordMappingEvidence(name, blends...)
		for _, mesh := range palettes[name] {
			clear(totals)
			meshBoxes := buildMotionMeshBoxes(nodes, totals, mesh.positions, mesh.blend, palette)
			mergeMotionBoxes(m.boxes, nodes, meshBoxes)
			m.meshes = append(m.meshes, viewerMotionMesh{
				index: mesh.index, vertices: len(mesh.blend) / 8, blend: packMotionBlend(mesh.blend, palette),
				boxes: meshBoxes,
			})
			m.anchorNodes[mesh.index] = anchorNode(totals, parents)
		}
	}

	for i, source := range sources {
		posed := slices.ContainsFunc(m.meshes, func(posed viewerMotionMesh) bool { return posed.index == i })
		if !posed && source.info.Bounds != nil {
			m.restBox.add(source.info.Bounds.Min)
			m.restBox.add(source.info.Bounds.Max)
		}
	}

	pose, bones := motion.NewPose(), make([][12]float32, nodes)
	for i, clip := range motion.Clips {
		box, err := clipBox(pose, m.boxes, bones, i, clip.Frames, motion.StowedNodes(i))
		if err != nil {
			return nil, fmt.Errorf("clip %q: %w", clip.Name, err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		m.clipBoxes[i], m.clipBoxesValid[i] = box, true

		frames := make([]modelviewer.ModelViewerAnimationFrame, clip.Frames)
		for f := range frames {
			frames[f] = modelviewer.ModelViewerAnimationFrame{
				Index: f,
				Time:  float64(f) / motion.FPS,
			}
		}
		m.clips = append(m.clips, modelviewer.ModelViewerAnimationClip{
			ID: strconv.Itoa(i), Label: clip.Name, FPS: motion.FPS,
			FrameStart: 0, FrameEnd: clip.Frames - 1, Loop: clip.Loop, LoopStart: clip.LoopStart, Frames: frames,
		})
	}
	return m, nil
}

// addMotionBoxes adds each finite vertex of a mesh to the box of every node that moves it and sums the weights of
// each node into totals. Vertices with no valid mapped node go into the last box.
func addMotionBoxes(boxes []viewerMotionBox, totals []float64, positions []float32, blend []uint16, palette []int32) {
	for vertex := range min(len(positions)/3, len(blend)/8) {
		x, y, z := positions[vertex*3], positions[vertex*3+1], positions[vertex*3+2]
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) ||
			math.IsNaN(float64(y)) || math.IsInf(float64(y), 0) ||
			math.IsNaN(float64(z)) || math.IsInf(float64(z), 0) {
			continue
		}
		p := vec3{float64(x), float64(y), float64(z)}
		mapped := false
		for k := range 4 {
			weight := blend[vertex*8+4+k]
			if weight == 0 {
				continue
			}
			node := motionNode(blend[vertex*8+k], palette)
			if node == viewerMotionNoNode || int(node) >= len(totals) {
				continue
			}
			boxes[node].add(p)
			totals[node] += float64(weight)
			mapped = true
		}
		if !mapped {
			boxes[len(boxes)-1].add(p)
		}
	}
}

func buildMotionMeshBoxes(
	nodes int,
	totals []float64,
	positions []float32,
	blend []uint16,
	palette []int32,
) []viewerMotionNodeBox {
	local := make([]viewerMotionBox, nodes+1)
	addMotionBoxes(local, totals, positions, blend, palette)
	var out []viewerMotionNodeBox
	for i, box := range local {
		if !box.ok {
			continue
		}
		node := int32(i)
		if i == nodes {
			node = -1
		}
		out = append(out, viewerMotionNodeBox{node: node, box: box})
	}
	return out
}

func mergeMotionBoxes(dst []viewerMotionBox, nodes int, entries []viewerMotionNodeBox) {
	for _, entry := range entries {
		index := len(dst) - 1
		if entry.node >= 0 && int(entry.node) < nodes {
			index = int(entry.node)
		}
		dst[index].merge(entry.box)
	}
}

// anchorNode is the deepest node whose subtree carries at least half of a mesh's bone weight, by the weight totals
// of each node and its parent (-1 for a root), the larger subtree breaking a tie, or -1 for a mesh with no weight. A
// face weighted to the neck, head, jaw and eyes anchors on the head.
func anchorNode(totals []float64, parents []int32) int32 {
	depths := make([]int, len(totals))
	order := make([]int, len(totals))
	var all float64
	for i, total := range totals {
		all += total
		order[i] = i
		for p := parents[i]; p >= 0 && depths[i] <= len(totals); p = parents[p] {
			depths[i]++
		}
	}
	if all <= 0 {
		return -1
	}

	slices.SortFunc(order, func(a, b int) int { return depths[b] - depths[a] })
	subtree := slices.Clone(totals)
	for _, i := range order {
		if p := parents[i]; p >= 0 {
			subtree[p] += subtree[i]
		}
	}
	anchor := int32(-1)
	for _, i := range order {
		if subtree[i]*2 < all {
			continue
		}
		if anchor < 0 || depths[i] > depths[anchor] || depths[i] == depths[anchor] && subtree[i] > subtree[anchor] {
			anchor = int32(i)
		}
	}
	return anchor
}

// motionBounds bounds the boxes as bones moves them, boxes[i] by bones[i] and the last box as it is, leaving out
// the boxes of hidden nodes. A vertex skins to a weighted mean of its nodes' moves, so it lies within the box around
// all the moved boxes this returns.
func motionBounds(boxes []viewerMotionBox, bones [][12]float32) (minimum, maximum vec3, ok bool) {
	var all viewerMotionBox
	for i, box := range boxes {
		if !box.ok {
			continue
		}
		bone := &viewerMotionIdentity
		if i < len(bones) {
			bone = &bones[i]
		}
		if *bone == viewerMotionHidden {
			continue
		}
		for corner := range 8 {
			p := box.minimum
			for axis := range 3 {
				if corner&(1<<axis) != 0 {
					p[axis] = box.maximum[axis]
				}
			}
			var moved vec3
			for row := range 3 {
				moved[row] = float64(bone[row*4])*p[0] + float64(bone[row*4+1])*p[1] + float64(bone[row*4+2])*p[2] +
					float64(bone[row*4+3])
			}
			all.add(moved)
		}
	}
	return all.minimum, all.maximum, all.ok
}

// anchor is the node that turns the face direction of scene mesh i, or -1.
func (m *viewerMotion) anchor(i int) int32 {
	if m == nil || i < 0 || i >= len(m.anchorNodes) {
		return -1
	}
	return m.anchorNodes[i]
}

// turnBy turns forward, a direction in the mod's space, by node, or leaves it for -1.
func (m *viewerMotion) turnBy(node int32, forward vec3) vec3 {
	if node < 0 || int(node) >= len(m.bones) {
		return forward
	}
	bone := &m.bones[node]
	var turned vec3
	for row := range 3 {
		turned[row] = float64(bone[row*4])*forward[0] + float64(bone[row*4+1])*forward[1] +
			float64(bone[row*4+2])*forward[2]
	}
	if turned.dot(turned) == 0 {
		return forward
	}
	return turned.normalize()
}

// activeMotion is the loaded motion of the scene shown, or nil.
func (v *ModelViewer) activeMotion() *viewerMotion {
	if m := v.charMotion; m != nil && m.scene == v.scene {
		return m
	}
	return nil
}

// viewerMotionFade is how long the pose shown takes to blend into a newly selected clip played at its own rate.
const viewerMotionFade = 250 * time.Millisecond

// startMotion selects a motion clip and plays it from its first frame when it has more than one, blending into it
// from the pose shown.
func (v *ModelViewer) startMotion(clipID string) { v.startMotionFade(clipID, viewerMotionFade) }

// startMotionFade starts a motion clip as startMotion does, blending into it over fade, or showing it at once when
// fade is zero.
func (v *ModelViewer) startMotionFade(clipID string, fade time.Duration) {
	m := v.activeMotion()
	if m == nil {
		return
	}
	m.play.clipID = clipID
	m.play.frame, m.play.resume = 0, nil
	clip := v.trackClip(trackMotion)
	m.play.playing = clip != nil && len(clip.Frames) > 1
	m.clipBox = viewerMotionBox{}
	if clip != nil {
		m.clipBox = m.clipBoxFor(m.clipNumber(clip.ID))
	}
	held := m.fadeStowed
	if !m.fading() {
		held = nil
	}
	m.fadeStart, m.fadeStowed = time.Time{}, nil
	if fade > 0 && m.posed != 0 && m.pose.Hold() {
		m.fadeStart, m.fadeLength = time.Now(), fade
		m.fadeStowed = unionStowed(held, m.motion.StowedNodes(m.posedAt.clip))
	}
	v.runTrackClock(trackMotion)
	v.setMotionFrame(0)
}

// fadeProgress is the share of the fade played at now, at the motion's playback speed.
func (m *viewerMotion) fadeProgress(now time.Time) float64 {
	return now.Sub(m.fadeStart).Seconds() * m.play.rate() / m.fade().Seconds()
}

// fade is how long the fade into the clip takes at the clip's own rate.
func (m *viewerMotion) fade() time.Duration {
	if m.fadeLength <= 0 {
		return viewerMotionFade
	}
	return m.fadeLength
}

// motionHold is the share of the pose held at the last change of clip that the pose at now keeps, easing from 1 to
// 0 over the fade, and ends the fade once it reaches 0.
func (m *viewerMotion) motionHold(now time.Time) float64 {
	if m.fadeStart.IsZero() {
		return 0
	}
	elapsed := m.fadeProgress(now)
	if elapsed >= 1 {
		m.fadeStart = time.Time{}
		return 0
	}
	elapsed = max(elapsed, 0)
	return 1 - elapsed*elapsed*(3-2*elapsed)
}

// fading reports whether the pose is still blending into the clip selected last.
func (m *viewerMotion) fading() bool {
	return m != nil && !m.fadeStart.IsZero()
}

// clipBox bounds boxes over every pose of the motion's clip with frames frames, evaluated with pose into bones
// without the nodes stowed marks, or over the rest pose for a clip with no frames.
func clipBox(
	pose *modelviewer.CharacterMotionPose,
	boxes []viewerMotionBox,
	bones [][12]float32,
	clip, frames int,
	stowed []bool,
) (viewerMotionBox, error) {
	for i := range bones {
		bones[i] = viewerMotionIdentity
	}

	var box viewerMotionBox
	for frame := range max(frames, 1) {
		if frames > 0 {
			if err := pose.Evaluate(clip, frame, bones); err != nil {
				return box, fmt.Errorf("frame %d: %w", frame, err)
			}
			hideStowed(bones, stowed)
		}
		if minimum, maximum, ok := motionBounds(boxes, bones); ok {
			box.add(minimum)
			box.add(maximum)
		}
	}
	return box, nil
}

// clipBoxFor returns the cached clip bounds of index, computing them on demand without disturbing m.pose.
func (m *viewerMotion) clipBoxFor(index int) viewerMotionBox {
	if m.motion == nil || index < 0 || index >= len(m.motion.Clips) {
		return viewerMotionBox{}
	}
	if index < len(m.clipBoxesValid) && m.clipBoxesValid[index] {
		return m.clipBoxes[index]
	}
	if len(m.clipBoxes) != len(m.motion.Clips) {
		m.clipBoxes = make([]viewerMotionBox, len(m.motion.Clips))
	}
	if len(m.clipBoxesValid) != len(m.motion.Clips) {
		m.clipBoxesValid = make([]bool, len(m.motion.Clips))
	}
	clip := m.motion.Clips[index]
	pose, bones := m.motion.NewPose(), make([][12]float32, m.motion.NodeCount())
	box, err := clipBox(pose, m.boxes, bones, index, clip.Frames, m.motion.StowedNodes(index))
	if err != nil {
		return viewerMotionBox{}
	}
	m.clipBoxes[index], m.clipBoxesValid[index] = box, true
	return box
}

// rebuildBoxes recomputes m.boxes and m.restBox after mesh blend weights change, and the clip bounds only when
// m.boxes changed, since evaluating a clip's every pose is the costly part.
func (m *viewerMotion) rebuildBoxes() {
	nodes := len(m.bones)
	if m.motion != nil {
		nodes = m.motion.NodeCount()
	}
	boxes := make([]viewerMotionBox, nodes+1)
	for _, mesh := range m.meshes {
		mergeMotionBoxes(boxes, nodes, mesh.boxes)
	}
	boxesChanged := !slices.Equal(boxes, m.boxes)
	m.boxes = boxes

	m.restBox = viewerMotionBox{}
	if m.scene != nil {
		for i, mesh := range m.scene.meshes {
			posed := slices.ContainsFunc(m.meshes, func(posed viewerMotionMesh) bool { return posed.index == i })
			if !posed && mesh.info.Bounds != nil {
				m.restBox.add(mesh.info.Bounds.Min)
				m.restBox.add(mesh.info.Bounds.Max)
			}
		}
	}

	if !boxesChanged {
		return
	}
	clear(m.clipBoxesValid)
	m.clipBox = viewerMotionBox{}
	if len(m.clips) > 0 {
		m.clipBox = m.clipBoxFor(m.clipNumber(m.play.clipID))
	}
}

// clipNumber is the index in the motion of the clip with id.
func (m *viewerMotion) clipNumber(id string) int {
	return max(slices.IndexFunc(m.clips, func(c modelviewer.ModelViewerAnimationClip) bool { return c.ID == id }), 0)
}

// viewerMotionPoseAt is a pose a share t of the way from frame to next of clip, mixed with the share hold of the pose
// held at the last change of clip.
type viewerMotionPoseAt struct {
	clip, frame, next int
	t, hold           float64
}

// setMotionFrame poses the skeleton at frame index of the motion's clip, or past it by the share of the frame
// already played.
func (v *ModelViewer) setMotionFrame(index int) {
	m, clip := v.activeMotion(), v.trackClip(trackMotion)
	if m == nil || clip == nil || len(clip.Frames) == 0 {
		return
	}
	m.play.frame = min(max(index, 0), len(clip.Frames)-1)
	if err := v.poseMotion(time.Now()); err != nil {
		v.failMotionPlay(err, "evaluate-pose")
	}
}

// poseMotion poses the skeleton as playback has it at now, between the frame shown and the next and blending from
// the pose held at the last change of clip, unless it is posed there.
func (v *ModelViewer) poseMotion(now time.Time) error {
	m, clip := v.activeMotion(), v.trackClip(trackMotion)
	if m == nil || clip == nil || len(clip.Frames) == 0 {
		return nil
	}
	at := viewerMotionPoseAt{
		clip: m.clipNumber(clip.ID), frame: m.play.frame, next: m.play.frame, hold: m.motionHold(now),
	}
	if next, ok := v.nextFrame(trackMotion); ok {
		if t := v.interpolation(trackMotion, now); t > 0 {
			at.next, at.t = next, t
		}
	}
	if m.posed != 0 && at == m.posedAt {
		return nil
	}
	if err := m.pose.EvaluateFrom(at.hold, at.clip, at.frame, at.next, at.t, m.bones); err != nil {
		return err
	}
	hideStowed(m.bones, m.motion.StowedNodes(at.clip))
	if at.hold > 0 {
		hideStowed(m.bones, m.fadeStowed)
	}
	m.posedAt = at
	v.revision++
	m.posed = v.revision
	return nil
}

// stopMotion ends the motion and its loading, and frees the motion's buffers.
func (v *ModelViewer) stopMotion() {
	if v.motionCancel != nil {
		v.motionCancel()
		v.motionCancel = nil
		v.revision++
	}
	m := v.charMotion
	if m == nil {
		return
	}
	v.charMotion = nil
	m.cancel()
	for i := range m.blendLoads {
		m.cancelBlendLoad(i)
	}
	if m.gpu != nil {
		_ = fairy.UseDevice(func(fairy.GPU) error {
			m.gpu.release()
			return nil
		})
		m.gpu = nil
	}
	v.revision++
}

// failMotionPlay reports a motion that could not be posed or drawn at stage and ends it.
func (v *ModelViewer) failMotionPlay(err error, stage string) {
	fields := map[string]any{"modPath": v.path}
	if c := v.character; c != nil {
		fields["game"], fields["character"] = c.game, c.ID
	}
	if m := v.activeMotion(); m != nil {
		fields["frame"], fields["meshes"], fields["nodes"] = m.play.frame, len(m.meshes), len(m.bones)
		if clip := v.trackClip(trackMotion); clip != nil {
			fields["clip"] = clip.Label
		}
	}
	if v.scene != nil && v.character != nil {
		v.motionFailed = v.motionKey()
	}
	v.stopMotion()
	err = infra.ReportError(v.log, err, "ModelViewer.characterMotion", infra.Diagnostic{
		Operation: "play-character-motion", Stage: stage, Fields: fields,
	})
	v.toasts.Show(kit.ToastError, T("page.tools.model_viewer.toast.motion_play_error"), err.Error())
}

// viewerSkinGPU is a motion's buffers on one device generation: the bone weights of each mesh it poses, by scene
// mesh index, and the pose.
type viewerSkinGPU struct {
	generation uint64
	blends     []viewerSkinBlend
	bones      d3d.Buffer
	bonesView  d3d.ShaderResourceView
	// posed is the pose bones holds.
	posed uint64
}

type viewerSkinBlend struct {
	buffer   d3d.Buffer
	view     d3d.ShaderResourceView
	vertices int
}

func (g *viewerSkinGPU) release() {
	for i := range g.blends {
		g.blends[i].view.Release()
		g.blends[i].buffer.Release()
	}
	g.bonesView.Release()
	g.bones.Release()
	*g = viewerSkinGPU{}
}

// viewerSkinBoneStride is the size of a SkinBone in model_viewer.hlsl.
const viewerSkinBoneStride = 48

// structuredView makes a shader resource view over all count elements of a structured buffer.
func structuredView(dev d3d.Device, buffer d3d.Buffer, count int) (d3d.ShaderResourceView, error) {
	return dev.CreateShaderResourceView(buffer, &d3d.SRVDesc{
		Format: d3d.FORMAT_UNKNOWN, ViewDimension: d3d.SRV_DIMENSION_BUFFER, U: [4]uint32{0, uint32(count)},
	})
}

// skinGPU returns the motion's buffers on dev of generation, with the pose shown written, making them on the first
// draw of a generation. When they cannot be made or written it returns nil and records why in gpuErr and gpuStage.
func (m *viewerMotion) skinGPU(ctx d3d.Context, dev d3d.Device, generation uint64) *viewerSkinGPU {
	if m.gpu != nil && m.gpu.generation != generation {
		m.gpu.release()
		m.gpu = nil
	}
	made := m.gpu == nil
	if made {
		g := &viewerSkinGPU{generation: generation, blends: make([]viewerSkinBlend, len(m.scene.meshes))}
		fail := func(err error) *viewerSkinGPU {
			g.release()
			m.gpuErr, m.gpuStage = fmt.Errorf("create character motion buffers: %w", err), "create-buffers"
			return nil
		}
		var err error
		for _, mesh := range m.meshes {
			if mesh.vertices == 0 {
				continue
			}
			blend := &g.blends[mesh.index]
			if blend.buffer, err = dev.CreateBuffer(&d3d.BufferDesc{
				ByteWidth: uint32(len(mesh.blend) * 4), Usage: d3d.USAGE_IMMUTABLE, BindFlags: d3d.BIND_SHADER_RESOURCE,
				MiscFlags: d3d.RESOURCE_MISC_BUFFER_STRUCTURED, StructureByteStride: 16,
			}, &d3d.SubresourceData{SysMem: unsafe.Pointer(&mesh.blend[0])}); err != nil {
				return fail(err)
			}
			if blend.view, err = structuredView(dev, blend.buffer, mesh.vertices); err != nil {
				return fail(err)
			}
			blend.vertices = mesh.vertices
		}
		if g.bones, err = dev.CreateBuffer(&d3d.BufferDesc{
			ByteWidth: uint32(len(m.bones) * viewerSkinBoneStride), Usage: d3d.USAGE_DYNAMIC,
			BindFlags: d3d.BIND_SHADER_RESOURCE, CPUAccessFlags: d3d.CPU_ACCESS_WRITE,
			MiscFlags: d3d.RESOURCE_MISC_BUFFER_STRUCTURED, StructureByteStride: viewerSkinBoneStride,
		}, nil); err != nil {
			return fail(err)
		}
		if g.bonesView, err = structuredView(dev, g.bones, len(m.bones)); err != nil {
			return fail(err)
		}
		m.gpu = g
	}
	if made || m.gpu.posed != m.posed {
		if err := d3d.WriteData(ctx, m.gpu.bones, m.bones); err != nil {
			m.gpuErr, m.gpuStage = fmt.Errorf("write character motion pose: %w", err), "write-pose"
			return nil
		}
		m.gpu.posed = m.posed
	}
	return m.gpu
}

// blend returns the bone weights of scene mesh i when the motion poses it and they still fit its vertices.
func (g *viewerSkinGPU) blend(i int, vertices int) (d3d.ShaderResourceView, bool) {
	if g == nil || i >= len(g.blends) || g.blends[i].view.P == nil || g.blends[i].vertices != vertices {
		return d3d.ShaderResourceView{}, false
	}
	return g.blends[i].view, true
}
